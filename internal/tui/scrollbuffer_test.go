package tui

import (
	"testing"
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
