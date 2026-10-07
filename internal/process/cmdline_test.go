package process

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

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

		// Start returns once the fork has happened, which on Linux is before
		// the exec — /proc/<pid>/cmdline is empty in that window. macOS hides
		// it by showing the parent's argv, so reading immediately passes
		// locally and fails in CI.
		require.Eventually(t, func() bool {
			return strings.Contains(CommandLine(cmd.Process.Pid), "sleep")
		}, 5*time.Second, 50*time.Millisecond, "the child never showed its argv")
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
