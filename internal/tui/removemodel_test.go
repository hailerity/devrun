package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

func pressD(t *testing.T, m model) model {
	t.Helper()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	return m2.(model)
}

func TestModel_RemoveOpensConfirmAndCancels(t *testing.T) {
	m := pressD(t, mustFirst(editModel(t, "run")))
	require.True(t, m.removeC.open)
	assert.Equal(t, "web", m.removeC.name)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m2.(model).removeC.open, "Esc closes the confirm")
}

func TestModel_RemoveConfirmCancelWithN(t *testing.T) {
	m, dir := editModel(t, "run")
	m = pressD(t, m)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = m2.(model)

	assert.False(t, m.removeC.open)
	assert.Contains(t, m.registry.Services, "web", "n leaves the service in place")
	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Services, "web", "devrun.yaml untouched")
}

func TestModel_RemoveKeyIgnoredWhenSidebarNotFocused(t *testing.T) {
	m := mustFirst(editModel(t, "run"))
	m.focus = focusMain
	assert.False(t, pressD(t, m).removeC.open, "d is inert unless a service row is focused")
}

func TestModel_RemoveRefusesRunningService(t *testing.T) {
	m, dir := editModelState(t, "run", "running")
	m = pressD(t, m)

	assert.False(t, m.removeC.open, "a running service cannot be removed")
	assert.Contains(t, m.footerC.toast, "stop web")
	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Services, "web", "devrun.yaml untouched")
}

func TestModel_RemoveConfirmPersistsAndMirrors(t *testing.T) {
	m, dir := editModel(t, "run")
	m = pressD(t, m)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = m2.(model)

	assert.False(t, m.removeC.open)
	assert.NotContains(t, m.registry.Services, "web", "in-memory registry mirrors the deletion")
	assert.Equal(t, "removed web", m.footerC.toast)
	assert.NotNil(t, cmd, "a daemon-notify + poll command is issued")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.NotContains(t, proj.Services, "web", "devrun.yaml updated")
}

func TestModel_RemoveRechecksRunningAtConfirm(t *testing.T) {
	m, dir := editModel(t, "run") // snapshot: web stopped
	m = pressD(t, m)
	require.True(t, m.removeC.open)

	// The service starts while the modal sits open — a later poll reflects it.
	m2, _ := m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{
		Services: []ipc.ServiceInfo{{Name: "web", State: "running"}},
	}})
	m = m2.(model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = m2.(model)

	assert.True(t, m.removeC.open, "confirm stays open when the service is now running")
	assert.Contains(t, m.removeC.errMsg, "running")
	assert.Contains(t, m.registry.Services, "web", "registry untouched")
	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Services, "web", "devrun.yaml untouched")
}

func TestModel_RemoveDaemonNotifySurfacesError(t *testing.T) {
	m, _ := editModel(t, "run")
	m.socketPath = filepath.Join(t.TempDir(), "nonexistent.sock")

	cmd := m.doRemoveFromDaemon("web")
	require.NotNil(t, cmd)
	msg := cmd()
	_, isErr := msg.(daemonErrMsg)
	assert.True(t, isErr, "an unreachable daemon surfaces as daemonErrMsg, not a swallowed error")
}

func TestModel_RemoveKeyIgnoredWithoutRegistry(t *testing.T) {
	m := newModel("", nil, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m2.(model)
	m2, _ = m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{
		Services: []ipc.ServiceInfo{{Name: "web", State: "stopped"}},
	}})
	m = m2.(model)

	assert.False(t, pressD(t, m).removeC.open, "d is inert without a registry")
}
