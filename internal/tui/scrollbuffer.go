package tui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// unsafeSeqRe matches control sequences that are not SGR colour codes.
// These corrupt TUI layout or bleed colour into adjacent widgets when rendered raw:
//   - Carriage return: physically moves cursor to column 0
//   - Non-SGR CSI: cursor movement, erase, show/hide cursor, etc. (final byte ≠ 'm').
//     The full ECMA-48 grammar is matched — parameter bytes 0x30-0x3F, intermediate
//     bytes 0x20-0x2F, then any final byte — not just a bare "digits + letter"
//     shape, so sequences like "ESC[6 q" (an intermediate byte) are caught too.
//   - String sequences (OSC/DCS/SOS/PM/APC): window title, hyperlinks, and other
//     ESC-]/P/X/^/_ ... (BEL or ST)-terminated payloads.
//   - Bare (no-argument) escapes: e.g. ESC M (reverse index — scrolls the real
//     terminal and can shove the TUI's own header off screen), ESC D (index),
//     ESC c (full reset), ESC 7/8 (save/restore cursor). These have no "[" or
//     "]" so the CSI/OSC branches above never see them, and a naive filter that
//     only strips bracketed sequences lets them straight through to the terminal.
var unsafeSeqRe = regexp.MustCompile(
	`\r` +
		`|\x1b[\]PX^_][^\x07\x1b]*(?:\x07|\x1b\\)` +
		`|\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x6c\x6e-\x7e]` +
		`|\x1b[\x30-\x4f\x51-\x5a\x5c\x60-\x7e]`,
)

func stripUnsafe(s string) string {
	// Expand literal tabs before anything else. ansi.Truncate's width pass
	// treats \t as a zero-width control byte (it's not a "print" action), but
	// a real terminal jumps the cursor to the next tab stop — up to 7 columns
	// it never told us about. That mismatch makes us under-truncate: the line
	// we hand the terminal is wider than we believe, so the terminal wraps it
	// into an extra physical row our fixed-height layout never budgeted for.
	// One tab-indented line (a Go panic's "\t<file>:<line>" stack frames are
	// the common case) is enough to push everything below it down a row, and
	// with several such lines the header itself scrolls out of view. Expanding
	// to plain spaces makes our width count match what actually gets printed.
	s = strings.ReplaceAll(s, "\t", "    ")
	return unsafeSeqRe.ReplaceAllString(s, "")
}

func (sb *scrollBuffer) enterVisual() {
	sb.visualMode = true
	sb.selStart = sb.cursor
	sb.selEnd = sb.cursor
}

func (sb *scrollBuffer) exitVisual() {
	sb.visualMode = false
	sb.selStart = 0
	sb.selEnd = 0
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

func (sb *scrollBuffer) View() string {
	if len(sb.lines) == 0 {
		return ""
	}
	end := min(sb.yOffset+sb.height, len(sb.lines))
	visible := sb.lines[sb.yOffset:end]
	var out strings.Builder
	for i, line := range visible {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(sb.renderLine(sb.yOffset+i, line))
	}
	return out.String()
}

func (sb *scrollBuffer) renderLine(idx int, line string) string {
	// Strip non-SGR control sequences (cursor movement, erase, OSC, CR) before
	// rendering. These would corrupt TUI layout or bleed into adjacent widgets.
	safe := stripUnsafe(line)
	colored := colorizeLog(safe)
	lo := min(sb.selStart, sb.selEnd)
	hi := max(sb.selStart, sb.selEnd)
	// Both highlighted styles carry a BorderLeft(true) gutter bar (+1 column),
	// so truncate to width-1 and let lipgloss pad the background to full width
	// — a short line still highlights edge to edge.
	if sb.visualMode && idx >= lo && idx <= hi {
		truncated := ansi.Truncate(colored, sb.width-1, "")
		return styleVisualLine.Width(sb.width - 1).Render(truncated)
	}
	if idx == sb.cursor {
		truncated := ansi.Truncate(colored, sb.width-1, "")
		return styleSelectedLine.Width(sb.width - 1).Render(truncated)
	}
	truncated := ansi.Truncate(colored, sb.width, "")
	// Append SGR reset so unclosed colour sequences don't bleed into adjacent
	// TUI widgets (header, sidebar, footer). ansi.Truncate only resets when
	// truncation fires; for short lines it returns the raw string unchanged.
	return truncated + "\x1b[m"
}

// handleMouse dispatches a bubbletea v1.3.10 tea.MouseMsg.
// topOffset is the terminal row where log content starts (header+tabbar = 3).
// leftOffset is the terminal column where the main panel starts (reserved for
// future character-level work, unused for line-level selection).
// Returns true if state changed.
func (sb *scrollBuffer) handleMouse(msg tea.MouseMsg, topOffset, leftOffset int) bool {
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
		sb.scrollUp(3)
		sb.followMode = false
		return true

	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
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

	case msg.Action == tea.MouseActionRelease &&
		(msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonNone):
		sb.mouseDown = false
		return true
	}
	return false
}
