package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Concurrent `start` requests for the same service must produce exactly one
// running process. The pre-fix code released s.mu between the "already running"
// check and the map write, so every caller spawned; all but the last were
// dropped from s.services and leaked.
func TestHandleStart_ConcurrentSameServiceStartsOnce(t *testing.T) {
	sup := newSupervisor("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	sup.statePath = filepath.Join(t.TempDir(), "state.json")

	payload, err := json.Marshal(ipc.StartPayload{
		Name:   "web",
		Config: &config.ServiceConfig{Name: "web", Command: "sleep 30"},
	})
	require.NoError(t, err)

	const n = 8
	var wg sync.WaitGroup
	resps := make([]*ipc.Response, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resps[i] = sup.handleStart(payload)
		}(i)
	}
	wg.Wait()

	oks, alreadyRunning := 0, 0
	for _, r := range resps {
		if r.OK {
			oks++
			continue
		}
		if strings.Contains(r.Error, "already running") {
			alreadyRunning++
		}
	}
	assert.Equal(t, 1, oks, "exactly one start should succeed")
	assert.Equal(t, n-1, alreadyRunning, "the rest should report already running")

	require.Len(t, sup.services, 1)
	svc := sup.services["web"]
	require.NotNil(t, svc)
	require.NotNil(t, svc.proc)

	// Clean up: watchExit owns Wait, so Stop's SIGTERM is reaped.
	pid := svc.proc.Cmd.Process.Pid
	require.NoError(t, svc.proc.Stop())
	assert.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil },
		5*time.Second, 20*time.Millisecond)
}
