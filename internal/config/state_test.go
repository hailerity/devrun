package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestState_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	pid := 12041
	port := 3000
	now := time.Now().UTC().Truncate(time.Second)

	s := &config.State{
		Version: 1,
		Services: map[string]*config.ServiceState{
			"web": {
				Status:    config.StatusRunning,
				PID:       &pid,
				Port:      &port,
				StartedAt: &now,
			},
		},
	}
	require.NoError(t, config.SaveState(path, s))

	loaded, err := config.LoadState(path)
	require.NoError(t, err)
	assert.Equal(t, config.StatusRunning, loaded.Services["web"].Status)
	assert.Equal(t, pid, *loaded.Services["web"].PID)
	assert.Equal(t, port, *loaded.Services["web"].Port)
}

func TestState_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := config.LoadState(path)
	require.NoError(t, err)
	assert.Empty(t, s.Services)
}

func TestState_AtomicWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := &config.State{Version: 1, Services: make(map[string]*config.ServiceState)}
	require.NoError(t, config.SaveState(path, s))
	// No .tmp file should remain after save
	_, err := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "tmp file should not exist after successful save")
}

func TestState_ReAdoptionLogic(t *testing.T) {
	// Re-adoption: alive PID → running+re_adopted, dead PID → crashed
	pid := os.Getpid() // our own PID is definitely alive
	deadPID := 99999999

	states := map[string]*config.ServiceState{
		"alive": {Status: config.StatusRunning, PID: &pid},
		"dead":  {Status: config.StatusRunning, PID: &deadPID},
	}

	config.ReAdoptServices(states)
	assert.Equal(t, config.StatusRunning, states["alive"].Status)
	assert.True(t, states["alive"].ReAdopted)
	assert.Equal(t, config.StatusCrashed, states["dead"].Status)
	assert.Nil(t, states["dead"].PID, "dead service PID should be cleared")
}
