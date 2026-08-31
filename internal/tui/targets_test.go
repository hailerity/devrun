package tui

import (
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

func TestSidebar_NoTargets_HidesBlockAndWrapsWithinServices(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web", "zoo"), nil)

	assert.False(t, sb.hasTargets())
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

func TestSidebar_TargetCursorFiltersServices(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "db", "web"), targetRows())

	// Move onto t1 (members: web).
	sb.section = sectionServices
	sb.selected = 0
	sb.moveUp()            // → targets[last] == t2
	sb.moveUp()            // → t1
	require.Equal(t, "t1", sb.targets[sb.targetSel].name)
	assert.Equal(t, []string{"web"}, svcNames(sb), "list filtered to t1's members")

	// Move onto "All services".
	sb.moveUp()
	require.Equal(t, "", sb.targets[sb.targetSel].name)
	assert.Equal(t, []string{"api", "db", "web"}, svcNames(sb), "All services shows everything")
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

	// A fresh poll with the same targets in the same order.
	sb.update(svcs("api", "web"), targetRows())
	assert.Equal(t, 2, sb.targetSel, "cursor stays on t2 by name")
	assert.Equal(t, "t2", sb.filterTarget)
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

func TestModel_BuildTargets_EmptyWhenNoTargets(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}},
		Targets:  map[string][]string{},
	}}
	assert.Nil(t, m.buildTargets(nil))
}

func TestModel_StartStopKey_NoopOnAllServicesRow(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}},
		Targets:  map[string][]string{"t1": {"web"}},
	}}
	m.sidebarC.update(svcs("web"), m.buildTargets(nil))
	m.sidebarC.section = sectionTargets
	m.sidebarC.targetSel = 0 // "All services"

	require.NotNil(t, m.sidebarC.selectedTarget())
	assert.Nil(t, m.doStartTarget(), "start on All services is a no-op")
	assert.Nil(t, m.doStopTarget(), "stop on All services is a no-op")
}

func TestSidebar_EmptyFilterShowsPlaceholderRow(t *testing.T) {
	sb := &sidebar{}
	// t2 members api/db, but no such services exist → filtered list is empty.
	sb.update(svcs("web"), targetRows())
	sb.section = sectionTargets
	sb.targetSel = 2
	sb.onTargetCursorMoved()
	require.Empty(t, sb.services)

	out := plain(sb.render(30, 24, true))
	assert.Contains(t, out, "no services in target")
	assert.Contains(t, out, "TARGETS")
}
