package tui

import (
	"errors"
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

// confirmY presses y on the open remove modal and, when that dispatches a
// daemon round-trip, delivers its serviceRemovedMsg reply back to the model.
func confirmY(t *testing.T, m model) model {
	t.Helper()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = m2.(model)
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	m2, _ = m.Update(msg)
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

func TestModel_RemoveRefusesStartingService(t *testing.T) {
	m, dir := editModelState(t, "run", "starting")
	m = pressD(t, m)

	assert.False(t, m.removeC.open, "a starting service is also refused (the daemon rejects it)")
	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Services, "web", "devrun.yaml untouched")
}

func TestModel_RemoveConfirmPersistsAndMirrors(t *testing.T) {
	m, dir := editModel(t, "run")
	m = pressD(t, m)
	m = confirmY(t, m)

	assert.False(t, m.removeC.open)
	assert.False(t, m.removeC.pending)
	assert.NotContains(t, m.registry.Services, "web", "in-memory registry mirrors the deletion")
	assert.Equal(t, "removed web", m.footerC.toast)

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.NotContains(t, proj.Services, "web", "devrun.yaml updated")
}

func TestModel_RemoveRechecksBusyAtConfirm(t *testing.T) {
	m, dir := editModel(t, "run") // snapshot: web stopped
	m = pressD(t, m)
	require.True(t, m.removeC.open)

	// The service starts while the modal sits open — a later poll reflects it.
	m2, _ := m.Update(daemonRespMsg{payload: ipc.ListResponsePayload{
		Services: []ipc.ServiceInfo{{Name: "web", State: "running"}},
	}})
	m = m2.(model)

	m = confirmY(t, m)

	assert.True(t, m.removeC.open, "confirm stays open when the service is now running")
	assert.False(t, m.removeC.pending)
	assert.Contains(t, m.removeC.errMsg, "running")
	assert.Contains(t, m.registry.Services, "web", "registry untouched")
	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Services, "web", "devrun.yaml untouched")
}

func TestModel_RemoveDaemonRejectionKeepsModalOpen(t *testing.T) {
	m, dir := editModel(t, "run")
	m = pressD(t, m)

	// The daemon has the final say: it refuses because the service is running.
	m2, _ := m.Update(serviceRemovedMsg{name: "web", err: errors.New("web is running; stop it before removing")})
	m = m2.(model)

	assert.True(t, m.removeC.open, "a daemon rejection keeps the modal open")
	assert.False(t, m.removeC.pending)
	assert.Contains(t, m.removeC.errMsg, "running")
	assert.Contains(t, m.registry.Services, "web", "registry untouched on rejection")
	proj, _ := config.LoadProject(dir)
	assert.Contains(t, proj.Services, "web", "devrun.yaml untouched on rejection")
}

func TestModel_RemoveUnreachableDaemonStillRemoves(t *testing.T) {
	m, dir := editModel(t, "run")
	m.socketPath = filepath.Join(t.TempDir(), "nonexistent.sock")
	m = pressD(t, m)
	m = confirmY(t, m)

	assert.False(t, m.removeC.open, "an unreachable daemon supervises nothing, so removal proceeds")
	proj, _ := config.LoadProject(dir)
	assert.NotContains(t, proj.Services, "web", "devrun.yaml updated")
}

func TestModel_RemovePendingIgnoresKeys(t *testing.T) {
	m, _ := editModel(t, "run")
	m = pressD(t, m)
	m.removeC.pending = true

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	assert.True(t, m2.(model).removeC.open, "keys are swallowed while a confirm is in flight")
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
