package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSupervisor(t *testing.T) *supervisor {
	t.Helper()
	sup := newSupervisor("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	sup.statePath = filepath.Join(t.TempDir(), "state.json")
	return sup
}

// A service whose inline config carries a blank command must be rejected, not
// handed to `sh -c` (which would exit 0 immediately and look like a clean run).
func TestHandleStart_RejectsEmptyCommand(t *testing.T) {
	sup := newTestSupervisor(t)

	for _, command := range []string{"", "   \n\t"} {
		payload, err := json.Marshal(ipc.StartPayload{
			Name:   "web",
			Config: &config.ServiceConfig{Name: "web", Command: command},
		})
		require.NoError(t, err)

		resp := sup.handleStart(payload)
		require.False(t, resp.OK, "command %q should be rejected", command)
		assert.Contains(t, resp.Error, "web")
		assert.NotContains(t, sup.services, "web", "no service should be registered")
	}
}
