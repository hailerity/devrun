package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestSidebar_ClearingTheQueryRestoresTheList(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)
	sb.setQuery("api")
	require.Len(t, sb.services, 1)

	sb.setQuery("")
	assert.Equal(t, []string{"Worker", "api", "web", "webhook"}, svcNames(sb))
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
	assert.Contains(t, plain(sb.render(28)), "no service matches /absent")
}

// With no query the empty list can only be the target's doing, and says so.
func TestSidebar_EmptyStateBlamesTheTargetWhenThereIsNoQuery(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), []sidebarTarget{{name: "empty", members: []string{"gone"}}})
	sb.setFilter("empty")
	assert.Contains(t, plain(sb.render(28)), "no services in target")
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

	title := plain(sb.frame(true).title)
	assert.Contains(t, title, "SERVICES")
	assert.Contains(t, title, "front")
	assert.Contains(t, title, "/hook", "the query carries its slash, so it does not read as a target")
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
	require.Equal(t, "Worker", m.sidebarC.selectedService().Name)

	m = typeString(pressKey(m, '/'), "api")
	require.Equal(t, "api", m.sidebarC.selectedService().Name)
	assert.Contains(t, m.logsC.filePath, "api.log")
}

// Nothing to filter: `/` must not open an input over an empty list, where every
// keystroke would be a no-op with no way to tell.
func TestModel_SlashIsInertWithNoServices(t *testing.T) {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(model)
	m.sidebarC.update(nil, nil)

	m = pressKey(m, '/')
	assert.False(t, m.searching)
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
