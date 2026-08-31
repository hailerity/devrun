package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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

func (f *footerBar) render(activeTab tabKind, focus focusKind, visualMode, targetFocused, onTargetRow bool, width int) string {
	base := lipgloss.NewStyle().
		Width(width).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder)

	if f.toast != "" {
		return base.Foreground(colorAccent).Render(f.toast)
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
		hints = append(hints, renderHint("y/^C", "copy"), renderHint("v", "select"), renderHint("f", "follow"))
	}
	if visualMode {
		hints = append(hints, renderHint("Esc", "cancel"))
	}
	hints = append(hints, renderHint("s", "start"), renderHint("x", "stop"), renderHint("q", "quit"))
	return base.Render(strings.Join(hints, "  "))
}

func renderHint(k, label string) string {
	key := lipgloss.NewStyle().
		Background(colorBorder).
		Foreground(colorText).
		Padding(0, 1).
		Render(k)
	return key + styleMuted.Render(" "+label)
}
