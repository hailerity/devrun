package tui

import (
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

// targetFilterModel returns a 120x40 model with a "frontend" target (member:
// web), no filter applied, focus on the sidebar.
func targetFilterModel(t *testing.T) model {
	t.Helper()
	m := newModel("", &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}, "api": {Name: "api"}},
		Targets:  map[string][]string{"frontend": {"web"}},
	}, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m.sidebarC.update(m.scopedServices(nil), m.buildTargets(nil))
	return m
}

// TestModel_TargetPickerFiltersAndClears drives the picker end to end: t opens
// it, j + Enter applies the target as the filter, and picking "All services"
// clears it again.
func TestModel_TargetPickerFiltersAndClears(t *testing.T) {
	m := targetFilterModel(t)

	m = pressKey(m, 't')
	require.True(t, m.pickerC.open)
	assert.Equal(t, 0, m.pickerC.cursor, "no filter → cursor on All services")

	m = pressKey(m, 'j')
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.False(t, m.pickerC.open, "Enter closes the picker")
	assert.Equal(t, "frontend", m.sidebarC.filterTarget)
	assert.Equal(t, []string{"web"}, svcNames(&m.sidebarC))
	assert.Equal(t, tabLogs, m.activeTab)

	// Reopening parks the cursor on the active filter; k moves to All services.
	m = pressKey(m, 't')
	assert.Equal(t, 1, m.pickerC.cursor)
	m = pressKey(m, 'k')
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Empty(t, m.sidebarC.filterTarget, "All services clears the filter")
	assert.Equal(t, []string{"api", "web"}, svcNames(&m.sidebarC))
}

// TestModel_TargetPickerIsAKeyboardTrap verifies keys do not leak to the panes
// while the picker is open, and that Esc closes it without touching the filter.
func TestModel_TargetPickerIsAKeyboardTrap(t *testing.T) {
	m := targetFilterModel(t)
	m = pressKey(m, 't')

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusSidebar, m.focus, "Tab must not switch panes under the picker")

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(model)
	assert.False(t, m.pickerC.open)
	assert.Empty(t, m.sidebarC.filterTarget)
}

// TestModel_TargetPickerWithoutTargets verifies t explains itself instead of
// opening an empty picker.
func TestModel_TargetPickerWithoutTargets(t *testing.T) {
	m := newModel("", &config.Registry{
		Services: map[string]*config.ServiceConfig{"web": {Name: "web"}},
	}, config.Source{}, "", clipboard{})
	m.sidebarC.update(m.scopedServices(nil), m.buildTargets(nil))

	m = pressKey(m, 't')
	assert.False(t, m.pickerC.open)
	assert.Contains(t, m.footerC.toast, "no targets defined")
}

// TestModel_EnterTogglesDetailsWithFilterActive verifies Enter means the same
// thing on every row: it toggles DETAILS even while a target filters the list.
func TestModel_EnterTogglesDetailsWithFilterActive(t *testing.T) {
	m := targetFilterModel(t)
	m.sidebarC.setFilter("frontend")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.Equal(t, tabDetails, m.activeTab)
	assert.Equal(t, "frontend", m.sidebarC.filterTarget, "Enter must not touch the filter")
}

// TestModel_ViewShowsPickerWithCountsAndMembers verifies the picker lists every
// target with its running count and the highlighted target's members.
func TestModel_ViewShowsPickerWithCountsAndMembers(t *testing.T) {
	m := targetFilterModel(t)
	m.sidebarC.update([]ipc.ServiceInfo{
		{Name: "api", State: "running"},
		{Name: "web", State: "stopped"},
	}, m.buildTargets(nil))
	m = pressKey(m, 't')
	m = pressKey(m, 'j')

	out := plain(m.View())
	assert.Contains(t, out, "Filter by target")
	assert.Contains(t, out, "All services")
	assert.Contains(t, out, "1/2")
	assert.Contains(t, out, "frontend")
	assert.Contains(t, out, "0/1")
	assert.Contains(t, out, "members")
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
