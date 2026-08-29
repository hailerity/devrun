package daemon

import (
	"encoding/json"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReexecHandoff_EnvRoundTrip(t *testing.T) {
	// Not set → not a re-exec.
	if _, ok := reexecHandoff(); ok {
		t.Fatal("reexecHandoff reported active without the env var")
	}

	port := 8080
	in := &handoffState{Version: 1, Services: []handoffService{{
		Name:   "web",
		PID:    4321,
		PTYFD:  3,
		Status: config.StatusRunning,
		Port:   &port,
		Config: &config.ServiceConfig{Name: "web", Command: "npm run dev"},
	}}}
	raw, err := json.Marshal(in)
	require.NoError(t, err)

	t.Setenv(reexecEnvActive, "1")
	t.Setenv(reexecEnvState, string(raw))

	out, ok := reexecHandoff()
	require.True(t, ok)
	require.Len(t, out.Services, 1)
	got := out.Services[0]
	assert.Equal(t, "web", got.Name)
	assert.Equal(t, 4321, got.PID)
	assert.Equal(t, 3, got.PTYFD)
	assert.Equal(t, config.StatusRunning, got.Status)
	require.NotNil(t, got.Port)
	assert.Equal(t, 8080, *got.Port)
	require.NotNil(t, got.Config)
	assert.Equal(t, "npm run dev", got.Config.Command)
}

func TestReexecHandoff_BadJSONIsSafe(t *testing.T) {
	t.Setenv(reexecEnvActive, "1")
	t.Setenv(reexecEnvState, "{not json")

	out, ok := reexecHandoff()
	assert.True(t, ok, "still a re-exec even if the payload is unreadable")
	assert.Empty(t, out.Services)
}
