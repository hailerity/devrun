package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const toastDuration = 1500 * time.Millisecond

type footerBar struct {
	toast    string
	toastAge time.Duration
}

func (f *footerBar) showToast(msg string) {
	f.toast = msg
	f.toastAge = 0
}

func (f *footerBar) tick(dt time.Duration) {
	if f.toast == "" {
		return
	}
	f.toastAge += dt
	if f.toastAge >= toastDuration {
		f.toast = ""
		f.toastAge = 0
	}
}

func (f *footerBar) render(activeTab tabKind, focus focusKind, visualMode bool, width int) string {
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
	if focus == focusMain && activeTab == tabLogs {
		hints = append(hints, renderHint("y", "copy"), renderHint("v", "select"), renderHint("f", "follow"))
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
