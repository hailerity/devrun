package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogSearch_MatchesCaseInsensitivelyThroughANSI(t *testing.T) {
	ls := logSearch{query: "error"}
	ls.refresh([]string{
		"all good",
		"an ERROR here",
		"\x1b[31mer\x1b[0mror split by colour codes",
		"Error twice: error",
		"nothing",
	})
	assert.Equal(t, []int{1, 2, 3}, ls.matches)
}

// The log only grows, and refresh runs on every poll: it must pick up appended
// lines without rescanning — and still be right when the query or file changes.
func TestLogSearch_RefreshIsIncrementalAndStaysCorrect(t *testing.T) {
	lines := []string{"hit", "miss", "hit"}
	ls := logSearch{query: "hit"}
	ls.refresh(lines)
	require.Equal(t, []int{0, 2}, ls.matches)
	require.Equal(t, 3, ls.scanned)

	// Poison an already-scanned line: an incremental refresh must not look at
	// it again, so the stale match at 0 surviving proves nothing was rescanned.
	lines[0] = "changed"
	lines = append(lines, "miss", "HIT again")
	ls.refresh(lines)
	assert.Equal(t, []int{0, 2, 4}, ls.matches)
	assert.Equal(t, 5, ls.scanned)

	// A new query rescans everything (and now sees the poisoned line as it is).
	ls.query = "miss"
	ls.refresh(lines)
	assert.Equal(t, []int{1, 3}, ls.matches)

	// A shorter buffer means another log file: rescan, never index past the end.
	ls.refresh([]string{"miss"})
	assert.Equal(t, []int{0}, ls.matches)

	ls.query = ""
	ls.refresh(lines)
	assert.Empty(t, ls.matches)
	assert.False(t, ls.active())
}

func TestLogSearch_NextWrapsBothWays(t *testing.T) {
	ls := logSearch{matches: []int{2, 5, 9}}
	step := func(line, dir int) int {
		idx, ok := ls.next(line, dir)
		require.True(t, ok)
		return idx
	}
	assert.Equal(t, 5, step(2, 1))
	assert.Equal(t, 2, step(9, 1), "down from the last match wraps to the first")
	assert.Equal(t, 2, step(0, 1))
	assert.Equal(t, 5, step(3, 1), "from a non-match line")

	assert.Equal(t, 2, step(5, -1))
	assert.Equal(t, 9, step(2, -1), "up from the first match wraps to the last")
	assert.Equal(t, 5, step(7, -1))

	_, ok := (&logSearch{}).next(3, 1)
	assert.False(t, ok, "no matches → nothing to step to")
}

func TestLogSearch_PositionAndNearest(t *testing.T) {
	ls := logSearch{matches: []int{2, 5, 9}}
	assert.Equal(t, 2, ls.position(5))
	assert.Equal(t, 0, ls.position(4))

	at := func(line int) int { idx, _ := ls.nearestAtOrBefore(line); return idx }
	assert.Equal(t, 5, at(5), "the cursor's own line counts")
	assert.Equal(t, 5, at(8), "otherwise the closest match above")
	assert.Equal(t, 9, at(1), "nothing above → wrap to the last match")
}

func TestHighlight_MarksEveryOccurrenceKeepingCase(t *testing.T) {
	out := highlight("Error: error in ERRORS", "error")
	assert.Equal(t, "Error: error in ERRORS", plain(out), "the visible text is untouched")
	assert.Equal(t, 3, strings.Count(out, styleMatch.Render("x")[:5]), "three matches marked")
	assert.Contains(t, out, styleMatch.Render("Error"))
	assert.Contains(t, out, styleMatch.Render("ERROR"))

	assert.Equal(t, "plain", highlight("plain", ""))
	assert.Equal(t, "plain", highlight("plain", "zzz"))
}

// strings.ToLower changes the byte length of a few code points (İ → i̇), which
// would make match offsets wrong. Such a line is left unhighlighted rather than
// sliced mid-rune.
func TestHighlight_LengthChangingLowercaseDoesNotCorruptText(t *testing.T) {
	line := "İstanbul error İ"
	require.NotEqual(t, len(line), len(strings.ToLower(line)), "the premise: lowering changes the length")
	assert.Equal(t, line, plain(highlight(line, "error")))
}

// A highlighted line must occupy exactly the rows and columns the layout
// budgeted for the plain one — rowsForLine measures the unhighlighted text.
func TestScrollBuffer_HighlightedLineKeepsItsShape(t *testing.T) {
	long := "error " + strings.Repeat("padding error ", 12)
	sb := &scrollBuffer{width: 30, height: 20, lines: []string{"cursor line", long}}
	before := sb.renderLine(1, long)

	sb.setQuery("error")
	after := sb.renderLine(1, long)
	assert.NotEqual(t, before, after, "matches are marked")
	assert.Equal(t, plain(before), plain(after), "same text, same wrap points")
	assert.Equal(t, sb.rowsForLine(1), strings.Count(after, "\n")+1)
	for _, row := range strings.Split(after, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(row), 30)
	}
}

// isQuit reports whether cmd is tea.Quit, without running it — the text input
// returns a cursor-blink command that sleeps for half a second when invoked.
func isQuit(cmd tea.Cmd) bool {
	return cmd != nil && reflect.ValueOf(cmd).Pointer() == reflect.ValueOf(tea.Quit).Pointer()
}

func typeString(m model, s string) model {
	for _, r := range s {
		m = pressKey(m, r)
	}
	return m
}

// searchModel is setupLogModel with a log that has "needle" on lines 3, 40, 77.
func searchModel() model {
	m := setupLogModel()
	m.logsC.sb.lines = nil
	for i := 0; i < 100; i++ {
		line := "hay"
		if i == 3 || i == 40 || i == 77 {
			line = "a Needle here"
		}
		m.logsC.sb.lines = append(m.logsC.sb.lines, line)
	}
	m.logsC.sb.gotoBottom()
	return m
}

func TestModel_SearchFromSidebarLandsInTheLogPane(t *testing.T) {
	m := searchModel()
	m.focus = focusSidebar
	m.activeTab = tabDetails

	m = pressKey(m, '/')
	assert.True(t, m.searching)
	assert.Equal(t, focusMain, m.focus)
	assert.Equal(t, tabLogs, m.activeTab)
	assert.Contains(t, plain(m.View()), "/", "the input shows in the footer")
}

func TestModel_SearchPreviewsWithoutMovingThenJumpsOnEnter(t *testing.T) {
	m := searchModel()
	m = pressKey(m, '/')
	m = typeString(m, "needle")

	assert.Equal(t, 99, m.logsC.sb.cursor, "typing must not move the cursor")
	assert.True(t, m.logsC.sb.followMode)
	assert.Contains(t, plain(m.View()), "3 matches", "the border previews the count while typing")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.False(t, m.searching)
	assert.Equal(t, 77, m.logsC.sb.cursor, "Enter lands on the nearest match above the cursor")
	assert.False(t, m.logsC.sb.followMode, "a new line must not yank the view off the match")
	assert.Contains(t, plain(m.View()), "3/3 matches")
}

func TestModel_SearchStepsWithNAndWraps(t *testing.T) {
	m := searchModel()
	m = typeString(pressKey(m, '/'), "needle")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.Equal(t, 77, m.logsC.sb.cursor)

	m = pressKey(m, 'N')
	assert.Equal(t, 40, m.logsC.sb.cursor)
	m = pressKey(m, 'N')
	assert.Equal(t, 3, m.logsC.sb.cursor)
	assert.Contains(t, plain(m.View()), "1/3 matches")
	m = pressKey(m, 'N')
	assert.Equal(t, 77, m.logsC.sb.cursor, "N from the first match wraps to the last")
	m = pressKey(m, 'n')
	assert.Equal(t, 3, m.logsC.sb.cursor, "n from the last match wraps to the first")

	// The match is actually on screen after each jump.
	assert.True(t, m.logsC.sb.lineVisible(m.logsC.sb.cursor))
	assert.Contains(t, plain(m.View()), "a Needle here")
}

// While the input has the keyboard every key is text: q must not quit, s must
// not start a service, ? must not open help.
func TestModel_SearchInputTrapsCommandKeys(t *testing.T) {
	m := searchModel()
	m = pressKey(m, '/')

	for _, r := range "qsx?t" {
		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(model)
		require.False(t, isQuit(cmd), "%q quit the app from inside the search input", r)
	}
	assert.Equal(t, "qsx?t", m.logsC.sb.search.query)
	assert.False(t, m.helpC.open)
	assert.False(t, m.pickerC.open)
	assert.True(t, m.searching)
}

func TestModel_SearchEscCancelsAndClears(t *testing.T) {
	m := searchModel()
	m = typeString(pressKey(m, '/'), "needle")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.False(t, m.searching)
	assert.False(t, m.logsC.sb.search.active(), "Esc in the input abandons the search")
	assert.Equal(t, 99, m.logsC.sb.cursor, "and never moved the cursor")

	// A confirmed search is cleared by Esc from the log pane — before Esc does
	// anything else.
	m = typeString(pressKey(m, '/'), "needle")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2, _ = m2.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.False(t, m.logsC.sb.search.active())
	assert.NotContains(t, plain(m.View()), "matches")
}

func TestModel_SearchWithNoMatchesSaysSo(t *testing.T) {
	m := searchModel()
	m = typeString(pressKey(m, '/'), "absent")
	assert.Contains(t, plain(m.View()), "no matches")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Contains(t, m.footerC.toast, "no matches for absent")
	assert.Equal(t, 99, m.logsC.sb.cursor)

	// n with nothing to step to toasts too, rather than doing nothing silently.
	m.footerC.toast = ""
	m = pressKey(m, 'n')
	assert.Contains(t, m.footerC.toast, "no matches")
}

// n / N are ordinary keys when no search is active — they must not toast or move.
func TestModel_NextPrevInertWithoutASearch(t *testing.T) {
	m := searchModel()
	m = pressKey(m, 'n')
	assert.Empty(t, m.footerC.toast)
	assert.Equal(t, 99, m.logsC.sb.cursor)
}

func TestModel_SearchReopensWithTheCurrentQuery(t *testing.T) {
	m := searchModel()
	m = typeString(pressKey(m, '/'), "needle")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = pressKey(m2.(model), '/')
	assert.Equal(t, "needle", m.searchC.Value(), "reopening edits the query instead of starting blank")
}

func TestModel_SearchViewFitsAtEveryWidth(t *testing.T) {
	m := searchModel()
	m = typeString(pressKey(m, '/'), strings.Repeat("a long query ", 8))
	for _, w := range []int{40, 61, 80, 100, 140} {
		m2, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 20})
		assertViewFits(t, m2.(model), w, 20)
	}
}

// isQuit is only worth trusting if it recognises the real thing.
func TestIsQuit_RecognisesTeaQuit(t *testing.T) {
	_, cmd := model{}.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.True(t, isQuit(cmd), "q outside any input quits")
	assert.False(t, isQuit(nil))
}
