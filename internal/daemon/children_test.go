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

// The reaper starts with the process, before the caller can store the handle.
// A child that dies in that window has its clear() run against a handle not
// yet set, and the assignment that follows would record a dead pid as live —
// status then claiming a gateway or tunnel that is gone.
func TestRecordLive(t *testing.T) {
	t.Run("a live child is recorded", func(t *testing.T) {
		var slot *gatewayChild
		child := &gatewayChild{pid: os.Getpid(), addr: "127.0.0.1:7788"}

		assert.True(t, recordLive(&slot, child, child.pid))
		require.NotNil(t, slot)
		assert.Equal(t, "127.0.0.1:7788", slot.addr)
	})

	t.Run("one that already went is not", func(t *testing.T) {
		var slot *gatewayChild
		// Reaped, so the probe gives ESRCH — the state the window produces.
		proc, err := os.StartProcess("/bin/sh", []string{"sh", "-c", "exit 0"}, &os.ProcAttr{})
		require.NoError(t, err)
		_, _ = proc.Wait()

		child := &gatewayChild{pid: proc.Pid, addr: "127.0.0.1:7788"}
		assert.False(t, recordLive(&slot, child, child.pid))
		assert.Nil(t, slot, "the handle must not hold a dead child")
	})

	t.Run("works for the tunnel slot too", func(t *testing.T) {
		var slot *tunnelChild
		assert.True(t, recordLive(&slot, &tunnelChild{pid: os.Getpid()}, os.Getpid()))
		assert.NotNil(t, slot)
	})
}
