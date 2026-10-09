package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/ipc"
)

// sidebarTarget is one configured target: its name and its declared members.
// Targets are not sidebar rows — they feed the target picker and the service
// filter.
type sidebarTarget struct {
	name    string
	members []string
}

// allServicesLabel names the "no filter" choice in the target picker.
const allServicesLabel = "All services"

type sidebar struct {
	allServices []ipc.ServiceInfo // full scoped list, by Name
	services    []ipc.ServiceInfo // allServices narrowed by the target filter and the name query
	selected    int               // cursor within services
	top         int               // first visible services row — the scroll window's offset
	rows        int               // visible row count, set by the model's layout; 0 = not laid out yet

	targets      []sidebarTarget // configured targets, sorted; empty → nothing to filter by
	filterTarget string          // name of the target filtering the list ("" = show all); set via the target picker
	filterQuery  string          // name query narrowing the list ("" = no query); set via the `/` input

	// anchor is the name of the service the cursor is on, remembered so the
	// highlight can be restored after the list is re-sorted or re-filtered. It
	// is held here rather than re-read from services[selected] at restore time
	// because a narrowing can empty the list: mid-query there is no row to read
	// an anchor from, and clearing the query would then drop the reader on row 0
	// instead of back where they were.
	anchor string

	// exposed names the services that may leave this machine. Held here, not
	// read off ipc.ServiceInfo, because the allowlist belongs to the gateway
	// and applies to services whether or not one is running.
	exposed map[string]bool

	loaded bool // true once the first daemon poll has resolved (response or error)
}

func (s *sidebar) update(svcs []ipc.ServiceInfo, targets []sidebarTarget) {
	s.loaded = true

	s.keepingCursor(func() {
		// Plain alphabetical, with no state in the ordering: a row keeps its
		// place for as long as it is configured, so the list a reader has
		// learned does not reshuffle itself under the cursor every time a
		// service changes state. A crash is announced by the row's ✖ and its
		// colour, not by its position — which also leaves the order free to
		// carry grouping later.
		//
		// Case-insensitively, so the order matches where a reader looks for a
		// name — and matches the query, which is also case-insensitive. Byte
		// order would file `Worker` under W-before-a, ahead of `api`, while
		// `/work` still found it.
		sorted := append([]ipc.ServiceInfo(nil), svcs...)
		sort.Slice(sorted, func(i, j int) bool {
			li, lj := strings.ToLower(sorted[i].Name), strings.ToLower(sorted[j].Name)
			if li != lj {
				return li < lj
			}
			// Names differing only in case still need a total order.
			return sorted[i].Name < sorted[j].Name
		})
		s.allServices = sorted
		s.targets = targets

		// Keep the target filter across the poll; drop it only if that target
		// is gone. The name query needs no such check — a query that matches
		// nothing is still a query the user typed, and saying so (render's
		// empty state) beats silently dropping it.
		if s.filterTarget != "" && !s.targetExists(s.filterTarget) {
			s.filterTarget = ""
		}
	})
}

// keepingCursor applies a change to what the list shows, then puts the
// highlight back on the anchored service — by name, since the row index means
// nothing once the list has been re-sorted or re-filtered. A service the change
// filtered out keeps the anchor, so widening the list again returns the cursor
// to it; the cursor sits at the top only while that service is not listed.
func (s *sidebar) keepingCursor(change func()) {
	s.recordAnchor()
	change()
	s.refilter()
	s.selectServiceByName(s.anchor)
}

// recordAnchor remembers the service under the cursor. It deliberately does
// nothing when the list is empty: there is no service to anchor to, and
// overwriting the anchor with "" is how a no-match query used to lose the
// cursor for good.
func (s *sidebar) recordAnchor() {
	if s.selected < len(s.services) {
		s.anchor = s.services[s.selected].Name
	}
}

// selectServiceByName moves the service cursor to the row named n, or to row 0
// when there is no such row. Call after the filtered service list changes.
func (s *sidebar) selectServiceByName(n string) {
	s.selected = 0
	for i, svc := range s.services {
		if svc.Name == n {
			s.selected = i
			break
		}
	}
	s.scrollToCursor()
}

// target returns the configured target called name, or nil.
func (s *sidebar) target(name string) *sidebarTarget {
	for i := range s.targets {
		if s.targets[i].name == name {
			return &s.targets[i]
		}
	}
	return nil
}

// targetExists reports whether a target with the given name is configured.
func (s *sidebar) targetExists(name string) bool { return s.target(name) != nil }

// targetMembers is the member set of the active target filter, or nil when no
// target is filtering — which callers read as "everything passes".
func (s *sidebar) targetMembers() map[string]bool {
	t := s.target(s.filterTarget)
	if s.filterTarget == "" || t == nil {
		return nil
	}
	members := make(map[string]bool, len(t.members))
	for _, m := range t.members {
		members[m] = true
	}
	return members
}

// inTargetCount is how many services the target filter alone would list: what
// the list would hold if the query were cleared. The empty state needs it to
// know which of the two narrowings to blame — a target that has already emptied
// the list is not fixed by deleting the query.
func (s *sidebar) inTargetCount() int {
	members := s.targetMembers()
	if members == nil {
		return len(s.allServices)
	}
	n := 0
	for _, svc := range s.allServices {
		if members[svc.Name] {
			n++
		}
	}
	return n
}

// refilter recomputes s.services from s.allServices, the active target filter
// and the active name query, then clamps the service cursor into range. The two
// narrowings compose: a query searches within the filtering target rather than
// escaping it, so what the frame's title says is on screen is what is on
// screen.
func (s *sidebar) refilter() {
	members := s.targetMembers()
	q := strings.ToLower(s.filterQuery)

	if members == nil && q == "" {
		s.services = s.allServices
	} else {
		out := make([]ipc.ServiceInfo, 0, len(s.allServices))
		for _, svc := range s.allServices {
			if members != nil && !members[svc.Name] {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(svc.Name), q) {
				continue
			}
			out = append(out, svc)
		}
		s.services = out
	}
	if s.selected >= len(s.services) {
		s.selected = max(0, len(s.services)-1)
	}
}

// setFilter makes the target called name the service filter ("" or an unknown
// name clears it), keeping the highlight on the same service if it survived.
func (s *sidebar) setFilter(name string) {
	s.keepingCursor(func() {
		if !s.targetExists(name) {
			name = ""
		}
		s.filterTarget = name
	})
}

// setQuery narrows the list to the services whose name contains q, ignoring
// case; "" clears it. Matching is a plain substring and deliberately not fuzzy:
// a list you are scanning has to stay predictable, and "api" pulling in
// "a-public-interface" is the opposite of that.
func (s *sidebar) setQuery(q string) {
	s.keepingCursor(func() { s.filterQuery = q })
}

// setRows tells the sidebar how many rows its pane can show, and re-anchors the
// scroll window on the cursor.
func (s *sidebar) setRows(n int) {
	s.rows = n
	s.scrollToCursor()
}

// scrollToCursor moves the window the least it must to keep the cursor visible,
// and never leaves blank rows below the list when there is more above. Called
// after anything that moves the cursor or changes the list.
func (s *sidebar) scrollToCursor() {
	if s.rows <= 0 {
		s.top = 0 // not laid out yet: render() shows everything
		return
	}
	if s.selected < s.top {
		s.top = s.selected
	}
	if s.selected >= s.top+s.rows {
		s.top = s.selected - s.rows + 1
	}
	s.top = max(0, min(s.top, len(s.services)-s.rows))
}

// moveDown / moveUp walk the (filtered) service list, wrapping at the ends.

func (s *sidebar) moveDown() {
	if len(s.services) == 0 {
		return
	}
	s.selected = (s.selected + 1) % len(s.services)
	s.scrollToCursor()
}

func (s *sidebar) moveUp() {
	if len(s.services) == 0 {
		return
	}
	s.selected = (s.selected - 1 + len(s.services)) % len(s.services)
	s.scrollToCursor()
}

func (s *sidebar) selectedService() *ipc.ServiceInfo {
	if len(s.services) == 0 {
		return nil
	}
	return &s.services[s.selected]
}

// stateLabel returns the short status token for a service: its port when
// running, otherwise the state word.
func stateLabel(svc ipc.ServiceInfo) string {
	if svc.State == "running" {
		if svc.Port != nil && *svc.Port != 0 {
			return fmt.Sprintf(":%d", *svc.Port)
		}
		return "detecting"
	}
	return svc.State
}

// truncateName shortens s to fit w display columns, keeping the head and the
// tail and marking the cut with "…" in the middle — so a shared prefix and the
// distinguishing suffix both stay visible.
func truncateName(s string, w int) string {
	if w < 1 {
		w = 1
	}
	total := lipgloss.Width(s)
	if total <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	// Cut by display columns, not runes: a CJK or emoji name is two columns per
	// rune, and a rune-count cut would hand back something wider than w. Both
	// ends are measured rune by rune rather than left to a library's handling of
	// a cut that lands inside a wide rune (ansi.TruncateLeft keeps the whole
	// rune, which overshoots by a column) — so the result can come back a column
	// under w, never over.
	keep := w - 1 // room taken by the ellipsis
	headW := (keep + 1) / 2
	tailW := keep - headW

	r := []rune(s)
	head, used := 0, 0
	for head < len(r) {
		rw := lipgloss.Width(string(r[head]))
		if used+rw > headW {
			break
		}
		used += rw
		head++
	}
	tail, used := len(r), 0
	for tail > head {
		rw := lipgloss.Width(string(r[tail-1]))
		if used+rw > tailW {
			break
		}
		used += rw
		tail--
	}
	return string(r[:head]) + "…" + string(r[tail:])
}

// padRight pads s with spaces to w display columns. fmt's %-*s counts runes,
// which under-pads nothing but over-runs on wide characters — this counts what
// the terminal will actually draw.
func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// stateGlyph returns the marker and colour for a service state. The shape alone
// identifies the state — ● running, ◐ in transition (starting / stopping),
// ✖ crashed, ○ not running — so it still reads under --no-color or for a
// colour-blind user; the colour only reinforces it.
func stateGlyph(state string) (string, lipgloss.TerminalColor) {
	switch state {
	case "running":
		return "●", colorGreen
	case "starting", "stopping":
		return "◐", colorYellow
	case "crashed":
		return "✖", colorRed
	default:
		return "○", colorMuted
	}
}

func stateDot(state string) string {
	glyph, fg := stateGlyph(state)
	return lipgloss.NewStyle().Foreground(fg).Render(glyph)
}

// frame is the sidebar's border: the title names the list and anything
// narrowing it — the target, then the name query — so the reason a service is
// missing is always on screen, and the bottom edge counts how many of the
// listed services are up.
func (s *sidebar) frame(focused bool, width int) paneFrame {
	const label = "SERVICES"
	title := styleMuted.Render(label)
	if focused {
		title = styleAccent.Bold(true).Render(label)
	}

	// Both narrowings want a chip here, and at the minimum pane width (31
	// columns) there is not room for both. edge() cuts a title that overruns
	// without an ellipsis, which turns an explanation into a word that looks
	// like a shorter name — so the fitting is done here instead.
	chip := func(s string) string { return styleMuted.Render(" · ") + styleAccent.Render(s) }
	const sep = 3 // " · "
	room := titleRoom(width) - lipgloss.Width(label)

	target := s.filterTarget
	// The query carries its "/" so it reads as the query it is rather than as a
	// second target name.
	query := ""
	if s.filterQuery != "" {
		query = "/" + s.filterQuery
	}
	// The target gives way first: the picker can always show it again, while
	// the query is the one the reader just typed and is about to undo.
	if target != "" && query != "" && 2*sep+lipgloss.Width(target)+lipgloss.Width(query) > room {
		target = ""
	}
	// Each chip is fitted, not only the query: a target name comes from user
	// config and can be long enough to overrun on its own.
	if target != "" {
		target = truncateName(target, max(1, room-sep))
		title += chip(target)
		room -= sep + lipgloss.Width(target)
	}
	if query != "" {
		title += chip(truncateName(query, max(1, room-sep)))
	}
	f := paneFrame{title: title, focused: focused}
	if len(s.services) > 0 {
		up := 0
		for _, svc := range s.services {
			if svc.State == "running" {
				up++
			}
		}
		f.footLeft = styleMuted.Render(fmt.Sprintf("%d/%d up", up, len(s.services)))
		// Say so when the list is windowed — otherwise rows above or below the
		// fold are invisible with nothing to hint they exist.
		if first, last := s.window(); last-first < len(s.services) {
			f.footRight = styleMuted.Render(fmt.Sprintf("%d–%d of %d", first+1, last, len(s.services)))
		}
	}
	return f
}

// render draws the visible window of list rows for a content area `width`
// columns wide.
func (s *sidebar) render(width int) string {
	switch {
	case len(s.allServices) == 0 && !s.loaded:
		return styleMuted.Render(" Loading services…")
	case len(s.allServices) == 0:
		return styleMuted.Render(" No services — run devrun add <name>")
	// Which of the two narrowings emptied the list, named: "nothing here" with
	// no cause is the one empty state a reader cannot act on. The query is
	// blamed only when clearing it would actually bring rows back — a target
	// that has already emptied the list on its own is the real cause, and
	// pointing at the query would send the reader to fix the wrong thing. The
	// query is fitted to the pane rather than left for the frame to cut, which
	// would stop the sentence mid-word.
	case len(s.services) == 0 && s.filterQuery != "" && s.inTargetCount() > 0:
		const lead = " no match for /"
		return styleMuted.Render(lead + truncateName(s.filterQuery, max(1, width-lipgloss.Width(lead))))
	case len(s.services) == 0:
		return styleMuted.Render(" (no services in target)")
	}
	first, last := s.window()
	rows := make([]string, 0, last-first)
	for i := first; i < last; i++ {
		svc := s.services[i]
		rows = append(rows, serviceRow(width, svc, i == s.selected, s.exposed[svc.Name]))
	}
	return strings.Join(rows, "\n")
}

// window returns the half-open range of service rows currently visible.
func (s *sidebar) window() (first, last int) {
	if s.rows <= 0 {
		return 0, len(s.services)
	}
	first = max(0, min(s.top, len(s.services)))
	return first, min(len(s.services), first+s.rows)
}

// Column widths of a service row: " ● name  ▲  :8080   2.1%".
const (
	rowStateW = 9 // "detecting" / "stopping" — the longest state token
	rowCPUW   = 6 // "100.0%"
	// A column of its own rather than a glyph beside the name: the mark stays
	// aligned down the list, so "which of these can leave the machine" is one
	// glance rather than a read. It costs its two columns on every row,
	// including when nothing is exposed, which is the price of that alignment.
	rowExposedW = 1
	// Below these row widths the CPU column, then the state column, is dropped
	// so the name keeps a usable share of a narrow sidebar.
	rowMinWForCPU   = 27
	rowMinWForState = 19
)

// serviceRow renders one table row of the service list — glyph, name, port or
// state, CPU — exactly `width` columns wide. Every segment of a selected row
// carries the selection background itself, so an SGR reset inside one styled
// segment cannot punch a hole in the highlight.
func serviceRow(width int, svc ipc.ServiceInfo, selected, exposed bool) string {
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(colorSelSidebar)
	}
	showState := width >= rowMinWForState
	showCPU := width >= rowMinWForCPU

	nameW := width - 3 // margin + glyph + space
	nameW -= 1 + rowExposedW
	if showState {
		nameW -= 1 + rowStateW
	}
	if showCPU {
		nameW -= 1 + rowCPUW
	}
	nameW = max(1, nameW)

	glyph, glyphFg := stateGlyph(svc.State)
	row := base.Foreground(glyphFg).Render(" "+glyph) +
		base.Foreground(colorText).Render(" "+padRight(truncateName(svc.Name, nameW), nameW))

	// Amber, as the header's published chip is: the two say the same thing at
	// different scales, and a reader should not have to learn two colours for
	// "can leave this machine".
	//
	// Filled and bold because amber is the palest colour in the theme
	// (#f0e68c against #c9d1d9 text), so a light glyph has nothing carrying
	// it — the dashed ⇡ this started as was missed outright. ▲ sits at the
	// weight of the ● beside it, and stays one column wide in every font,
	// which ☁ and ⬆ do not.
	mark, style := " ", base.Foreground(colorYellow)
	if exposed {
		mark, style = "▲", style.Bold(true)
	}
	row += style.Render(" " + mark)

	if showState {
		// A running service shows where to reach it; any other state is named,
		// in the state's own colour.
		label, fg := stateLabel(svc), glyphFg
		switch {
		case svc.State == "running" && strings.HasPrefix(label, ":"):
			fg = colorAccent
		case svc.State == "running":
			fg = colorMuted
		}
		row += base.Foreground(fg).Render(" " + fmt.Sprintf("%-*s", rowStateW, label))
	}
	if showCPU {
		cpu := ""
		if svc.State == "running" {
			cpu = fmt.Sprintf("%.1f%%", svc.CPUPct)
		}
		row += base.Foreground(cpuColor(svc.CPUPct)).Render(" " + fmt.Sprintf("%*s", rowCPUW, cpu))
	}
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += base.Render(strings.Repeat(" ", pad))
	}
	return row
}

// cpuColor keeps an idle CPU figure quiet: only a busy service is coloured, so
// yellow and red mean something when they appear.
func cpuColor(pct float64) lipgloss.TerminalColor {
	switch {
	case pct > 80:
		return colorRed
	case pct > 50:
		return colorYellow
	default:
		return colorMuted
	}
}
