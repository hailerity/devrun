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

	// noWrap disables line wrapping (see fitLine): a long line is truncated
	// to one row instead of spread across continuation rows. false (wrap) is
	// the zero value so every scrollBuffer{...} literal that doesn't set it —
	// tests included — keeps wrapping, the current default.
	noWrap bool
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
	sb.yOffset = min(sb.maxYOffset(), sb.yOffset+n)
}

// rowsForLine returns how many physical rows `idx` takes once wrapped to the
// buffer's width — 1 for a line that fits, more for one that doesn't. Wrap
// disabled (noWrap) or a non-positive width (not yet sized, or mid-resize)
// both degrade to 1 row, which keeps every formula below identical to the
// old fixed "1 line = 1 row" math whenever wrapping doesn't apply.
//
// This always measures against the plain-line width (sb.width), even for the
// cursor/visual-selected line, which actually renders one column narrower to
// make room for its gutter bar (see renderLine) and so can occasionally wrap
// one row later than this predicts. That's harmless: View()'s render loop
// caps total output at the row budget regardless, so the only possible
// effect is the highlighted line's last wrapped row being trimmed a touch
// early in a rare edge case — never a budget overrun.
func (sb *scrollBuffer) rowsForLine(idx int) int {
	if sb.noWrap || sb.width <= 0 {
		return 1
	}
	prepared := colorizeLog(stripUnsafe(sb.lines[idx]))
	wrapped := ansi.Hardwrap(prepared, sb.width, true)
	return strings.Count(wrapped, "\n") + 1
}

// topForBottom returns the largest starting line index such that the lines
// from that index through `end` (inclusive) fit within the row budget — i.e.
// the yOffset that puts line `end`'s last wrapped row at the bottom of the
// visible window. Generalizes the old "yOffset = end-height+1" arithmetic to
// variable per-line row counts.
func (sb *scrollBuffer) topForBottom(end int) int {
	if end <= 0 || end >= len(sb.lines) {
		return max(0, min(end, len(sb.lines)-1))
	}
	top := end
	total := sb.rowsForLine(top)
	for top > 0 {
		r := sb.rowsForLine(top - 1)
		if total+r > sb.height {
			break
		}
		total += r
		top--
	}
	return top
}

// maxYOffset returns the largest yOffset that still shows a full screen of
// trailing content — the natural scroll-down limit, and what gotoBottom
// scrolls to.
func (sb *scrollBuffer) maxYOffset() int {
	if len(sb.lines) == 0 {
		return 0
	}
	return sb.topForBottom(len(sb.lines) - 1)
}

// lineVisible reports whether line idx is fully within the current window —
// i.e. rendering from yOffset through idx (inclusive) stays within the row
// budget. Used to decide whether moveDown needs to scroll at all.
func (sb *scrollBuffer) lineVisible(idx int) bool {
	if idx < sb.yOffset || idx >= len(sb.lines) {
		return false
	}
	budget := sb.height
	for i := sb.yOffset; i <= idx; i++ {
		budget -= sb.rowsForLine(i)
		if budget < 0 {
			return false
		}
	}
	return true
}

// lineAtRow maps a 0-based row offset within the visible window (e.g. a
// mouse click's row under the cursor) to the logical line index whose
// (possibly wrapped) rows cover it. A row past the last visible line's rows
// clamps to the last line.
func (sb *scrollBuffer) lineAtRow(row int) int {
	if len(sb.lines) == 0 {
		return 0
	}
	if row < 0 {
		row = 0
	}
	idx := sb.yOffset
	for idx < len(sb.lines)-1 {
		rows := sb.rowsForLine(idx)
		if row < rows {
			return idx
		}
		row -= rows
		idx++
	}
	return len(sb.lines) - 1
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
	sb.yOffset = sb.maxYOffset()
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
		if !sb.lineVisible(sb.cursor) {
			sb.yOffset = sb.topForBottom(sb.cursor)
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
	// Expand literal tabs before anything else. The ANSI width/wrap helpers we
	// render through treat \t as a zero-width control byte (it's not a "print"
	// action), but a real terminal jumps the cursor to the next tab stop — up
	// to 7 columns they never told us about. That mismatch makes our own width
	// accounting disagree with what the terminal actually consumes, which
	// used to (before line wrapping existed here) make a tab-indented line
	// wrap on the real terminal into an extra physical row our fixed-height
	// layout never budgeted for — one Go panic's "\t<file>:<line>" stack
	// frames were enough to scroll the header off screen. Expanding tabs to
	// plain spaces up front keeps our width count and wrap points equal to
	// what actually gets printed, regardless of which helper measures it.
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

// View renders the visible window starting at yOffset. A line wider than the
// pane wraps onto continuation rows instead of being cut off, so a long line
// stays fully readable — but that means a variable number of logical lines
// fit per screen, not one-per-row. This walks forward spending the row
// budget (sb.height) rather than slicing sb.lines directly, and if the last
// line admitted would blow the budget, its trailing wrapped rows are cut so
// the total never exceeds the space the caller allocated us.
func (sb *scrollBuffer) View() string {
	if len(sb.lines) == 0 {
		return ""
	}
	budget := sb.height
	var rows []string
	for idx := sb.yOffset; budget > 0 && idx < len(sb.lines); idx++ {
		lineRows := strings.Split(sb.renderLine(idx, sb.lines[idx]), "\n")
		if len(lineRows) > budget {
			lineRows = lineRows[:budget]
		}
		rows = append(rows, lineRows...)
		budget -= len(lineRows)
	}
	return strings.Join(rows, "\n")
}

// renderLine prepares one logical line and fits it to the pane width,
// returning a "\n"-joined string of one or more physical rows. By default
// that means wrapping onto continuation rows so a long line stays fully
// visible (see View); toggling noWrap (the 'w' key) switches back to
// truncating it to a single row instead — see fitLine. A highlighted
// (cursor/visual) line fits one column narrower to leave room for its gutter
// bar; lipgloss applies that bar and the background to every row of a
// wrapped line, not just the first, so the whole line reads as selected.
func (sb *scrollBuffer) renderLine(idx int, line string) string {
	// Strip non-SGR control sequences (cursor movement, erase, OSC, CR) before
	// rendering. These would corrupt TUI layout or bleed into adjacent widgets.
	safe := stripUnsafe(line)
	colored := colorizeLog(safe)
	lo := min(sb.selStart, sb.selEnd)
	hi := max(sb.selStart, sb.selEnd)
	// Both highlighted styles carry a BorderLeft(true) gutter bar (+1 column),
	// so fit to width-1 and let lipgloss pad the background to full width —
	// a short line still highlights edge to edge.
	if sb.visualMode && idx >= lo && idx <= hi {
		return styleVisualLine.Width(sb.width - 1).Render(sb.fitLine(colored, sb.width-1))
	}
	if idx == sb.cursor {
		return styleSelectedLine.Width(sb.width - 1).Render(sb.fitLine(colored, sb.width-1))
	}
	// Append SGR reset so unclosed colour sequences don't bleed into adjacent
	// TUI widgets (header, sidebar, footer). Neither Hardwrap nor Truncate
	// reset on their own — Hardwrap just breaks the line, and Truncate only
	// resets when it actually cuts something — so an open colour would
	// otherwise ride past the last row into whatever prints next.
	return sb.fitLine(colored, sb.width) + "\x1b[m"
}

// fitLine fits already-stripped-and-colourized content to width: wrapped
// onto "\n"-joined continuation rows by default (so a long line stays fully
// visible), or truncated to a single row when wrapping is toggled off.
func (sb *scrollBuffer) fitLine(colored string, width int) string {
	if sb.noWrap {
		return ansi.Truncate(colored, width, "")
	}
	return ansi.Hardwrap(colored, width, true)
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
		sb.cursor = sb.clampLine(sb.lineAtRow(msg.Y - topOffset))
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
		sb.selEnd = sb.clampLine(sb.lineAtRow(msg.Y - topOffset))
		sb.cursor = sb.selEnd
		return true

	case msg.Action == tea.MouseActionRelease &&
		(msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonNone):
		sb.mouseDown = false
		return true
	}
	return false
}
