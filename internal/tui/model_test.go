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

// TestModel_MouseClick_SetsCorrectCursor verifies topOffset=2 (bubbletea clips
// the 2-row header from the top; tab bar occupies terminal rows 0-1; log
// content starts at terminal row 2).
func TestModel_MouseClick_SetsCorrectCursor(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain

	// Click on the first visible log line (terminal row 2).
	m2, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      2, // first log line: topOffset(2) + lineIdx(0)
	})
	mm := m2.(model)
	assert.Equal(t, 0, mm.logsC.sb.cursor, "clicking terminal row 2 should select log line index 0")

	// Click on the fifth visible log line (terminal row 6 = topOffset 2 + index 4).
	m3, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      6, // topOffset(2) + lineIdx(4)
	})
	mm3 := m3.(model)
	assert.Equal(t, 4, mm3.logsC.sb.cursor, "clicking terminal row 6 should select log line index 4")
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
