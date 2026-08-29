package tui

import (
	"strings"
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
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

func TestSidebar_MoveUpDownWraps(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}})

	// moveUp from first wraps to last
	sb.selected = 0
	sb.moveUp()
	assert.Equal(t, 2, sb.selected)

	// moveDown from last wraps to first
	sb.selected = 2
	sb.moveDown()
	assert.Equal(t, 0, sb.selected)

	// normal movement still works
	sb.selected = 0
	sb.moveDown()
	assert.Equal(t, 1, sb.selected)

	sb.moveUp()
	assert.Equal(t, 0, sb.selected)
}

func TestSidebar_MoveUpDownNoopWhenEmpty(t *testing.T) {
	sb := &sidebar{}
	sb.moveUp()   // must not panic
	sb.moveDown() // must not panic
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

func TestTruncateName_FitsUnchanged(t *testing.T) {
	assert.Equal(t, "web", truncateName("web", 10))
	assert.Equal(t, "exactfit", truncateName("exactfit", 8))
}

func TestTruncateName_MiddleTruncatesOverflow(t *testing.T) {
	// head + "…" + tail, total width preserved.
	assert.Equal(t, "my-real…ervice", truncateName("my-really-long-service", 14))
	assert.Equal(t, 14, len([]rune(truncateName("my-really-long-service", 14))))
	// shared prefix stays visible, distinguishing tail stays visible.
	out := truncateName("frontend-web-server", 15)
	assert.True(t, strings.HasPrefix(out, "fronten"))
	assert.True(t, strings.HasSuffix(out, "server"))
}

func TestTruncateName_TinyWidths(t *testing.T) {
	assert.Equal(t, "…", truncateName("anything", 1))
	assert.Equal(t, "…", truncateName("anything", 0)) // clamped to 1
	assert.Equal(t, "a…", truncateName("anything", 2))
}
