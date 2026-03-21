package process_test

import (
	"syscall"
	"testing"

	"github.com/hailerity/procet/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunner_StartStop(t *testing.T) {
	proc, err := process.Start("sleep 60", "", nil)
	require.NoError(t, err)
	defer proc.Stop()

	// Process should be alive
	assert.NotNil(t, proc.PTY)
	assert.NotNil(t, proc.Cmd.Process)
	err = syscall.Kill(proc.Cmd.Process.Pid, 0)
	assert.NoError(t, err, "process should be alive")

	require.NoError(t, proc.Stop())

	// In production the daemon's watchExit goroutine calls Wait() to reap the
	// child. Here there is no supervisor, so we reap it ourselves; without this
	// the shell becomes a zombie whose PID still responds to kill -0.
	proc.Cmd.Wait() //nolint:errcheck // ignoring wait error in test cleanup

	err = syscall.Kill(proc.Cmd.Process.Pid, 0)
	assert.Error(t, err, "process should be dead after stop")
}
