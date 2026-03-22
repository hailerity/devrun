# TUI Logs Panel Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `viewport.Model` in the TUI logs panel with a custom `scrollBuffer` that supports mouse scroll, mouse line selection, and ANSI color passthrough.

**Architecture:** A new `scrollBuffer` type (`internal/tui/scrollbuffer.go`) owns all line data, scroll state, cursor, and selection state. `logsPanel` (`logs.go`) becomes a thin file-reading wrapper around it. `model.go` is updated to route mouse events and fix all field accesses.

**Tech Stack:** Go 1.25, charmbracelet/bubbletea v1.3.10 (`tea.MouseMsg` with `Action`/`Button` fields), charmbracelet/lipgloss v1.1.0, charmbracelet/x/ansi v0.11.6 (`ansi.Truncate`)

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/tui/scrollbuffer.go` | **Create** | `scrollBuffer` struct + all methods |
| `internal/tui/scrollbuffer_test.go` | **Create** | Tests for `scrollBuffer` |
| `internal/tui/logs.go` | **Modify** | Slim `logsPanel` wrapper around `scrollBuffer` |
| `internal/tui/logs_test.go` | **Modify** | Update field accesses; no new tests needed |
| `internal/tui/model.go` | **Modify** | Mouse handler, keyboard handler updates, `logTickMsg` fix, `View()` fix |

---

### Task 1: Create `scrollBuffer` with scroll/navigate/resize — TDD

**Files:**
- Create: `internal/tui/scrollbuffer.go`
- Create: `internal/tui/scrollbuffer_test.go`

- [ ] **Step 1: Write failing tests for navigation and scroll**

Create `internal/tui/scrollbuffer_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_" -v 2>&1 | head -30
```

Expected: compile error — `scrollBuffer` undefined.

- [ ] **Step 3: Create `internal/tui/scrollbuffer.go` with the struct and methods**

```go
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
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_" -v
```

Expected: all `TestScrollBuffer_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && git add internal/tui/scrollbuffer.go internal/tui/scrollbuffer_test.go && git commit -m "feat(tui): add scrollBuffer struct with scroll/navigate/resize"
```

---

### Task 2: Add visual selection and clipboard methods to `scrollBuffer`

**Files:**
- Modify: `internal/tui/scrollbuffer.go`
- Modify: `internal/tui/scrollbuffer_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/tui/scrollbuffer_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_(Enter|Exit|Copy|MoveDown.*Visual|MoveUp.*Visual)" -v 2>&1 | head -20
```

Expected: compile error — `enterVisual`, `copyLine` etc. undefined.

- [ ] **Step 3: Add visual selection and clipboard methods to `scrollbuffer.go`**

Add to `internal/tui/scrollbuffer.go` (after the `moveDown` function):

```go
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func (sb *scrollBuffer) enterVisual() {
	sb.visualMode = true
	sb.selStart = sb.cursor
	sb.selEnd = sb.cursor
}

func (sb *scrollBuffer) exitVisual() {
	sb.visualMode = false
}

// copyLine returns the current cursor line with ANSI codes stripped.
// Raw ANSI in stored lines must not reach the clipboard.
func (sb *scrollBuffer) copyLine() string {
	if len(sb.lines) == 0 || sb.cursor >= len(sb.lines) {
		return ""
	}
	return stripANSI(sb.lines[sb.cursor])
}

// copySelection returns selected lines joined by \n, ANSI stripped.
func (sb *scrollBuffer) copySelection() string {
	if !sb.visualMode || len(sb.lines) == 0 {
		return ""
	}
	start, end := sb.selStart, sb.selEnd
	if start > end {
		start, end = end, start
	}
	if end >= len(sb.lines) {
		end = len(sb.lines) - 1
	}
	parts := make([]string, 0, end-start+1)
	for _, l := range sb.lines[start : end+1] {
		parts = append(parts, stripANSI(l))
	}
	return strings.Join(parts, "\n")
}
```

Add `"regexp"` to the imports in `scrollbuffer.go`.

- [ ] **Step 4: Run tests**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_" -v
```

Expected: all `TestScrollBuffer_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && git add internal/tui/scrollbuffer.go internal/tui/scrollbuffer_test.go && git commit -m "feat(tui): add visual selection and clipboard methods to scrollBuffer"
```

---

### Task 3: Add `View()` and `renderLine()` to `scrollBuffer`

**Files:**
- Modify: `internal/tui/scrollbuffer.go`
- Modify: `internal/tui/scrollbuffer_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/tui/scrollbuffer_test.go`:

```go
func TestScrollBuffer_ViewRendersVisibleWindow(t *testing.T) {
    sb := scrollBuffer{
        lines:  []string{"line0", "line1", "line2", "line3", "line4"},
        height: 3,
        width:  80,
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
    // We can't easily test the ANSI codes directly, so check "d" appears
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_View" -v 2>&1 | head -20
```

Expected: FAIL — current stub `View()` doesn't slice the visible window.

- [ ] **Step 3: Replace the stub `View()` and add `renderLine()`**

Replace the stub `View()` in `scrollbuffer.go` and add `renderLine`. Also add the `ansi` import from `charmbracelet/x/ansi`:

```go
import (
    "regexp"
    "strings"

    "github.com/charmbracelet/x/ansi"
)
```

Replace the stub `View()`:

```go
func (sb *scrollBuffer) View() string {
    if len(sb.lines) == 0 {
        return ""
    }
    end := min(sb.yOffset+sb.height, len(sb.lines))
    visible := sb.lines[sb.yOffset:end]
    var out strings.Builder
    for i, line := range visible {
        out.WriteString(sb.renderLine(sb.yOffset+i, line))
        out.WriteByte('\n')
    }
    return out.String()
}

func (sb *scrollBuffer) renderLine(idx int, line string) string {
    colored := colorizeLog(line)
    lo := min(sb.selStart, sb.selEnd)
    hi := max(sb.selStart, sb.selEnd)
    if sb.visualMode && idx >= lo && idx <= hi {
        // styleVisualLine has BorderLeft(true) which adds 1 column.
        // Truncate to width-1 so border+content fits within sb.width.
        truncated := ansi.Truncate(colored, uint(sb.width-1), "")
        return styleVisualLine.Render(truncated)
    }
    truncated := ansi.Truncate(colored, uint(sb.width), "")
    if idx == sb.cursor {
        return styleSelectedLine.Render(truncated)
    }
    return truncated
}
```

> **Note on `ansi.Truncate` signature:** Check the actual signature in `charmbracelet/x/ansi`. In some versions it takes `(s string, width int, tail string)` and in others `(s string, width uint, tail string)`. Run `go doc github.com/charmbracelet/x/ansi Truncate` or check the source to confirm — adjust the cast accordingly.

- [ ] **Step 4: Run tests**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_" -v
```

Expected: all `TestScrollBuffer_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && git add internal/tui/scrollbuffer.go internal/tui/scrollbuffer_test.go && git commit -m "feat(tui): add View() and renderLine() to scrollBuffer"
```

---

### Task 4: Add `handleMouse()` to `scrollBuffer`

**Files:**
- Modify: `internal/tui/scrollbuffer.go`
- Modify: `internal/tui/scrollbuffer_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/tui/scrollbuffer_test.go`:

```go
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
    msg := tea.MouseMsg{Button: tea.MouseButtonWheelUp}
    sb.handleMouse(msg, 3, 23)
    assert.Equal(t, 7, sb.yOffset)
    assert.False(t, sb.followMode)
}

func TestScrollBuffer_MouseWheelDownScrolls(t *testing.T) {
    sb := newFilledScrollBuffer(30, 10)
    sb.yOffset = 0
    msg := tea.MouseMsg{Button: tea.MouseButtonWheelDown}
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

    // drag to Y=5 → lineIdx = 0+(5-3) = 2... let's use Y=6 → 3
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
    msg := tea.MouseMsg{Action: tea.MouseActionRelease}
    sb.handleMouse(msg, 3, 23)
    assert.False(t, sb.mouseDown)
}

func TestScrollBuffer_MouseClickEmptyBufferNoopNoPanic(t *testing.T) {
    var sb scrollBuffer
    sb.height = 10
    msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: 5}
    sb.handleMouse(msg, 3, 23) // must not panic
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_Mouse" -v 2>&1 | head -20
```

Expected: compile error — `handleMouse` undefined; also needs `tea` import.

- [ ] **Step 3: Add `handleMouse()` to `scrollbuffer.go`**

Add `tea "github.com/charmbracelet/bubbletea"` to imports.

```go
// handleMouse dispatches a bubbletea v1.3.10 tea.MouseMsg.
// topOffset is the terminal row where log content starts (header+tabbar = 3).
// leftOffset is the terminal column where the main panel starts (unused for
// line-level selection but reserved for future character-level work).
// Returns true if state changed.
func (sb *scrollBuffer) handleMouse(msg tea.MouseMsg, topOffset, leftOffset int) bool {
    switch {
    case msg.Button == tea.MouseButtonWheelUp:
        sb.scrollUp(3)
        sb.followMode = false
        return true

    case msg.Button == tea.MouseButtonWheelDown:
        sb.scrollDown(3)
        sb.followMode = false
        return true

    case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
        if len(sb.lines) == 0 {
            return false
        }
        sb.cursor = sb.clampLine(sb.yOffset + (msg.Y - topOffset))
        sb.exitVisual()
        sb.followMode = false
        sb.mouseDown = true
        return true

    case msg.Action == tea.MouseActionMotion && sb.mouseDown:
        if len(sb.lines) == 0 {
            return false
        }
        if !sb.visualMode {
            sb.enterVisual()
        }
        sb.selEnd = sb.clampLine(sb.yOffset + (msg.Y - topOffset))
        sb.cursor = sb.selEnd
        return true

    case msg.Action == tea.MouseActionRelease:
        sb.mouseDown = false
        return true
    }
    return false
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -run "TestScrollBuffer_" -v
```

Expected: all `TestScrollBuffer_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && git add internal/tui/scrollbuffer.go internal/tui/scrollbuffer_test.go && git commit -m "feat(tui): add handleMouse() to scrollBuffer"
```

---

### Task 5: Migrate `logs.go` and update `logs_test.go`

**Files:**
- Modify: `internal/tui/logs.go`
- Modify: `internal/tui/logs_test.go`

After this task all existing logs tests pass against the new structure.

- [ ] **Step 1: Rewrite `logs.go`**

Replace the entire contents of `internal/tui/logs.go` with:

```go
package tui

import (
    "bufio"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "regexp"
    "strings"
)

type logsPanel struct {
    sb         scrollBuffer
    filePath   string
    fileOffset int64
    noLogMsg   string
}

func newLogsPanel() logsPanel {
    return logsPanel{
        sb: scrollBuffer{followMode: true},
    }
}

// setFile switches the panel to tail a new log file.
func (lp *logsPanel) setFile(path string) {
    lp.filePath = path
    lp.fileOffset = 0
    lp.sb.lines = nil
    lp.sb.cursor = 0
    lp.sb.yOffset = 0
    lp.sb.visualMode = false
    lp.sb.followMode = true
    lp.noLogMsg = ""
}

// poll reads any new lines appended to the log file since the last poll.
func (lp *logsPanel) poll() bool {
    if lp.filePath == "" {
        return false
    }
    f, err := os.Open(lp.filePath)
    if err != nil {
        lp.noLogMsg = fmt.Sprintf("No logs yet for %s", logName(lp.filePath))
        return false
    }
    defer f.Close()

    if _, err := f.Seek(lp.fileOffset, io.SeekStart); err != nil {
        lp.fileOffset = 0
        lp.sb.lines = nil
        f.Seek(0, io.SeekStart) //nolint:errcheck
    }
    scanner := bufio.NewScanner(f)
    var added bool
    for scanner.Scan() {
        lp.sb.lines = append(lp.sb.lines, scanner.Text()) // raw, ANSI preserved
        added = true
    }
    lp.fileOffset, _ = f.Seek(0, io.SeekCurrent)
    lp.noLogMsg = ""

    if added && lp.sb.followMode {
        lp.sb.gotoBottom()
    }
    return added
}

// view renders the panel: shows noLogMsg if no file, otherwise delegates to scrollBuffer.
func (lp *logsPanel) view() string {
    if lp.noLogMsg != "" {
        return styleMuted.Render(lp.noLogMsg)
    }
    return lp.sb.View()
}

// colorizeLog wraps HTTP status codes (2xx/4xx/5xx) with palette colors.
var httpStatusRe = regexp.MustCompile(`\b([2-5]\d{2})\b`)

func colorizeLog(line string) string {
    return httpStatusRe.ReplaceAllStringFunc(line, func(code string) string {
        switch code[0] {
        case '2':
            return styleGreen.Render(code)
        case '4':
            return styleYellow.Render(code)
        case '5':
            return styleRed.Render(code)
        }
        return code
    })
}

// logName extracts "myservice" from "/path/to/logs/myservice.log".
func logName(path string) string {
    return strings.TrimSuffix(filepath.Base(path), ".log")
}
```

> **Note:** `ansiRe`, `stripANSI`, `rebuildViewport`, `renderLine`, `moveUp`, `moveDown`, `enterVisual`, `exitVisual`, `copyLine`, `copySelection` are all gone from `logs.go` — they now live in `scrollbuffer.go`. `httpStatusRe` / `colorizeLog` stay in `logs.go` since `scrollbuffer.go`'s `renderLine` calls `colorizeLog` (same package — no import needed).

- [ ] **Step 2: Update `logs_test.go`**

The existing tests access `lp.lines`, `lp.cursor`, etc. — update to `lp.sb.lines`, `lp.sb.cursor`, etc. Replace the entire file:

```go
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
```

- [ ] **Step 3: Run all tui tests**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./internal/tui/ -v 2>&1 | tail -20
```

Expected: all tests PASS. If `model.go` still references `lp.vp` or `rebuildViewport`, there will be compile errors — those are fixed in Task 6.

- [ ] **Step 4: Commit**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && git add internal/tui/logs.go internal/tui/logs_test.go && git commit -m "feat(tui): migrate logsPanel to use scrollBuffer"
```

---

### Task 6: Update `model.go` — mouse handler, keyboard wiring, View() fix

**Files:**
- Modify: `internal/tui/model.go`

No new tests needed — all behaviour is wired to `scrollBuffer` methods already tested above.

- [ ] **Step 1: Update `WindowSizeMsg` handler**

Find in `model.go`:
```go
m.logsC.resize(mainW, bodyH-2)
```
Replace with:
```go
m.logsC.sb.resize(mainW, bodyH-2)
```

- [ ] **Step 2: Update `logTickMsg` handler — remove `rebuildViewport`**

Find:
```go
case logTickMsg:
    changed := m.logsC.poll()
    if changed {
        m.logsC.rebuildViewport()
    }
    return m, tickLog()
```
Replace with:
```go
case logTickMsg:
    m.logsC.poll()
    return m, tickLog()
```

- [ ] **Step 3: Add `tea.MouseMsg` case to `Update`**

Add after the `tea.KeyMsg` case (or after `spinTickMsg` case):
```go
case tea.MouseMsg:
    if m.activeTab == tabLogs {
        m.logsC.sb.handleMouse(msg, 3, 23)
    }
```

- [ ] **Step 4: Update all keyboard handlers in `handleKey`**

Replace the entire `handleKey` method body. The `keys.Up` / `keys.Down` / `keys.Top` / `keys.Bottom` / `keys.Follow` / `keys.Visual` / `keys.Escape` / `keys.Copy` cases all need updating. Replace `handleKey` with:

```go
func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch {
    case key.Matches(msg, keys.Quit):
        return m, tea.Quit

    case key.Matches(msg, keys.Left):
        m.focus = focusSidebar

    case key.Matches(msg, keys.Right):
        m.focus = focusMain

    case key.Matches(msg, keys.Tab):
        if m.focus == focusSidebar {
            m.focus = focusMain
        } else {
            if m.activeTab == tabLogs {
                m.activeTab = tabDetails
            } else {
                m.activeTab = tabLogs
            }
        }

    case key.Matches(msg, keys.Up):
        if m.focus == focusSidebar {
            m.sidebarC.moveUp()
            m.updateLogFile()
        } else if m.activeTab == tabLogs {
            m.logsC.sb.moveUp()
        }

    case key.Matches(msg, keys.Down):
        if m.focus == focusSidebar {
            m.sidebarC.moveDown()
            m.updateLogFile()
        } else if m.activeTab == tabLogs {
            m.logsC.sb.moveDown()
        }

    case key.Matches(msg, keys.Top):
        if m.activeTab == tabLogs {
            m.logsC.sb.gotoTop()
        }

    case key.Matches(msg, keys.Bottom):
        if m.activeTab == tabLogs {
            m.logsC.sb.gotoBottom()
        }

    case key.Matches(msg, keys.Follow):
        if m.focus == focusMain && m.activeTab == tabLogs {
            m.logsC.sb.followMode = !m.logsC.sb.followMode
        }

    case key.Matches(msg, keys.Visual):
        if m.focus == focusMain && m.activeTab == tabLogs {
            m.logsC.sb.enterVisual()
        }

    case key.Matches(msg, keys.Escape):
        m.logsC.sb.exitVisual()

    case key.Matches(msg, keys.Copy):
        if m.focus == focusMain && m.activeTab == tabLogs {
            var text string
            if m.logsC.sb.visualMode {
                text = m.logsC.sb.copySelection()
                m.logsC.sb.exitVisual()
            } else {
                text = m.logsC.sb.copyLine()
            }
            if !m.cb.Available() {
                m.footerC.showToast("No clipboard available")
            } else if err := m.cb.Copy(text); err != nil {
                m.footerC.showToastLong("Copy failed")
            } else {
                m.footerC.showToast("Copied!")
            }
        }

    case key.Matches(msg, keys.Start):
        return m, m.doStart()

    case key.Matches(msg, keys.Stop):
        return m, m.doStop()
    }

    return m, nil
}
```

- [ ] **Step 5: Update `renderMain()` and `View()`**

In `renderMain`, find:
```go
content = m.logsC.vp.View()
```
Replace with:
```go
content = m.logsC.view()
```

Find the follow indicator (in `renderMain`):
```go
if m.activeTab == tabLogs && m.logsC.followMode {
```
Replace with:
```go
if m.activeTab == tabLogs && m.logsC.sb.followMode {
```

In `View()`, find the footer call:
```go
footer := m.footerC.render(m.activeTab, m.focus, m.logsC.visualMode, m.width)
```
Replace with:
```go
footer := m.footerC.render(m.activeTab, m.focus, m.logsC.sb.visualMode, m.width)
```

- [ ] **Step 6: Build and run all tests**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go build ./... && go test ./internal/tui/ -v 2>&1 | tail -30
```

Expected: build succeeds, all tests PASS. If there are remaining references to deleted methods (`rebuildViewport`, `lp.vp`, `lp.followMode`, `lp.visualMode`), fix them now.

- [ ] **Step 7: Run full test suite**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && go test ./... 2>&1
```

Expected: all packages PASS.

- [ ] **Step 8: Commit**

```bash
cd /Users/hailerity/Workspaces/hailerity/procet && git add internal/tui/model.go && git commit -m "feat(tui): wire scrollBuffer into model — mouse scroll, selection, ANSI color"
```
