package tui

import "strings"

// scrollBuffer is a self-contained scrollable line buffer. It owns the log
// lines, scroll position, cursor, and selection state for the logs panel.
type scrollBuffer struct {
	lines      []string // raw log lines, ANSI codes preserved
	yOffset    int      // index of first visible line
	width      int
	height     int

	cursor     int
	selStart   int
	selEnd     int
	visualMode bool
	followMode bool
	mouseDown  bool // true while left mouse button is held
}

func (sb *scrollBuffer) resize(w, h int) {
	sb.width = w
	sb.height = h
}

func (sb *scrollBuffer) clampLine(idx int) int {
	if len(sb.lines) == 0 {
		return 0
	}
	return max(0, min(idx, len(sb.lines)-1))
}

func (sb *scrollBuffer) scrollUp(n int) {
	sb.yOffset = max(0, sb.yOffset-n)
}

func (sb *scrollBuffer) scrollDown(n int) {
	sb.yOffset = min(max(0, len(sb.lines)-sb.height), sb.yOffset+n)
}

func (sb *scrollBuffer) gotoTop() {
	sb.cursor = 0
	sb.yOffset = 0
	sb.followMode = false
}

func (sb *scrollBuffer) gotoBottom() {
	if len(sb.lines) == 0 {
		return
	}
	sb.cursor = len(sb.lines) - 1
	sb.yOffset = max(0, len(sb.lines)-sb.height)
	sb.followMode = true
}

func (sb *scrollBuffer) moveUp() {
	if sb.cursor > 0 {
		sb.cursor--
		sb.followMode = false
		if sb.visualMode {
			sb.selEnd = sb.cursor
		}
		if sb.cursor < sb.yOffset {
			sb.yOffset = sb.cursor
		}
	}
}

func (sb *scrollBuffer) moveDown() {
	if sb.cursor < len(sb.lines)-1 {
		sb.cursor++
		sb.followMode = false
		if sb.visualMode {
			sb.selEnd = sb.cursor
		}
		if sb.cursor >= sb.yOffset+sb.height {
			sb.yOffset = sb.cursor - sb.height + 1
		}
	}
}

// View, renderLine, enterVisual, exitVisual, copyLine, copySelection,
// handleMouse are added in subsequent tasks.
// Stub View() so the file compiles.
func (sb *scrollBuffer) View() string { return strings.Join(sb.lines, "\n") }
