package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupModel is a laid-out model over groupedServices with the sidebar focused.
func groupModel() model {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(model)
	m.focus = focusSidebar
	m.sidebarC.update(groupedServices(), nil)
	m.relayout()
	return m
}

func pressSpace(m model) model {
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	return m2.(model)
}

func TestSidebar_CollapseHidesAGroupsServicesAndKeepsItsHeader(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectGroupHeader("backend")

	require.True(t, sb.toggleCollapse())
	assert.Equal(t, []string{
		"header:backend", "gap", "header:frontend", "svc:assets", "svc:web", "gap", "header:(no group)", "svc:scratch",
	}, rowShape(sb))

	// And the cursor stays on the header the key was pressed on, so pressing
	// it again reopens the group.
	assert.Equal(t, 0, sb.selected)
	require.True(t, sb.toggleCollapse())
	assert.Equal(t, "svc:api", rowShape(sb)[1], "folded back open")
	assert.Equal(t, 0, sb.selected)
}

// The glyph is the only thing saying which way a group is folded, and shape
// alone has to carry it for --no-color.
func TestSidebar_CollapsedHeaderShowsItsGlyph(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)
	assert.Contains(t, plain(sb.render(31)), "▾")

	sb.selectGroupHeader("backend")
	require.True(t, sb.toggleCollapse())
	out := plain(sb.render(31))
	assert.Contains(t, out, "▸", "collapsed")
	assert.Contains(t, out, "▾", "frontend is still open")
}

// Space is for headers. On a service row it must do nothing — collapsing the
// group the cursor sits inside would move the highlight off the row the reader
// was looking at and onto a header they never aimed at.
func TestSidebar_CollapseIsANoOpOnAServiceRow(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectServiceByName("db")
	before := rowShape(sb)

	assert.False(t, sb.toggleCollapse())
	assert.Equal(t, before, rowShape(sb))
	assert.Equal(t, "db", sb.selectedService().Name)
}

// Collapse is keyed by group name, so a poll that rebuilds every row from
// scratch must not quietly unfold everything — nor move the cursor off the
// header it was left on. The dashboard polls every two seconds, so a cursor
// that only survives until the next one does not survive at all.
func TestSidebar_CollapseAndTheCursorBothSurviveAPoll(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectGroupHeader("backend")
	require.True(t, sb.toggleCollapse())
	folded := rowShape(sb)
	require.True(t, sb.onGroupHeader())

	sb.update(groupedServices(), nil)
	assert.Equal(t, folded, rowShape(sb), "still folded")
	require.True(t, sb.onGroupHeader(), "still on a header")
	assert.Equal(t, "backend", sb.rows[sb.selected].group, "still the same header")

	// And again, because the anchor has to be re-recorded each time, not just
	// set once by the toggle.
	sb.update(groupedServices(), nil)
	require.True(t, sb.onGroupHeader())
	assert.Equal(t, "backend", sb.rows[sb.selected].group)
}

// A header the reader walked onto without folding anything must hold the cursor
// across a poll too — the anchor is about where the cursor is, not about folds.
//
// Run over every group, the no-group bucket included. Its key is "", so an
// anchor that inferred "set" from a non-empty name silently excluded exactly
// one header — and it was the cursor drift this whole mechanism exists to stop.
func TestSidebar_CursorOnAnyHeaderSurvivesAPoll(t *testing.T) {
	for _, group := range []string{"backend", "frontend", ""} {
		t.Run("group="+groupLabel(group), func(t *testing.T) {
			sb := &sidebar{}
			sb.update(groupedServices(), nil)
			sb.selectGroupHeader(group)
			require.True(t, sb.onGroupHeader())

			sb.update(groupedServices(), nil)
			require.True(t, sb.onGroupHeader(), "still on a header")
			assert.Equal(t, group, sb.rows[sb.selected].group)
		})
	}
}

// Same for folding: fold any group, including the no-group bucket, and the
// cursor stays on its header across polls.
func TestSidebar_FoldedHeaderOfAnyGroupHoldsTheCursorAcrossPolls(t *testing.T) {
	for _, group := range []string{"backend", ""} {
		t.Run("group="+groupLabel(group), func(t *testing.T) {
			sb := &sidebar{}
			sb.update(groupedServices(), nil)
			sb.selectGroupHeader(group)
			require.True(t, sb.toggleCollapse())

			sb.update(groupedServices(), nil)
			require.True(t, sb.onGroupHeader())
			assert.Equal(t, group, sb.rows[sb.selected].group)
			assert.True(t, sb.collapsed[group], "and stays folded")
		})
	}
}

// A group that stops being drawn cannot hold the cursor, so it falls back to a
// service rather than parking somewhere that selects nothing.
func TestSidebar_HeaderAnchorFallsBackWhenTheGroupGoesAway(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectGroupHeader("frontend")
	require.True(t, sb.onGroupHeader())

	// Only backend services remain: one group, so no headers at all.
	sb.update([]ipc.ServiceInfo{
		{Name: "api", Group: "backend"}, {Name: "db", Group: "backend"},
	}, nil)
	assert.False(t, sb.onGroupHeader())
	require.NotNil(t, sb.selectedService())
	assert.Equal(t, "api", sb.selectedService().Name)
}

// The decision: an active query suspends every collapse. A reader who typed a
// query is asking to be shown what matches, and a match hidden inside a folded
// group would make the filter a liar.
func TestSidebar_QuerySuspendsCollapseAndRestoresIt(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectGroupHeader("backend")
	require.True(t, sb.toggleCollapse())
	require.NotContains(t, rowShape(sb), "svc:api")

	// api is in the folded group and matches — it has to show.
	sb.setQuery("api")
	assert.Equal(t, []string{"svc:api"}, rowShape(sb),
		"a match is never hidden behind a fold")

	// A query spanning groups unfolds them all — including the folded one.
	sb.setQuery("a") // api (backend), assets (frontend), scratch (ungrouped)
	assert.Equal(t, []string{
		"header:backend", "svc:api", "gap", "header:frontend", "svc:assets", "gap", "header:(no group)", "svc:scratch",
	}, rowShape(sb), "backend is folded, yet its match shows")

	// Space is refused while a query is in force: it could only change state
	// the reader cannot see — arming a fold that springs when the query clears,
	// or disarming one they deliberately set.
	sb.selectGroupHeader("frontend")
	require.True(t, sb.onGroupHeader())
	assert.False(t, sb.toggleCollapse(), "refused under a query")
	assert.False(t, sb.collapsed["frontend"], "and left no hidden state behind")

	// Clearing the query folds it back exactly as it was: suspended, not lost.
	sb.setQuery("")
	assert.True(t, sb.collapsed["backend"], "the fold was remembered throughout")
	assert.Equal(t, []string{
		"header:backend", "gap", "header:frontend", "svc:assets", "svc:web", "gap", "header:(no group)", "svc:scratch",
	}, rowShape(sb))
}

// Folding a group the cursor was inside moves the cursor to that group's
// header; it must not be left pointing past the end of a now-shorter list.
func TestSidebar_CollapseLeavesTheCursorOnTheHeader(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)
	sb.selectGroupHeader("") // the no-group bucket: last, so folding shortens the end
	require.Equal(t, len(sb.rows)-2, sb.selected)

	require.True(t, sb.toggleCollapse())
	assert.Less(t, sb.selected, len(sb.rows), "cursor is inside the list")
	assert.Equal(t, rowHeader, sb.rows[sb.selected].kind)
	assert.Equal(t, "", sb.rows[sb.selected].group, "the no-group bucket")
}

// Unfolding has to scroll to show what it opened. The cursor is on the header,
// which was already visible, so keeping the cursor visible moves nothing —
// unfolding a group whose header sat at the bottom of the window redrew an
// identical pane with the glyph flipped and not one service revealed.
func TestSidebar_UnfoldRevealsChildrenAtTheBottomOfTheWindow(t *testing.T) {
	var svcs []ipc.ServiceInfo
	for i := 0; i < 6; i++ {
		svcs = append(svcs, ipc.ServiceInfo{Name: fmt.Sprintf("a%02d", i), Group: "aaa"})
		svcs = append(svcs, ipc.ServiceInfo{Name: fmt.Sprintf("b%02d", i), Group: "bbb"})
	}
	sb := &sidebar{}
	sb.update(svcs, nil)
	sb.setRows(8)
	require.Len(t, sb.rows, 15) // 12 services + 2 headers + 1 gap

	// Fold bbb, whose header is then at the very bottom of the window.
	sb.selectGroupHeader("bbb")
	require.True(t, sb.toggleCollapse())
	require.True(t, sb.onGroupHeader())
	require.Equal(t, "bbb", sb.rows[sb.selected].group)
	_, last := sb.window()
	require.Equal(t, last-1, sb.selected, "the last row the pane can show")

	// Unfold it: at least one of its services must now be on screen.
	require.True(t, sb.toggleCollapse())
	first, last := sb.window()
	assert.True(t, sb.selected >= first && sb.selected < last, "the header stays visible")

	out := plain(sb.render(31))
	assert.Contains(t, out, "bbb", "its header")
	assert.Contains(t, out, "b00", "and the services it just opened")
}

// Scrolling to reveal must not overshoot: a header with room below it already
// shows its children, so the window should not move at all.
func TestSidebar_UnfoldDoesNotScrollWhenChildrenAlreadyFit(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20) // the whole list fits
	sb.selectGroupHeader("backend")
	require.True(t, sb.toggleCollapse())
	before := sb.top

	require.True(t, sb.toggleCollapse())
	assert.Equal(t, before, sb.top, "nothing to scroll when it all fits")
}

// --- through the key handler ---

func TestModel_SpaceTogglesTheGroupUnderTheCursor(t *testing.T) {
	m := groupModel()
	m.sidebarC.selectGroupHeader("backend")

	m = pressSpace(m)
	assert.NotContains(t, rowShape(&m.sidebarC), "svc:api", "folded")

	m = pressSpace(m)
	assert.Contains(t, rowShape(&m.sidebarC), "svc:api", "unfolded")
}

// Space on a service row falls through to nothing — in particular it must not
// be swallowed into starting, stopping or editing anything.
func TestModel_SpaceOnAServiceRowDoesNothing(t *testing.T) {
	m := groupModel()
	m.sidebarC.selectServiceByName("db")
	before := rowShape(&m.sidebarC)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = m2.(model)
	assert.Nil(t, cmd, "no daemon request")
	assert.Equal(t, before, rowShape(&m.sidebarC))
	assert.Equal(t, "db", m.sidebarC.selectedService().Name)
	assert.False(t, m.editC.open)
	assert.False(t, m.removeC.open)
}

// The pinned distinction: folding is not filtering. A query or a target
// excludes services from what you are working on; folding only gets a group out
// of the way on screen, and its header stays there counting its members. So
// S / X still act on a folded group's services — the opposite choice would mean
// tidying the view silently changed what the keys do.
func TestModel_StartAllIncludesFoldedGroups(t *testing.T) {
	m := groupModel()
	all := m.listedServiceNames()
	require.Len(t, all, 6)

	m.sidebarC.selectGroupHeader("backend")
	require.True(t, m.sidebarC.toggleCollapse())
	require.NotContains(t, rowShape(&m.sidebarC), "svc:api", "api is off screen")

	assert.Equal(t, all, m.listedServiceNames(),
		"folding changed the view, not the scope")

	// Whereas a query does change the scope.
	m.sidebarC.setQuery("web")
	assert.Equal(t, []string{"web"}, m.listedServiceNames())
}

// Space is text while the `/` input holds the keyboard — a query can contain
// one, and it must not fold a group behind the input.
func TestModel_SpaceIsTextInsideTheFilterInput(t *testing.T) {
	m := groupModel()
	m = pressKey(m, '/')
	require.True(t, m.searching)

	m = typeString(m, "a b")
	assert.Equal(t, "a b", m.sidebarC.filterQuery, "the space landed in the query")
	assert.True(t, m.searching)
}

// On a header Enter has no service to show, so it folds instead. Before this it
// toggled to a DETAILS pane with nothing in it — and in the narrow layout
// opened that empty pane full-screen.
func TestModel_EnterOnAGroupHeaderFoldsItInsteadOfOpeningAnEmptyPane(t *testing.T) {
	m := groupModel()
	m.sidebarC.selectGroupHeader("backend")
	require.Equal(t, tabLogs, m.activeTab)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabLogs, m.activeTab, "no switch to an empty DETAILS")
	assert.NotContains(t, rowShape(&m.sidebarC), "svc:api", "it folded")

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Contains(t, rowShape(&m.sidebarC), "svc:api", "and unfolds")
	assert.Equal(t, tabLogs, m.activeTab)
}

// On a service row Enter keeps meaning what it always did.
func TestModel_EnterOnAServiceRowStillTogglesDetails(t *testing.T) {
	m := groupModel()
	m.sidebarC.selectServiceByName("api")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabDetails, m.activeTab)
}

// The footer must not advertise keys that do nothing on the row the cursor is
// on: a header has no service to start, stop or restart.
func TestFooter_GroupHeaderOffersFoldNotStartStop(t *testing.T) {
	f := &footerBar{}
	svcRow := plain(f.render(footerCtx{focus: focusSidebar, tab: tabLogs}, 120))
	assert.Contains(t, svcRow, "start")
	assert.NotContains(t, svcRow, "fold")

	header := plain(f.render(footerCtx{focus: focusSidebar, tab: tabLogs, onGroupHeader: true}, 120))
	assert.Contains(t, header, "fold")
	assert.NotContains(t, header, "start", "nothing here to start")
	assert.NotContains(t, header, "restart")
}

// Refusing has to say so, or Space just looks broken.
func TestModel_SpaceUnderAQuerySaysWhyItRefused(t *testing.T) {
	m := groupModel()
	m.sidebarC.setQuery("a")
	m.sidebarC.selectGroupHeader("backend")
	require.True(t, m.sidebarC.onGroupHeader())

	m = pressSpace(m)
	assert.Contains(t, m.footerC.toast, "suspended")
	assert.Contains(t, m.footerC.toast, "/a", "and names the query holding it")
	assert.False(t, m.sidebarC.collapsed["backend"], "no state changed")
}

// `/` then Esc from a group header has to come back to that header.
//
// The query deliberately dissolves the header on the way: `api` narrows to one
// service, so the list drops to a single group and stops having headers at all.
// That is what makes the open-time snapshot load-bearing — the running anchor
// gets overwritten with a service once the header it named stops being drawn,
// so only the position saved when `/` opened can bring the cursor back. A query
// that leaves the header standing (`we` does) would pass either way.
func TestModel_EscFromAGroupHeaderReturnsToThatHeader(t *testing.T) {
	m := groupModel()
	m.sidebarC.selectGroupHeader("frontend")
	require.True(t, m.sidebarC.onGroupHeader())

	m = typeString(pressKey(m, '/'), "api")
	require.Equal(t, []string{"svc:api"}, rowShape(&m.sidebarC),
		"one group left, so no headers survive the query")
	require.False(t, m.sidebarC.onGroupHeader())

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)

	require.True(t, m.sidebarC.onGroupHeader(), "back on a header, not on some service")
	assert.Equal(t, "frontend", m.sidebarC.rows[m.sidebarC.selected].group)
}

// The same from the no-group bucket's header, whose key is "" — the one the
// anchor used to treat as "nowhere".
func TestModel_EscFromTheNoGroupHeaderReturnsToIt(t *testing.T) {
	m := groupModel()
	m.sidebarC.selectGroupHeader("")
	require.True(t, m.sidebarC.onGroupHeader())

	m = typeString(pressKey(m, '/'), "api") // dissolves every header
	require.False(t, m.sidebarC.onGroupHeader())

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	require.True(t, m.sidebarC.onGroupHeader())
	assert.Equal(t, "", m.sidebarC.rows[m.sidebarC.selected].group)
}

// Under a query, folding is suspended — so Enter must say so rather than being
// silently inert. It reached this row by giving up its LOGS ⇄ DETAILS job, so
// doing nothing at all would look broken.
func TestModel_EnterOnAHeaderUnderAQuerySaysWhyItRefused(t *testing.T) {
	m := groupModel()
	m.sidebarC.setQuery("a")
	m.sidebarC.selectGroupHeader("backend")
	require.True(t, m.sidebarC.onGroupHeader())
	require.Equal(t, tabLogs, m.activeTab)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Contains(t, m.footerC.toast, "suspended")
	assert.Equal(t, tabLogs, m.activeTab, "and no switch to an empty DETAILS")
	assert.False(t, m.sidebarC.collapsed["backend"])
}

// One action, one hint. `↵` folds too, but a second slot saying so costs a real
// hint at the widths where they are scarcest — the header row must not end up
// with fewer useful keys on screen than a service row.
func TestFooter_HeaderNamesTheFoldOnceAndKeepsItsOtherHints(t *testing.T) {
	f := &footerBar{}
	ctx := footerCtx{focus: focusSidebar, tab: tabLogs, onGroupHeader: true}

	wide := plain(f.render(ctx, 120))
	assert.Equal(t, 1, strings.Count(wide, "fold"), "named once, not twice")
	assert.Contains(t, wide, "Space")

	// At 70 columns the duplicate used to push `t target` off the row.
	at70 := plain(f.render(ctx, 70))
	assert.Contains(t, at70, "fold")
	assert.Contains(t, at70, "target")
}

// The footer must not promise a fold the query will refuse.
func TestFooter_HeaderUnderAQueryPromisesNeitherFoldNorStart(t *testing.T) {
	f := &footerBar{}

	open := plain(f.render(footerCtx{focus: focusSidebar, tab: tabLogs, onGroupHeader: true}, 120))
	assert.Contains(t, open, "fold")

	filtered := plain(f.render(footerCtx{
		focus: focusSidebar, tab: tabLogs, onGroupHeader: true, hasFilter: true,
	}, 120))
	assert.NotContains(t, filtered, "fold", "folding is suspended under a query")
	assert.NotContains(t, filtered, "start", "and there is no service here either")
	assert.Contains(t, filtered, "clear", "what is left is still offered")
	assert.Contains(t, filtered, "filter")
}

// A group genuinely named "ungrouped" is its own group: it sorts by its name,
// folds independently of the services that have no group at all, and — the part
// that matters on screen — is labelled differently from them. Separating the
// keys without separating the labels left two sections reading the same thing
// and folding independently, with nothing to tell them apart.
func TestSidebar_ARealGroupNamedUngroupedIsDistinctFromTheNoGroupBucket(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "api", Group: "ungrouped"},
		{Name: "zed", Group: "zoo"},
		{Name: "scratch"}, // genuinely no group
	}, nil)
	sb.setRows(20)

	// The real group sorts by its name; the bucket stays last and is labelled
	// as the absence of a group rather than as one called "ungrouped".
	assert.Equal(t, []string{
		"header:ungrouped", "svc:api", "gap", "header:zoo", "svc:zed", "gap", "header:(no group)", "svc:scratch",
	}, rowShape(sb))

	// And the two headers are distinguishable on screen, not just internally.
	out := plain(sb.render(31))
	assert.Contains(t, out, "ungrouped")
	assert.Contains(t, out, "(no group)")

	// Folding the real one leaves the bucket alone.
	sb.selectGroupHeader("ungrouped")
	require.True(t, sb.toggleCollapse())
	assert.Equal(t, []string{
		"header:ungrouped", "gap", "header:zoo", "svc:zed", "gap", "header:(no group)", "svc:scratch",
	}, rowShape(sb), "only the named group folded")
}

// The log pane follows the cursor, and folding moves the cursor onto a header
// that selects no service. The pane must not keep showing the log of a service
// that is no longer on screen.
func TestModel_CollapseLeavesNoServiceSelected(t *testing.T) {
	m := groupModel()
	m.sidebarC.selectServiceByName("api")
	require.Equal(t, "api", m.sidebarC.selectedService().Name)

	m.sidebarC.selectGroupHeader("backend")
	m = pressSpace(m)
	assert.Nil(t, m.sidebarC.selectedService(), "the cursor is on the folded header")
}

// Unfolding must scroll the least it can, and a group's body is its services —
// not "everything up to the next header", which now includes the blank line
// between them. Aiming at that gap scrolled one row too far and pushed a real
// row off the top to reveal a line with nothing on it.
func TestSidebar_UnfoldDoesNotScrollToTheTrailingGap(t *testing.T) {
	var svcs []ipc.ServiceInfo
	for _, g := range []string{"aaa", "bbb", "ccc"} {
		for i := 1; i <= 2; i++ {
			svcs = append(svcs, ipc.ServiceInfo{Name: fmt.Sprintf("%s%d", g[:1], i), Group: g})
		}
	}
	sb := &sidebar{}
	sb.update(svcs, nil)
	sb.setRows(5)

	sb.selectGroupHeader("bbb")
	require.True(t, sb.toggleCollapse())
	sb.top = 0
	require.True(t, sb.toggleCollapse()) // unfold

	first, last := sb.window()
	shown := rowShape(sb)[first:last]

	// bbb's header and both its services are on screen...
	assert.Contains(t, shown, "header:bbb")
	assert.Contains(t, shown, "svc:b1")
	assert.Contains(t, shown, "svc:b2")
	// ...and the window is not padded out with blank rows to get there.
	gaps := 0
	for _, r := range shown {
		if r == "gap" {
			gaps++
		}
	}
	assert.LessOrEqual(t, gaps, 1, "at most the one gap above bbb: %v", shown)
}

// A header with nothing under it has nothing to reveal, so unfolding one must
// not move the window at all.
func TestSidebar_RevealUnderIsANoOpForAnEmptyBody(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(4)
	sb.selectGroupHeader("backend")
	sb.top = 2
	before := sb.top

	// Point it at the last row, which is a service with no body under it.
	sb.revealUnder(len(sb.rows) - 1)
	assert.Equal(t, before, sb.top)
}
