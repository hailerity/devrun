// Package termtext cleans raw process output — captured from a PTY, so full of
// terminal control sequences — for display. The TUI keeps colour and strips only
// what would corrupt its layout (StripUnsafe); an agent reading logs wants plain
// text with no escape sequences at all (Plain).
package termtext

import (
	"regexp"
	"strings"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes CSI sequences of the simple "ESC [ digits ; letter" shape —
// in practice, SGR colour codes.
func StripANSI(s string) string {
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

// StripUnsafe removes every control sequence except SGR colour codes, and
// expands tabs.
func StripUnsafe(s string) string {
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

// Plain reduces a line to printable text: every escape sequence goes (StripUnsafe
// then StripANSI), and so does any C0 control byte left behind — a lone ESC from
// a sequence cut off mid-line, a bell, a backspace. What remains is safe to hand
// to an agent or a JSON consumer as-is.
func Plain(s string) string {
	s = StripANSI(StripUnsafe(s))
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
