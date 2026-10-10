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

// The field holds a service's *own* group, so `e` shows what it has rather than
// an empty box that would clear it on save.
func TestEditPanel_PrefillsAnExplicitGroup(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "yarn", CWD: "/app", Group: "frontend"}, "shop")

	_, _, _, group := p.values()
	assert.Equal(t, "frontend", group)
}

// But an *inherited* group is shown as a placeholder, not as a value. cfg.Group
// is the derived group, so a project service that sets none arrives carrying the
// project's name — prefilling that would make editing the command write
// `group: <project>` into the committed devrun.yaml, grouping a service nobody
// asked to group and freezing it against a later rename of the project.
func TestEditPanel_LeavesAnInheritedGroupEmptyWithAPlaceholder(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "yarn", Group: "shop"}, "shop")

	_, _, _, group := p.values()
	assert.Empty(t, group, "inherited, so the field is empty")
	assert.Equal(t, "shop", p.inputs[fieldGroup].Placeholder,
		"and the inherited value is shown as the placeholder")
}

// The global registry inherits from nothing, so there is no placeholder and an
// empty field means ungrouped.
func TestEditPanel_GlobalScopeHasNoInheritedGroup(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "yarn"}, "")

	_, _, _, group := p.values()
	assert.Empty(t, group)
	assert.Empty(t, p.inputs[fieldGroup].Placeholder)
}

// A group is a display label, not an identity: it is not held to the service
// name rule, and emptying it is a legitimate edit rather than a validation
// failure.
func TestEditPanel_GroupIsNotValidatedLikeAName(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "yarn"}, "")
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
	require.Empty(t, m.editC.inputs[fieldGroup].Value(),
		"web inherits its group, so the field is empty rather than prefilled")
	require.Equal(t, "shop", m.editC.inputs[fieldGroup].Placeholder)

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

// The bug this guards: editing an unrelated field used to stamp the inherited
// group into the committed devrun.yaml, because the field was prefilled with
// the derived value and written back unconditionally. Changing only the command
// must leave the file's group exactly as it was — absent.
func TestModel_EditingAnotherFieldDoesNotStampTheInheritedGroup(t *testing.T) {
	m, dir := projectEditModel(t, "name: shop\nservices:\n  web:\n    command: yarn\n  api:\n    command: go run .\n")
	m.sidebarC.selectServiceByName("web")

	m2, _ := m.openEditor()
	m = m2.(model)
	m.editC.inputs[fieldCommand].SetValue("yarn dev") // only the command
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.False(t, m.editC.open, "saved: %s", m.editC.errMsg)

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "yarn dev", proj.Services["web"].Command, "the command did change")
	assert.Empty(t, proj.Services["web"].Group,
		"and the inherited group was not written into the file")
	assert.Empty(t, proj.Services["api"].Group)

	// The derived group is unchanged either way, so nothing moved on screen.
	assert.Equal(t, "shop", m.registry.Services["web"].Group)

	// And renaming the project still carries every service with it — which is
	// what a stamped group would have broken.
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: bazaar\nservices:\n  web:\n    command: yarn dev\n  api:\n    command: go run .\n"), 0644))
	proj, err = config.LoadProject(dir)
	require.NoError(t, err)
	cfgs := proj.ToServiceConfigs(dir)
	assert.Equal(t, "bazaar", cfgs["web"].Group)
	assert.Equal(t, "bazaar", cfgs["api"].Group)
}

// Typing the inherited name explicitly is equivalent to leaving it inherited:
// the field means "a group of its own", and the project's name is not one.
func TestModel_TypingTheInheritedNameLeavesItInherited(t *testing.T) {
	m, dir := projectEditModel(t, "name: shop\nservices:\n  web:\n    command: yarn\n")
	m.sidebarC.selectServiceByName("web")

	m2, _ := m.openEditor()
	m = m2.(model)
	m.editC.inputs[fieldGroup].SetValue("shop")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	require.False(t, m.editC.open, "saved: %s", m.editC.errMsg)

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "shop", proj.Services["web"].Group,
		"stored as given — it is what the reader typed")
	assert.Equal(t, "shop", proj.ToServiceConfigs(dir)["web"].Group)
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
