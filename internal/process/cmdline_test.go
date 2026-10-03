package process

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLine(t *testing.T) {
	t.Run("reads our own argv", func(t *testing.T) {
		got := CommandLine(os.Getpid())
		require.NotEmpty(t, got)
		assert.Contains(t, got, os.Args[0])
	})

	t.Run("tells two live processes apart", func(t *testing.T) {
		// The whole point is distinguishing a recorded pid from whatever holds
		// that number now, so two different commands must read differently.
		cmd := exec.Command("sleep", "30")
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		})

		assert.Contains(t, CommandLine(cmd.Process.Pid), "sleep")
		assert.NotContains(t, CommandLine(os.Getpid()), "sleep 30")
	})

	// "" means "cannot confirm", which is what a caller about to signal a
	// process needs to hear.
	t.Run("says nothing about a pid it cannot read", func(t *testing.T) {
		for _, pid := range []int{0, -1, 1 << 30} {
			assert.Empty(t, CommandLine(pid), "pid %d", pid)
		}
	})

	t.Run("an exited process reads as gone", func(t *testing.T) {
		cmd := exec.Command("true")
		require.NoError(t, cmd.Start())
		pid := cmd.Process.Pid
		_ = cmd.Wait()

		assert.Empty(t, CommandLine(pid))
	})
}
