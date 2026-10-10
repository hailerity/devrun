package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hailerity/devrun/internal/config"
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
		"header:backend",
		"header:frontend", "svc:assets", "svc:web",
		"header:ungrouped", "svc:scratch",
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
// scratch must not quietly unfold everything.
func TestSidebar_CollapseSurvivesAPoll(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectGroupHeader("backend")
	require.True(t, sb.toggleCollapse())
	folded := rowShape(sb)

	sb.update(groupedServices(), nil)
	assert.Equal(t, folded, rowShape(sb))
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
		"header:backend", "svc:api",
		"header:frontend", "svc:assets",
		"header:ungrouped", "svc:scratch",
	}, rowShape(sb), "backend is folded, yet its match shows")

	// Clearing the query folds it back exactly as it was: suspended, not lost.
	sb.setQuery("")
	assert.True(t, sb.collapsed["backend"], "the fold was remembered throughout")
	assert.Equal(t, []string{
		"header:backend",
		"header:frontend", "svc:assets", "svc:web",
		"header:ungrouped", "svc:scratch",
	}, rowShape(sb))
}

// Folding a group the cursor was inside moves the cursor to that group's
// header; it must not be left pointing past the end of a now-shorter list.
func TestSidebar_CollapseLeavesTheCursorOnTheHeader(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)
	sb.selectGroupHeader("ungrouped") // the last group, so folding shortens the end
	require.Equal(t, len(sb.rows)-2, sb.selected)

	require.True(t, sb.toggleCollapse())
	assert.Less(t, sb.selected, len(sb.rows), "cursor is inside the list")
	assert.Equal(t, rowHeader, sb.rows[sb.selected].kind)
	assert.Equal(t, "ungrouped", sb.rows[sb.selected].group)
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
