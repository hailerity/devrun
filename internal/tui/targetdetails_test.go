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
	tgt := &sidebarTarget{name: "backend", members: []string{"api", "db"}, active: true}
	infos := []ipc.ServiceInfo{
		{Name: "api", State: "running", Port: intp(8080), PID: intp(4321)},
		{Name: "db", State: "stopped"},
	}

	out := plain(tp.render(tgt, infos, 60, 20))

	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "started") // target activation state
	assert.Contains(t, out, "1 running / 2")
	assert.Contains(t, out, "api")
	assert.Contains(t, out, ":8080")
	assert.Contains(t, out, "4321")
	assert.Contains(t, out, "db")
}

func TestTargetDetails_InactiveTargetReadsStopped(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api"}}
	out := plain(tp.render(tgt, []ipc.ServiceInfo{{Name: "api", State: "stopped"}}, 60, 20))
	assert.Contains(t, out, "stopped")
	assert.NotContains(t, out, "started")
	assert.Contains(t, out, "0 running / 1")
}

// TestTargetDetails_ActiveWithNoMembersUp guards against the state row
// contradicting the live member count: an active target with everything stopped
// reads "started" + "0 running / N", never "running".
func TestTargetDetails_ActiveWithNoMembersUp(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api", "db"}, active: true}
	infos := []ipc.ServiceInfo{{Name: "api", State: "stopped"}, {Name: "db", State: "crashed"}}
	out := plain(tp.render(tgt, infos, 60, 20))
	assert.Contains(t, out, "started")
	assert.Contains(t, out, "0 running / 2")
	assert.NotContains(t, out, "● running")
}

func TestTargetDetails_UnreportedMember(t *testing.T) {
	var tp targetDetailsPanel
	tgt := &sidebarTarget{name: "backend", members: []string{"api", "ghost"}}
	out := plain(tp.render(tgt, []ipc.ServiceInfo{{Name: "api", State: "running"}}, 60, 20))
	assert.Contains(t, out, "ghost")
	assert.Contains(t, out, "not reported")
	assert.Contains(t, out, "1 running / 2")
}

func TestTargetDetails_NoMembers(t *testing.T) {
	var tp targetDetailsPanel
	out := plain(tp.render(&sidebarTarget{name: "empty"}, nil, 60, 20))
	assert.Contains(t, out, "empty")
	assert.Contains(t, out, "(no services)")
	assert.Contains(t, out, "0 running / 0")
}
