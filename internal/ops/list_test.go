package ops

import (
	"errors"
	"os"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intp(n int) *int { return &n }

func TestScopeToRegistry_FiltersFillsAndSorts(t *testing.T) {
	reg := &config.Registry{Services: map[string]*config.ServiceConfig{
		"web": {Name: "web", Group: "g"},
		"api": {Name: "api"},
	}}
	got := ScopeToRegistry([]ipc.ServiceInfo{
		{Name: "web", State: "running", PID: intp(10)},
		{Name: "other", State: "running"},
	}, reg)
	require.Len(t, got, 2)
	assert.Equal(t, "api", got[0].Name)
	assert.Equal(t, "stopped", got[0].State, "never started → stopped")
	assert.Equal(t, "web", got[1].Name)
	assert.Equal(t, "g", got[1].Group, "the group comes from the config")
	assert.Equal(t, 10, *got[1].PID)
}

// With no daemon to ask, List falls back to the state file and says so. A
// process the state file thinks is running but which is gone reads as crashed.
func TestListOffline_FromStateFile(t *testing.T) {
	sandbox(t)
	st := &config.State{Services: map[string]*config.ServiceState{
		"gone": {Status: config.StatusRunning, PID: intp(999999)},
		"done": {Status: config.StatusExited},
	}}
	require.NoError(t, config.SaveState(config.StatePath(), st))
	reg := &config.Registry{Services: map[string]*config.ServiceConfig{
		"gone": {Name: "gone"}, "done": {Name: "done"}, "new": {Name: "new"},
	}}

	res, err := listOffline(reg)
	require.NoError(t, err)
	assert.True(t, res.Offline)
	states := map[string]string{}
	for _, s := range res.Services {
		states[s.Name] = s.State
	}
	assert.Equal(t, map[string]string{"gone": "crashed", "done": "exited", "new": "stopped"}, states)
}

func TestServiceState_ReadsTheStateFile(t *testing.T) {
	sandbox(t)
	ss, err := ServiceState("web")
	require.NoError(t, err)
	assert.Nil(t, ss, "never started")

	require.NoError(t, config.SaveState(config.StatePath(), &config.State{Services: map[string]*config.ServiceState{
		"web": {Status: config.StatusCrashed},
	}}))
	ss, err = ServiceState("web")
	require.NoError(t, err)
	assert.Equal(t, config.StatusCrashed, ss.Status)
}

// Stop and StopTarget never start a daemon; without one they report
// ErrNoDaemon while keeping the connection error's own text.
func TestStop_WithoutDaemonIsErrNoDaemon(t *testing.T) {
	sandbox(t)
	_, err := Stop("web")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoDaemon))
	assert.Contains(t, err.Error(), "connect to daemon: ")

	err = StopTarget("t")
	assert.True(t, errors.Is(err, ErrNoDaemon))

	_, statErr := os.Stat(config.SocketPath())
	assert.True(t, os.IsNotExist(statErr), "no daemon was started to answer")
}

func TestActiveTargets_WithoutDaemonIsEmpty(t *testing.T) {
	sandbox(t)
	assert.Empty(t, ActiveTargets())
}
