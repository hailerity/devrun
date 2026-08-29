package daemon

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReexecHandoff_NotActiveWithoutEnv(t *testing.T) {
	if _, ok := reexecHandoff(); ok {
		t.Fatal("reexecHandoff reported active without the env var")
	}
}

func TestReadHandoffState_PipeRoundTrip(t *testing.T) {
	port := 8080
	in := &handoffState{Version: 1, Services: []handoffService{{
		Name:   "web",
		PID:    4321,
		PTYFD:  firstHandoffFD,
		Status: config.StatusRunning,
		Port:   &port,
		Config: &config.ServiceConfig{Name: "web", Command: "npm run dev"},
	}}}
	raw, err := json.Marshal(in)
	require.NoError(t, err)

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	go func() {
		_, _ = pw.Write(raw)
		_ = pw.Close()
	}()

	out := readHandoffState(pr) // closes pr
	require.Len(t, out.Services, 1)
	got := out.Services[0]
	assert.Equal(t, "web", got.Name)
	assert.Equal(t, 4321, got.PID)
	assert.Equal(t, firstHandoffFD, got.PTYFD)
	assert.Equal(t, config.StatusRunning, got.Status)
	require.NotNil(t, got.Port)
	assert.Equal(t, 8080, *got.Port)
	require.NotNil(t, got.Config)
	assert.Equal(t, "npm run dev", got.Config.Command)
}

func TestReadHandoffState_BadJSONIsSafe(t *testing.T) {
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	go func() {
		_, _ = pw.Write([]byte("{not json"))
		_ = pw.Close()
	}()

	out := readHandoffState(pr)
	assert.Empty(t, out.Services)
}

func TestReadHandoffState_EmptyIsSafe(t *testing.T) {
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, pw.Close()) // no data, immediate EOF

	out := readHandoffState(pr)
	assert.Empty(t, out.Services)
}
