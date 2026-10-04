package daemon

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A child started with StartProcess stays this process's child whether or not
// the Go handle is released, so an exited one remains a zombie until reaped —
// and kill(pid, 0) answers "alive" for a zombie. Before reaping, a gateway or
// cloudflared that died reported as running for as long as the daemon lived:
// status lied, and `up` saw no reason to replace it.
func TestReap_AnExitedChildStopsProbingAsAlive(t *testing.T) {
	s := quietSupervisor(t)
	s.statePath = t.TempDir() + "/state.json"

	proc, err := os.StartProcess("/bin/sh", []string{"sh", "-c", "exit 0"}, &os.ProcAttr{
		Sys: &syscall.SysProcAttr{Setsid: true},
	})
	require.NoError(t, err)
	pid := proc.Pid

	s.mu.Lock()
	s.gateway = &gatewayChild{pid: pid, addr: "127.0.0.1:7788"}
	s.mu.Unlock()

	s.reap(proc, "gateway", s.clearGatewayPid)

	require.Eventually(t, func() bool { return !pidAlive(pid) }, 5*time.Second, 20*time.Millisecond,
		"an unreaped zombie probes as alive forever")

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Nil(t, s.gateway, "and the handle goes with it, so status is right at once")
	assert.False(t, s.gatewayStatusLocked().Running)
}

// A child that exits after a replacement has been recorded must not clear the
// replacement's handle.
func TestReap_DoesNotClearASuccessor(t *testing.T) {
	s := quietSupervisor(t)
	s.statePath = t.TempDir() + "/state.json"

	s.mu.Lock()
	s.gateway = &gatewayChild{pid: 424242, addr: "127.0.0.1:7788"}
	s.mu.Unlock()

	s.clearGatewayPid(999999) // some earlier child, long replaced

	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotNil(t, s.gateway)
	assert.Equal(t, 424242, s.gateway.pid)
}

func TestClearTunnelPid(t *testing.T) {
	s := quietSupervisor(t)
	s.statePath = t.TempDir() + "/state.json"

	s.mu.Lock()
	s.tunnel = &tunnelChild{pid: 424242, kind: KindQuick, cfg: config.TunnelConfig{}}
	s.mu.Unlock()

	s.clearTunnelPid(999999)
	s.mu.Lock()
	assert.NotNil(t, s.tunnel, "a different pid is not this tunnel")
	s.mu.Unlock()

	s.clearTunnelPid(424242)
	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Nil(t, s.tunnel)
}
