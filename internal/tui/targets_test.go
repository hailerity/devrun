package tui

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plain strips SGR colour codes so assertions can match the visible text even
// when lipgloss styles a header character-by-character.
func plain(s string) string { return sgrRe.ReplaceAllString(s, "") }

func targetRows() []sidebarTarget {
	return []sidebarTarget{
		{name: ""},
		{name: "t1", members: []string{"web"}, active: true},
		{name: "t2", members: []string{"api", "db"}},
	}
}

func svcs(names ...string) []ipc.ServiceInfo {
	out := make([]ipc.ServiceInfo, len(names))
	for i, n := range names {
		out[i] = ipc.ServiceInfo{Name: n, State: "stopped"}
	}
	return out
}

func TestSidebar_NoConfigContext_HidesBlockAndWrapsWithinServices(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web", "zoo"), nil)

	assert.False(t, sb.showTargets())
	assert.Nil(t, sb.selectedTarget())

	sb.selected = 0
	sb.moveUp()
	assert.Equal(t, 2, sb.selected, "wraps to last service")
	sb.moveDown()
	assert.Equal(t, 0, sb.selected, "wraps back to first")

	out := sb.render(28, 24, true)
	assert.NotContains(t, out, "TARGETS")
}

func TestSidebar_WithTargets_CircularCursor(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())

	// Starts in the services section at row 0.
	require.Equal(t, sectionServices, sb.section)
	require.Equal(t, 0, sb.selected)

	// Up from services[0] → last target row.
	sb.moveUp()
	assert.Equal(t, sectionTargets, sb.section)
	assert.Equal(t, 2, sb.targetSel)

	// Up through the targets to the top.
	sb.moveUp()
	assert.Equal(t, 1, sb.targetSel)
	sb.moveUp()
	assert.Equal(t, 0, sb.targetSel)

	// Up from targets[0] → wraps to the bottom of services.
	sb.moveUp()
	assert.Equal(t, sectionServices, sb.section)
	assert.Equal(t, 1, sb.selected)

	// Down from services[last] → back to targets[0].
	sb.moveDown()
	assert.Equal(t, sectionTargets, sb.section)
	assert.Equal(t, 0, sb.targetSel)
}

func TestSidebar_TargetCursorDoesNotFilter(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "db", "web"), targetRows())

	// Walk the cursor onto t1 (members: web) without pressing Enter.
	sb.section = sectionServices
	sb.selected = 0
	sb.moveUp() // → targets[last] == t2
	sb.moveUp() // → t1
	require.Equal(t, "t1", sb.targets[sb.targetSel].name)
	assert.Empty(t, sb.filterTarget, "cursor movement alone selects nothing")
	assert.Equal(t, []string{"api", "db", "web"}, svcNames(sb), "list stays unfiltered")
}

func TestSidebar_EnterSelectsAndClearsFilter(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "db", "web"), targetRows())

	sb.section = sectionTargets
	sb.targetSel = 1 // t1 (members: web)

	sb.toggleTargetSelection()
	assert.Equal(t, "t1", sb.filterTarget)
	assert.Equal(t, []string{"web"}, svcNames(sb), "filtered to t1's members")

	// Enter again on the same row clears it.
	sb.toggleTargetSelection()
	assert.Empty(t, sb.filterTarget)
	assert.Equal(t, []string{"api", "db", "web"}, svcNames(sb))
}

func TestSidebar_ToggleFilterKeepsSelectedService(t *testing.T) {
	sb := &sidebar{}
	// t2 members: api, db.
	sb.update(svcs("api", "db", "web"), targetRows())
	sb.selected = 1 // "db" in the unfiltered list
	require.Equal(t, "db", sb.services[sb.selected].Name)

	sb.section = sectionTargets
	sb.targetSel = 2 // t2
	sb.toggleTargetSelection()
	assert.Equal(t, "db", sb.services[sb.selected].Name, "highlight follows the service, not the index")

	// Clearing the filter keeps it on db too.
	sb.toggleTargetSelection()
	assert.Equal(t, "db", sb.services[sb.selected].Name)
}

func TestSidebar_EnterAllServicesClearsFilter(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "db", "web"), targetRows())
	sb.section = sectionTargets

	sb.targetSel = 2 // t2
	sb.toggleTargetSelection()
	require.Equal(t, "t2", sb.filterTarget)

	sb.targetSel = 0 // "All services"
	sb.toggleTargetSelection()
	assert.Empty(t, sb.filterTarget, "All services row always clears the filter")
	assert.Equal(t, []string{"api", "db", "web"}, svcNames(sb))
}

func TestSidebar_FilterMarkerShownWhileCursorElsewhere(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	sb.section = sectionTargets
	sb.targetSel = 1 // t1
	sb.toggleTargetSelection()

	// Move the cursor away; the filter (and its ▸ marker) must persist.
	sb.targetSel = 2
	out := plain(sb.render(30, 24, true))
	assert.Contains(t, out, "▸")
	require.Equal(t, "t1", sb.filterTarget)
}

func TestSidebar_SelectedTargetOnlyInTargetsSection(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())

	sb.section = sectionServices
	assert.Nil(t, sb.selectedTarget())

	sb.section = sectionTargets
	sb.targetSel = 1
	tgt := sb.selectedTarget()
	require.NotNil(t, tgt)
	assert.Equal(t, "t1", tgt.name)

	sb.targetSel = 0
	require.NotNil(t, sb.selectedTarget())
	assert.Equal(t, "", sb.selectedTarget().name, "All services row is still a selectable target row")
}

func TestSidebar_TargetSelectionPreservedAcrossUpdate(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	sb.section = sectionTargets
	sb.targetSel = 2 // t2
	sb.toggleTargetSelection()
	require.Equal(t, "t2", sb.filterTarget)

	// A fresh poll with the same targets in the same order keeps both the
	// cursor (by name) and the selected filter.
	sb.update(svcs("api", "web"), targetRows())
	assert.Equal(t, 2, sb.targetSel, "cursor stays on t2 by name")
	assert.Equal(t, "t2", sb.filterTarget, "selected filter survives the poll")
}

func TestSidebar_FilterClearedWhenTargetVanishes(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	sb.section = sectionTargets
	sb.targetSel = 2 // t2
	sb.toggleTargetSelection()
	require.Equal(t, "t2", sb.filterTarget)

	// t2 is gone from the next poll.
	sb.update(svcs("api", "web"), []sidebarTarget{
		{name: ""},
		{name: "t1", members: []string{"web"}, active: true},
	})
	assert.Empty(t, sb.filterTarget, "filter drops when its target no longer exists")
	assert.Equal(t, []string{"api", "web"}, svcNames(sb))
}

func TestSidebar_RendersTargetsBlock(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	out := plain(sb.render(30, 24, true))
	assert.Contains(t, out, "TARGETS")
	assert.Contains(t, out, "All services")
	assert.Contains(t, out, "t1")
	assert.Contains(t, out, "SERVICES")
}

// TestSidebar_AllServicesRowWithoutRealTargets covers the single-row TARGETS
// block: the "All services" row is shown, navigable, and carries a running-count
// info block even though no real target is defined.
func TestSidebar_AllServicesRowWithoutRealTargets(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "api", State: "running"},
		{Name: "web", State: "stopped"},
	}, []sidebarTarget{{name: ""}})

	require.True(t, sb.showTargets())
	require.Equal(t, sectionServices, sb.section, "cursor starts in SERVICES")

	// Up from services[0] lands on the lone "All services" row.
	sb.moveUp()
	assert.Equal(t, sectionTargets, sb.section)
	assert.Equal(t, 0, sb.targetSel)
	require.NotNil(t, sb.selectedTarget())
	assert.Equal(t, "", sb.selectedTarget().name)

	// Down from the row returns to the top of SERVICES.
	sb.moveDown()
	assert.Equal(t, sectionServices, sb.section)
	assert.Equal(t, 0, sb.selected)

	sb.section = sectionTargets
	out := plain(sb.render(30, 24, true))
	assert.Contains(t, out, "TARGETS")
	assert.Contains(t, out, "All services")
	assert.Contains(t, out, "1/2 running", "info block counts running services")
}

func TestModel_BuildTargets(t *testing.T) {
	m := model{}
	assert.Nil(t, m.buildTargets(nil), "nil registry → no targets")

	m.registry = &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}, "api": {Name: "api"}},
		Targets:  map[string][]string{"zeta": {"web"}, "alpha": {"api"}},
	}
	rows := m.buildTargets([]string{"alpha"})
	require.Len(t, rows, 3)
	assert.Equal(t, "", rows[0].name, "All services first")
	assert.Equal(t, "alpha", rows[1].name, "targets sorted")
	assert.True(t, rows[1].active)
	assert.Equal(t, "zeta", rows[2].name)
	assert.False(t, rows[2].active)
}

func TestModel_SidebarWidth_GrowsForLongTargetName(t *testing.T) {
	m := model{width: 200}
	m.sidebarC.allServices = []ipc.ServiceInfo{{Name: "a"}}
	m.sidebarC.targets = []sidebarTarget{{name: ""}, {name: "a-very-long-target-name-here"}}
	assert.Equal(t, len("a-very-long-target-name-here")+4, m.sidebarWidth())
}

func TestModel_BuildTargets_AllServicesRowWhenNoRealTargets(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}},
		Targets:  map[string][]string{},
	}}
	rows := m.buildTargets(nil)
	require.Len(t, rows, 1, "a registry with services still yields the All services row")
	assert.Equal(t, "", rows[0].name)
}

func TestModel_BuildTargets_NilWhenRegistryDefinesNothing(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{},
		Targets:  map[string][]string{},
	}}
	assert.Nil(t, m.buildTargets(nil), "no services and no targets → no TARGETS block")
}

func TestModel_StartStopAll_OnAllServicesRow(t *testing.T) {
	m := model{
		socketPath: filepath.Join(t.TempDir(), "nonexistent.sock"),
		registry: &config.Registry{
			Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}},
			Targets:  map[string][]string{"t1": {"web"}},
		},
	}
	m.sidebarC.update(svcs("web"), m.buildTargets(nil))
	m.sidebarC.section = sectionTargets
	m.sidebarC.targetSel = 0 // "All services"

	require.NotNil(t, m.sidebarC.selectedTarget())
	// The target-scoped commands stay no-ops on the synthetic row...
	assert.Nil(t, m.doStartTarget(), "start-target is a no-op on All services")
	assert.Nil(t, m.doStopTarget(), "stop-target is a no-op on All services")

	// ...and start/stop-all take over. With the daemon unreachable the batch
	// reports its failure rather than returning nil.
	startMsg := m.doStartAll()()
	if err, ok := startMsg.(daemonErrMsg); assert.True(t, ok, "doStartAll surfaces the unreachable daemon") {
		assert.Contains(t, err.err.Error(), "start all:")
	}
	stopMsg := m.doStopAll()()
	if err, ok := stopMsg.(daemonErrMsg); assert.True(t, ok, "doStopAll surfaces the unreachable daemon") {
		assert.Contains(t, err.err.Error(), "stop all:")
	}
}

func TestModel_StartStopAll_NoopWithoutSocket(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}},
	}}
	m.sidebarC.update(svcs("web"), m.buildTargets(nil))
	assert.Nil(t, m.doStartAll(), "no socket → no command")
	assert.Nil(t, m.doStopAll(), "no socket → no command")
}

func TestSidebar_EmptyFilterShowsPlaceholderRow(t *testing.T) {
	sb := &sidebar{}
	// t2 members api/db, but no such services exist → filtered list is empty.
	sb.update(svcs("web"), targetRows())
	sb.section = sectionTargets
	sb.targetSel = 2
	sb.toggleTargetSelection()
	require.Empty(t, sb.services)

	out := plain(sb.render(30, 24, true))
	assert.Contains(t, out, "no services in target")
	assert.Contains(t, out, "TARGETS")
}
