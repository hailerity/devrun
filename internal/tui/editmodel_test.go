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

// editModel builds a 120x40 model backed by a temp project devrun.yaml holding
// one service "web" (reported "stopped"), with the sidebar focused on it.
func editModel(t *testing.T, command string) (model, string) {
	t.Helper()
	return editModelState(t, command, "stopped")
}

func editModelState(t *testing.T, command, state string) (model, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: "+command+"\n"), 0644))
	reg, src, err := config.Resolve(dir, false)
	require.NoError(t, err)

	m := newModel("", reg, src, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m2, _ = m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{
		Services: []ipc.ServiceInfo{{Name: "web", State: state}},
	}})
	return m2.(model), dir
}

func pressE(t *testing.T, m model) model {
	t.Helper()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	return m2.(model)
}

func TestModel_EditOpensPrefilledAndCancels(t *testing.T) {
	m := pressE(t, mustFirst(editModel(t, "yarn dev")))
	require.True(t, m.editC.open)
	n, c, _ := m.editC.values()
	assert.Equal(t, "web", n)
	assert.Equal(t, "yarn dev", c)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m2.(model).editC.open, "Esc closes the modal")
}

func TestModel_EditKeyIgnoredWhenSidebarNotFocused(t *testing.T) {
	m := mustFirst(editModel(t, "run"))
	m.focus = focusMain
	assert.False(t, pressE(t, m).editC.open, "e is inert unless a service row is focused")
}

func TestModel_EditSaveValidationKeepsModalOpen(t *testing.T) {
	m, dir := editModel(t, "run")
	m = pressE(t, m)
	m.editC.inputs[fieldName].SetValue("")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	assert.True(t, m.editC.open, "an invalid save keeps the modal open")
	assert.NotEmpty(t, m.editC.errMsg)

	proj, _ := config.LoadProject(dir)
	assert.Equal(t, "run", proj.Services["web"].Command, "devrun.yaml left untouched")
}

func TestModel_EditSavePersistsCommand(t *testing.T) {
	m, dir := editModel(t, "old")
	m = pressE(t, m)
	m.editC.focusDelta(1) // command field
	m.editC.inputs[fieldCommand].SetValue("new cmd")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	assert.False(t, m.editC.open)
	assert.Equal(t, "new cmd", m.registry.Services["web"].Command, "in-memory registry updated")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "new cmd", proj.Services["web"].Command, "devrun.yaml updated")
}

func TestModel_EditRenamePersists(t *testing.T) {
	m, dir := editModel(t, "run")
	m = pressE(t, m)
	m.editC.inputs[fieldName].SetValue("ui")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	assert.NotContains(t, m.registry.Services, "web")
	assert.Contains(t, m.registry.Services, "ui")

	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Services, "ui")
	assert.NotContains(t, proj.Services, "web")
}

func TestModel_EditSaveRestartsRunningService(t *testing.T) {
	m, _ := editModelState(t, "run", "running")
	m = pressE(t, m)
	m.editC.inputs[fieldName].SetValue("ui") // rename a running service

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	assert.False(t, m.editC.open)
	assert.NotNil(t, cmd, "a restart command is issued")
	assert.Contains(t, m.footerC.toast, "restarting", "toast announces the restart")
	assert.Contains(t, m.registry.Services, "ui")
}

func TestModel_EditSaveResolvesProjectCWDInMemory(t *testing.T) {
	m, dir := editModel(t, "run")
	m = pressE(t, m)
	m.editC.focusDelta(1)
	m.editC.focusDelta(1) // cwd field
	m.editC.inputs[fieldCWD].SetValue("frontend")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	assert.Equal(t, filepath.Join(dir, "frontend"), m.registry.Services["web"].CWD,
		"in-memory cwd is absolute, matching a reload")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "frontend", proj.Services["web"].CWD, "devrun.yaml keeps it relative")
}

func TestModel_EditSaveDoesNotRestartStoppedService(t *testing.T) {
	m, _ := editModelState(t, "run", "stopped")
	m = pressE(t, m)
	m.editC.focusDelta(1)
	m.editC.inputs[fieldCommand].SetValue("run2")

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)

	assert.Equal(t, "saved web", m.footerC.toast, "a stopped service is only saved, not restarted")
}

func mustFirst(m model, _ string) model { return m }

func TestModel_EditKeyIgnoredWithoutRegistry(t *testing.T) {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m2, _ = m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{
		Services: []ipc.ServiceInfo{{Name: "web", State: "stopped"}},
	}})
	m = m2.(model)
	require.NotNil(t, m.sidebarC.selectedService())

	assert.False(t, m.onServiceRow(), "no registry → nothing to persist an edit to")
	assert.False(t, pressE(t, m).editC.open, "e is inert without a registry")
}
