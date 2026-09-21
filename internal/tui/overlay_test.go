package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tea "github.com/charmbracelet/bubbletea"
)

func bodyOf(w, h int, fill string) string {
	rows := make([]string, h)
	for i := range rows {
		rows[i] = strings.Repeat(fill, w)
	}
	return strings.Join(rows, "\n")
}

// The overlay replaces the body in the layout, so it must be exactly the body's
// size whatever the box is — including a box larger than the body, which is
// clipped rather than allowed to push the header and footer off the terminal.
func TestOverlay_AlwaysExactlyBodySize(t *testing.T) {
	boxes := map[string]string{
		"small":     "╭──╮\n│hi│\n╰──╯",
		"too tall":  strings.Repeat("row\n", 60) + "end",
		"too wide":  strings.Repeat("w", 400),
		"wide rune": "日本語のモーダル",
		"ragged":    "a\nlonger row\nmid",
	}
	for name, box := range boxes {
		for _, size := range [][2]int{{80, 24}, {20, 5}, {7, 3}, {1, 1}} {
			w, h := size[0], size[1]
			for _, fill := range []string{".", "語"} {
				rows := strings.Split(overlay(bodyOf(w, h, fill), box, w, h), "\n")
				require.Len(t, rows, h, "%s at %dx%d", name, w, h)
				for i, row := range rows {
					assert.Equal(t, w, lipgloss.Width(row), "%s at %dx%d over %q, row %d", name, w, h, fill, i)
				}
			}
		}
	}
}

func TestOverlay_KeepsThePanesVisibleAroundTheBox(t *testing.T) {
	out := plain(overlay(bodyOf(40, 9, "."), "╭────╮\n│ hi │\n╰────╯", 40, 9))
	rows := strings.Split(out, "\n")

	assert.Equal(t, strings.Repeat(".", 40), rows[0], "rows clear of the box show the body")
	mid := rows[4]
	assert.Contains(t, mid, "│ hi │")
	assert.True(t, strings.HasPrefix(mid, "....."), "the body shows to the left of the box")
	assert.True(t, strings.HasSuffix(mid, "....."), "and to the right")
	assert.Equal(t, 17, strings.Index(mid, "│"), "the box is centred")
}

func TestOverlay_DimsTheBodyButNotTheBox(t *testing.T) {
	red := "\x1b[31mERROR\x1b[m" + strings.Repeat(".", 35)
	out := overlay(strings.Join([]string{red, red, red}, "\n"), styleAccent.Render("box"), 40, 3)
	rows := strings.Split(out, "\n")
	assert.NotContains(t, rows[0], "\x1b[31m", "the body's own colours are stripped behind a modal")
	assert.Contains(t, plain(rows[0]), "ERROR", "but its text stays readable")
	assert.Contains(t, rows[1], styleAccent.Render("box"), "the box keeps its styling")
}

func TestOverlay_ZeroSizeIsEmpty(t *testing.T) {
	assert.Equal(t, "", overlay("x", "y", 0, 5))
	assert.Equal(t, "", overlay("x", "y", 5, 0))
}

// With a modal open the full view must still be exactly the terminal size, and
// the panes must still be on screen behind it.
func TestModel_ModalFloatsOverPanesWithinTerminal(t *testing.T) {
	m := targetFilterModel(t)
	for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 8}} {
		m2, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		mm := pressKey(m2.(model), 't')
		require.True(t, mm.pickerC.open)

		rows := strings.Split(mm.View(), "\n")
		assert.Len(t, rows, size[1], "%dx%d: a modal must not change the row count", size[0], size[1])
		for i, row := range rows {
			assert.LessOrEqual(t, lipgloss.Width(row), size[0], "%dx%d row %d", size[0], size[1], i)
		}
	}

	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := plain(pressKey(m2.(model), 't').View())
	assert.Contains(t, out, "Filter by target")
	assert.Contains(t, out, "SERVICES", "the sidebar is still drawn behind the picker")
}
