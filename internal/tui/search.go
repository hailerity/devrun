package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// logSearch is the scrollBuffer's search state: the query and the indices of
// the lines that contain it. Matching is a case-insensitive substring test
// against the line with its ANSI codes stripped, so colour codes inside a word
// cannot hide a match.
//
// The log only ever grows (or is reset when another service is selected), and
// View runs ten times a second, so matches are maintained incrementally:
// refresh scans just the lines added since the last call.
type logSearch struct {
	query   string
	matches []int // ascending line indices
	scanned int   // lines[:scanned] are reflected in matches
	forQ    string
}

func (ls *logSearch) active() bool { return ls.query != "" }

// refresh brings matches up to date with lines. A changed query, or a buffer
// that shrank (a different log file), forces a full rescan.
func (ls *logSearch) refresh(lines []string) {
	if ls.query != ls.forQ || ls.scanned > len(lines) {
		ls.matches, ls.scanned, ls.forQ = nil, 0, ls.query
	}
	if ls.query == "" {
		ls.scanned = len(lines)
		return
	}
	needle := strings.ToLower(ls.query)
	for i := ls.scanned; i < len(lines); i++ {
		if strings.Contains(strings.ToLower(stripANSI(stripUnsafe(lines[i]))), needle) {
			ls.matches = append(ls.matches, i)
		}
	}
	ls.scanned = len(lines)
}

// position returns the 1-based rank of line among the matches, or 0 when that
// line is not a match.
func (ls *logSearch) position(line int) int {
	i := sort.SearchInts(ls.matches, line)
	if i < len(ls.matches) && ls.matches[i] == line {
		return i + 1
	}
	return 0
}

// next returns the first match after line (dir > 0) or before it (dir < 0),
// wrapping round the ends; ok is false when there are no matches at all.
func (ls *logSearch) next(line, dir int) (int, bool) {
	if len(ls.matches) == 0 {
		return 0, false
	}
	if dir > 0 {
		i := sort.SearchInts(ls.matches, line+1)
		return ls.matches[i%len(ls.matches)], true
	}
	i := sort.SearchInts(ls.matches, line) - 1
	if i < 0 {
		i = len(ls.matches) - 1
	}
	return ls.matches[i], true
}

// nearestAtOrBefore returns the match closest to line looking upward, line
// itself included — where a search lands when it is confirmed. A log is read
// from its tail, so the match the user wants is almost always the most recent
// one above them, not the first one in the file.
func (ls *logSearch) nearestAtOrBefore(line int) (int, bool) {
	if ls.position(line) > 0 {
		return line, true
	}
	return ls.next(line, -1)
}

var styleMatch = lipgloss.NewStyle().
	Background(lipgloss.AdaptiveColor{Light: "#fae17d", Dark: "#9e6a03"}).
	Foreground(lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#ffffff"})

// highlight marks every occurrence of query in plain text (no ANSI codes).
// Case-insensitive; the original casing is what gets printed.
func highlight(text, query string) string {
	if query == "" {
		return text
	}
	lower, needle := strings.ToLower(text), strings.ToLower(query)
	// ToLower can change a string's byte length for a few code points, which
	// would make offsets found in `lower` wrong for `text`. Rare enough to
	// simply not highlight that line rather than risk slicing mid-rune.
	if len(lower) != len(text) {
		return text
	}
	var b strings.Builder
	for {
		i := strings.Index(lower, needle)
		if i < 0 {
			break
		}
		b.WriteString(text[:i])
		b.WriteString(styleMatch.Render(text[i : i+len(needle)]))
		text, lower = text[i+len(needle):], lower[i+len(needle):]
	}
	b.WriteString(text)
	return b.String()
}
