package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/config"

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

// rowShape renders the row list as "header:name" / "svc:name" / "gap" so a test
// can assert the whole layout in one line.
//
// Gaps are reported rather than filtered: the blank line between groups is a
// real row that the scroll window counts, and a test that hid it could not
// catch one appearing where it should not — above the first group, say, or
// inside one.
func rowShape(sb *sidebar) []string {
	out := make([]string, 0, len(sb.rows))
	for i := range sb.rows {
		switch {
		case sb.rows[i].kind == rowSpacer:
			out = append(out, "gap")
		case sb.serviceAt(i) != nil:
			out = append(out, "svc:"+sb.serviceAt(i).Name)
		default:
			out = append(out, "header:"+groupLabel(sb.rows[i].group))
		}
	}
	return out
}

func TestSidebar_GroupsAreSectionedAlphabeticallyWithUngroupedLast(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)

	assert.Equal(t, []string{
		"header:backend", "svc:api", "svc:db", "svc:worker", "gap", "header:frontend", "svc:assets", "svc:web", "gap", "header:(no group)", "svc:scratch",
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
		"header:backend", "svc:api", "gap", "header:frontend", "svc:assets", "gap", "header:(no group)", "svc:scratch",
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

// selectServiceByName's own fallback, reached when the anchor names a real
// service that the current filter excludes. The sibling paths (moveTo with no
// anchor, selectGroupHeader for a vanished group) have their own fallbacks, so
// this one needs a case that goes through none of them: an anchored service,
// filtered out, with headers still drawn above it.
func TestSidebar_FallbackSkipsHeadersWhenTheAnchoredServiceIsFilteredOut(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.selectServiceByName("db")
	require.Equal(t, "db", sb.selectedService().Name)

	// "e" keeps worker, web and assets — two groups, so headers are drawn — and
	// drops db, so the anchor cannot be honoured.
	sb.setQuery("e")
	require.Equal(t, rowHeader, sb.rows[0].kind, "a header is row 0")
	require.NotContains(t, rowShape(sb), "svc:db")

	assert.False(t, sb.onGroupHeader(), "must not fall back onto that header")
	require.NotNil(t, sb.selectedService())
	assert.Equal(t, "worker", sb.selectedService().Name, "the first service row")
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
//
// And once there are headers that total differs from the service count in
// footLeft, so it is named: "rows 1–4 of 9" beside "3/6 up" is two measurements,
// where a bare "1–4 of 9" reads as a second, contradictory count of services.
func TestSidebar_WindowCountsHeaderRowsAndNamesTheUnit(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	require.Len(t, sb.rows, 11) // 6 services + 3 headers + 2 gaps
	sb.setRows(4)

	first, last := sb.window()
	assert.Equal(t, 0, first)
	assert.Equal(t, 4, last)

	f := sb.frame(true, 40)
	assert.Contains(t, plain(f.footRight), "rows 1–4 of 11")
	assert.Contains(t, plain(f.footLeft), "3/6 up", "services, not rows")
}

// With no headers a row *is* a service, so the range stays the bare one it has
// always been — naming the unit there would be noise.
func TestSidebar_WindowOmitsTheUnitWhenThereAreNoHeaders(t *testing.T) {
	var svcs []ipc.ServiceInfo
	for i := 0; i < 12; i++ {
		svcs = append(svcs, ipc.ServiceInfo{Name: fmt.Sprintf("svc-%02d", i)})
	}
	sb := &sidebar{}
	sb.update(svcs, nil)
	sb.setRows(5)

	out := plain(sb.frame(true, 40).footRight)
	assert.Contains(t, out, "1–5 of 12")
	assert.NotContains(t, out, "rows")
}

// The pane sizes itself to the longest service name; a group header is a row
// too, and a group name can be as long — a project's name is the default for
// every service that sets none — so it has to count as well or headers truncate
// while the pane could have grown.
func TestModel_SidebarWidthAccountsForGroupNames(t *testing.T) {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	m = m2.(model)

	m.sidebarC.update([]ipc.ServiceInfo{{Name: "a", Group: "g"}, {Name: "b", Group: "h"}}, nil)
	narrow := m.sidebarWidth()

	m.sidebarC.update([]ipc.ServiceInfo{
		{Name: "a", Group: "customer-portal-backend"},
		{Name: "b", Group: "h"},
	}, nil)
	assert.Greater(t, m.sidebarWidth(), narrow,
		"a long group name widens the pane, as a long service name does")

	// And the header is then drawn in full rather than shortened.
	m.relayout()
	sideW, _ := m.paneWidths()
	w, _ := m.sidebarC.frame(true, sideW).innerSize(sideW, 20)
	assert.Contains(t, plain(m.sidebarC.render(w)), "customer-portal-backend")
}

// ...but only when a header is actually drawn. A project whose services set no
// `group:` of their own is one group — every service inherits the project's
// name — and one group gets no header, so reserving room for one took columns
// off the log pane for a row that never renders. Still the common shape of a
// devrun.yaml, even now that a service can name its own group.
func TestModel_SidebarWidthIgnoresGroupNamesWithOnlyOneGroup(t *testing.T) {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	m = m2.(model)

	short := []ipc.ServiceInfo{{Name: "a", Group: "cpb"}, {Name: "b", Group: "cpb"}}
	long := []ipc.ServiceInfo{
		{Name: "a", Group: "customer-portal-backend"},
		{Name: "b", Group: "customer-portal-backend"},
	}

	m.sidebarC.update(short, nil)
	require.False(t, m.sidebarC.hasHeaders(), "one group draws no header")
	narrow := m.sidebarWidth()

	m.sidebarC.update(long, nil)
	require.False(t, m.sidebarC.hasHeaders())
	assert.Equal(t, narrow, m.sidebarWidth(),
		"a group name nobody will see must not cost the log pane a column")
	assert.Equal(t, sidebarMinW, narrow, "and short service names keep the minimum")
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
	assert.Contains(t, out, "(no group)")
}

// --- the blank line between groups ---

// A gap above every header but the first. Not above the first, where it would
// only waste the pane's top row and read as a gap the list had failed to fill.
func TestSidebar_GapSeparatesGroupsButNotTheFirst(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)

	shape := rowShape(sb)
	assert.NotEqual(t, "gap", shape[0], "nothing above the first group")
	for i, r := range shape {
		if strings.HasPrefix(r, "header:") && i > 0 {
			assert.Equal(t, "gap", shape[i-1], "a gap above %s", r)
		}
	}
	// Exactly one per header after the first: 3 groups → 2 gaps.
	gaps := 0
	for _, r := range shape {
		if r == "gap" {
			gaps++
		}
	}
	assert.Equal(t, 2, gaps)
}

// One group draws no header, so there is nothing to separate and no gap.
func TestSidebar_NoGapWithoutHeaders(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api", Group: "g"}, {Name: "web", Group: "g"}}, nil)
	assert.NotContains(t, rowShape(sb), "gap")
}

// The cursor must never rest on a blank line — it would simply be invisible.
// j and k step over them in both directions, including across the wrap.
func TestSidebar_CursorNeverRestsOnAGap(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)

	// Walk the whole list forwards, then backwards, twice round each way.
	for i := 0; i < 2*len(sb.rows); i++ {
		sb.moveDown()
		require.NotEqualf(t, rowSpacer, sb.rows[sb.selected].kind,
			"landed on a gap after %d moveDown", i+1)
	}
	for i := 0; i < 2*len(sb.rows); i++ {
		sb.moveUp()
		require.NotEqualf(t, rowSpacer, sb.rows[sb.selected].kind,
			"landed on a gap after %d moveUp", i+1)
	}
}

// Walking down still reaches every service and every header — stepping over
// gaps must not step over anything else with them.
func TestSidebar_WalkingDownVisitsEveryRowButTheGaps(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)

	seen := map[int]bool{sb.selected: true}
	for i := 0; i < len(sb.rows); i++ {
		sb.moveDown()
		seen[sb.selected] = true
	}
	for i := range sb.rows {
		if sb.rows[i].kind == rowSpacer {
			assert.Falsef(t, seen[i], "row %d is a gap and was visited", i)
			continue
		}
		assert.Truef(t, seen[i], "row %d (%v) was never visited", i, sb.rows[i].kind)
	}
}

// A gap is a row, so it occupies a line and the render has to emit one for it —
// otherwise the pane would draw fewer lines than the window claims and the rows
// below would shift up by one per gap.
func TestSidebar_GapRendersAsABlankFullWidthLine(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)

	lines := strings.Split(plain(sb.render(31)), "\n")
	require.Len(t, lines, len(sb.rows), "one line per row, gaps included")
	for i := range sb.rows {
		if sb.rows[i].kind != rowSpacer {
			continue
		}
		assert.Empty(t, strings.TrimSpace(lines[i]), "row %d is blank", i)
		assert.Equal(t, 31, len([]rune(lines[i])), "and still fills its width")
	}
}

// fgSeq is the escape sequence a foreground colour emits, for asserting which
// colour a rendered row actually used. Comparing whole styled strings does not
// work: groupRow styles the name together with its padding in one call, so the
// trailing reset lands somewhere a fragment-sized expectation never reaches.
func fgSeq(t *testing.T, c lipgloss.AdaptiveColor) string {
	t.Helper()
	rendered := lipgloss.NewStyle().Foreground(c).Render("X")
	i := strings.Index(rendered, "X")
	require.Positive(t, i, "no escape sequence emitted — the colour profile is off in tests")
	return rendered[:i]
}

// A header must not take a service name's colour. That was the confusion the
// real terminal exposed: same colour, heavier weight, so it read as an
// emphasised service rather than as a label for the ones under it.
func TestGroupRow_IsNotColouredLikeAServiceName(t *testing.T) {
	header := groupRow(31, "backend", 2, 3, false, false)
	svc := serviceRow(31, ipc.ServiceInfo{Name: "api", State: "running", Port: intp(8080)}, false, false)

	assert.Contains(t, svc, fgSeq(t, colorText), "a service name is colorText")
	assert.NotContains(t, header, fgSeq(t, colorText),
		"a group header must not use a service name's colour anywhere on its row")
	assert.Contains(t, header, fgSeq(t, colorGroup), "it is colorGroup")
	assert.Contains(t, plain(header), "backend", "and still shows the name")

	// Worth knowing what this does and does not prove: colorGroup and colorMuted
	// share their Light side on purpose — only the dark side was too dim — so on
	// a light profile these two sequences are identical and the positive
	// assertion above would pass for either. It is meaningful because lipgloss
	// is on its dark default here, which the next line pins.
	require.NotEqual(t, colorGroup.Dark, colorMuted.Dark,
		"the dark sides differ, which is what makes the assertion above mean something")
}

// The gap backstop moves the cursor one row on, to the header the gap belongs
// to — not to the top of the list, which would take the reader and the log pane
// somewhere they never asked to be.
func TestSidebar_GapBackstopMovesOneRowNotToTheTop(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)

	gap := -1
	for i := range sb.rows {
		if sb.rows[i].kind == rowSpacer {
			gap = i
			break
		}
	}
	require.Positive(t, gap, "the fixture has a gap, and not at row 0")

	sb.selected = gap
	sb.refilter()
	assert.Equal(t, gap+1, sb.selected, "one row on, onto the header the gap introduces")
	assert.True(t, sb.onGroupHeader())
}

// If the cursor ever does end up on a gap, the row has to show it. The
// invariant says it cannot happen, and the two things upholding that are a skip
// in step() and a nudge in refilter() — so the failure mode is worth degrading
// gracefully: a highlighted blank line, not a cursor that is nowhere.
func TestSidebar_ASelectedGapStillShowsTheCursor(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(20)

	gap := -1
	for i := range sb.rows {
		if sb.rows[i].kind == rowSpacer {
			gap = i
			break
		}
	}
	require.Positive(t, gap)

	sb.selected = gap // deliberately, bypassing every guard
	lines := strings.Split(sb.render(31), "\n")
	require.Len(t, lines, len(sb.rows))

	assert.NotEqual(t, strings.Repeat(" ", 31), lines[gap],
		"a selected gap must not render as plain spaces")
	assert.Empty(t, strings.TrimSpace(plain(lines[gap])), "but is still blank text")
}

// The window never starts on a blank line. scrollToCursor's clamp is kind-blind,
// so with the cursor near the end of a short pane the pinned top row can be a
// gap — spending a third of a three-row pane on a separator that separates
// nothing visible, the same waste a gap above the first group was rejected for.
func TestSidebar_WindowNeverStartsOnAGap(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(3)

	// The reported case: the cursor on the very last service, where the clamp
	// has no freedom and lands on the gap before the last header.
	sb.selectServiceByName("scratch")
	first, last := sb.window()
	shape := rowShape(sb)
	require.Less(t, first, last)
	assert.NotEqual(t, "gap", shape[first],
		"window starts on a gap: %v", shape[first:last])
	assert.Contains(t, shape[first:last], "svc:scratch", "and the cursor is still shown")

	// Exhaustively: no cursor position, at any pane height, starts the window
	// on a gap.
	for _, rows := range []int{1, 2, 3, 4, 5, 20} {
		sb.setRows(rows)
		for i := range sb.rows {
			if sb.rows[i].kind == rowSpacer {
				continue
			}
			sb.selected = i
			sb.scrollToCursor()
			first, last := sb.window()
			if first >= last {
				continue
			}
			require.NotEqualf(t, rowSpacer, sb.rows[first].kind,
				"paneRows=%d cursor=%d starts the window on a gap", rows, i)
			require.GreaterOrEqualf(t, i, first, "cursor %d above the window", i)
			require.Lessf(t, i, last, "cursor %d below the window", i)
		}
	}
}

// Pushing the leading gap off the top puts the blank at the bottom instead,
// where the pane's own padding makes it read as the end of the list rather than
// as a stray separator.
func TestSidebar_LeadingGapBecomesTrailingBlank(t *testing.T) {
	sb := &sidebar{}
	sb.update(groupedServices(), nil)
	sb.setRows(3)
	sb.selectServiceByName("scratch")

	first, last := sb.window()
	assert.Equal(t, len(sb.rows), last, "the window runs to the end of the list")
	assert.Less(t, last-first, 3, "showing fewer rows than the pane holds")

	// Which the pane then pads, so the blank is below the content.
	lines := strings.Split(plain(sb.render(31)), "\n")
	assert.Len(t, lines, last-first)
}
