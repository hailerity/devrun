package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func filterServices() []ipc.ServiceInfo {
	return []ipc.ServiceInfo{
		{Name: "api", State: "running"},
		{Name: "web", State: "running"},
		{Name: "webhook", State: "stopped"},
		{Name: "Worker", State: "crashed"},
	}
}

// filterModel is a laid-out model with four services and the sidebar focused —
// the state `/` filters from.
func filterModel() model {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(model)
	m.focus = focusSidebar
	m.sidebarC.update(filterServices(), targetRows())
	m.relayout()
	return m
}

// --- the sidebar's own filtering ---

func TestSidebar_QueryMatchesNameSubstringIgnoringCase(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)

	sb.setQuery("web")
	assert.Equal(t, []string{"web", "webhook"}, svcNames(sb))

	sb.setQuery("WEB")
	assert.Equal(t, []string{"web", "webhook"}, svcNames(sb), "the query ignores case")

	sb.setQuery("work")
	assert.Equal(t, []string{"Worker"}, svcNames(sb), "and so does the name")

	sb.setQuery("hook")
	assert.Equal(t, []string{"webhook"}, svcNames(sb), "matching is not anchored to the start")

	sb.setQuery("wb")
	assert.Empty(t, svcNames(sb), "matching is substring, not fuzzy: wb must not find webhook")
}

// The order has to agree with the query: `/work` finds `Worker`, so the list
// must file it where a reader hunting for a "w" would look. Byte order would
// put every capitalised name ahead of every lowercase one.
func TestSidebar_OrderIsCaseInsensitive(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "Worker"}, {Name: "api"}, {Name: "Zebra"}, {Name: "beta"},
	}, nil)
	assert.Equal(t, []string{"api", "beta", "Worker", "Zebra"}, svcNames(sb))

	// Names differing only in case still come out in a stable, total order.
	sb.update([]ipc.ServiceInfo{{Name: "web"}, {Name: "WEB"}, {Name: "Web"}}, nil)
	assert.Equal(t, []string{"WEB", "Web", "web"}, svcNames(sb))
}

func TestSidebar_ClearingTheQueryRestoresTheList(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)
	sb.setQuery("api")
	require.Len(t, sb.services, 1)

	sb.setQuery("")
	assert.Equal(t, []string{"api", "web", "webhook", "Worker"}, svcNames(sb))
}

// The two narrowings compose rather than replace each other: a query searches
// inside the filtering target, so a service the target excludes stays excluded.
func TestSidebar_QueryNarrowsWithinTheTargetFilter(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), []sidebarTarget{
		{name: "front", members: []string{"web", "webhook"}},
	})
	sb.setFilter("front")
	require.Equal(t, []string{"web", "webhook"}, svcNames(sb))

	sb.setQuery("hook")
	assert.Equal(t, []string{"webhook"}, svcNames(sb))

	// "api" matches a service, but not one this target holds.
	sb.setQuery("api")
	assert.Empty(t, svcNames(sb), "the query must not reach outside the target")

	sb.setFilter("")
	assert.Equal(t, []string{"api"}, svcNames(sb), "dropping the target leaves the query in force")
}

func TestSidebar_QueryKeepsTheHighlightAndSurvivesAPoll(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)
	sb.setQuery("web")
	sb.selected = 1
	require.Equal(t, "webhook", sb.selectedService().Name)

	// A poll re-sorts and re-filters from scratch; neither the query nor the
	// highlight may be lost in it.
	sb.update(filterServices(), nil)
	assert.Equal(t, "web", sb.filterQuery)
	assert.Equal(t, []string{"web", "webhook"}, svcNames(sb))
	assert.Equal(t, "webhook", sb.selectedService().Name)
}

// A query that matches nothing is still the query the user typed: it is kept and
// reported, not silently dropped on the next poll.
func TestSidebar_QueryMatchingNothingIsKeptAndNamed(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)
	sb.setQuery("absent")
	sb.update(filterServices(), nil)

	assert.Equal(t, "absent", sb.filterQuery)
	assert.Empty(t, sb.services)
	assert.Nil(t, sb.selectedService())
	assert.Contains(t, plain(sb.render(28)), "no match for /absent")
}

// With no query the empty list can only be the target's doing, and says so.
func TestSidebar_EmptyStateBlamesTheTargetWhenThereIsNoQuery(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), []sidebarTarget{{name: "empty", members: []string{"gone"}}})
	sb.setFilter("empty")
	assert.Contains(t, plain(sb.render(28)), "no services in target")
}

// A target that has already emptied the list is still the cause once a query is
// typed on top of it: deleting the query would not bring back a single row, so
// blaming it would send the reader to fix the wrong thing.
func TestSidebar_EmptyStateKeepsBlamingTheTargetUnderAQuery(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), []sidebarTarget{{name: "empty", members: []string{"gone"}}})
	sb.setFilter("empty")
	sb.setQuery("web")

	out := plain(sb.render(28))
	assert.Contains(t, out, "no services in target")
	assert.NotContains(t, out, "no match for", "the query is not what emptied this list")
}

// A long target name comes from user config and can overrun on its own, with no
// query involved to trigger the drop-the-target branch.
func TestSidebar_FrameFitsALoneLongTarget(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), []sidebarTarget{
		{name: "my-long-frontend-target", members: []string{"web"}},
	})
	sb.setFilter("my-long-frontend-target")

	title := plain(sb.frame(true, sidebarMinW).title)
	assert.LessOrEqual(t, lipgloss.Width(title), titleRoom(sidebarMinW))
	assert.Contains(t, title, "…", "shortened here, not cut by the frame")
}

// The frame names both narrowings, so the reason a service is missing from the
// list is always on screen.
func TestSidebar_FrameNamesTargetAndQuery(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), []sidebarTarget{
		{name: "front", members: []string{"web", "webhook"}},
	})
	sb.setFilter("front")
	sb.setQuery("hook")

	title := plain(sb.frame(true, 40).title)
	assert.Contains(t, title, "SERVICES")
	assert.Contains(t, title, "front")
	assert.Contains(t, title, "/hook", "the query carries its slash, so it does not read as a target")
}

// The sidebar is 31 columns whenever the longest service name is short, and the
// frame cuts an over-long title without an ellipsis. Rather than print half a
// word, the title drops the target — which the picker can show again — and
// keeps the query, shortening it if it still does not fit.
func TestSidebar_FrameFitsTheTitleAtMinimumWidth(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), []sidebarTarget{
		{name: "frontend", members: []string{"web", "webhook"}},
	})
	sb.setFilter("frontend")
	sb.setQuery("nomatchhere")

	title := plain(sb.frame(true, sidebarMinW).title)
	assert.LessOrEqual(t, lipgloss.Width(title), titleRoom(sidebarMinW), "the title must fit the pane's edge")
	assert.NotContains(t, title, "frontend", "the target gives way to the query")
	assert.Contains(t, title, "/", "the query is still named")

	// A query long enough that dropping the target is not enough on its own:
	// what is left has to be shortened too, with the ellipsis that says so.
	sb.setQuery("a-really-long-query-nobody-would-type")
	long := plain(sb.frame(true, sidebarMinW).title)
	assert.LessOrEqual(t, lipgloss.Width(long), titleRoom(sidebarMinW), "a long query must still fit")
	assert.Contains(t, long, "…", "and be visibly shortened rather than silently cut")

	// Given room, both are shown in full.
	sb.setQuery("nomatchhere")
	wide := plain(sb.frame(true, 60).title)
	assert.Contains(t, wide, "frontend")
	assert.Contains(t, wide, "/nomatchhere")
}

// Same budget, same reason: the empty state has to stay a whole sentence.
func TestSidebar_EmptyStateFitsAtMinimumWidth(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)
	sb.setQuery("nomatchhereatall")

	out := plain(sb.render(sidebarMinW - 2)) // a minimum-width pane's content area
	assert.LessOrEqual(t, lipgloss.Width(out), sidebarMinW-2)
	assert.Contains(t, out, "no match for /")
}

// --- the `/` input, driving the sidebar ---

func TestModel_SlashFromSidebarFiltersTheList(t *testing.T) {
	m := filterModel()
	m = pressKey(m, '/')
	require.True(t, m.searching)
	require.Equal(t, scopeServices, m.searchScope)
	assert.Equal(t, focusSidebar, m.focus, "filtering must not move focus off the list it narrows")

	// The list is the preview: it narrows on every keystroke.
	m = typeString(m, "web")
	assert.Equal(t, []string{"web", "webhook"}, svcNames(&m.sidebarC))
	assert.Contains(t, plain(m.View()), "/web")
}

func TestModel_ServiceFilterEnterKeepsTheQuery(t *testing.T) {
	m := filterModel()
	m = typeString(pressKey(m, '/'), "web")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.False(t, m.searching, "Enter hands the keyboard back")
	assert.Equal(t, "web", m.sidebarC.filterQuery, "and leaves the list narrowed")
	assert.Equal(t, []string{"web", "webhook"}, svcNames(&m.sidebarC))
}

func TestModel_ServiceFilterEscCancelsAndRestores(t *testing.T) {
	m := filterModel()
	m = typeString(pressKey(m, '/'), "web")
	require.Len(t, m.sidebarC.services, 2)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.False(t, m.searching)
	assert.Empty(t, m.sidebarC.filterQuery)
	assert.Len(t, m.sidebarC.services, 4, "Esc in the input puts the whole list back")
}

// `/` prefills with the live query so a filter can be amended, and the footer
// calls Esc "cancel" — so cancelling an amendment must put the old query back,
// not throw the filter away.
func TestModel_ServiceFilterEscRestoresTheQueryItOpenedOn(t *testing.T) {
	m := filterModel()
	m = typeString(pressKey(m, '/'), "web")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.Equal(t, "web", m.sidebarC.filterQuery)

	// Reopen to amend, type more, then back out.
	m = typeString(pressKey(m, '/'), "hook")
	require.Equal(t, "webhook", m.sidebarC.filterQuery, "the input opened prefilled")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)

	assert.Equal(t, "web", m.sidebarC.filterQuery, "cancel restores, it does not clear")
	assert.Equal(t, []string{"web", "webhook"}, svcNames(&m.sidebarC))
}

// Typing a query that matches nothing leaves no row to read a cursor anchor
// from. Backing out must still return to the service the cursor was on — the
// `/` input promises that abandoning it costs nothing.
func TestModel_ServiceFilterEscReturnsToTheOriginalServiceAfterNoMatch(t *testing.T) {
	m := filterModel()
	m.sidebarC.selectServiceByName("web")
	require.Equal(t, "web", m.sidebarC.selectedService().Name)

	m = typeString(pressKey(m, '/'), "zz")
	require.Empty(t, m.sidebarC.services, "nothing matches, so there is no row to anchor to")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.Equal(t, "web", m.sidebarC.selectedService().Name)
	assert.Contains(t, m.logsC.filePath, "web.log", "and the log pane came back with it")
}

// --- S / X act on what is listed ---

// `S` / `X` are documented as "everything listed", and a query is part of what
// is listed. Starting 30 services because 2 are on screen is the failure this
// guards.
func TestModel_StartStopAllRespectsTheNameQuery(t *testing.T) {
	m := filterModel()
	m.socketPath = filepath.Join(t.TempDir(), "nonexistent.sock")

	assert.Equal(t, []string{"api", "web", "webhook", "Worker"}, m.listedServiceNames(),
		"no query: everything is listed")

	m = typeString(pressKey(m, '/'), "web")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.Equal(t, []string{"web", "webhook"}, svcNames(&m.sidebarC))
	assert.Equal(t, []string{"web", "webhook"}, m.listedServiceNames(),
		"the batch must see the narrowed list, not allServices")

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	require.NotNil(t, cmd)
	toast := m2.(model).footerC.toast
	assert.Contains(t, toast, "/web", "the toast names the scope it acted on")
	assert.Contains(t, toast, "2 listed")

	// The daemon is unreachable, so every attempted member fails — and the
	// error names exactly the members that were attempted.
	if err, ok := cmd().(daemonErrMsg); assert.True(t, ok) {
		msg := err.err.Error()
		assert.Contains(t, msg, "web")
		assert.Contains(t, msg, "webhook")
		assert.NotContains(t, msg, "api", "a service the query excluded must not be touched")
		assert.NotContains(t, msg, "Worker")
	}
}

// A target and a query together: the daemon has no name for the intersection,
// so the target request cannot carry it and the by-name batch must be used.
func TestModel_StartAllWithTargetAndQueryActsOnTheIntersection(t *testing.T) {
	m := filterModel()
	m.socketPath = filepath.Join(t.TempDir(), "nonexistent.sock")
	m.sidebarC.setFilter("t2") // api, db — only api is in this model
	m.sidebarC.setQuery("api")
	require.Equal(t, []string{"api"}, m.listedServiceNames())

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	require.NotNil(t, cmd)
	assert.Contains(t, m2.(model).footerC.toast, "/api")
	if err, ok := cmd().(daemonErrMsg); assert.True(t, ok) {
		assert.Contains(t, err.err.Error(), "start all:",
			"a query forces the by-name batch, not one target-start for the whole target")
	}
}

// A confirmed filter is cleared by Esc from the sidebar — the footer offers
// exactly that, and it is the only way back to the full list.
func TestModel_EscFromTheSidebarClearsAConfirmedFilter(t *testing.T) {
	m := filterModel()
	m = typeString(pressKey(m, '/'), "web")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.Equal(t, "web", m.sidebarC.filterQuery)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.Empty(t, m.sidebarC.filterQuery)
	assert.Len(t, m.sidebarC.services, 4)
}

// Esc leaves the target filter alone: that is a mode chosen from a picker, and
// undoing it as a side effect of clearing a query would be a surprise.
func TestModel_EscClearsTheQueryButKeepsTheTarget(t *testing.T) {
	m := filterModel()
	m.sidebarC.setFilter("t2") // api, db
	m = typeString(pressKey(m, '/'), "api")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2, _ = m2.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)

	assert.Empty(t, m.sidebarC.filterQuery)
	assert.Equal(t, "t2", m.sidebarC.filterTarget)
	assert.Equal(t, []string{"api"}, svcNames(&m.sidebarC), "only api is both configured and in t2")
}

// While the input has the keyboard every key is text: q must not quit, s must
// not start a service, t must not open the target picker.
func TestModel_ServiceFilterTrapsCommandKeys(t *testing.T) {
	m := filterModel()
	m = pressKey(m, '/')

	for _, r := range "qsx?t" {
		m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(model)
		require.False(t, isQuit(cmd), "%q quit the app from inside the filter input", r)
	}
	assert.Equal(t, "qsx?t", m.sidebarC.filterQuery)
	assert.False(t, m.helpC.open)
	assert.False(t, m.pickerC.open)
	assert.True(t, m.searching)
}

// Filtering can hand the highlight to another service; the log pane behind the
// input has to follow it.
func TestModel_ServiceFilterMovesTheLogPaneWithTheHighlight(t *testing.T) {
	m := filterModel()
	m.sidebarC.selectServiceByName("Worker")
	require.Equal(t, "Worker", m.sidebarC.selectedService().Name)

	m = typeString(pressKey(m, '/'), "api")
	require.Equal(t, "api", m.sidebarC.selectedService().Name)
	assert.Contains(t, m.logsC.filePath, "api.log")
}

// Nothing to filter: `/` must not open an input over an empty list, where every
// keystroke would be a no-op with no way to tell. A query only subtracts, so
// this holds for a target that has emptied the list just as much as for an
// empty config.
func TestModel_SlashIsInertWithNothingListed(t *testing.T) {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(model)

	m.sidebarC.update(nil, nil) // nothing configured
	assert.False(t, pressKey(m, '/').searching)

	// Configured, but the target filter leaves no rows.
	m.sidebarC.update(filterServices(), []sidebarTarget{{name: "empty", members: []string{"gone"}}})
	m.sidebarC.setFilter("empty")
	require.Empty(t, m.sidebarC.services)
	require.NotEmpty(t, m.sidebarC.allServices)
	assert.False(t, pressKey(m, '/').searching, "a query can only narrow zero rows to zero rows")
}

// --- the footer ---

// 100 columns is an ordinary terminal, and the hint for a key nobody can guess
// has to survive at one: a `/` that only advertises itself on a wide screen is
// half a feature.
func TestFooter_SidebarOffersFilterThenClear(t *testing.T) {
	f := &footerBar{}
	plainRender := func(c footerCtx) string { return plain(f.render(c, 100)) }

	out := plainRender(footerCtx{focus: focusSidebar, tab: tabLogs})
	assert.Contains(t, out, "filter")
	assert.NotContains(t, out, "clear", "nothing to clear until a query is active")

	out = plainRender(footerCtx{focus: focusSidebar, tab: tabLogs, hasFilter: true})
	assert.Contains(t, out, "clear", "with the list narrowed, the way back is the hint that matters")
}

// Enter is named for what it does to the list being narrowed: it jumps the log
// to a match, but the service list has already narrowed while typing.
func TestFooter_SearchInputNamesEnterPerScope(t *testing.T) {
	f := &footerBar{}
	assert.Contains(t, plain(f.render(footerCtx{searching: true, searchScope: scopeLog, searchInput: "/x"}, 100)), "find")
	assert.Contains(t, plain(f.render(footerCtx{searching: true, searchScope: scopeServices, searchInput: "/x"}, 100)), "keep")
}

// --- the help overlay ---

// / does two jobs, so it is listed under both the list it filters and the log it
// searches — each worded for that heading.
func TestHelp_ListsSlashUnderServicesAndLogs(t *testing.T) {
	out := plain(helpPanel{open: true}.view())
	assert.Contains(t, out, "filter the list by name")
	assert.Contains(t, out, "search the log")
}

// The overlay has no scrolling and overlay() hard-clips it to height-2, so its
// row count is a fixed budget: the first thing lost is the bottom border, then
// the "Esc or ? to close" line — the one instruction a reader needs. 26 rows is
// the floor for two columns with these four groups, and keeps the overlay whole
// down to a 28-row terminal.
//
// A ratchet, not a target: adding a key to the taller column breaks this, and
// the fix is to rebalance helpGroups rather than raise the number. Getting
// under 26 needs scrolling or a third column, which is its own change.
func TestHelp_ViewFitsASmallTerminal(t *testing.T) {
	const budget = 26
	got := len(strings.Split(helpPanel{open: true}.view(), "\n"))
	assert.LessOrEqualf(t, got, budget,
		"the ? overlay is %d rows, over its %d-row budget — rebalance helpGroups' columns "+
			"rather than letting overlay() clip the bottom border off", got, budget)
}
