# TUI Logs Panel Improvements Design

**Date:** 2026-03-22
**Status:** Approved

## Overview

Enhance the TUI logs panel with three capabilities:

1. **Mouse scroll** — wheel up/down scrolls the log view
2. **Mouse text selection** — click to position cursor, drag to extend line selection (TUI-level); Shift+drag uses terminal-native character selection (free — terminals bypass mouse reporting for Shift events)
3. **ANSI color passthrough** — preserve ANSI escape codes from service output instead of stripping them

## Motivation

The current `logsPanel` uses `viewport.Model` from charmbracelet/bubbles for scrolling, but `model.Update` never forwards `tea.MouseMsg` events, so mouse wheel events are silently dropped. Log lines have ANSI codes stripped before storage (`stripANSI` in `poll()`), losing color output from services. There is no way to position the log cursor or start a selection using the mouse.

## Approach: Custom `scrollBuffer`

Replace `viewport.Model` with a purpose-built `scrollBuffer` type that owns lines, scroll state, cursor, and selection state in one place. This eliminates the awkward "rebuild content + set YOffset" pattern and allows tight integration between scroll position and cursor/selection.

---

## Architecture

### New file: `internal/tui/scrollbuffer.go`

```go
type scrollBuffer struct {
    lines      []string  // raw log lines, ANSI codes preserved
    yOffset    int       // index of first visible line
    width      int
    height     int

    cursor     int
    selStart   int
    selEnd     int
    visualMode bool
    followMode bool
    mouseDown  bool      // true while left mouse button is held
}
```

### Modified: `internal/tui/logs.go`

`logsPanel` becomes a thin file-state wrapper around `scrollBuffer`:

```go
type logsPanel struct {
    sb         scrollBuffer
    filePath   string
    fileOffset int64
    noLogMsg   string
}
```

Fields removed from `logsPanel` (moved to `scrollBuffer`): `lines`, `cursor`, `selStart`, `selEnd`, `visualMode`, `followMode`.
`vp viewport.Model` removed.

### Modified: `internal/tui/model.go`

- New mouse message cases in `Update`
- `m.logsC.vp.View()` → `m.logsC.view()`
- All `rebuildViewport()` calls removed

---

## Components

### `scrollBuffer.View() string`

Renders the visible window on demand (called every frame by `model.View()`). No separate "rebuild" step.

```
visible = lines[yOffset : min(yOffset+height, len(lines))]
for each line: renderLine(absIdx, line)
join with \n
```

### `scrollBuffer.renderLine(idx int, line string) string`

1. Apply `colorizeLog(line)` — HTTP status code coloring on top of preserved ANSI
2. ANSI-aware truncation to `width` using `ansi.Truncate` from `charmbracelet/x/ansi` (already in dependency tree)
3. Apply selection/cursor highlight styles if applicable

```go
func (sb *scrollBuffer) renderLine(idx int, line string) string {
    colored := colorizeLog(line)
    truncated := ansi.Truncate(colored, sb.width, "")
    lo, hi := min(sb.selStart, sb.selEnd), max(sb.selStart, sb.selEnd)
    if sb.visualMode && idx >= lo && idx <= hi {
        return styleVisualLine.Render(truncated)
    }
    if idx == sb.cursor {
        return styleSelectedLine.Render(truncated)
    }
    return truncated
}
```

### `scrollBuffer.handleMouse(msg tea.Msg, topOffset, leftOffset int) bool`

Returns `true` if the buffer state changed (view needs redraw). The model passes:
- `topOffset = 3` (header 1 row + tab bar content 1 row + tab bar border 1 row)
- `leftOffset = 23` (sidebarW 22 + divider 1; not used for line-level selection but available for future character-level work)

| Event | Behavior |
|---|---|
| `tea.MouseWheelMsg` (up) | `scrollUp(3)`, `followMode = false` |
| `tea.MouseWheelMsg` (down) | `scrollDown(3)`, `followMode = false` |
| `tea.MouseClickMsg` (left) | Set `cursor = clamp(yOffset + (Y - topOffset))`, exit visual, disable follow, `mouseDown = true` |
| `tea.MouseMotionMsg` (button held) | If `mouseDown` and not in visual: enter visual (`selStart = cursor`). Update `selEnd = clamp(yOffset + (Y - topOffset))`, `cursor = selEnd` |
| `tea.MouseReleaseMsg` | `mouseDown = false` |

With `tea.WithMouseCellMotion()` (already enabled), `MouseMotionMsg` only fires when a button is held — no hover noise.

**Shift+drag terminal-native selection:** No code needed. Most terminals (iTerm2, Terminal.app, kitty, alacritty) bypass mouse reporting for Shift+events even when mouse capture is enabled, passing them directly to the terminal's own text selection.

### `logsPanel.view() string`

```go
func (lp *logsPanel) view() string {
    if lp.noLogMsg != "" {
        return styleMuted.Render(lp.noLogMsg)
    }
    return lp.sb.View()
}
```

### `logsPanel.poll() bool`

Same file-reading logic as before, but:
- Appends `scanner.Text()` directly (no `stripANSI`)
- On new lines with `sb.followMode`: calls `sb.gotoBottom()`

### Scroll helpers on `scrollBuffer`

```go
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
}
```

---

## Changes to `model.go`

### New mouse cases in `Update`

```go
case tea.MouseWheelMsg, tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
    if m.activeTab == tabLogs {
        m.logsC.sb.handleMouse(msg, 3, sidebarW+1)
    }
```

### Removed from `Update`

- All calls to `m.logsC.rebuildViewport()` (method deleted)

### Updated keyboard handlers

```go
case key.Matches(msg, keys.Top):
    if m.activeTab == tabLogs {
        m.logsC.sb.gotoTop()
    }
case key.Matches(msg, keys.Bottom):
    if m.activeTab == tabLogs {
        m.logsC.sb.gotoBottom()
    }
```

### Updated `View()`

```go
// was: content = m.logsC.vp.View()
content = m.logsC.view()
```

### `WindowSizeMsg` handler

```go
m.logsC.sb.resize(mainW, bodyH-2)
```

---

## Changes to `logs.go`

- Delete: `ansiRe`, `stripANSI`, `rebuildViewport`
- Delete from `logsPanel`: `vp`, `lines`, `cursor`, `selStart`, `selEnd`, `visualMode`, `followMode`
- Delete from `logsPanel`: `moveUp`, `moveDown`, `enterVisual`, `exitVisual`, `copyLine`, `copySelection`
- These methods all move to `scrollBuffer`
- `setFile` resets `sb` fields: `sb.lines = nil`, `sb.cursor = 0`, `sb.yOffset = 0`, `sb.visualMode = false`
- Add `logsPanel.view()` wrapper

---

## Changes to `logs_test.go`

Tests that currently access `lp.lines`, `lp.cursor`, `lp.visualMode`, `lp.selStart`, `lp.selEnd` are updated to access `lp.sb.lines`, `lp.sb.cursor`, etc.

Tests that call `lp.moveUp()`, `lp.copyLine()`, etc. are updated to call `lp.sb.moveUp()`, `lp.sb.copyLine()`, etc.

New tests added for `scrollBuffer`:
- `TestScrollBuffer_MouseWheelScrollsUp`
- `TestScrollBuffer_MouseWheelScrollsDown`
- `TestScrollBuffer_MouseClickSetsCursor`
- `TestScrollBuffer_MouseDragExtendsSelection`

---

## Deleted

- `internal/tui/logs.go`: `ansiRe`, `stripANSI`, `rebuildViewport`
- Dependency on `github.com/charmbracelet/bubbles/viewport` (no longer imported in `logs.go`)

---

## Non-goals

- Character-level mouse selection (line-level is consistent with existing keyboard visual mode)
- Log search / filtering
- Horizontal scrolling
