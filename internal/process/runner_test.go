package process_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunner_StartStop(t *testing.T) {
	proc, err := process.Start("sleep 60", "", nil)
	require.NoError(t, err)
	defer proc.Stop()

	// In production the daemon's watchExit goroutine owns Wait(); stand in for
	// it here so the leader-exit poll in Stop() sees the process go away
	// instead of hanging on an unreaped zombie for the full grace period.
	reaped := make(chan struct{})
	go func() { proc.Cmd.Wait(); close(reaped) }() //nolint:errcheck

	// Process should be alive
	assert.NotNil(t, proc.PTY)
	assert.NotNil(t, proc.Cmd.Process)
	err = syscall.Kill(proc.Cmd.Process.Pid, 0)
	assert.NoError(t, err, "process should be alive")

	require.NoError(t, proc.Stop())
	<-reaped

	err = syscall.Kill(proc.Cmd.Process.Pid, 0)
	assert.Error(t, err, "process should be dead after stop")
}

func TestTerminateGroup_GracefulExit(t *testing.T) {
	proc, err := process.Start("sleep 60", "", nil)
	require.NoError(t, err)
	pid := proc.Cmd.Process.Pid

	// Mirror the daemon: a concurrent reaper (watchExit) owns Wait, so the
	// zombie is cleared as soon as the process exits.
	waitErr := make(chan error, 1)
	go func() { waitErr <- proc.Cmd.Wait() }()

	killed, err := process.TerminateGroup(pid, 2*time.Second)
	require.NoError(t, err)
	assert.False(t, killed, "a process that dies on SIGTERM should not need SIGKILL")

	<-waitErr
	assert.Error(t, syscall.Kill(pid, 0), "process should be gone after SIGTERM")
}

func TestTerminateGroup_ForceKillAfterGrace(t *testing.T) {
	// A shell that ignores SIGTERM and never exits on its own.
	proc, err := process.Start("trap '' TERM; while true; do sleep 0.2; done", "", nil)
	require.NoError(t, err)
	pid := proc.Cmd.Process.Pid

	start := time.Now()
	killed, err := process.TerminateGroup(pid, 500*time.Millisecond)
	require.NoError(t, err)
	assert.True(t, killed, "a process that ignores SIGTERM must be SIGKILLed")
	assert.GreaterOrEqual(t, time.Since(start), 500*time.Millisecond, "SIGKILL must wait for the full grace period")

	proc.Cmd.Wait() //nolint:errcheck
	assert.Error(t, syscall.Kill(pid, 0), "process should be gone after SIGKILL")
}

func TestTerminateGroup_SignalsWholeGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// The shell backgrounds a long sleep, records its PID, then waits on it.
	// Job control is off for `sh -c`, so the child shares the shell's process
	// group and a group-directed signal reaches it too.
	proc, err := process.Start("sleep 120 & echo $! > "+pidFile+"; wait", "", nil)
	require.NoError(t, err)
	leader := proc.Cmd.Process.Pid

	var childPid int
	require.Eventually(t, func() bool {
		b, e := os.ReadFile(pidFile)
		if e != nil {
			return false
		}
		childPid, e = strconv.Atoi(strings.TrimSpace(string(b)))
		return e == nil && childPid > 0
	}, 2*time.Second, 20*time.Millisecond, "child PID should be recorded")

	require.NoError(t, syscall.Kill(childPid, 0), "child should be alive before terminate")

	waitErr := make(chan error, 1)
	go func() { waitErr <- proc.Cmd.Wait() }()

	_, err = process.TerminateGroup(leader, 2*time.Second)
	require.NoError(t, err)

	<-waitErr
	assert.Eventually(t, func() bool {
		return syscall.Kill(childPid, 0) != nil
	}, 2*time.Second, 20*time.Millisecond, "backgrounded child must be terminated along with the group")
}

func TestStart_ProcessIgnoresSIGHUP(t *testing.T) {
	proc, err := process.Start("sleep 60", "", nil)
	require.NoError(t, err)
	pid := proc.Cmd.Process.Pid

	reaped := make(chan struct{})
	go func() { proc.Cmd.Wait(); close(reaped) }() //nolint:errcheck
	t.Cleanup(func() {
		_, _ = process.TerminateGroup(pid, 2*time.Second)
		<-reaped
	})

	// Let the shell reach its `trap '' HUP` (the disposition is SIG_DFL for the
	// brief moment before it runs).
	time.Sleep(200 * time.Millisecond)

	// SIGHUP would kill a default-disposition process. A managed process must
	// shrug it off so it survives the daemon closing the PTY master on restart.
	require.NoError(t, syscall.Kill(pid, syscall.SIGHUP))
	time.Sleep(200 * time.Millisecond)
	assert.NoError(t, syscall.Kill(pid, 0), "managed process should ignore SIGHUP")
}
