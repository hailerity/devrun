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
for i, line := range visible:
    absIdx = yOffset + i          // absolute index into lines; must be yOffset+i, not i
    renderLine(absIdx, line)
join rendered lines with \n, with a trailing \n after the last line
```

`absIdx` must be `yOffset + i` (not the slice-local `i`) because `renderLine` compares against `cursor`, `selStart`, and `selEnd` which are all absolute line indices.

The trailing `\n` matches the existing `rebuildViewport` behavior and prevents lipgloss height-padding from dropping the last log line when the content block has a fixed `Height` set.

### `scrollBuffer.renderLine(idx int, line string) string`

1. Apply `colorizeLog(line)` — HTTP status code coloring on top of preserved ANSI
2. ANSI-aware truncation to `width` using `ansi.Truncate` from `charmbracelet/x/ansi` (already in dependency tree)
3. Apply selection/cursor highlight styles if applicable

```go
func (sb *scrollBuffer) renderLine(idx int, line string) string {
    colored := colorizeLog(line)
    lo, hi := min(sb.selStart, sb.selEnd), max(sb.selStart, sb.selEnd)
    if sb.visualMode && idx >= lo && idx <= hi {
        // styleVisualLine has BorderLeft(true) which adds 1 glyph of width.
        // Truncate to width-1 so the border + content fits within sb.width total.
        truncated := ansi.Truncate(colored, sb.width-1, "")
        return styleVisualLine.Render(truncated)
    }
    truncated := ansi.Truncate(colored, sb.width, "")
    if idx == sb.cursor {
        return styleSelectedLine.Render(truncated)
    }
    return truncated
}
```

`styleSelectedLine` has no border, so it does not expand width. `styleVisualLine` has `BorderLeft(true)` (+1 glyph), requiring a `width-1` truncation before wrapping.

### `scrollBuffer.handleMouse(msg tea.MouseMsg, topOffset, leftOffset int) bool`

Returns `true` if the buffer state changed (view needs redraw). The model passes:
- `topOffset = 3` (header 1 row + tab bar content 1 row + tab bar border 1 row)
- `leftOffset = 23` (sidebarW 22 + divider 1; not used for line-level selection but available for future character-level work)

bubbletea v1.3.10 uses a single `tea.MouseMsg` type with `Action` and `Button` fields. Dispatch by inspecting these fields:

| Condition | Behavior |
|---|---|
| `msg.Button == tea.MouseButtonWheelUp` | `scrollUp(3)`, `followMode = false` |
| `msg.Button == tea.MouseButtonWheelDown` | `scrollDown(3)`, `followMode = false` |
| `msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft` | Set `cursor = clampLine(yOffset + (msg.Y - topOffset))`, exit visual, disable follow, `mouseDown = true` |
| `msg.Action == tea.MouseActionMotion && mouseDown` | If not in visual: enter visual (`selStart = cursor`). Update `selEnd = clampLine(yOffset + (msg.Y - topOffset))`, `cursor = selEnd` |
| `msg.Action == tea.MouseActionRelease` | `mouseDown = false` |

With `tea.WithMouseCellMotion()` (already enabled), `MouseActionMotion` only fires when a button is held — no hover noise.

**`clampLine` helper** — clamps a line index to the valid range, guarding against an empty buffer:

```go
func (sb *scrollBuffer) clampLine(idx int) int {
    if len(sb.lines) == 0 {
        return 0
    }
    return max(0, min(idx, len(sb.lines)-1))
}
```

When `lines` is empty, click/drag handlers clamp to 0 and are no-ops (cursor stays at 0, visual mode is entered but has no lines to highlight). `handleMouse` should early-return if `len(sb.lines) == 0` for press and motion actions.

**Auto-scroll during drag** is declared a non-goal (see Non-goals). Dragging outside the visible window clamps to the nearest visible line.

**Wheel scroll does not move the cursor.** `scrollUp`/`scrollDown` only adjust `yOffset`. The cursor may go off-screen after a wheel scroll; this is intentional. Pressing `j`/`k` will re-sync the viewport to the cursor position.

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

### `scrollBuffer.moveUp()` and `scrollBuffer.moveDown()`

Preserve the existing behavior (disable follow, extend visual selection) and additionally keep the cursor within the visible window by adjusting `yOffset`:

```go
func (sb *scrollBuffer) moveUp() {
    if sb.cursor > 0 {
        sb.cursor--
        sb.followMode = false
        if sb.visualMode {
            sb.selEnd = sb.cursor
        }
        // scroll up if cursor moved above visible window
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
        // scroll down if cursor moved below visible window
        if sb.cursor >= sb.yOffset+sb.height {
            sb.yOffset = sb.cursor - sb.height + 1
        }
    }
}
```

### `scrollBuffer.enterVisual()`, `exitVisual()`, `copyLine()`, `copySelection()`

```go
func (sb *scrollBuffer) enterVisual() {
    sb.visualMode = true
    sb.selStart = sb.cursor
    sb.selEnd = sb.cursor
}

func (sb *scrollBuffer) exitVisual() {
    sb.visualMode = false
}

// copyLine and copySelection strip ANSI codes before returning text for the
// clipboard. Stored lines now contain raw ANSI from service output; passing
// those escape sequences to the clipboard would produce garbage in most apps.
func (sb *scrollBuffer) copyLine() string {
    if len(sb.lines) == 0 || sb.cursor >= len(sb.lines) {
        return ""
    }
    return stripANSI(sb.lines[sb.cursor])
}

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

`stripANSI` and `ansiRe` are **not** deleted from `logs.go` — they move to `scrollbuffer.go` where `copyLine`/`copySelection` still need them. Only `viewport.Model` usage is removed from `logs.go`.

### Scroll and resize helpers on `scrollBuffer`

```go
func (sb *scrollBuffer) resize(w, h int) {
    sb.width = w
    sb.height = h
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
    sb.followMode = true  // re-enable follow so new lines keep scrolling down
}
```

---

## Changes to `model.go`

### New mouse cases in `Update`

bubbletea v1.3.10 delivers all mouse events as `tea.MouseMsg`:

```go
case tea.MouseMsg:
    if m.activeTab == tabLogs {
        m.logsC.sb.handleMouse(msg, 3, sidebarW+1)
    }
```

### Removed from `Update`

- All calls to `m.logsC.rebuildViewport()` (method deleted), including in `logTickMsg`:

```go
// before:
case logTickMsg:
    changed := m.logsC.poll()
    if changed {
        m.logsC.rebuildViewport()
    }
    return m, tickLog()

// after (rebuildViewport removed; View() renders on-demand each frame):
case logTickMsg:
    m.logsC.poll()
    return m, tickLog()
```

### Updated keyboard handlers

All handlers that previously accessed `logsPanel` fields/methods now route through `scrollBuffer`:

```go
case key.Matches(msg, keys.Up):
    if m.focus == focusSidebar { ... } else if m.activeTab == tabLogs {
        m.logsC.sb.moveUp()
    }
case key.Matches(msg, keys.Down):
    if m.focus == focusSidebar { ... } else if m.activeTab == tabLogs {
        m.logsC.sb.moveDown()
    }
case key.Matches(msg, keys.Top):
    if m.activeTab == tabLogs { m.logsC.sb.gotoTop() }
case key.Matches(msg, keys.Bottom):
    if m.activeTab == tabLogs { m.logsC.sb.gotoBottom() }
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
        // clipboard handling unchanged
    }
```

### Updated `View()` / `renderMain()`

```go
// log content
content = m.logsC.view()

// follow indicator — was m.logsC.followMode
if m.activeTab == tabLogs && m.logsC.sb.followMode { ... }

// footer — was m.logsC.visualMode
footer := m.footerC.render(m.activeTab, m.focus, m.logsC.sb.visualMode, m.width)
```

### `WindowSizeMsg` handler

```go
// bodyH = m.height - 2  (subtracts header row + footer row)
// bodyH-2 subtracts tab bar content row + tab bar border row
// → sb.height = m.height - 4, matching renderMain's contentH = bodyH-2
m.logsC.sb.resize(mainW, bodyH-2)
```

---

## Changes to `logs.go`

- Delete: `ansiRe`, `stripANSI`, `rebuildViewport`
- Delete from `logsPanel`: `vp`, `lines`, `cursor`, `selStart`, `selEnd`, `visualMode`, `followMode`
- Delete from `logsPanel`: `moveUp`, `moveDown`, `enterVisual`, `exitVisual`, `copyLine`, `copySelection`
- These methods all move to `scrollBuffer`
- `setFile` resets `sb` fields: `sb.lines = nil`, `sb.cursor = 0`, `sb.yOffset = 0`, `sb.visualMode = false`, `sb.followMode = true`
- Add `logsPanel.view()` wrapper
- `newLogsPanel()` must initialize `sb.followMode = true` (zero value is `false`, which would break initial follow-on-startup behaviour):

```go
func newLogsPanel() logsPanel {
    return logsPanel{
        sb: scrollBuffer{followMode: true},
    }
}
```

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

## Deleted / Moved

- `internal/tui/logs.go`: `rebuildViewport` deleted; `ansiRe` and `stripANSI` **moved** to `scrollbuffer.go` (still needed for clipboard copy)
- `viewport.Model` usage removed from `logs.go`; `github.com/charmbracelet/bubbles/viewport` import removed from `logs.go`

**Note:** `ansiRe` / `stripANSI` are still used in `scrollBuffer.copyLine()` and `scrollBuffer.copySelection()` to strip ANSI before writing to the clipboard. They are not deleted from the codebase.

---

## Non-goals

- Character-level mouse selection (line-level is consistent with existing keyboard visual mode)
- Auto-scroll during drag (selection clamps to visible window edges; keyboard `j`/`k` can extend after scrolling)
- Log search / filtering
- Horizontal scrolling
