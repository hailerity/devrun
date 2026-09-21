package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	lp.sb.lines = []string{"line 0", "line 1", "line 2"}
	lp.sb.cursor = 1
	assert.Equal(t, "line 1", lp.sb.copyLine())
}

func TestLogsPanel_CopyLineEmpty(t *testing.T) {
	lp := newLogsPanel()
	assert.Equal(t, "", lp.sb.copyLine())
}

func TestLogsPanel_CopySelection(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"line 0", "line 1", "line 2", "line 3"}
	lp.sb.visualMode = true
	lp.sb.selStart = 1
	lp.sb.selEnd = 2
	assert.Equal(t, "line 1\nline 2", lp.sb.copySelection())
}

func TestLogsPanel_MoveUpDisablesFollow(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"a", "b", "c"}
	lp.sb.cursor = 2
	lp.sb.followMode = true
	lp.sb.height = 10

	lp.sb.moveUp()
	assert.Equal(t, 1, lp.sb.cursor)
	assert.False(t, lp.sb.followMode)
}

func TestLogsPanel_MoveDownDisablesFollow(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"a", "b", "c"}
	lp.sb.cursor = 0
	lp.sb.followMode = true
	lp.sb.height = 10

	lp.sb.moveDown()
	assert.Equal(t, 1, lp.sb.cursor)
	assert.False(t, lp.sb.followMode)
}

func TestLogsPanel_MoveDownDoesNotExceedBounds(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"a", "b"}
	lp.sb.cursor = 1
	lp.sb.height = 10
	lp.sb.moveDown()
	assert.Equal(t, 1, lp.sb.cursor)
}

func TestLogsPanel_EnterVisualSetsRange(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"a", "b", "c"}
	lp.sb.cursor = 1
	lp.sb.enterVisual()
	assert.True(t, lp.sb.visualMode)
	assert.Equal(t, 1, lp.sb.selStart)
	assert.Equal(t, 1, lp.sb.selEnd)
}

func TestLogsPanel_VisualMoveExtendsSelection(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"a", "b", "c", "d"}
	lp.sb.cursor = 1
	lp.sb.height = 10
	lp.sb.enterVisual()
	lp.sb.moveDown()
	assert.Equal(t, 2, lp.sb.selEnd)
	assert.Equal(t, 2, lp.sb.cursor)
}

func TestLogsPanel_ExitVisualClearsMode(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"a", "b"}
	lp.sb.cursor = 0
	lp.sb.enterVisual()
	lp.sb.exitVisual()
	assert.False(t, lp.sb.visualMode)
}

func TestLogsPanel_CopySelectionReversed(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"line 0", "line 1", "line 2", "line 3"}
	lp.sb.visualMode = true
	lp.sb.selStart = 2
	lp.sb.selEnd = 1
	assert.Equal(t, "line 1\nline 2", lp.sb.copySelection())
}

func TestLogsPanel_MoveUpDoesNotGoBelowZero(t *testing.T) {
	lp := newLogsPanel()
	lp.sb.lines = []string{"a", "b"}
	lp.sb.cursor = 0
	lp.sb.height = 10
	lp.sb.moveUp()
	assert.Equal(t, 0, lp.sb.cursor)
}

func TestLogsPanel_NewLogsPanelFollowModeIsTrue(t *testing.T) {
	lp := newLogsPanel()
	assert.True(t, lp.sb.followMode)
}

func appendToFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// poll() is what feeds the unseen counter in the real app: tail a real file and
// check that lines written while the view is parked are counted, that poll
// still reports whether anything arrived, and that switching file resets it.
func TestLogsPanel_PollCountsLinesArrivingWhileScrolledAway(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.log")
	lp := newLogsPanel()
	lp.sb.resize(40, 5)
	lp.setFile(path)

	appendToFile(t, path, strings.Repeat("old line\n", 30))
	assert.True(t, lp.poll())
	assert.Equal(t, 0, lp.sb.unseen, "following: nothing is unseen")
	assert.Equal(t, 29, lp.sb.cursor)

	assert.False(t, lp.poll(), "no new bytes → nothing added")

	lp.sb.gotoTop()
	appendToFile(t, path, "new 1\nnew 2\nnew 3\n")
	assert.True(t, lp.poll())
	assert.Equal(t, 3, lp.sb.unseen)
	assert.Equal(t, 0, lp.sb.cursor, "the parked view did not move")

	lp.setFile(filepath.Join(t.TempDir(), "web.log"))
	assert.Equal(t, 0, lp.sb.unseen, "another service's log starts clean")
}
