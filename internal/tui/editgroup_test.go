package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// projectEditModel is a model over a real devrun.yaml on disk, so the editor's
// save path writes a file a test can read back.
func projectEditModel(t *testing.T, yaml string) (model, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte(yaml), 0644))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	reg := &config.Registry{Version: "1", Services: proj.ToServiceConfigs(dir)}
	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}

	m := newModel("", reg, src, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m2.(model)
	m.focus = focusSidebar

	var svcs []ipc.ServiceInfo
	for name, cfg := range reg.Services {
		svcs = append(svcs, ipc.ServiceInfo{Name: name, Group: cfg.Group, State: "stopped"})
	}
	m.sidebarC.update(m.scopedServices(svcs), nil)
	m.relayout()
	return m, dir
}

// The editor prefills the group it is given, so `e` shows what a service has
// rather than an empty field that would clear it on save.
func TestEditPanel_PrefillsTheGroup(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "yarn", CWD: "/app", Group: "frontend"})

	_, _, _, group := p.values()
	assert.Equal(t, "frontend", group)
}

// A group is a display label, not an identity: it is not held to the service
// name rule, and emptying it is a legitimate edit rather than a validation
// failure.
func TestEditPanel_GroupIsNotValidatedLikeAName(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "yarn"})
	p.inputs[fieldGroup].SetValue("not a valid *service* name")

	assert.Empty(t, p.validate(map[string]bool{"web": true}),
		"the group is free-form")

	p.inputs[fieldGroup].SetValue("")
	assert.Empty(t, p.validate(map[string]bool{"web": true}),
		"and clearing it is allowed")
}

// End to end through the modal: `e`, type a group, Enter — the devrun.yaml has
// it, and the sidebar sections by it without waiting for a daemon poll.
func TestModel_EditorWritesTheGroupToTheProjectFile(t *testing.T) {
	m, dir := projectEditModel(t, "name: shop\nservices:\n  web:\n    command: yarn\n  api:\n    command: go run .\n")
	m.sidebarC.selectServiceByName("web")
	require.Equal(t, "web", m.sidebarC.selectedService().Name)

	m2, _ := m.openEditor()
	m = m2.(model)
	require.True(t, m.editC.open)
	require.Equal(t, "shop", m.editC.inputs[fieldGroup].Value(), "prefilled with the inherited group")

	m.editC.inputs[fieldGroup].SetValue("frontend")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.False(t, m.editC.open, "saved: %s", m.editC.errMsg)

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "frontend", proj.Services["web"].Group)
	assert.Empty(t, proj.Services["api"].Group, "its sibling is untouched")

	// And the in-memory view already reflects it, before any poll.
	assert.Equal(t, "frontend", m.registry.Services["web"].Group)
}

// Clearing the field is the interesting direction: a project service with no
// group inherits the project's name, and the in-memory mirror has to say so too
// — otherwise the sidebar would file it under "(no group)" until a reload.
func TestModel_EditorClearingTheGroupFallsBackToTheProjectName(t *testing.T) {
	m, dir := projectEditModel(t, "name: shop\nservices:\n  web:\n    command: yarn\n    group: frontend\n")
	m.sidebarC.selectServiceByName("web")

	m2, _ := m.openEditor()
	m = m2.(model)
	require.Equal(t, "frontend", m.editC.inputs[fieldGroup].Value())

	m.editC.inputs[fieldGroup].SetValue("")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.False(t, m.editC.open, "saved: %s", m.editC.errMsg)

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Empty(t, proj.Services["web"].Group, "the file field is cleared")
	assert.Equal(t, "shop", m.registry.Services["web"].Group,
		"and the in-memory group falls back to the project, as a reload would")
}

// The active config decides a service's group, not the daemon's copy of it:
// clearing a global group has to clear it on screen rather than leave the
// daemon's stale value showing.
func TestModel_ScopedServicesTakesTheGroupFromTheConfig(t *testing.T) {
	reg := &config.Registry{Version: "1", Services: map[string]*config.ServiceConfig{
		"web": {Name: "web", Command: "yarn"}, // no group
	}}
	m := newModel("", reg, config.Source{}, "", clipboard{})

	out := m.scopedServices([]ipc.ServiceInfo{{Name: "web", Group: "stale", State: "running"}})
	require.Len(t, out, 1)
	assert.Empty(t, out[0].Group, "the config says ungrouped, so ungrouped it is")
}
