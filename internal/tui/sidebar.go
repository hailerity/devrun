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

// rowKind distinguishes what a drawn line of the list is.
type rowKind int

const (
	rowService rowKind = iota
	rowHeader
)

// sidebarRow is one line of the list as drawn. The cursor indexes these rather
// than s.services, because once the list is grouped a header is a line the
// cursor can sit on — and one that has no service behind it. With a single
// group there are no headers and the two indexes coincide, which is why
// introducing this changed no behaviour.
type sidebarRow struct {
	kind  rowKind
	group string // the header's group, or the group a service row sits under
	svc   int    // index into s.services; -1 on a header
}

type sidebar struct {
	allServices []ipc.ServiceInfo // full scoped list, by Name
	services    []ipc.ServiceInfo // allServices narrowed by the target filter and the name query
	rows        []sidebarRow      // services as drawn: headers interleaved, collapsed groups omitted
	selected    int               // cursor within rows
	top         int               // first visible row — the scroll window's offset
	paneRows    int               // visible row count, set by the model's layout; 0 = not laid out yet

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

	// collapsed holds the groups the reader has folded shut, by group name.
	// Keyed by name rather than by index so it survives a poll re-grouping the
	// list, and kept even for a group that is not currently listed — a query
	// that hides a group should not forget that it was collapsed.
	collapsed map[string]bool

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
// nothing when there is no service there — an empty list, or a group header —
// because overwriting the anchor with "" is how a no-match query used to lose
// the cursor for good.
func (s *sidebar) recordAnchor() {
	if svc := s.serviceAt(s.selected); svc != nil {
		s.anchor = svc.Name
	}
}

// selectServiceByName moves the cursor to the row showing the service named n,
// or to the first service row when no row does. Call after the drawn rows
// change.
func (s *sidebar) selectServiceByName(n string) {
	s.selected = s.firstServiceRow()
	for i := range s.rows {
		if svc := s.serviceAt(i); svc != nil && svc.Name == n {
			s.selected = i
			break
		}
	}
	s.scrollToCursor()
}

// firstServiceRow is the first row with a service behind it, or 0 when there is
// none. This is the fallback for "the service we wanted is not listed", and it
// skips headers deliberately: row 0 of a grouped list is a header, so falling
// back there would open the dashboard with the cursor on a row that selects
// nothing — s/x/e/d inert and the log pane empty — which is how the list looks
// on every first load once grouping is on.
func (s *sidebar) firstServiceRow() int {
	for i := range s.rows {
		if s.serviceAt(i) != nil {
			return i
		}
	}
	return 0
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
	s.rebuildRows()
	if s.selected >= len(s.rows) {
		s.selected = max(0, len(s.rows)-1)
	}
}

// ungroupedLabel heads the services that carry no group. Only the global
// registry can produce them: a devrun.yaml stamps every service with the
// project's name.
const ungroupedLabel = "ungrouped"

// rebuildRows lays s.services out as drawn lines, one group at a time. With a
// single distinct group there is nothing to tell the reader, so no header is
// drawn and the rows are simply the services — the list looks exactly as it did
// before grouping existed. A header only earns its line when it is dividing
// something.
//
// Services arrive sorted by name and that order is kept within each group, so a
// service's place is still predictable; only the grouping moves it.
func (s *sidebar) rebuildRows() {
	s.rows = make([]sidebarRow, 0, len(s.services))

	groups := s.groupOrder()
	if len(groups) < 2 {
		for i := range s.services {
			s.rows = append(s.rows, sidebarRow{kind: rowService, svc: i})
		}
		return
	}

	for _, g := range groups {
		s.rows = append(s.rows, sidebarRow{kind: rowHeader, group: g, svc: -1})
		if s.isCollapsed(g) {
			continue
		}
		for i := range s.services {
			if groupOf(s.services[i]) == g {
				s.rows = append(s.rows, sidebarRow{kind: rowService, group: g, svc: i})
			}
		}
	}
}

// isCollapsed reports whether a group is folded shut *right now*.
//
// An active name query suspends every collapse. A reader who types a query is
// asking to be shown what matches, and a match hidden inside a folded group
// would make the filter a liar — worse, the empty-state and the header counts
// would disagree with what is on screen. The collapsed set is left untouched
// while this is in force, so clearing the query folds the groups back exactly
// as they were.
func (s *sidebar) isCollapsed(group string) bool {
	if s.filterQuery != "" {
		return false
	}
	return s.collapsed[group]
}

// onGroupHeader reports whether the cursor is on a group header — a row with no
// service behind it, where the keys that act on a service have nothing to do.
func (s *sidebar) onGroupHeader() bool {
	return s.selected < len(s.rows) && s.rows[s.selected].kind == rowHeader
}

// toggleCollapse folds or unfolds the group under the cursor and leaves the
// cursor on its header, which is the row the reader pressed the key on and the
// row they need to press it again.
//
// A no-op on a service row: collapsing the group a service belongs to would
// move the cursor off the row the reader was looking at, and put it on a header
// they did not aim at.
func (s *sidebar) toggleCollapse() bool {
	if s.selected >= len(s.rows) || s.rows[s.selected].kind != rowHeader {
		return false
	}
	g := s.rows[s.selected].group
	if s.collapsed == nil {
		s.collapsed = map[string]bool{}
	}
	s.collapsed[g] = !s.collapsed[g]
	s.refilter()
	s.selectGroupHeader(g)
	return true
}

// selectGroupHeader puts the cursor on the named group's header, or leaves it
// clamped into the list when that group is no longer drawn.
func (s *sidebar) selectGroupHeader(group string) {
	for i := range s.rows {
		if s.rows[i].kind == rowHeader && s.rows[i].group == group {
			s.selected = i
			s.scrollToCursor()
			return
		}
	}
	s.selected = min(s.selected, max(0, len(s.rows)-1))
	s.scrollToCursor()
}

// groupOf is a service's group as the sidebar labels it, mapping the empty
// group to the "ungrouped" bucket so every service belongs somewhere.
func groupOf(svc ipc.ServiceInfo) string {
	if svc.Group == "" {
		return ungroupedLabel
	}
	return svc.Group
}

// groupOrder lists the distinct groups among the listed services, alphabetical,
// with the ungrouped bucket last. Last because it is the absence of an answer:
// a reader scanning for a named group should not have to pass a pile of
// unlabelled services to reach it.
func (s *sidebar) groupOrder() []string {
	seen := make(map[string]bool, len(s.services))
	var named []string
	ungrouped := false
	for _, svc := range s.services {
		g := groupOf(svc)
		if seen[g] {
			continue
		}
		seen[g] = true
		if g == ungroupedLabel {
			ungrouped = true
			continue
		}
		named = append(named, g)
	}
	sort.Slice(named, func(i, j int) bool {
		li, lj := strings.ToLower(named[i]), strings.ToLower(named[j])
		if li != lj {
			return li < lj
		}
		return named[i] < named[j]
	})
	if ungrouped {
		named = append(named, ungroupedLabel)
	}
	return named
}

// serviceAt returns the service a row points at, or nil for a header.
func (s *sidebar) serviceAt(row int) *ipc.ServiceInfo {
	if row < 0 || row >= len(s.rows) {
		return nil
	}
	r := s.rows[row]
	if r.kind != rowService || r.svc < 0 || r.svc >= len(s.services) {
		return nil
	}
	return &s.services[r.svc]
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

// cancelQuery undoes an abandoned `/` session: the query the input opened on
// goes back, and so does the cursor. Both are needed for the footer's "cancel"
// to be honest — while the query was in force the narrowing will have forced
// the cursor onto whichever row survived it, and restoring only the query
// leaves the highlight (and the log pane behind it) on that row rather than on
// the service the reader started from.
//
// Deliberately not keepingCursor: that records the cursor as it is now, which
// is the forced row this is undoing.
func (s *sidebar) cancelQuery(query, anchor string) {
	s.filterQuery = query
	s.refilter()
	if anchor != "" {
		s.anchor = anchor
	}
	s.selectServiceByName(s.anchor)
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
	s.paneRows = n
	s.scrollToCursor()
}

// scrollToCursor moves the window the least it must to keep the cursor visible,
// and never leaves blank rows below the list when there is more above. Called
// after anything that moves the cursor or changes the list.
func (s *sidebar) scrollToCursor() {
	if s.paneRows <= 0 {
		s.top = 0 // not laid out yet: render() shows everything
		return
	}
	if s.selected < s.top {
		s.top = s.selected
	}
	if s.selected >= s.top+s.paneRows {
		s.top = s.selected - s.paneRows + 1
	}
	s.top = max(0, min(s.top, len(s.rows)-s.paneRows))
}

// moveDown / moveUp walk the drawn rows, headers included, wrapping at the ends.

func (s *sidebar) moveDown() {
	if len(s.rows) == 0 {
		return
	}
	s.selected = (s.selected + 1) % len(s.rows)
	s.scrollToCursor()
}

func (s *sidebar) moveUp() {
	if len(s.rows) == 0 {
		return
	}
	s.selected = (s.selected - 1 + len(s.rows)) % len(s.rows)
	s.scrollToCursor()
}

// selectedService is the service under the cursor, or nil when the cursor is on
// a group header or the list is empty. Every caller already had to handle nil
// for the empty list, which is why headers became cursorable without a hunt
// through the key handlers.
func (s *sidebar) selectedService() *ipc.ServiceInfo {
	return s.serviceAt(s.selected)
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
	// columns) there is not room for both. edge() does fit an over-long title,
	// with an ellipsis — but it cuts from the tail, which eats the query chip
	// whole and leaves a title naming only the target. Fitting here instead
	// keeps the chip that matters and shortens it in the middle, so both ends
	// of the query stay readable.
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
		// fold are invisible with nothing to hint they exist. Counted in drawn
		// rows, which is what is actually scrolling.
		if first, last := s.window(); last-first < len(s.rows) {
			f.footRight = styleMuted.Render(fmt.Sprintf("%d–%d of %d", first+1, last, len(s.rows)))
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
	// query is fitted here rather than left to the pane's own truncation, which
	// cuts from the tail and would stop the sentence mid-query.
	case len(s.services) == 0 && s.filterQuery != "" && s.inTargetCount() > 0:
		const lead = " no match for /"
		return styleMuted.Render(lead + truncateName(s.filterQuery, max(1, width-lipgloss.Width(lead))))
	case len(s.services) == 0:
		return styleMuted.Render(" (no services in target)")
	}
	first, last := s.window()
	out := make([]string, 0, last-first)
	for i := first; i < last; i++ {
		if svc := s.serviceAt(i); svc != nil {
			out = append(out, serviceRow(width, *svc, i == s.selected, s.exposed[svc.Name]))
			continue
		}
		g := s.rows[i].group
		up, total := s.groupCount(g)
		out = append(out, groupRow(width, g, up, total, i == s.selected, s.isCollapsed(g)))
	}
	return strings.Join(out, "\n")
}

// groupCount is how many of a group's listed services are running, and how many
// there are. Counted over s.services, so it describes the group as filtered —
// the header cannot claim members a query has hidden.
func (s *sidebar) groupCount(group string) (up, total int) {
	for _, svc := range s.services {
		if groupOf(svc) != group {
			continue
		}
		total++
		if svc.State == "running" {
			up++
		}
	}
	return up, total
}

// groupRow renders a group header: "▾ backend              2/3", exactly width
// columns wide. The running count is the whole of what the header says about
// its members, and it is the only thing on screen about a group that is
// collapsed — a service that is down inside one is not otherwise visible.
func groupRow(width int, group string, up, total int, selected, collapsed bool) string {
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(colorSelSidebar)
	}

	count := fmt.Sprintf("%d/%d", up, total)
	// Dimmer than a service row's name and without the state glyph's column, so
	// a header reads as structure rather than as another service.
	nameW := max(1, width-3-1-lipgloss.Width(count))

	row := base.Foreground(colorMuted).Render(" "+collapseGlyph(collapsed)) +
		base.Foreground(colorText).Bold(true).Render(" "+padRight(truncateName(group, nameW), nameW)) +
		base.Foreground(colorMuted).Render(" "+count)
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += base.Render(strings.Repeat(" ", pad))
	}
	return row
}

// collapseGlyph is the header's disclosure marker. Shape alone carries it, as
// the state glyphs do, so it survives --no-color.
func collapseGlyph(collapsed bool) string {
	if collapsed {
		return "▸"
	}
	return "▾"
}

// window returns the half-open range of drawn rows currently visible.
func (s *sidebar) window() (first, last int) {
	if s.paneRows <= 0 {
		return 0, len(s.rows)
	}
	first = max(0, min(s.top, len(s.rows)))
	return first, min(len(s.rows), first+s.paneRows)
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
