package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// The header is exactly one row at every width. lipgloss's Width() style only
// pads a short line — it never caps a long one — and an overlong header wraps
// on the real terminal and scrolls the whole layout, so render() must truncate.

func TestHeaderBar_NeverWrapsAtNarrowWidth(t *testing.T) {
	h := headerBar{}
	out := h.render("a-very-long-project-name · devrun.yaml", 999, 999, 999, 0, true, 15)
	assert.Equal(t, 1, lipgloss.Height(out))
	assert.LessOrEqual(t, lipgloss.Width(out), 15)
}

func TestHeaderBar_ShowsSourceAndCounts(t *testing.T) {
	h := headerBar{}
	out := plain(h.render("shop · devrun.yaml", 5, 3, 0, 0, false, 80))
	assert.Equal(t, 1, lipgloss.Height(out))
	assert.Equal(t, 80, lipgloss.Width(out), "the right-hand counts sit against the right edge")
	assert.Contains(t, out, "devrun")
	assert.Contains(t, out, "shop · devrun.yaml")
	assert.Contains(t, out, "3/5 running")
	assert.NotContains(t, out, "crashed", "no crashed count while nothing is down")
}

func TestHeaderBar_CrashedCountAppearsWhenSomethingIsDown(t *testing.T) {
	h := headerBar{}
	assert.Contains(t, plain(h.render("", 5, 3, 1, 0, false, 80)), "✖ 1 crashed")
}

// The counts outrank the config label: when both cannot fit, the label goes.
func TestHeaderBar_DropsSourceBeforeCounts(t *testing.T) {
	h := headerBar{}
	out := plain(h.render("a-very-long-project-name · devrun.yaml", 5, 3, 1, 0, false, 44))
	assert.Contains(t, out, "3/5 running")
	assert.Contains(t, out, "1 crashed")
	assert.NotContains(t, out, "a-very-long-project-name")
}
