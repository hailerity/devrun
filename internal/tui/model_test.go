package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestModel_WindowSizeSetsWidthHeight(t *testing.T) {
	m := model{}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m2.(model)
	assert.Equal(t, 120, mm.width)
	assert.Equal(t, 40, mm.height)
}

func TestModel_QuitKeyReturnsQuitCmd(t *testing.T) {
	m := model{}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd)
}

// setupLogModel returns a model sized to 100x30 with the log tab active and
// 20 log lines pre-loaded, ready for mouse/keyboard testing.
func setupLogModel() model {
	m := newModel("", nil, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(model)
	m.activeTab = tabLogs
	for i := 0; i < 20; i++ {
		m.logsC.sb.lines = append(m.logsC.sb.lines, "line")
	}
	m.logsC.sb.followMode = false
	m.logsC.noLogMsg = ""
	return m
}

// TestModel_MouseClick_SetsCorrectCursor verifies topOffset=4 (header 2 rows +
// tab-bar label+border 2 rows = 4 rows above log content; no bubbletea clipping
// since total render equals terminal height exactly).
func TestModel_MouseClick_SetsCorrectCursor(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain

	// Click on the first visible log line (terminal row 4).
	m2, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      4, // first log line: topOffset(4) + lineIdx(0)
	})
	mm := m2.(model)
	assert.Equal(t, 0, mm.logsC.sb.cursor, "clicking terminal row 4 should select log line index 0")

	// Click on the fifth visible log line (terminal row 8 = topOffset 4 + index 4).
	m3, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      8, // topOffset(4) + lineIdx(4)
	})
	mm3 := m3.(model)
	assert.Equal(t, 4, mm3.logsC.sb.cursor, "clicking terminal row 8 should select log line index 4")
}

// TestModel_CtrlC_CopiesWhenVisualModeActive verifies that ctrl+c (Cmd+C on
// macOS / Ctrl+Shift+C on Ubuntu when forwarded by the terminal) copies the
// visual selection rather than quitting, when focus is on the log panel.
func TestModel_CtrlC_CopiesWhenVisualModeActive(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain
	m.logsC.sb.visualMode = true
	m.logsC.sb.selStart = 1
	m.logsC.sb.selEnd = 3

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm := m2.(model)

	assert.Nil(t, cmd, "ctrl+c with visual selection should not quit")
	assert.False(t, mm.logsC.sb.visualMode, "visual mode should be exited after copy")
	// clipboard{} has no backend, so the toast is "No clipboard available"
	assert.Equal(t, "No clipboard available", mm.footerC.toast)
}

// TestModel_CtrlC_QuitsWhenNoVisualMode verifies that ctrl+c without an active
// visual selection still quits as normal.
func TestModel_CtrlC_QuitsWhenNoVisualMode(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain
	// visualMode is false (default)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd, "ctrl+c without visual selection should quit")
}

// TestModel_TabCycles_SidebarLogsDetailsSidebar verifies the 3-state Tab cycle.
func TestModel_TabCycles_SidebarLogsDetailsSidebar(t *testing.T) {
	m := model{}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)

	// Default state: focusSidebar
	assert.Equal(t, focusSidebar, m.focus)

	// Tab 1: sidebar → main/logs
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusMain, m.focus)
	assert.Equal(t, tabLogs, m.activeTab)

	// Tab 2: logs → details
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusMain, m.focus)
	assert.Equal(t, tabDetails, m.activeTab)

	// Tab 3: details → sidebar
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusSidebar, m.focus)
}

// TestModel_MouseClick_SetsFocusMain verifies that clicking in the log area
// automatically moves focus to the main panel so that y/v/f shortcuts work.
func TestModel_MouseClick_SetsFocusMain(t *testing.T) {
	m := setupLogModel()
	m.focus = focusSidebar // start with focus on sidebar

	m2, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      4,
	})
	mm := m2.(model)
	assert.Equal(t, focusMain, mm.focus, "clicking in the log area should auto-focus the main panel")
}
