package tui

import (
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cursor indexes drawn rows, not services. The invariant that made that
// switch invisible is this one: with nothing to group by there are no headers,
// so a row is a service and the two indexes coincide. Every test written
// against `selected` as a service index still means what it meant.
func TestSidebar_RowsAreServicesWhenThereIsNothingToGroup(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)

	require.Len(t, sb.rows, len(sb.services))
	for i := range sb.rows {
		assert.Equal(t, rowService, sb.rows[i].kind, "row %d is a service row", i)
		assert.Equal(t, i, sb.rows[i].svc, "row %d points at service %d", i, i)
	}

	// And the cursor reads through to the same service either way.
	sb.selected = 2
	require.NotNil(t, sb.selectedService())
	assert.Equal(t, sb.services[2].Name, sb.selectedService().Name)
}

// serviceAt is the only route from a row to a service, so it has to be honest
// at the edges: nothing outside the list, and nothing behind a header.
func TestSidebar_ServiceAtIsBoundedAndHeaderAware(t *testing.T) {
	sb := &sidebar{}
	sb.update(filterServices(), nil)

	assert.Nil(t, sb.serviceAt(-1))
	assert.Nil(t, sb.serviceAt(len(sb.rows)))
	assert.NotNil(t, sb.serviceAt(0))

	// A header row has no service behind it, whatever its svc field says.
	sb.rows = []sidebarRow{{kind: rowHeader, group: "backend", svc: -1}}
	assert.Nil(t, sb.serviceAt(0))
	assert.Nil(t, sb.selectedService(), "and the cursor on it selects nothing")
}

// The cursor walks rows, so an empty list must not move it and a one-row list
// must wrap onto itself rather than off the end.
func TestSidebar_MoveWrapsOverRows(t *testing.T) {
	sb := &sidebar{}
	sb.moveDown() // no rows at all: must not panic or move
	sb.moveUp()
	assert.Equal(t, 0, sb.selected)

	sb.update([]ipc.ServiceInfo{{Name: "only"}}, nil)
	sb.moveDown()
	assert.Equal(t, 0, sb.selected)
	sb.moveUp()
	assert.Equal(t, 0, sb.selected)
}
