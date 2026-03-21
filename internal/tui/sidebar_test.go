package tui

import (
	"testing"

	"github.com/hailerity/procet/internal/ipc"
	"github.com/stretchr/testify/assert"
)

func svcNames(sb *sidebar) []string {
	names := make([]string, len(sb.services))
	for i, s := range sb.services {
		names[i] = s.Name
	}
	return names
}

func TestSidebar_AlphabeticalOrder(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "zoo", State: "running"},
		{Name: "api", State: "stopped"},
		{Name: "web", State: "running"},
	})
	assert.Equal(t, []string{"api", "web", "zoo"}, svcNames(sb))
}

func TestSidebar_SelectionPreservedByName(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}})
	sb.selected = 1 // "web"

	sb.update([]ipc.ServiceInfo{{Name: "zoo"}, {Name: "web", State: "running"}, {Name: "api"}})
	assert.Equal(t, 1, sb.selected) // still index of "web" after re-sort
}

func TestSidebar_SelectionFallsBackWhenServiceGone(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}})
	sb.selected = 2 // "zoo"

	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}})
	assert.Equal(t, 0, sb.selected) // "zoo" gone, falls back to 0
}

func TestSidebar_MoveUpDownClamps(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}})

	sb.moveUp()
	assert.Equal(t, 0, sb.selected) // can't go above 0

	sb.moveDown()
	assert.Equal(t, 1, sb.selected)

	sb.moveDown()
	assert.Equal(t, 1, sb.selected) // can't go past last
}

func TestStateLabel_RunningWithPort(t *testing.T) {
	port := 8080
	assert.Equal(t, ":8080", stateLabel(ipc.ServiceInfo{State: "running", Port: &port}))
}

func TestStateLabel_RunningNoPort(t *testing.T) {
	assert.Equal(t, "detecting", stateLabel(ipc.ServiceInfo{State: "running", Port: nil}))
}

func TestStateLabel_RunningZeroPort(t *testing.T) {
	port := 0
	assert.Equal(t, "detecting", stateLabel(ipc.ServiceInfo{State: "running", Port: &port}))
}

func TestStateLabel_Crashed(t *testing.T) {
	assert.Equal(t, "crashed", stateLabel(ipc.ServiceInfo{State: "crashed"}))
}
