package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
		{name: "t1", members: []string{"web"}},
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

func TestSidebar_TargetsAreNotRows(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web", "zoo"), targetRows())

	// The cursor only ever walks services, wrapping at the ends.
	sb.moveUp()
	assert.Equal(t, 2, sb.selected, "wraps to last service")
	sb.moveDown()
	assert.Equal(t, 0, sb.selected, "wraps back to first")

	out := plain(sb.render(28))
	assert.NotContains(t, out, "TARGETS")
	assert.NotContains(t, out, "t1")
}

func TestSidebar_SetFilterAndClear(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "db", "web"), targetRows())

	sb.setFilter("t1")
	assert.Equal(t, "t1", sb.filterTarget)
	assert.Equal(t, []string{"web"}, svcNames(sb), "filtered to t1's members")

	sb.setFilter("")
	assert.Empty(t, sb.filterTarget)
	assert.Equal(t, []string{"api", "db", "web"}, svcNames(sb))
}

func TestSidebar_SetFilterUnknownTargetClears(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	sb.setFilter("t1")

	sb.setFilter("nope")
	assert.Empty(t, sb.filterTarget, "an unknown target must not leave a filter nothing matches")
	assert.Equal(t, []string{"api", "web"}, svcNames(sb))
}

func TestSidebar_SetFilterKeepsSelectedService(t *testing.T) {
	sb := &sidebar{}
	// t2 members: api, db.
	sb.update(svcs("api", "db", "web"), targetRows())
	sb.selected = 1 // "db" in the unfiltered list
	require.Equal(t, "db", sb.services[sb.selected].Name)

	sb.setFilter("t2")
	assert.Equal(t, "db", sb.services[sb.selected].Name, "highlight follows the service, not the index")

	sb.setFilter("")
	assert.Equal(t, "db", sb.services[sb.selected].Name)
}

func TestSidebar_FilterShownInPaneTitle(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	assert.NotContains(t, plain(sb.frame(true).title), "·")

	sb.setFilter("t1")
	assert.Contains(t, plain(sb.frame(true).title), "SERVICES · t1")
}

func TestSidebar_FilterPreservedAcrossUpdate(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	sb.setFilter("t2")

	sb.update(svcs("api", "web"), targetRows())
	assert.Equal(t, "t2", sb.filterTarget, "selected filter survives the poll")
}

func TestSidebar_FilterClearedWhenTargetVanishes(t *testing.T) {
	sb := &sidebar{}
	sb.update(svcs("api", "web"), targetRows())
	sb.setFilter("t2")

	// t2 is gone from the next poll.
	sb.update(svcs("api", "web"), []sidebarTarget{
		{name: "t1", members: []string{"web"}},
	})
	assert.Empty(t, sb.filterTarget, "filter drops when its target no longer exists")
	assert.Equal(t, []string{"api", "web"}, svcNames(sb))
}

func TestTargetPicker_CursorWrapsAndSelects(t *testing.T) {
	var p targetPicker
	rows := targetRows()
	p.openAt(rows, "")
	assert.Equal(t, 0, p.cursor)
	assert.Equal(t, "", p.selected(rows), "row 0 is All services")

	p.move(-1, len(rows))
	assert.Equal(t, "t2", p.selected(rows), "up from the top wraps to the last target")
	p.move(1, len(rows))
	assert.Equal(t, "", p.selected(rows))

	p.openAt(rows, "t2")
	assert.Equal(t, "t2", p.selected(rows), "opens on the active filter")

	// A list that shrank under an open picker must not index out of range.
	assert.Equal(t, "", p.selected(rows[:1]))
}

func TestTargetPicker_ViewMarksFilterAndUnreportedMembers(t *testing.T) {
	var p targetPicker
	rows := targetRows()
	p.openAt(rows, "t2") // members: api, db — db is not reported
	out := plain(p.view(rows, []ipc.ServiceInfo{{Name: "api", State: "running"}}, "t2"))
	assert.Contains(t, out, "▸")
	assert.Contains(t, out, "1/2")
	assert.Contains(t, out, "not reported")
}

func TestModel_BuildTargets(t *testing.T) {
	m := model{}
	assert.Nil(t, m.buildTargets(), "nil registry → no targets")

	m.registry = &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}, "api": {Name: "api"}},
		Targets:  map[string][]string{"zeta": {"web"}, "alpha": {"api"}},
	}
	rows := m.buildTargets()
	require.Len(t, rows, 2)
	assert.Equal(t, "alpha", rows[0].name, "targets sorted")
	assert.Equal(t, []string{"api"}, rows[0].members)
	assert.Equal(t, "zeta", rows[1].name)
}

func TestModel_BuildTargets_NilWithoutTargets(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}},
		Targets:  map[string][]string{},
	}}
	assert.Nil(t, m.buildTargets())
}

func TestModel_StartStopTarget_UnknownTargetIsNoop(t *testing.T) {
	m := model{
		socketPath: filepath.Join(t.TempDir(), "nonexistent.sock"),
		registry: &config.Registry{
			Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}},
			Targets:  map[string][]string{"t1": {"web"}},
		},
	}
	m.sidebarC.update(svcs("web"), m.buildTargets())

	assert.Nil(t, m.doStartTarget(""), "no target name → no command")
	assert.Nil(t, m.doStopTarget("gone"), "unknown target → no command")
	assert.NotNil(t, m.doStartTarget("t1"))
	assert.NotNil(t, m.doStopTarget("t1"))
}

func TestModel_StartStopAll_SurfacesUnreachableDaemon(t *testing.T) {
	m := model{
		socketPath: filepath.Join(t.TempDir(), "nonexistent.sock"),
		registry: &config.Registry{
			Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}},
		},
	}
	m.sidebarC.update(svcs("web"), nil)

	startMsg := m.doStartAll()()
	if err, ok := startMsg.(daemonErrMsg); assert.True(t, ok, "doStartAll surfaces the unreachable daemon") {
		assert.Contains(t, err.err.Error(), "start all:")
	}
	stopMsg := m.doStopAll()()
	if err, ok := stopMsg.(daemonErrMsg); assert.True(t, ok, "doStopAll surfaces the unreachable daemon") {
		assert.Contains(t, err.err.Error(), "stop all:")
	}
}

func TestClassifyBatchResp(t *testing.T) {
	cases := []struct {
		name    string
		resp    *ipc.Response
		err     error
		benign  string
		wantOut string
	}{
		{"ok", &ipc.Response{OK: true}, nil, "is already running", ""},
		{"benign already running", &ipc.Response{Error: "web is already running"}, nil, "is already running", ""},
		{"benign not running", &ipc.Response{Error: "web is not running"}, nil, "is not running", ""},
		{"real failure", &ipc.Response{Error: "exec: no such file"}, nil, "is already running", "web: exec: no such file"},
		{"not-OK with blank error", &ipc.Response{OK: false, Error: ""}, nil, "is already running", "web: request failed"},
		{"transport error", nil, errors.New("dial: connection refused"), "is already running", "web: dial: connection refused"},
		{"nil response, nil error", nil, nil, "is already running", "web: no response from daemon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantOut, classifyBatchResp(tc.resp, tc.err, "web", tc.benign))
		})
	}
}

func TestModel_StartStopAll_NoopWithoutSocket(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}},
	}}
	m.sidebarC.update(svcs("web"), m.buildTargets())
	assert.Nil(t, m.doStartAll(), "no socket → no command")
	assert.Nil(t, m.doStopAll(), "no socket → no command")
}

func TestSidebar_EmptyFilterShowsPlaceholderRow(t *testing.T) {
	sb := &sidebar{}
	// t2 members api/db, but no such services exist → filtered list is empty.
	sb.update(svcs("web"), targetRows())
	sb.setFilter("t2")
	require.Empty(t, sb.services)

	out := plain(sb.render(30))
	assert.Contains(t, out, "no services in target")
}

// A config with many targets and a large target must not grow the picker past a
// normal terminal: rows window around the cursor and members are capped.
func TestTargetPicker_LongListsStayBounded(t *testing.T) {
	var rows []sidebarTarget
	var big []string
	for i := 0; i < 40; i++ {
		big = append(big, fmt.Sprintf("svc-%02d", i))
	}
	for i := 0; i < 30; i++ {
		rows = append(rows, sidebarTarget{name: fmt.Sprintf("target-%02d", i), members: big})
	}
	var p targetPicker
	p.openAt(rows, "target-29") // cursor on the last row

	out := plain(p.view(rows, nil, "target-29"))
	assert.LessOrEqual(t, lipgloss.Height(out), 36)
	assert.Contains(t, out, "target-29", "the window follows the cursor")
	assert.NotContains(t, out, "target-05")
	assert.Contains(t, out, "+32 more")
}
