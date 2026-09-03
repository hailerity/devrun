package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

func TestModel_ScopedServices_FiltersToRegistry(t *testing.T) {
	m := model{registry: &config.Registry{Services: map[string]*config.ServiceConfig{
		"web": {Name: "web", Group: "proj"},
		"api": {Name: "api", Group: "proj"},
	}}}

	got := m.scopedServices([]ipc.ServiceInfo{
		{Name: "web", State: "running"},
		{Name: "other", State: "running"}, // not in registry — dropped
	})

	assert.Len(t, got, 2)
	assert.Equal(t, "api", got[0].Name) // sorted, filled in as stopped
	assert.Equal(t, string(config.StatusStopped), got[0].State)
	assert.Equal(t, "proj", got[0].Group)
	assert.Equal(t, "web", got[1].Name)
	assert.Equal(t, "running", got[1].State)
}

func TestModel_ScopedServices_NilRegistryPassesThrough(t *testing.T) {
	m := model{}
	in := []ipc.ServiceInfo{{Name: "a"}, {Name: "b"}}
	assert.Equal(t, in, m.scopedServices(in))
}

func TestModel_ScopedServices_EmptyRegistryHidesEverything(t *testing.T) {
	m := model{registry: &config.Registry{Services: map[string]*config.ServiceConfig{}}}
	got := m.scopedServices([]ipc.ServiceInfo{{Name: "a", State: "running"}})
	assert.Empty(t, got)
}

func TestModel_DaemonErrorEndsLoadingState(t *testing.T) {
	m := model{}
	assert.False(t, m.sidebarC.loaded)

	m2, _ := m.Update(daemonErrMsg{err: assert.AnError})
	m = m2.(model)
	assert.True(t, m.sidebarC.loaded, "a first-poll error ends the loading state")

	out := plain(m.sidebarC.render(28, 24, false))
	assert.Contains(t, out, "devrun add", "sidebar shows the empty state, not a spinner, after the error")
}

func TestModel_WindowSizeSetsWidthHeight(t *testing.T) {
	m := model{}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m2.(model)
	assert.Equal(t, 120, mm.width)
	assert.Equal(t, 40, mm.height)
}

func TestModel_SidebarWidth_Adaptive(t *testing.T) {
	mk := func(names ...string) model {
		m := model{width: 200}
		for _, n := range names {
			m.sidebarC.allServices = append(m.sidebarC.allServices, ipc.ServiceInfo{Name: n})
		}
		return m
	}

	// Short names → floor.
	assert.Equal(t, sidebarMinW, mk("web", "api", "db").sidebarWidth())

	// A long name grows the sidebar (longest name + dot + space + margin).
	assert.Equal(t, len("my-really-long-service")+3, mk("api", "my-really-long-service").sidebarWidth())

	// Pathologically long name → capped at the ceiling.
	assert.Equal(t, sidebarMaxW, mk("this-name-is-absurdly-long-and-keeps-going-forever").sidebarWidth())

	// Never wider than a third of the terminal.
	narrow := mk("this-name-is-absurdly-long-and-keeps-going-forever")
	narrow.width = 60
	assert.Equal(t, 20, narrow.sidebarWidth())
}

func TestModel_QuitKeyReturnsQuitCmd(t *testing.T) {
	m := model{}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd)
}

// setupLogModel returns a model sized to 100x30 with the log tab active and
// 20 log lines pre-loaded, ready for mouse/keyboard testing.
func setupLogModel() model {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(model)
	m.activeTab = tabLogs
	for i := 0; i < 20; i++ {
		m.logsC.sb.lines = append(m.logsC.sb.lines, "line")
	}
	m.logsC.sb.followMode = false
	m.logsC.noLogMsg = ""
	return m
}

// TestModel_MouseClick_SetsCorrectCursor verifies topOffset=4 (header 2 rows +
// tab-bar label+border 2 rows = 4 rows above log content; no bubbletea clipping
// since total render equals terminal height exactly).
func TestModel_MouseClick_SetsCorrectCursor(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain

	// Click on the first visible log line (terminal row 4).
	m2, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      4, // first log line: topOffset(4) + lineIdx(0)
	})
	mm := m2.(model)
	assert.Equal(t, 0, mm.logsC.sb.cursor, "clicking terminal row 4 should select log line index 0")

	// Click on the fifth visible log line (terminal row 8 = topOffset 4 + index 4).
	m3, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      8, // topOffset(4) + lineIdx(4)
	})
	mm3 := m3.(model)
	assert.Equal(t, 4, mm3.logsC.sb.cursor, "clicking terminal row 8 should select log line index 4")
}

// TestModel_CtrlC_CopiesWhenVisualModeActive verifies that ctrl+c (Cmd+C on
// macOS / Ctrl+Shift+C on Ubuntu when forwarded by the terminal) copies the
// visual selection rather than quitting, when focus is on the log panel.
func TestModel_CtrlC_CopiesWhenVisualModeActive(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain
	m.logsC.sb.visualMode = true
	m.logsC.sb.selStart = 1
	m.logsC.sb.selEnd = 3

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mm := m2.(model)

	assert.Nil(t, cmd, "ctrl+c with visual selection should not quit")
	assert.False(t, mm.logsC.sb.visualMode, "visual mode should be exited after copy")
	// clipboard{} has no backend, so the toast is "No clipboard available"
	assert.Equal(t, "No clipboard available", mm.footerC.toast)
}

// TestModel_CtrlC_QuitsWhenNoVisualMode verifies that ctrl+c without an active
// visual selection still quits as normal.
func TestModel_CtrlC_QuitsWhenNoVisualMode(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain
	// visualMode is false (default)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd, "ctrl+c without visual selection should quit")
}

// TestModel_TabTogglesFocus verifies Tab is a 2-way focus toggle between the
// sidebar and the main panel, and that the main panel is always LOGS.
func TestModel_TabTogglesFocus(t *testing.T) {
	m := model{}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)

	assert.Equal(t, focusSidebar, m.focus)
	assert.Equal(t, tabLogs, m.activeTab)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusMain, m.focus)
	assert.Equal(t, tabLogs, m.activeTab)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusSidebar, m.focus)
}

// TestModel_EnterShowsDetailsWithoutFocus verifies Enter shows DETAILS while
// leaving focus on the sidebar — DETAILS is a read-only overlay, not focusable.
func TestModel_EnterShowsDetailsWithoutFocus(t *testing.T) {
	m := model{}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabDetails, m.activeTab)
	assert.Equal(t, focusSidebar, m.focus, "DETAILS is not a focus target")

	// Esc back to LOGS.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.Equal(t, tabLogs, m.activeTab)
	assert.Equal(t, focusSidebar, m.focus)
}

// TestModel_EnterTogglesDetails verifies Enter flips LOGS <-> DETAILS both ways.
func TestModel_EnterTogglesDetails(t *testing.T) {
	m := model{focus: focusSidebar, activeTab: tabLogs}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabDetails, m.activeTab)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabLogs, m.activeTab, "Enter again returns to LOGS")
	assert.Equal(t, focusSidebar, m.focus)
}

// TestModel_EnterSelectsTargetFilter verifies Enter on a target row toggles the
// service filter instead of the LOGS/DETAILS view.
func TestModel_EnterSelectsTargetFilter(t *testing.T) {
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}, "api": {Name: "api"}},
		Targets:  map[string][]string{"frontend": {"web"}},
	}}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m.sidebarC.update(m.scopedServices(nil), m.buildTargets(nil))
	m.sidebarC.section = sectionTargets
	m.sidebarC.targetSel = 1 // "frontend"

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, "frontend", m.sidebarC.filterTarget)
	assert.Equal(t, []string{"web"}, svcNames(&m.sidebarC))
	assert.Equal(t, tabLogs, m.activeTab, "Enter on a target must not open DETAILS")

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Empty(t, m.sidebarC.filterTarget, "Enter again clears the filter")
}

// targetFilterModel returns a 120x40 model with a "frontend" target (member:
// web) and the sidebar cursor parked on that target row, focus on the sidebar.
func targetFilterModel(t *testing.T) model {
	t.Helper()
	m := model{registry: &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}, "api": {Name: "api"}},
		Targets:  map[string][]string{"frontend": {"web"}},
	}}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m.sidebarC.update(m.scopedServices(nil), m.buildTargets(nil))
	m.sidebarC.section = sectionTargets
	m.sidebarC.targetSel = 1 // "frontend"
	m.focus = focusSidebar
	return m
}

// TestModel_EnterInMainPanelDoesNotFilter verifies the target-filter toggle is
// gated on sidebar focus: with focus on the main panel, Enter neither selects a
// filter nor opens service DETAILS — the target roll-up (PR #23) owns the pane
// while the cursor sits on a real target row.
func TestModel_EnterInMainPanelDoesNotFilter(t *testing.T) {
	m := targetFilterModel(t)
	m.focus = focusMain
	m.activeTab = tabLogs

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Empty(t, m.sidebarC.filterTarget, "Enter from the main panel must not touch the filter")
	assert.Equal(t, tabLogs, m.activeTab, "target detail owns the pane; Enter does not open service DETAILS")
}

// TestModel_EnterOnTargetClosesDetails verifies selecting a filter from the
// targets section also drops the DETAILS overlay back to LOGS.
func TestModel_EnterOnTargetClosesDetails(t *testing.T) {
	m := targetFilterModel(t)
	m.activeTab = tabDetails

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabLogs, m.activeTab, "DETAILS closes when a filter is toggled")
	assert.Equal(t, "frontend", m.sidebarC.filterTarget)
}

// TestModel_EnterFromLogsReturnsFocusToSidebar verifies that opening DETAILS
// from the focused LOGS panel hands focus back to the sidebar.
func TestModel_EnterFromLogsReturnsFocusToSidebar(t *testing.T) {
	m := model{focus: focusMain, activeTab: tabLogs}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabDetails, m.activeTab)
	assert.Equal(t, focusSidebar, m.focus)
}

// TestModel_EscCollapsesDetails verifies Esc backs DETAILS out to LOGS.
func TestModel_EscCollapsesDetails(t *testing.T) {
	m := model{focus: focusSidebar, activeTab: tabDetails}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.Equal(t, tabLogs, m.activeTab)
	assert.Equal(t, focusSidebar, m.focus)
}

// TestModel_TabCollapsesDetails verifies Tabbing into the main panel collapses
// DETAILS back to LOGS, since DETAILS is not focusable.
func TestModel_TabCollapsesDetails(t *testing.T) {
	m := model{focus: focusSidebar, activeTab: tabDetails}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusMain, m.focus)
	assert.Equal(t, tabLogs, m.activeTab, "Tab into the main panel collapses DETAILS")
}

// TestModel_EscCancelsVisualBeforeCollapsing verifies Esc cancels an active
// visual selection first and only collapses DETAILS on a later press.
func TestModel_EscCancelsVisualBeforeCollapsing(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain
	m.logsC.sb.visualMode = true

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.False(t, m.logsC.sb.visualMode, "first Esc exits visual mode")
	assert.Equal(t, tabLogs, m.activeTab, "first Esc does not touch the view")
}

// TestModel_EnterIgnoredDuringVisualSelection verifies Enter does not jump to
// DETAILS while a visual selection is in progress.
func TestModel_EnterIgnoredDuringVisualSelection(t *testing.T) {
	m := setupLogModel()
	m.focus = focusMain
	m.logsC.sb.visualMode = true

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabLogs, m.activeTab, "Enter is ignored mid visual-selection")
}

// targetFocusedModel returns a 120x40 model with one real target ("backend")
// highlighted in the sidebar's TARGETS section.
func targetFocusedModel() model {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m.sidebarC.update(
		[]ipc.ServiceInfo{
			{Name: "api", State: "running", Port: intp(8080)},
			{Name: "web", State: "stopped"},
		},
		[]sidebarTarget{
			{name: ""},
			{name: "backend", members: []string{"api"}, active: true},
		},
	)
	m.sidebarC.section = sectionTargets
	m.sidebarC.targetSel = 1
	return m
}

// TestModel_FocusedTarget verifies focusedTarget only reports a real target row.
func TestModel_FocusedTarget(t *testing.T) {
	m := targetFocusedModel()
	tgt := m.focusedTarget()
	require.NotNil(t, tgt)
	assert.Equal(t, "backend", tgt.name)

	m.sidebarC.targetSel = 0 // synthetic "All services" row
	assert.Nil(t, m.focusedTarget())

	m.sidebarC.targetSel = 1
	m.sidebarC.section = sectionServices // cursor back in SERVICES
	assert.Nil(t, m.focusedTarget())
}

// TestModel_RenderMain_TargetDetailWhenTargetFocused verifies the main pane
// shows the target roll-up (not logs) while a target row is focused.
func TestModel_RenderMain_TargetDetailWhenTargetFocused(t *testing.T) {
	m := targetFocusedModel()
	out := plain(m.renderMain(80, 24))
	assert.Equal(t, 1, strings.Count(out, "TARGET"), "the TARGET label must not be doubled")
	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "api")
	assert.Contains(t, out, ":8080")
	assert.NotContains(t, out, "LOGS")
}

// TestModel_RenderMain_LogsWhenServiceFocused verifies the main pane returns to
// logs once the cursor leaves the TARGETS section.
func TestModel_RenderMain_LogsWhenServiceFocused(t *testing.T) {
	m := targetFocusedModel()
	m.sidebarC.section = sectionServices
	assert.Contains(t, plain(m.renderMain(80, 24)), "LOGS")
}

// TestModel_EnterIgnoredWhenTargetFocused verifies Enter does not flip to
// DETAILS while a target row is focused (the pane already shows target detail).
func TestModel_EnterIgnoredWhenTargetFocused(t *testing.T) {
	m := targetFocusedModel()
	require.Equal(t, tabLogs, m.activeTab)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabLogs, m.activeTab)
}

// TestModel_TabAndRightInertWhenTargetFocused verifies focus cannot move into
// the (non-focusable) target roll-up, which would otherwise arm the hidden
// log-pane shortcuts.
func TestModel_TabAndRightInertWhenTargetFocused(t *testing.T) {
	m := targetFocusedModel()
	require.Equal(t, focusSidebar, m.focus)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, focusSidebar, m2.(model).focus, "Tab must not move focus into a focused target's pane")

	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, focusSidebar, m3.(model).focus, "→ must not move focus into a focused target's pane")
}

// allServicesRowModel returns a 120x40 model with the sidebar cursor parked on
// the synthetic "All services" row (one running service, one stopped).
func allServicesRowModel() model {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m.sidebarC.update(
		[]ipc.ServiceInfo{
			{Name: "api", State: "running", Port: intp(8080)},
			{Name: "web", State: "stopped"},
		},
		[]sidebarTarget{{name: ""}, {name: "backend", members: []string{"api"}}},
	)
	m.sidebarC.section = sectionTargets
	m.sidebarC.targetSel = 0 // "All services"
	return m
}

// TestModel_RenderMain_SummaryOnAllServicesRow verifies the main pane shows the
// SUMMARY roll-up (not logs) with a running count while the cursor sits on the
// synthetic row.
func TestModel_RenderMain_SummaryOnAllServicesRow(t *testing.T) {
	m := allServicesRowModel()
	out := plain(m.renderMain(80, 24))
	assert.Contains(t, out, "SUMMARY")
	assert.Contains(t, out, "All services")
	assert.Contains(t, out, "1 running / 2")
	assert.Contains(t, out, "api")
	assert.Contains(t, out, "web")
	assert.NotContains(t, out, "LOGS")
}

// TestModel_TabAndRightInertOnAllServicesRow verifies focus cannot slip into a
// hidden logs pane while the summary fills the main area.
func TestModel_TabAndRightInertOnAllServicesRow(t *testing.T) {
	m := allServicesRowModel()
	require.Equal(t, focusSidebar, m.focus)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, focusSidebar, m2.(model).focus, "Tab must not focus the hidden logs pane")

	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, focusSidebar, m3.(model).focus, "→ must not focus the hidden logs pane")
}

// TestModel_OptimisticAllServicesHighlight verifies s / x on the "All services"
// row flips its highlight immediately, and that the next poll reconciles it
// against live service state.
func TestModel_OptimisticAllServicesHighlight(t *testing.T) {
	m := model{registry: &config.Registry{Services: map[string]*config.ServiceConfig{
		"web": {Name: "web", Command: "x"},
		"api": {Name: "api", Command: "y"},
	}}}
	mixed := []ipc.ServiceInfo{{Name: "api", State: "running"}, {Name: "web", State: "stopped"}}
	m.sidebarC.update(mixed, m.buildTargets(mixed))
	m.sidebarC.section = sectionTargets
	m.sidebarC.targetSel = 0 // "All services"
	require.False(t, m.sidebarC.targets[0].active, "mixed state → highlight off")

	// s lights the row before any poll.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = m2.(model)
	assert.True(t, m.sidebarC.targets[0].active, "s optimistically highlights All services")

	// A poll still showing web stopped clears it again.
	m3, _ := m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{Services: mixed}})
	m = m3.(model)
	assert.False(t, m.sidebarC.targets[0].active, "poll recomputes the highlight from live state")

	// Everything running → derived highlight on; x clears it immediately.
	allUp := []ipc.ServiceInfo{{Name: "api", State: "running"}, {Name: "web", State: "running"}}
	m4, _ := m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{Services: allUp}})
	m = m4.(model)
	require.True(t, m.sidebarC.targets[0].active, "all running → highlight on")
	m5, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = m5.(model)
	assert.False(t, m.sidebarC.targets[0].active, "x un-highlights All services immediately")
}

// TestModel_MouseClick_SetsFocusMain verifies that clicking in the log area
// automatically moves focus to the main panel so that y/v/f shortcuts work.
func TestModel_MouseClick_SetsFocusMain(t *testing.T) {
	m := setupLogModel()
	m.focus = focusSidebar // start with focus on sidebar

	m2, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Y:      4,
	})
	mm := m2.(model)
	assert.Equal(t, focusMain, mm.focus, "clicking in the log area should auto-focus the main panel")
}
