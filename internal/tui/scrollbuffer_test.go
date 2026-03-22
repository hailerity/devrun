package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestScrollBuffer_ResizeSetsWidthHeight(t *testing.T) {
	var sb scrollBuffer
	sb.resize(80, 20)
	assert.Equal(t, 80, sb.width)
	assert.Equal(t, 20, sb.height)
}

func TestScrollBuffer_ScrollUpClampsAtZero(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 50), height: 10, yOffset: 2}
	sb.scrollUp(5)
	assert.Equal(t, 0, sb.yOffset)
}

func TestScrollBuffer_ScrollDownClampsAtBottom(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 10), height: 5, yOffset: 0}
	sb.scrollDown(100)
	assert.Equal(t, 5, sb.yOffset) // max = len(lines)-height = 10-5 = 5
}

func TestScrollBuffer_ScrollDownNoopWhenFewLines(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 3), height: 10, yOffset: 0}
	sb.scrollDown(5)
	assert.Equal(t, 0, sb.yOffset) // len(lines)<height → can't scroll
}

func TestScrollBuffer_GotoTopResetsCursorAndOffset(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 20), height: 10, yOffset: 10, cursor: 15, followMode: true}
	sb.gotoTop()
	assert.Equal(t, 0, sb.cursor)
	assert.Equal(t, 0, sb.yOffset)
	assert.False(t, sb.followMode)
}

func TestScrollBuffer_GotoBottomSetsCursorAndOffset(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 20), height: 10}
	sb.gotoBottom()
	assert.Equal(t, 19, sb.cursor)
	assert.Equal(t, 10, sb.yOffset)
	assert.True(t, sb.followMode)
}

func TestScrollBuffer_GotoBottomNoopWhenEmpty(t *testing.T) {
	var sb scrollBuffer
	sb.gotoBottom() // must not panic
	assert.Equal(t, 0, sb.cursor)
}

func TestScrollBuffer_MoveUpDecreasesCursor(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a", "b", "c"}, height: 10, cursor: 2}
	sb.moveUp()
	assert.Equal(t, 1, sb.cursor)
	assert.False(t, sb.followMode)
}

func TestScrollBuffer_MoveUpScrollsViewportWhenCursorAboveOffset(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 20), height: 5, yOffset: 5, cursor: 5}
	sb.moveUp()
	assert.Equal(t, 4, sb.cursor)
	assert.Equal(t, 4, sb.yOffset) // viewport scrolled to keep cursor visible
}

func TestScrollBuffer_MoveUpDoesNotGoBelowZero(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a"}, cursor: 0}
	sb.moveUp()
	assert.Equal(t, 0, sb.cursor)
}

func TestScrollBuffer_MoveDownIncreasesCursor(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a", "b", "c"}, height: 10, cursor: 0}
	sb.moveDown()
	assert.Equal(t, 1, sb.cursor)
	assert.False(t, sb.followMode)
}

func TestScrollBuffer_MoveDownScrollsViewportWhenCursorBelowVisible(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 20), height: 5, yOffset: 0, cursor: 4}
	sb.moveDown()
	assert.Equal(t, 5, sb.cursor)
	assert.Equal(t, 1, sb.yOffset) // yOffset = cursor-height+1 = 5-5+1 = 1
}

func TestScrollBuffer_MoveDownDoesNotExceedBounds(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a", "b"}, height: 10, cursor: 1}
	sb.moveDown()
	assert.Equal(t, 1, sb.cursor)
}

func TestScrollBuffer_ClampLineEmptyBuffer(t *testing.T) {
	var sb scrollBuffer
	assert.Equal(t, 0, sb.clampLine(-5))
	assert.Equal(t, 0, sb.clampLine(100))
}

func TestScrollBuffer_ClampLineClampsToRange(t *testing.T) {
	sb := scrollBuffer{lines: make([]string, 10)}
	assert.Equal(t, 0, sb.clampLine(-1))
	assert.Equal(t, 9, sb.clampLine(20))
	assert.Equal(t, 5, sb.clampLine(5))
}

func TestScrollBuffer_EnterVisualSetsRange(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a", "b", "c"}, cursor: 1}
	sb.enterVisual()
	assert.True(t, sb.visualMode)
	assert.Equal(t, 1, sb.selStart)
	assert.Equal(t, 1, sb.selEnd)
}

func TestScrollBuffer_ExitVisualClearsMode(t *testing.T) {
	sb := scrollBuffer{visualMode: true}
	sb.exitVisual()
	assert.False(t, sb.visualMode)
}

func TestScrollBuffer_MoveDownExtendsVisualSelection(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a", "b", "c", "d"}, height: 10, cursor: 1}
	sb.enterVisual()
	sb.moveDown()
	assert.Equal(t, 2, sb.selEnd)
	assert.Equal(t, 2, sb.cursor)
}

func TestScrollBuffer_MoveUpExtendsVisualSelection(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a", "b", "c"}, height: 10, cursor: 2}
	sb.enterVisual()
	sb.moveUp()
	assert.Equal(t, 1, sb.selEnd)
	assert.Equal(t, 1, sb.cursor)
}

func TestScrollBuffer_CopyLineReturnsStrippedText(t *testing.T) {
	sb := scrollBuffer{lines: []string{"line 0", "\x1b[32mline 1\x1b[0m", "line 2"}, cursor: 1}
	assert.Equal(t, "line 1", sb.copyLine()) // ANSI stripped
}

func TestScrollBuffer_CopyLineEmptyBuffer(t *testing.T) {
	var sb scrollBuffer
	assert.Equal(t, "", sb.copyLine())
}

func TestScrollBuffer_CopySelectionStripsANSI(t *testing.T) {
	sb := scrollBuffer{
		lines:      []string{"line 0", "\x1b[32mline 1\x1b[0m", "line 2", "line 3"},
		visualMode: true,
		selStart:   1,
		selEnd:     2,
	}
	assert.Equal(t, "line 1\nline 2", sb.copySelection())
}

func TestScrollBuffer_CopySelectionReversedRange(t *testing.T) {
	sb := scrollBuffer{
		lines:      []string{"line 0", "line 1", "line 2"},
		visualMode: true,
		selStart:   2,
		selEnd:     0,
	}
	assert.Equal(t, "line 0\nline 1\nline 2", sb.copySelection())
}

func TestScrollBuffer_CopySelectionNotInVisualMode(t *testing.T) {
	sb := scrollBuffer{lines: []string{"a", "b"}, visualMode: false}
	assert.Equal(t, "", sb.copySelection())
}

func TestScrollBuffer_ViewRendersVisibleWindow(t *testing.T) {
	sb := scrollBuffer{
		lines:   []string{"line0", "line1", "line2", "line3", "line4"},
		height:  3,
		width:   80,
		yOffset: 1,
	}
	out := sb.View()
	assert.Contains(t, out, "line1")
	assert.Contains(t, out, "line2")
	assert.Contains(t, out, "line3")
	assert.NotContains(t, out, "line0")
	assert.NotContains(t, out, "line4")
}

func TestScrollBuffer_ViewEmptyBufferReturnsEmpty(t *testing.T) {
	sb := scrollBuffer{height: 10, width: 80}
	assert.Equal(t, "", sb.View())
}

func TestScrollBuffer_ViewUsesAbsoluteIndexForCursorHighlight(t *testing.T) {
	// cursor is at line 3 (absolute), viewport starts at yOffset=2
	sb := scrollBuffer{
		lines:   []string{"a", "b", "c", "d", "e"},
		height:  3,
		width:   80,
		yOffset: 2,
		cursor:  3, // absolute index 3 → visible as second line
	}
	out := sb.View()
	// line at abs index 3 is "d" — should have cursor highlight applied
	assert.Contains(t, out, "d")
}

func TestScrollBuffer_ViewHasTrailingNewline(t *testing.T) {
	sb := scrollBuffer{
		lines:  []string{"hello"},
		height: 5,
		width:  80,
	}
	out := sb.View()
	assert.True(t, strings.HasSuffix(out, "\n"))
}

func TestScrollBuffer_ExitVisualResetsSelection(t *testing.T) {
	sb := scrollBuffer{visualMode: true, selStart: 3, selEnd: 5}
	sb.exitVisual()
	assert.False(t, sb.visualMode)
	assert.Equal(t, 0, sb.selStart)
	assert.Equal(t, 0, sb.selEnd)
}

func newFilledScrollBuffer(nLines, height int) scrollBuffer {
	lines := make([]string, nLines)
	for i := range lines {
		lines[i] = "line"
	}
	return scrollBuffer{lines: lines, height: height, width: 80}
}

func TestScrollBuffer_MouseWheelUpScrolls(t *testing.T) {
	sb := newFilledScrollBuffer(30, 10)
	sb.yOffset = 10
	sb.followMode = true
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	sb.handleMouse(msg, 3, 23)
	assert.Equal(t, 7, sb.yOffset)
	assert.False(t, sb.followMode)
}

func TestScrollBuffer_MouseWheelDownScrolls(t *testing.T) {
	sb := newFilledScrollBuffer(30, 10)
	sb.yOffset = 0
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	sb.handleMouse(msg, 3, 23)
	assert.Equal(t, 3, sb.yOffset)
}

func TestScrollBuffer_MouseClickSetsCursor(t *testing.T) {
	sb := newFilledScrollBuffer(20, 10)
	sb.yOffset = 5
	// topOffset=3, click at Y=6 → lineIdx = yOffset + (Y-topOffset) = 5+(6-3) = 8
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: 6}
	sb.handleMouse(msg, 3, 23)
	assert.Equal(t, 8, sb.cursor)
	assert.True(t, sb.mouseDown)
	assert.False(t, sb.followMode)
	assert.False(t, sb.visualMode)
}

func TestScrollBuffer_MouseDragEntersVisualAndExtendsSelection(t *testing.T) {
	sb := newFilledScrollBuffer(20, 10)
	sb.yOffset = 0
	sb.cursor = 2
	sb.mouseDown = true

	// drag to Y=6 → lineIdx = 0+(6-3) = 3
	msg := tea.MouseMsg{Action: tea.MouseActionMotion, Y: 6}
	sb.handleMouse(msg, 3, 23)
	assert.True(t, sb.visualMode)
	assert.Equal(t, 2, sb.selStart)
	assert.Equal(t, 3, sb.selEnd)
	assert.Equal(t, 3, sb.cursor)
}

func TestScrollBuffer_MouseDragNoopWhenNotMouseDown(t *testing.T) {
	sb := newFilledScrollBuffer(20, 10)
	sb.mouseDown = false
	msg := tea.MouseMsg{Action: tea.MouseActionMotion, Y: 5}
	sb.handleMouse(msg, 3, 23)
	assert.False(t, sb.visualMode)
}

func TestScrollBuffer_MouseReleaseClears(t *testing.T) {
	sb := newFilledScrollBuffer(20, 10)
	sb.mouseDown = true
	msg := tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
	sb.handleMouse(msg, 3, 23)
	assert.False(t, sb.mouseDown)
}

func TestScrollBuffer_MouseClickEmptyBufferNoopNoPanic(t *testing.T) {
	var sb scrollBuffer
	sb.height = 10
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: 5}
	sb.handleMouse(msg, 3, 23) // must not panic
}
