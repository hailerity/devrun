package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resized(m model, w, h int) model {
	m2, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m2.(model)
}

func TestNarrow_ThresholdIsExact(t *testing.T) {
	m := setupLogModel()
	assert.True(t, resized(m, narrowBelow-1, 20).narrow())
	assert.False(t, resized(m, narrowBelow, 20).narrow())
	assert.False(t, model{}.narrow(), "an unsized model is not narrow")
}

// Below the threshold only the focused pane is drawn, at full width, and Tab
// swaps which one that is.
func TestNarrow_ShowsOnlyTheFocusedPane(t *testing.T) {
	m := resized(setupLogModel(), 60, 20)
	m.focus = focusSidebar

	out := plain(m.View())
	assert.Contains(t, out, "SERVICES")
	assert.NotContains(t, out, "[LOGS]", "the log pane is not drawn while the list has focus")
	assert.NotContains(t, out, "line-00")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	out = plain(m2.(model).View())
	assert.Contains(t, out, "[LOGS]")
	assert.Contains(t, out, "line-")
	assert.NotContains(t, out, "SERVICES", "and the list is not drawn while the log pane has focus")
}

func TestNarrow_ViewFitsInEveryState(t *testing.T) {
	for _, size := range [][2]int{{69, 20}, {50, 12}, {31, 8}, {20, 6}} {
		for _, focus := range []focusKind{focusSidebar, focusMain} {
			for _, tab := range []tabKind{tabLogs, tabDetails} {
				m := resized(setupLogModel(), size[0], size[1])
				m.focus, m.activeTab = focus, tab
				assertViewFits(t, m, size[0], size[1])

				m = pressKey(m, '?') // and with a modal over it
				assertViewFits(t, m, size[0], size[1])
			}
		}
	}
}

// With one pane on screen the log gets the whole width to wrap into, and gives
// it back when the terminal widens again.
func TestNarrow_LogPaneUsesTheFullWidth(t *testing.T) {
	m := resized(setupLogModel(), 60, 20)
	assert.Equal(t, 60-paneChrome-mainPadLeft, m.logsC.sb.width)

	wide := resized(m, 120, 20)
	_, mainW := wide.paneWidths()
	assert.Equal(t, mainW-paneChrome-mainPadLeft, wide.logsC.sb.width)
	assert.Less(t, mainW, 120, "side by side again, the log pane shares the width")
}

// The view Enter would toggle is off screen from the list, so there it opens
// the selected service; inside the main pane it toggles LOGS / DETAILS as usual.
func TestNarrow_EnterOpensTheServiceFromTheList(t *testing.T) {
	m := resized(setupLogModel(), 60, 20)
	m.focus = focusSidebar

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, focusMain, m.focus)
	assert.Equal(t, tabLogs, m.activeTab, "opening must not also flip the view")

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tabDetails, m2.(model).activeTab)

	// Wide layout: Enter from the list keeps its usual meaning.
	w := resized(setupLogModel(), 120, 20)
	w.focus = focusSidebar
	m2, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, focusSidebar, m2.(model).focus)
	assert.Equal(t, tabDetails, m2.(model).activeTab)
}

func TestNarrow_MouseFollowsTheVisiblePane(t *testing.T) {
	m := resized(setupLogModel(), 60, 20)

	// List on screen: a click where the log would be must do nothing.
	m.focus = focusSidebar
	m2, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: 5})
	assert.Equal(t, focusSidebar, m2.(model).focus)
	assert.Equal(t, m.logsC.sb.cursor, m2.(model).logsC.sb.cursor)

	// Log pane on screen: a click selects the line drawn under it.
	m.focus = focusMain
	row := screenRow(m, "line-03")
	require.Greater(t, row, 0)
	m2, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: row})
	assert.Equal(t, 3, m2.(model).logsC.sb.cursor)
}

// With one pane visible the way to the other one is the hint that must survive.
func TestNarrow_FooterAlwaysShowsThePaneSwitch(t *testing.T) {
	for _, w := range []int{69, 50, 36} {
		m := resized(setupLogModel(), w, 20)

		m.focus = focusSidebar
		assert.Contains(t, plain(m.View()), "open", "width %d: list → ↵ open", w)

		m.focus = focusMain
		assert.Contains(t, plain(m.View()), "services", "width %d: log pane → Tab services", w)
		m.activeTab = tabDetails
		assert.Contains(t, plain(m.View()), "services", "width %d: details → Tab services", w)
	}
}
