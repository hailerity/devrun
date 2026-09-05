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

func (f *footerBar) render(activeTab tabKind, focus focusKind, visualMode, targetFocused, onTargetRow, canEditRow, canRemoveRow, editing, confirming bool, width int) string {
	base := lipgloss.NewStyle().
		Width(width).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder)

	if f.toast != "" {
		return base.Foreground(colorAccent).Render(ansi.Truncate(f.toast, width, ""))
	}

	// The modals draw their own hint line; the footer just names its keys.
	if confirming {
		return base.Render(ansi.Truncate(strings.Join([]string{
			renderHint("y", "remove"), renderHint("n/Esc", "cancel"),
		}, "  "), width, ""))
	}
	if editing {
		return base.Render(ansi.Truncate(strings.Join([]string{
			renderHint("Tab", "field"), renderHint("↵", "save"), renderHint("Esc", "cancel"),
		}, "  "), width, ""))
	}

	var hints []string
	hints = append(hints, renderHint("Tab", "switch"))
	// On a target row Enter selects/clears the filter. Otherwise, unless a
	// target roll-up fills the main pane, Enter toggles LOGS <-> DETAILS and the
	// log-pane shortcuts (copy, follow) apply.
	switch {
	case onTargetRow:
		hints = append(hints, renderHint("↵", "filter"))
	case targetFocused:
		// target detail fills the main pane — nothing for Enter to toggle
	case activeTab == tabDetails:
		hints = append(hints, renderHint("↵", "logs"))
	default:
		hints = append(hints, renderHint("↵", "details"))
	}
	if !targetFocused && focus == focusMain && activeTab == tabLogs {
		hints = append(hints, renderHint("y/^C", "copy"), renderHint("v", "select"), renderHint("f", "follow"), renderHint("w", "wrap"))
	}
	if visualMode {
		hints = append(hints, renderHint("Esc", "cancel"))
	}
	hints = append(hints, renderHint("s", "start"), renderHint("x", "stop"))
	if canEditRow {
		hints = append(hints, renderHint("e", "edit"))
	}
	if canRemoveRow {
		hints = append(hints, renderHint("d", "remove"))
	}
	hints = append(hints, renderHint("q", "quit"))
	// The hint list grows with context (visual mode, edit/remove-able rows,
	// the log-pane shortcuts) and can outgrow a narrow terminal. lipgloss's
	// Width() only ever pads short content up to width — it never caps long
	// content back down — so an overlong, unbudgeted line here would wrap in
	// the real terminal and scroll the whole screen, pushing the header off
	// the top exactly like the earlier logs-panel bug. Truncate defensively:
	// losing the least-essential (rightmost) hints beats corrupting the
	// layout.
	return base.Render(ansi.Truncate(strings.Join(hints, "  "), width, ""))
}

func renderHint(k, label string) string {
	key := lipgloss.NewStyle().
		Background(colorBorder).
		Foreground(colorText).
		Padding(0, 1).
		Render(k)
	return key + styleMuted.Render(" "+label)
}
