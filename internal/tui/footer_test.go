package tui

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFooter_ToastAppearsAndClears(t *testing.T) {
	f := &footerBar{}
	f.showToast("Copied!")
	assert.Equal(t, "Copied!", f.toast)

	f.tick(1 * time.Second)
	assert.Equal(t, "Copied!", f.toast) // still visible

	f.tick(600 * time.Millisecond) // total 1.6s > 1.5s threshold
	assert.Equal(t, "", f.toast)   // cleared
}

func TestFooter_ToastNoOpWhenEmpty(t *testing.T) {
	f := &footerBar{}
	f.tick(5 * time.Second) // should not panic
	assert.Equal(t, "", f.toast)
}

func TestFooter_ToastResetOnNew(t *testing.T) {
	f := &footerBar{}
	f.showToast("first")
	f.tick(1 * time.Second)
	f.showToast("second") // resets timer
	f.tick(1 * time.Second)
	assert.Equal(t, "second", f.toast) // 1s < 1.5s, still showing
}

func allFooterContexts() map[string]footerCtx {
	return map[string]footerCtx{
		"sidebar":         {tab: tabLogs, focus: focusSidebar},
		"sidebar+service": {tab: tabLogs, focus: focusSidebar, onServiceRow: true},
		"sidebar+details": {tab: tabDetails, focus: focusSidebar, onServiceRow: true},
		"logs":            {tab: tabLogs, focus: focusMain},
		"logs+visual":     {tab: tabLogs, focus: focusMain, visual: true},
		"editing":         {editing: true},
		"confirming":      {confirming: true},
		"picking":         {picking: true},
		"helping":         {helping: true},
	}
}

// onlyWholeHints reports whether line is made of nothing but complete hints
// from pool and spacing — i.e. no hint was cut part-way through.
func onlyWholeHints(line string, pool []hint) bool {
	rest := plain(line)
	// Longest first, so "y/^C copy" is consumed before a shorter hint could
	// match inside it.
	sorted := append([]hint(nil), pool...)
	sort.Slice(sorted, func(i, j int) bool {
		return len(plain(renderHint(sorted[i].key, sorted[i].label))) > len(plain(renderHint(sorted[j].key, sorted[j].label)))
	})
	for _, h := range sorted {
		rest = strings.Replace(rest, plain(renderHint(h.key, h.label)), "", 1)
	}
	return strings.TrimSpace(rest) == ""
}

// The footer is one row and never wider than the terminal at any width, in any
// context — lipgloss's Width() pads short content but word-wraps long content
// into extra rows, and an extra row scrolls the header off a real terminal. And
// whatever it drops to fit, it drops whole: no hint is ever cut mid-label.
func TestFooter_OneRowOfWholeHintsAtEveryWidth(t *testing.T) {
	for name, ctx := range allFooterContexts() {
		pool := append(ctx.hints(), pinnedHints...)
		for w := 0; w <= 140; w++ {
			out := (&footerBar{}).render(ctx, w)
			require.Equal(t, 1, lipgloss.Height(out), "%s at width %d", name, w)
			require.LessOrEqual(t, lipgloss.Width(out), max(w, 1), "%s at width %d", name, w)
			require.True(t, onlyWholeHints(out, pool), "%s at width %d cut a hint: %q", name, w, plain(out))
		}
	}
}

func TestFooter_ShowsEverythingWhenThereIsRoom(t *testing.T) {
	ctx := footerCtx{tab: tabLogs, focus: focusSidebar, onServiceRow: true}
	out := plain((&footerBar{}).render(ctx, 160))
	for _, h := range append(ctx.hints(), pinnedHints...) {
		assert.Contains(t, out, plain(renderHint(h.key, h.label)))
	}
	assert.True(t, strings.HasSuffix(strings.TrimRight(out, " "), "quit"), "quit is pinned to the right edge: %q", out)
}

// As the terminal narrows, hints go lowest-priority first: quit and help
// outlast every context hint, and start outlasts the rest of the left side.
func TestFooter_DropsLowestPriorityFirst(t *testing.T) {
	ctx := footerCtx{tab: tabLogs, focus: focusSidebar, onServiceRow: true}
	has := func(w int, h hint) bool {
		return strings.Contains(plain((&footerBar{}).render(ctx, w)), plain(renderHint(h.key, h.label)))
	}
	lastSeen := func(h hint) int { // the narrowest width that still shows h
		for w := 0; w <= 160; w++ {
			if has(w, h) {
				return w
			}
		}
		return 999
	}
	quit, help := hint{"q", "quit", 0}, hint{"?", "help", 1}
	start, all := hint{"s", "start", 0}, hint{"S/X", "all", 7}

	assert.Less(t, lastSeen(quit), lastSeen(help), "quit outlasts help")
	assert.Less(t, lastSeen(help), lastSeen(start), "the pinned pair outlasts every context hint")
	assert.Less(t, lastSeen(start), lastSeen(all), "start outlasts the low-priority S/X hint")
}

func TestFitHints_KeepsDisplayOrder(t *testing.T) {
	hints := []hint{{"a", "one", 2}, {"b", "two", 0}, {"c", "three", 1}}
	full := plain(fitHints(hints, 100))
	assert.Less(t, strings.Index(full, "one"), strings.Index(full, "two"))
	assert.Less(t, strings.Index(full, "two"), strings.Index(full, "three"))

	// Too narrow for all three: "one" (pri 2) goes, the others keep their order.
	narrow := plain(fitHints(hints, lipgloss.Width(fitHints(hints, 100))-1))
	assert.NotContains(t, narrow, "one")
	assert.Less(t, strings.Index(narrow, "two"), strings.Index(narrow, "three"))

	assert.Equal(t, "", fitHints(hints, 2), "nothing fits → empty, not a cut hint")
}

func TestFooter_ModalContextsShowOnlyTheirOwnKeys(t *testing.T) {
	out := plain((&footerBar{}).render(footerCtx{picking: true}, 120))
	assert.Contains(t, out, "filter")
	assert.NotContains(t, out, "quit", "q closes the picker, it does not quit — do not advertise it")
	assert.NotContains(t, out, "start")
}

func TestFooter_LongToastNeverWrapsAtNarrowWidth(t *testing.T) {
	f := &footerBar{}
	f.showToastLong("error: " + strings.Repeat("x", 200))
	out := f.render(footerCtx{tab: tabLogs, focus: focusSidebar}, 20)
	assert.Equal(t, 1, lipgloss.Height(out))
	assert.LessOrEqual(t, lipgloss.Width(out), 20)
}
