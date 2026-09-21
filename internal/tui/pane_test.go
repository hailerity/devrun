package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Panes are joined side by side, so a frame must be exactly w columns on every
// row and exactly h rows — whatever the content's shape.
func TestPaneFrame_AlwaysExactlyOuterSize(t *testing.T) {
	contents := map[string]string{
		"empty":     "",
		"short":     "one\ntwo",
		"too tall":  strings.Repeat("row\n", 50),
		"too wide":  strings.Repeat("x", 300),
		"open SGR":  "\x1b[31mred text that never resets",
		"wide rune": "日本語のログ行 " + strings.Repeat("語", 80),
	}
	f := paneFrame{title: "SERVICES", titleRight: "[LOGS] details", footLeft: "3/5 up", footRight: "⇣ follow", padLeft: 1}
	for name, c := range contents {
		for _, size := range [][2]int{{40, 10}, {12, 4}, {4, 2}, {80, 24}} {
			w, h := size[0], size[1]
			rows := strings.Split(f.render(c, w, h), "\n")
			require.Len(t, rows, h, "%s at %dx%d", name, w, h)
			for i, row := range rows {
				assert.Equal(t, w, lipgloss.Width(row), "%s at %dx%d, row %d", name, w, h, i)
			}
		}
	}
}

func TestPaneFrame_TooSmallToDrawIsEmpty(t *testing.T) {
	assert.Equal(t, "", paneFrame{}.render("x", 1, 5))
	assert.Equal(t, "", paneFrame{}.render("x", 5, 1))
}

func TestPaneFrame_LabelsSitInTheBorders(t *testing.T) {
	f := paneFrame{title: "api", titleRight: "[LOGS]", footLeft: "20 lines", footRight: "follow"}
	rows := strings.Split(plain(f.render("hello", 40, 5)), "\n")
	assert.True(t, strings.HasPrefix(rows[0], "╭─ api "), rows[0])
	assert.True(t, strings.HasSuffix(rows[0], " [LOGS] ─╮"), rows[0])
	assert.True(t, strings.HasPrefix(rows[4], "╰─ 20 lines "), rows[4])
	assert.True(t, strings.HasSuffix(rows[4], " follow ─╯"), rows[4])
	assert.Contains(t, rows[1], "hello")
}

// The left label names the pane, so when both do not fit the right one goes
// first; a left label that still does not fit is cut with an ellipsis.
func TestPaneFrame_NarrowDropsRightLabelThenTruncatesLeft(t *testing.T) {
	f := paneFrame{title: "a-long-service-name", titleRight: "[LOGS] details"}

	top := strings.Split(plain(f.render("", 30, 3)), "\n")[0]
	assert.Contains(t, top, "a-long-service-name")
	assert.NotContains(t, top, "LOGS")

	top = strings.Split(plain(f.render("", 14, 3)), "\n")[0]
	assert.Contains(t, top, "…")
	assert.Equal(t, 14, lipgloss.Width(top))
}

// A log line that ends mid-colour must not tint the right border or whatever
// is drawn in the next pane.
func TestPaneFrame_ClosesOpenColourBeforeTheBorder(t *testing.T) {
	out := paneFrame{}.render("\x1b[31mred", 20, 3)
	row := strings.Split(out, "\n")[1]
	reset := strings.Index(row, "\x1b[m")
	require.GreaterOrEqual(t, reset, 0)
	assert.Greater(t, strings.LastIndex(row, "│"), reset, "the reset comes before the right border")
}

func TestPaneFrame_FocusChangesBorderColour(t *testing.T) {
	assert.NotEqual(t, paneFrame{}.render("", 10, 3), paneFrame{focused: true}.render("", 10, 3))
}
