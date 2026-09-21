package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	}, nil)
	assert.Equal(t, []string{"api", "web", "zoo"}, svcNames(sb))
}

func TestSidebar_SelectionPreservedByName(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}}, nil)
	sb.selected = 1 // "web"

	sb.update([]ipc.ServiceInfo{{Name: "zoo"}, {Name: "web", State: "running"}, {Name: "api"}}, nil)
	assert.Equal(t, 1, sb.selected) // still index of "web" after re-sort
}

func TestSidebar_SelectionFallsBackWhenServiceGone(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}}, nil)
	sb.selected = 2 // "zoo"

	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}}, nil)
	assert.Equal(t, 0, sb.selected) // "zoo" gone, falls back to 0
}

func TestSidebar_MoveUpDownWraps(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}}, nil)

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

func TestSidebar_LoadingBeforeFirstPoll(t *testing.T) {
	sb := &sidebar{}
	out := plain(sb.render(28))
	assert.Contains(t, out, "Loading services…")
	assert.NotContains(t, out, "devrun add")
}

func TestSidebar_EmptyStateAfterFirstPoll(t *testing.T) {
	sb := &sidebar{}
	sb.update(nil, nil) // first poll returned zero services
	out := plain(sb.render(28))
	assert.Contains(t, out, "No services — run devrun add <name>")
	assert.NotContains(t, out, "Loading")
}

func intp(n int) *int { return &n }

func TestSidebar_CrashedSortsFirst(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "api", State: "running"},
		{Name: "web", State: "crashed"},
		{Name: "chat", State: "crashed"},
		{Name: "db", State: "stopped"},
	}, nil)
	assert.Equal(t, []string{"chat", "web", "api", "db"}, svcNames(sb),
		"crashed services lead, alphabetical within each group")
}

// A service that crashes jumps to the top of the list; the highlight must go
// with it rather than stay on whatever row now holds its old index.
func TestSidebar_CursorFollowsServiceThatCrashes(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api", State: "running"}, {Name: "web", State: "running"}}, nil)
	sb.selected = 1 // "web"

	sb.update([]ipc.ServiceInfo{{Name: "api", State: "running"}, {Name: "web", State: "crashed"}}, nil)
	assert.Equal(t, "web", sb.selectedService().Name)
	assert.Equal(t, 0, sb.selected)
}

func TestServiceRow_ShowsPortStateAndCPU(t *testing.T) {
	running := plain(serviceRow(30, ipc.ServiceInfo{Name: "api", State: "running", Port: intp(8080), CPUPct: 2.14}, false))
	assert.Contains(t, running, "● api")
	assert.Contains(t, running, ":8080")
	assert.Contains(t, running, "2.1%")

	crashed := plain(serviceRow(30, ipc.ServiceInfo{Name: "chat", State: "crashed", CPUPct: 9}, false))
	assert.Contains(t, crashed, "✖ chat")
	assert.Contains(t, crashed, "crashed")
	assert.NotContains(t, crashed, "%", "a service that is not running has no CPU figure")
}

// Every row — selected or not, any state — must be exactly the pane width, or
// the selection background stops short and the bordered pane's edge drifts.
func TestServiceRow_AlwaysExactlyWidth(t *testing.T) {
	svcs := []ipc.ServiceInfo{
		{Name: "api", State: "running", Port: intp(8080), CPUPct: 100},
		{Name: "a-service-with-a-very-long-name-indeed", State: "stopping"},
		{Name: "x"},
		// Two columns per rune: padding or truncating by rune count overshoots.
		{Name: "決済", State: "running", Port: intp(9000), CPUPct: 3},
		{Name: "決済サービス-とても長い名前のサービス", State: "crashed"},
		{Name: "🚀-rocket", State: "running"},
	}
	for _, w := range []int{8, 17, 18, 25, 26, 30, 44} {
		for _, svc := range svcs {
			for _, sel := range []bool{false, true} {
				assert.Equal(t, w, lipgloss.Width(serviceRow(w, svc, sel)), "width %d, %s, selected=%v", w, svc.Name, sel)
			}
		}
	}
}

func TestServiceRow_NarrowDropsCPUThenState(t *testing.T) {
	svc := ipc.ServiceInfo{Name: "api", State: "running", Port: intp(8080), CPUPct: 12.5}
	assert.Contains(t, plain(serviceRow(rowMinWForCPU, svc, false)), "12.5%")

	noCPU := plain(serviceRow(rowMinWForCPU-1, svc, false))
	assert.NotContains(t, noCPU, "12.5%")
	assert.Contains(t, noCPU, ":8080")

	assert.NotContains(t, plain(serviceRow(rowMinWForState-1, svc, false)), ":8080")
}

func TestCPUColor_OnlyBusyIsColoured(t *testing.T) {
	assert.Equal(t, colorMuted, cpuColor(0))
	assert.Equal(t, colorMuted, cpuColor(50))
	assert.Equal(t, colorYellow, cpuColor(50.1))
	assert.Equal(t, colorRed, cpuColor(80.1))
}

func TestSidebar_NoInfoBlockOrDuplicateHints(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api", State: "running"}, {Name: "web"}}, nil)
	out := plain(sb.render(30))
	assert.NotContains(t, out, "PID")
	assert.NotContains(t, out, "start", "s/x hints live in the footer only")
}

// State must be readable from the glyph's shape alone — with --no-color, or for
// a colour-blind user, a crashed service cannot look like a stopped one.
func TestStateGlyph_DistinctShapePerState(t *testing.T) {
	seen := map[string]string{}
	for _, st := range []string{"running", "starting", "crashed", "stopped"} {
		g, _ := stateGlyph(st)
		if prev, dup := seen[g]; dup {
			t.Fatalf("%q and %q share the glyph %q", prev, st, g)
		}
		seen[g] = st
	}
	stopping, _ := stateGlyph("stopping")
	starting, _ := stateGlyph("starting")
	assert.Equal(t, starting, stopping, "both transitions use the in-progress glyph")
	exited, _ := stateGlyph("exited")
	stopped, _ := stateGlyph("stopped")
	assert.Equal(t, stopped, exited, "any not-running state falls back to the hollow glyph")
}

// truncateName must never return more than w display columns, including for
// names whose runes are two columns wide.
func TestTruncateName_WideRunesNeverExceedWidth(t *testing.T) {
	name := "決済サービス-とても長い名前"
	for w := 1; w <= lipgloss.Width(name)+2; w++ {
		got := truncateName(name, w)
		assert.LessOrEqual(t, lipgloss.Width(got), w, "w=%d got %q", w, got)
	}
	assert.Equal(t, name, truncateName(name, lipgloss.Width(name)), "a name that fits is untouched")
}

func TestPadRight_CountsDisplayColumns(t *testing.T) {
	assert.Equal(t, 8, lipgloss.Width(padRight("決済", 8)))
	assert.Equal(t, "ab  ", padRight("ab", 4))
	assert.Equal(t, "toolong", padRight("toolong", 3), "never truncates")
}

func manyServices(n int) []ipc.ServiceInfo {
	out := make([]ipc.ServiceInfo, n)
	for i := range out {
		out[i] = ipc.ServiceInfo{Name: fmt.Sprintf("svc-%02d", i), State: "stopped"}
	}
	return out
}

// The pane clips the list to its height, so the sidebar must scroll: wherever
// the cursor goes, its row has to be inside the rendered window.
func TestSidebar_WindowFollowsCursor(t *testing.T) {
	sb := &sidebar{}
	sb.update(manyServices(40), nil)
	sb.setRows(10)

	visible := func() bool {
		first, last := sb.window()
		return sb.selected >= first && sb.selected < last && last-first == 10
	}
	for i := 0; i < 45; i++ { // past the end: wraps back to the top
		require.True(t, visible(), "moving down, selected=%d top=%d", sb.selected, sb.top)
		sb.moveDown()
	}
	for i := 0; i < 45; i++ { // and back up through the wrap to the bottom
		require.True(t, visible(), "moving up, selected=%d top=%d", sb.selected, sb.top)
		sb.moveUp()
	}
}

func TestSidebar_WindowMovesOnlyWhenItMust(t *testing.T) {
	sb := &sidebar{}
	sb.update(manyServices(40), nil)
	sb.setRows(10)

	for i := 0; i < 9; i++ {
		sb.moveDown()
	}
	assert.Equal(t, 0, sb.top, "the cursor reaches the last visible row without scrolling")
	sb.moveDown()
	assert.Equal(t, 1, sb.top, "one more row scrolls by exactly one")
	sb.moveUp()
	assert.Equal(t, 1, sb.top, "moving back inside the window does not scroll")
}

func TestSidebar_WindowSurvivesShrinkingListAndResize(t *testing.T) {
	sb := &sidebar{}
	sb.update(manyServices(40), nil)
	sb.setRows(10)
	sb.selected = 39
	sb.scrollToCursor()
	require.Equal(t, 30, sb.top)

	// The list shrinks under the cursor: no blank rows, cursor still visible.
	sb.update(manyServices(12), nil)
	first, last := sb.window()
	assert.Equal(t, 10, last-first, "the window stays full while there are rows to fill it")
	assert.True(t, sb.selected >= first && sb.selected < last)

	// A taller pane than the list shows everything from the top.
	sb.setRows(50)
	first, last = sb.window()
	assert.Equal(t, 0, first)
	assert.Equal(t, 12, last)
}

func TestSidebar_FooterSaysWhenTheListIsWindowed(t *testing.T) {
	sb := &sidebar{}
	sb.update(manyServices(40), nil)
	sb.setRows(10)
	assert.Contains(t, plain(sb.frame(true).footRight), "1–10 of 40")

	sb.update(manyServices(5), nil)
	assert.Empty(t, sb.frame(true).footRight, "nothing to say when every row fits")
}
