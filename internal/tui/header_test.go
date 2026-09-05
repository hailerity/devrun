package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// header.render's gap is clamped to a minimum of 1, not capped: a left+right
// wide enough (many services, a long spinner frame) at a narrow terminal
// width can still leave the assembled line wider than width. lipgloss's
// Width() style would then word-wrap it into extra lines instead of capping
// it back down (confirmed by inspection — see the footer's equivalent
// tests), which is exactly the mechanism that pushed the header off screen
// via the footer's hint line. render() must truncate the line first so the
// result always stays content(1)+border(1) = 2 rows regardless.

func TestHeaderBar_NeverWrapsAtNarrowWidth(t *testing.T) {
	h := headerBar{}
	out := h.render(999, 999, 0, true, 15)
	assert.Equal(t, 2, lipgloss.Height(out),
		"header must stay content(1)+border(1) rows even when the service count overflows a narrow width")
}

func TestHeaderBar_FitsNormallyAtComfortableWidth(t *testing.T) {
	h := headerBar{}
	out := h.render(3, 1, 0, false, 80)
	assert.Equal(t, 2, lipgloss.Height(out))
	assert.Contains(t, out, "devrun")
	assert.Contains(t, out, "3 services")
}
