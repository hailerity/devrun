package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestFooter_ToastAppearsAndClears(t *testing.T) {
	f := &footerBar{}
	f.showToast("Copied!")
	assert.Equal(t, "Copied!", f.toast)

	f.tick(1 * time.Second)
	assert.Equal(t, "Copied!", f.toast) // still visible

	f.tick(600 * time.Millisecond) // total 1.6s > 1.5s threshold
	assert.Equal(t, "", f.toast)   // cleared
}

func TestFooter_ToastNoOpWhenEmpty(t *testing.T) {
	f := &footerBar{}
	f.tick(5 * time.Second) // should not panic
	assert.Equal(t, "", f.toast)
}

func TestFooter_ToastResetOnNew(t *testing.T) {
	f := &footerBar{}
	f.showToast("first")
	f.tick(1 * time.Second)
	f.showToast("second") // resets timer
	f.tick(1 * time.Second)
	assert.Equal(t, "second", f.toast) // 1s < 1.5s, still showing
}

// lipgloss's Width() style pads content shorter than width, but word-wraps
// content longer than width into extra lines instead of capping it back
// down — confirmed by inspection, not an assumption. A footer whose hint
// list (or a long toast) outgrows the terminal would silently grow past its
// allotted single content row, and the extra row is exactly what pushed the
// header off screen in the reported bug. render() must truncate first so
// the result always stays border(1) + content(1) = 2 rows, however much
// hint text or toast text would otherwise overflow.

func TestFooter_HintLineNeverWrapsAtNarrowWidth(t *testing.T) {
	f := &footerBar{}
	// Every hint group at once: log-pane shortcuts, visual-mode cancel, edit,
	// remove — the longest the hint line ever gets.
	out := f.render(tabLogs, focusMain, true, false, false, true, true, false, false, 20)
	assert.Equal(t, 2, lipgloss.Height(out),
		"footer must stay border(1)+content(1) rows even when hints overflow a narrow width")
}

func TestFooter_LongToastNeverWrapsAtNarrowWidth(t *testing.T) {
	f := &footerBar{}
	f.showToastLong("error: " + strings.Repeat("x", 200))
	out := f.render(tabLogs, focusSidebar, false, false, false, false, false, false, false, 20)
	assert.Equal(t, 2, lipgloss.Height(out))
}

func TestFooter_EditingHintNeverWrapsAtNarrowWidth(t *testing.T) {
	f := &footerBar{}
	out := f.render(tabLogs, focusMain, false, false, false, false, false, true, false, 5)
	assert.Equal(t, 2, lipgloss.Height(out))
}

func TestFooter_ConfirmingHintNeverWrapsAtNarrowWidth(t *testing.T) {
	f := &footerBar{}
	out := f.render(tabLogs, focusMain, false, false, false, false, false, false, true, 5)
	assert.Equal(t, 2, lipgloss.Height(out))
}
