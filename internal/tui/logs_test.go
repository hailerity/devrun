package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColorizeLog_200IsGreen(t *testing.T) {
	out := colorizeLog("[10:14:30] GET /health 200 1ms")
	assert.Contains(t, out, "200")
	assert.Greater(t, len(out), len("[10:14:30] GET /health 200 1ms"))
}

func TestColorizeLog_404IsYellow(t *testing.T) {
	out := colorizeLog("GET /missing 404")
	assert.Contains(t, out, "404")
	assert.Greater(t, len(out), len("GET /missing 404"))
}

func TestColorizeLog_500IsRed(t *testing.T) {
	out := colorizeLog("POST /fail 500")
	assert.Contains(t, out, "500")
	assert.Greater(t, len(out), len("POST /fail 500"))
}

func TestColorizeLog_NoStatusUnchanged(t *testing.T) {
	line := "[10:14:30] connected to db"
	assert.Equal(t, line, colorizeLog(line))
}

func TestLogsPanel_CopyLine(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"line 0", "line 1", "line 2"}
	lp.cursor = 1
	assert.Equal(t, "line 1", lp.copyLine())
}

func TestLogsPanel_CopyLineEmpty(t *testing.T) {
	lp := newLogsPanel()
	assert.Equal(t, "", lp.copyLine())
}

func TestLogsPanel_CopySelection(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"line 0", "line 1", "line 2", "line 3"}
	lp.visualMode = true
	lp.selStart = 1
	lp.selEnd = 2
	assert.Equal(t, "line 1\nline 2", lp.copySelection())
}

func TestLogsPanel_MoveUpDisablesFollow(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c"}
	lp.cursor = 2
	lp.followMode = true

	lp.moveUp()
	assert.Equal(t, 1, lp.cursor)
	assert.False(t, lp.followMode)
}

func TestLogsPanel_MoveDownDisablesFollow(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c"}
	lp.cursor = 0
	lp.followMode = true

	lp.moveDown()
	assert.Equal(t, 1, lp.cursor)
	assert.False(t, lp.followMode)
}

func TestLogsPanel_MoveDownDoesNotExceedBounds(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b"}
	lp.cursor = 1
	lp.moveDown()
	assert.Equal(t, 1, lp.cursor)
}

func TestLogsPanel_EnterVisualSetsRange(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c"}
	lp.cursor = 1
	lp.enterVisual()
	assert.True(t, lp.visualMode)
	assert.Equal(t, 1, lp.selStart)
	assert.Equal(t, 1, lp.selEnd)
}

func TestLogsPanel_VisualMoveExtendsSelection(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c", "d"}
	lp.cursor = 1
	lp.enterVisual()
	lp.moveDown()
	assert.Equal(t, 2, lp.selEnd)
	assert.Equal(t, 2, lp.cursor)
}

func TestLogsPanel_ExitVisualClearsMode(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b"}
	lp.cursor = 0
	lp.enterVisual()
	lp.exitVisual()
	assert.False(t, lp.visualMode)
}

func TestLogsPanel_CopySelectionReversed(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"line 0", "line 1", "line 2", "line 3"}
	lp.visualMode = true
	lp.selStart = 2
	lp.selEnd = 1 // reversed — selEnd < selStart
	assert.Equal(t, "line 1\nline 2", lp.copySelection())
}

func TestLogsPanel_MoveUpDoesNotGoBelowZero(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b"}
	lp.cursor = 0
	lp.moveUp()
	assert.Equal(t, 0, lp.cursor)
}
