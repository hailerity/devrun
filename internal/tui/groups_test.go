package tui

import (
	"strings"
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupedServices spans the cases that matter: two named groups, a service with
// no group at all, and names that would sort differently within a group than
// across the whole list.
func groupedServices() []ipc.ServiceInfo {
	return []ipc.ServiceInfo{
		{Name: "api", Group: "backend", State: "running"},
		{Name: "db", Group: "backend", State: "running"},
		{Name: "worker", Group: "backend", State: "crashed"},
		{Name: "web", Group: "frontend", State: "running"},
		{Name: "assets", Group: "frontend", State: "stopped"},
		{Name: "scratch", State: "stopped"}, // no group
	}
}

// rowShape renders the row list as "header:name" / "svc:name" so a test can
// assert the whole layout in one line.
func rowShape(sb *sidebar) []string {
	out := make([]string, 0, len(sb.rows))
	for i := range sb.rows {
		if svc := sb.serviceAt(i); svc != nil {
			out = append(out, "svc:"+svc.Name)
			continue
		}
		out = append(out, "header:"+groupLabel(sb.rows[i].group))
	}
	return out
}

func TestSidebar_GroupsAreSectionedAlphabeticallyWithUngroupedLast(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)

	assert.Equal(t, []string{
		"header:backend", "svc:api", "svc:db", "svc:worker",
		"header:frontend", "svc:assets", "svc:web",
		"header:ungrouped", "svc:scratch",
	}, rowShape(sb))
}

// Group names come from user config and a project's directory name, so they are
// as likely to be capitalised as not. The order has to agree with where a
// reader looks for a name — byte order would file every capitalised group ahead
// of every lowercase one, exactly as it did for service names.
func TestSidebar_GroupOrderIsCaseInsensitive(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "s1", Group: "Zebra"},
		{Name: "s2", Group: "api"},
		{Name: "s3", Group: "Backend"},
		{Name: "s4", Group: "charlie"},
		{Name: "s5"}, // ungrouped, still last
	}, nil)

	var headers []string
	for i := range sb.rows {
		if sb.rows[i].kind == rowHeader {
			headers = append(headers, groupLabel(sb.rows[i].group))
		}
	}
	assert.Equal(t, []string{"api", "Backend", "charlie", "Zebra", ungroupedLabel}, headers)
}

// Groups differing only in case are distinct groups and still need a stable,
// total order between them.
func TestSidebar_GroupOrderIsTotalForCaseOnlyDifferences(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "s1", Group: "web"},
		{Name: "s2", Group: "WEB"},
		{Name: "s3", Group: "Web"},
	}, nil)

	var headers []string
	for i := range sb.rows {
		if sb.rows[i].kind == rowHeader {
			headers = append(headers, groupLabel(sb.rows[i].group))
		}
	}
	assert.Equal(t, []string{"WEB", "Web", "web"}, headers)
}

// One group is nothing to tell the reader, so no header is drawn — this is what
// keeps the ungrouped common case looking as it always did.
func TestSidebar_NoHeaderForASingleGroup(t *testing.T) {
	for _, group := range []string{"", "backend"} {
		sb := &sidebar{}
		sb.update([]ipc.ServiceInfo{
			{Name: "api", Group: group}, {Name: "web", Group: group},
		}, nil)
		assert.Equal(t, []string{"svc:api", "svc:web"}, rowShape(sb),
			"group %q alone needs no header", group)
	}
}

// The grouping is of the *listed* services, so a query that empties a group
// takes its header with it — a header for a section with no rows under it would
// be claiming members that are not on screen.
func TestSidebar_QueryDropsEmptiedGroupsAndTheirHeaders(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)

	sb.setQuery("a") // api, assets, scratch — one from each group
	assert.Equal(t, []string{
		"header:backend", "svc:api",
		"header:frontend", "svc:assets",
		"header:ungrouped", "svc:scratch",
	}, rowShape(sb))

	// Narrow to one group's worth and the headers go entirely: one group left.
	sb.setQuery("work")
	assert.Equal(t, []string{"svc:worker"}, rowShape(sb),
		"one surviving group needs no header")
}

// The header's count describes the group as filtered. Claiming 3 members while
// showing 1 would make the number a lie about what is on screen.
func TestSidebar_GroupCountFollowsTheFilter(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)

	up, total := sb.groupCount("backend")
	assert.Equal(t, 2, up)
	assert.Equal(t, 3, total)

	sb.setQuery("w") // worker (crashed), web
	up, total = sb.groupCount("backend")
	assert.Equal(t, 0, up, "worker is crashed")
	assert.Equal(t, 1, total, "and is the only backend service listed")
}

func TestGroupRow_ShowsGlyphNameAndCount(t *testing.T) {
	out := plain(groupRow(31, "backend", 2, 3, false, false))
	assert.Contains(t, out, "▾")
	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "2/3")
	assert.Equal(t, 31, len([]rune(out)), "a header fills its row exactly")
}

// A long group name must not push the count off the row.
func TestGroupRow_FitsALongGroupName(t *testing.T) {
	out := plain(groupRow(sidebarMinW-2, "a-very-long-group-name-indeed", 1, 9, false, false))
	assert.Equal(t, sidebarMinW-2, len([]rune(out)))
	assert.Contains(t, out, "1/9", "the count survives")
	assert.Contains(t, out, "…", "the name is shortened instead")
}

// The cursor walks headers, so j from the last service of one group lands on
// the next group's header rather than skipping to its first service.
func TestSidebar_CursorWalksOntoHeaders(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)

	require.Equal(t, "header:backend", rowShape(sb)[0])
	require.NotNil(t, sb.selectedService(), "a fresh list opens on a service, not a header")
	require.Equal(t, "api", sb.selectedService().Name)

	// Up from the first service lands on its header.
	sb.moveUp()
	assert.Nil(t, sb.selectedService(), "the cursor can sit on a header")
	assert.Equal(t, "backend", sb.rows[sb.selected].group)
	sb.moveDown()

	// Walk to the end of backend, then one more onto frontend's header.
	sb.moveDown() // db
	sb.moveDown() // worker
	require.Equal(t, "worker", sb.selectedService().Name)
	sb.moveDown()
	assert.Nil(t, sb.selectedService(), "next row is frontend's header")
	assert.Equal(t, "frontend", sb.rows[sb.selected].group)
}

// Row 0 of a grouped list is a header, so the fallback for "no such service"
// has to skip it — otherwise the dashboard opens with the cursor selecting
// nothing, and s/x/e/d and the log pane are all inert until the reader presses
// j. Also the state a query lands in when it excludes the anchored service.
func TestSidebar_FallbackCursorSkipsHeaders(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)

	// Fresh list, no anchor recorded yet.
	require.Equal(t, rowHeader, sb.rows[0].kind)
	assert.Equal(t, 1, sb.selected, "landed on the first service, not its header")
	require.NotNil(t, sb.selectedService())
	assert.Equal(t, "api", sb.selectedService().Name)

	// A query that excludes the anchored service does the same.
	sb.selectServiceByName("worker")
	require.Equal(t, "worker", sb.selectedService().Name)
	sb.setQuery("front") // nothing named front; by group it is not a name match
	if len(sb.services) > 0 {
		assert.NotNil(t, sb.selectedService(), "never parked on a header")
	}

	sb.setQuery("e") // assets, scratch, web — spread over three groups
	require.NotEmpty(t, sb.services)
	assert.NotNil(t, sb.selectedService(), "never parked on a header")
}

// A poll re-sorts and re-groups from scratch; the highlight is anchored by
// service name and has to survive the headers shifting every index.
func TestSidebar_CursorSurvivesAPollWithGroups(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectServiceByName("web")
	require.Equal(t, "web", sb.selectedService().Name)
	before := sb.selected

	sb.update(groupedServices(), nil)
	require.NotNil(t, sb.selectedService())
	assert.Equal(t, "web", sb.selectedService().Name)
	assert.Equal(t, before, sb.selected)
}

// Headers are rows, so they count toward the scroll window and the "a–b of N"
// the border shows — otherwise the numbers would not match what is scrolling.
func TestSidebar_WindowCountsHeaderRows(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	require.Len(t, sb.rows, 9) // 6 services + 3 headers
	sb.setRows(4)

	first, last := sb.window()
	assert.Equal(t, 0, first)
	assert.Equal(t, 4, last)
	assert.Contains(t, plain(sb.frame(true, 40).footRight), "of 9")
}

// The render has to draw headers, not silently skip rows it cannot map to a
// service — the bug the row model would most easily hide.
func TestSidebar_RenderDrawsAHeaderPerGroup(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)

	out := plain(sb.render(31))
	assert.Equal(t, len(sb.rows), strings.Count(out, "\n")+1,
		"every row gets a line, headers included")
	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "frontend")
	assert.Contains(t, out, "ungrouped")
}
