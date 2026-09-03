package tui

import (
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
)

func intp(n int) *int { return &n }

func TestTargetDetails_NilOrSyntheticRow(t *testing.T) {
	var tp targetDetailsPanel
	assert.Contains(t, plain(tp.render(nil, nil, 40, 20)), "No target selected")
	assert.Contains(t, plain(tp.render(&sidebarTarget{name: ""}, nil, 40, 20)), "No target selected")
}

func TestTargetDetails_ShowsNameStateAndMembers(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api", "db"}}
	infos := []ipc.ServiceInfo{
		{Name: "api", State: "running", Port: intp(8080), PID: intp(4321)},
		{Name: "db", State: "running"},
	}

	out := plain(tp.render(tgt, infos, 60, 20))

	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "● running") // every member up → state reads running
	assert.Contains(t, out, "2 running / 2")
	assert.Contains(t, out, "api")
	assert.Contains(t, out, ":8080")
	assert.Contains(t, out, "4321")
	assert.Contains(t, out, "db")
}

func TestTargetDetails_PartiallyUpReadsStopped(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api", "db"}}
	infos := []ipc.ServiceInfo{{Name: "api", State: "running"}, {Name: "db", State: "stopped"}}
	out := plain(tp.render(tgt, infos, 60, 20))
	assert.Contains(t, out, "○ stopped", "state reads stopped until every member is up")
	assert.NotContains(t, out, "● running")
	assert.Contains(t, out, "1 running / 2")
}

// TestTargetDetails_StateFollowsCountNotActiveFlag: the panel's state word is
// driven by the live member count, not sidebarTarget.active — so an
// optimistically-lit "All services" row (s pressed, poll not yet back) never
// shows "● running" next to a "0 running / N" line.
func TestTargetDetails_StateFollowsCountNotActiveFlag(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api", "db"}, active: true}
	infos := []ipc.ServiceInfo{{Name: "api", State: "stopped"}, {Name: "db", State: "stopped"}}
	out := plain(tp.render(tgt, infos, 60, 20))
	assert.Contains(t, out, "○ stopped")
	assert.NotContains(t, out, "● running")
	assert.Contains(t, out, "0 running / 2")
}

func TestTargetDetails_InactiveTargetReadsStopped(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api"}}
	out := plain(tp.render(tgt, []ipc.ServiceInfo{{Name: "api", State: "stopped"}}, 60, 20))
	assert.Contains(t, out, "stopped")
	assert.NotContains(t, out, "● running")
	assert.Contains(t, out, "0 running / 1")
}

func TestTargetDetails_UnreportedMember(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api", "ghost"}}
	out := plain(tp.render(tgt, []ipc.ServiceInfo{{Name: "api", State: "running"}}, 60, 20))
	assert.Contains(t, out, "ghost")
	assert.Contains(t, out, "not reported")
	assert.Contains(t, out, "1 running / 2")
}

func TestTargetDetails_NegativeDimensionsDoNotPanic(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api"}}
	assert.NotPanics(t, func() {
		tp.render(tgt, []ipc.ServiceInfo{{Name: "api", State: "running"}}, -5, -3)
	})
}

func TestTargetDetails_NoMembers(t *testing.T) {
	var tp targetDetailsPanel
	out := plain(tp.render(&sidebarTarget{name: "empty"}, nil, 60, 20))
	assert.Contains(t, out, "empty")
	assert.Contains(t, out, "(no services)")
	assert.Contains(t, out, "0 running / 0")
}
