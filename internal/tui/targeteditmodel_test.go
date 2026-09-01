package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

// targetEditModel builds a 120x40 model backed by a temp project devrun.yaml
// with services web+api and a target "fe" (member: web), the sidebar cursor
// parked on that target row.
func targetEditModel(t *testing.T) (model, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\n  api:\n    command: b\ntargets:\n  fe: [web]\n"), 0644))
	reg, src, err := config.Resolve(dir, false)
	require.NoError(t, err)

	m := newModel("", reg, src, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m2, _ = m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{
		Services:      []ipc.ServiceInfo{{Name: "web", State: "stopped"}, {Name: "api", State: "stopped"}},
		ActiveTargets: nil,
	}})
	m = m2.(model)
	// Park the cursor on the "fe" target row.
	m.sidebarC.section = sectionTargets
	for i, tt := range m.sidebarC.targets {
		if tt.name == "fe" {
			m.sidebarC.targetSel = i
		}
	}
	return m, dir
}

func pressKey(m model, r rune) model {
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return m2.(model)
}

func TestModel_TargetEditOpensPrefilledAndCancels(t *testing.T) {
	m, _ := targetEditModel(t)
	require.True(t, m.onTargetEditRow())

	m = pressKey(m, 'e')
	require.True(t, m.targetEditC.open)
	name, members := m.targetEditC.values()
	assert.Equal(t, "fe", name)
	assert.Equal(t, []string{"web"}, members)
	assert.Equal(t, []string{"api", "web"}, m.targetEditC.services)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m2.(model).targetEditC.open)
}

func TestModel_TargetEditKeyIgnoredOnAllServicesRow(t *testing.T) {
	m, _ := targetEditModel(t)
	m.sidebarC.targetSel = 0 // "All services"
	assert.False(t, m.onTargetEditRow())
	assert.False(t, pressKey(m, 'e').targetEditC.open)
}

func TestModel_TargetEditSaveTogglesMembership(t *testing.T) {
	m, dir := targetEditModel(t)
	m = pressKey(m, 'e')

	m.targetEditC.focusSwap()      // focus the list (services: api, web)
	m.targetEditC.toggleAtCursor() // api on
	m.targetEditC.moveCursor(1)    // -> web
	m.targetEditC.toggleAtCursor() // web off

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	assert.False(t, m.targetEditC.open)
	assert.Equal(t, []string{"api"}, m.registry.Targets["fe"], "in-memory registry updated")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"api"}, proj.Targets["fe"], "devrun.yaml updated")
}

func TestModel_TargetEditRenamePersists(t *testing.T) {
	m, dir := targetEditModel(t)
	m = pressKey(m, 'e')
	m.targetEditC.nameInput.SetValue("frontend")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	assert.NotContains(t, m.registry.Targets, "fe")
	assert.Contains(t, m.registry.Targets, "frontend")

	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Targets, "frontend")
	assert.NotContains(t, proj.Targets, "fe")
}

func TestModel_TargetEditSave_InMemoryOrderMatchesFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\n  api:\n    command: b\ntargets:\n  fe: [web, ghost]\n"), 0644))
	reg, src, err := config.Resolve(dir, false)
	require.NoError(t, err)

	m := newModel("", reg, src, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m2, _ = m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{
		Services: []ipc.ServiceInfo{{Name: "web", State: "stopped"}, {Name: "api", State: "stopped"}},
	}})
	m = m2.(model)
	m.sidebarC.section = sectionTargets
	for i, tt := range m.sidebarC.targets {
		if tt.name == "fe" {
			m.sidebarC.targetSel = i
		}
	}

	m = pressKey(m, 'e')
	m.targetEditC.focusSwap()      // list: api, web, ghost(missing)
	m.targetEditC.toggleAtCursor() // api on

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "ghost", "web"}, proj.Targets["fe"])
	assert.Equal(t, proj.Targets["fe"], m.registry.Targets["fe"],
		"in-memory members are stored in the same order as the file")
}

func TestModel_ApplyTargetEditToRegistry_NilMap(t *testing.T) {
	m := model{registry: &config.Registry{Services: map[string]*config.ServiceConfig{"web": {Name: "web"}}}}
	require.Nil(t, m.registry.Targets)

	m.applyTargetEditToRegistry("old", "new", []string{"web"})
	assert.Equal(t, []string{"web"}, m.registry.Targets["new"], "nil Targets map is initialised, not skipped")
}

func TestModel_TargetEditValidationKeepsModalOpen(t *testing.T) {
	m, dir := targetEditModel(t)
	m = pressKey(m, 'e')
	m.targetEditC.nameInput.SetValue("")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.True(t, m.targetEditC.open)
	assert.NotEmpty(t, m.targetEditC.errMsg)

	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Targets, "fe", "devrun.yaml untouched")
}
