package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	toastDurationShort = 1500 * time.Millisecond
	toastDurationLong  = 3000 * time.Millisecond
)

type footerBar struct {
	toast    string
	toastAge time.Duration
	toastDur time.Duration
}

func (f *footerBar) showToast(msg string) {
	f.toast = msg
	f.toastAge = 0
	f.toastDur = toastDurationShort
}

func (f *footerBar) showToastLong(msg string) {
	f.toast = msg
	f.toastAge = 0
	f.toastDur = toastDurationLong
}

func (f *footerBar) tick(dt time.Duration) {
	if f.toast == "" {
		return
	}
	f.toastAge += dt
	if f.toastAge >= f.toastDur {
		f.toast = ""
		f.toastAge = 0
	}
}

// footerCtx is what the footer needs to know to pick its hints.
type footerCtx struct {
	tab          tabKind
	focus        focusKind
	visual       bool   // a visual selection is active in the log pane
	onServiceRow bool   // e / d apply to the selected service
	editing      bool   // a form modal (service or target editor) is open
	confirming   bool   // the remove confirm is open
	picking      bool   // the target picker is open
	helping      bool   // the help overlay is open
	searching    bool   // the search input has the keyboard
	hasQuery     bool   // a search is active in the log pane
	searchInput  string // the rendered search input, shown while searching
}

// hint is one key/label pair in the footer. pri ranks it for narrow terminals:
// 0 is kept longest, larger numbers are dropped first.
type hint struct {
	key, label string
	pri        int
}

const hintGap = "  "

// fitHints renders as many hints as fit in width, in their given order. When
// they do not all fit, whole hints are removed — highest pri number first —
// rather than the line being cut mid-label, so what remains always reads
// correctly and the most useful keys are the last to go.
func fitHints(hints []hint, width int) string {
	kept := append([]hint(nil), hints...)
	for {
		parts := make([]string, len(kept))
		for i, h := range kept {
			parts[i] = renderHint(h.key, h.label)
		}
		line := strings.Join(parts, hintGap)
		if lipgloss.Width(line) <= width || len(kept) == 0 {
			if len(kept) == 0 {
				return ""
			}
			return line
		}
		drop := 0
		for i, h := range kept {
			// >= so that among equal priorities the rightmost goes first.
			if h.pri >= kept[drop].pri {
				drop = i
			}
		}
		kept = append(kept[:drop], kept[drop+1:]...)
	}
}

// hints returns the context's key hints in display order.
func (c footerCtx) hints() []hint {
	// The modals are keyboard traps: only their own keys apply.
	switch {
	case c.confirming:
		return []hint{{"y", "remove", 0}, {"n/Esc", "cancel", 1}}
	case c.editing:
		return []hint{{"Tab", "field", 2}, {"↵", "save", 0}, {"Esc", "cancel", 1}}
	case c.picking:
		return []hint{{"↵", "filter", 0}, {"e", "edit", 2}, {"Esc", "close", 1}}
	case c.helping:
		return []hint{{"Esc", "close", 0}}
	}

	if c.focus == focusMain && c.tab == tabLogs {
		if c.visual {
			return []hint{{"y/^C", "copy", 0}, {"Esc", "cancel", 1}, {"j/k", "extend", 2}}
		}
		out := []hint{}
		if c.hasQuery {
			// While a search is active stepping through it is the point.
			out = append(out, hint{"n/N", "next/prev", 0}, hint{"Esc", "clear", 1})
		}
		return append(out,
			hint{"/", "search", 1},
			hint{"Tab", "services", 2},
			hint{"f", "follow", 2},
			hint{"y", "copy", 3},
			hint{"v", "select", 4},
			hint{"w", "wrap", 6},
			hint{"g/G", "top/end", 7},
			hint{"↵", "details", 5},
		)
	}

	enter := "details"
	if c.tab == tabDetails {
		enter = "logs"
	}
	out := []hint{
		{"s", "start", 0},
		{"x", "stop", 1},
		{"↵", enter, 2},
		{"t", "target", 4},
		{"/", "search", 5},
	}
	if c.onServiceRow {
		out = append(out, hint{"e", "edit", 6}, hint{"d", "remove", 7})
	}
	return append(out, hint{"S/X", "all", 8}, hint{"Tab", "logs", 3})
}

// pinnedHints sit at the right edge in every non-modal context, so the way out
// and the way to learn the rest are never the hints a narrow terminal loses.
var pinnedHints = []hint{{"?", "help", 1}, {"q", "quit", 0}}

// render draws the one-row footer: context hints on the left, help and quit
// pinned right. One row, no rule — the pane borders above already separate it.
//
// Everything is fitted to the width, never left to wrap: lipgloss's Width()
// only pads short content, it never caps long content, and a footer that wraps
// on the real terminal scrolls the header off the top.
func (f *footerBar) render(c footerCtx, width int) string {
	base := lipgloss.NewStyle().Width(width).PaddingLeft(1)
	inner := max(0, width-1)

	if f.toast != "" {
		return base.Foreground(colorAccent).Render(ansi.Truncate(f.toast, inner, ""))
	}
	if c.confirming || c.editing || c.picking || c.helping {
		return base.Render(fitHints(c.hints(), inner))
	}
	if c.searching {
		// The input takes the left; its two keys are pinned right. The input is
		// truncated to what is left, so a long query cannot wrap the row.
		right := fitHints([]hint{{"↵", "find", 0}, {"Esc", "cancel", 1}}, inner)
		room := max(0, inner-lipgloss.Width(right)-len(hintGap))
		left := ansi.Truncate(c.searchInput, room, "")
		gap := max(0, inner-lipgloss.Width(left)-lipgloss.Width(right))
		return base.Render(left + strings.Repeat(" ", gap) + right)
	}

	// The pinned pair claims its space first; the context hints get the rest.
	right := fitHints(pinnedHints, inner)
	left := fitHints(c.hints(), max(0, inner-lipgloss.Width(right)-len(hintGap)))
	gap := max(0, inner-lipgloss.Width(left)-lipgloss.Width(right))
	return base.Render(left + strings.Repeat(" ", gap) + right)
}

func renderHint(k, label string) string {
	key := lipgloss.NewStyle().
		Background(colorBorder).
		Foreground(colorText).
		Padding(0, 1).
		Render(k)
	return key + styleMuted.Render(" "+label)
}
