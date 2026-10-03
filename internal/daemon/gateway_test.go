package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quietSupervisor(t *testing.T) *supervisor {
	t.Helper()
	return &supervisor{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		services: map[string]*managedService{},
	}
}

// Two concurrent lifecycle calls must not each spawn a child: the second
// assignment would orphan the first's pid, leaving a listener holding a port
// with nothing left that can stop it.
func TestGatewayOps_SerialisesLifecycle(t *testing.T) {
	s := quietSupervisor(t)

	var mu sync.Mutex
	concurrent, peak := 0, 0

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Stand in for the body of handleGatewayUp, which holds gatewayOps
			// across the spawn.
			s.gatewayOps.Lock()
			defer s.gatewayOps.Unlock()

			mu.Lock()
			concurrent++
			if concurrent > peak {
				peak = concurrent
			}
			mu.Unlock()

			mu.Lock()
			concurrent--
			mu.Unlock()
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, peak, "gateway lifecycle operations must not overlap")
}

func TestGatewayStatusLocked_NotRunning(t *testing.T) {
	s := quietSupervisor(t)
	assert.False(t, s.gatewayStatusLocked().Running, "no child, nothing running")

	// A pid that cannot be alive stands in for a child that died.
	s.gateway = &gatewayChild{pid: -1, addr: "127.0.0.1:7788", cfg: (&config.GatewayConfig{}).Defaults()}
	assert.False(t, s.gatewayStatusLocked().Running, "a dead pid is not a running gateway")
}

// os.Process.Release sets Pid to -1, so a pid read after releasing is a
// sentinel, not a process. This is the shape of the bug that made a healthy
// gateway report "not running".
func TestPidAlive(t *testing.T) {
	assert.False(t, pidAlive(-1), "the value Release leaves behind")
	assert.False(t, pidAlive(0))
	assert.True(t, pidAlive(1), "init is always there")
}

func TestSameGatewayConfig(t *testing.T) {
	a := (&config.GatewayConfig{Port: 7788}).Defaults()
	b := (&config.GatewayConfig{Port: 7788}).Defaults()
	assert.True(t, sameGatewayConfig(a, b), "an unchanged config must not restart the child")

	c := (&config.GatewayConfig{Port: 7799}).Defaults()
	assert.False(t, sameGatewayConfig(a, c))
}

// The child holds the only write end of the pipe, so its death closes it. That
// is what makes a failed spawn fail at once instead of waiting out the whole
// gatewayStartTimeout — the bug was the parent keeping its copy open.
func TestReadGatewayAddr(t *testing.T) {
	t.Run("announced", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		go func() {
			_, _ = io.WriteString(pw, "gateway listening 127.0.0.1:7788\n")
			_ = pw.Close()
		}()
		addr, err := readGatewayAddr(pr)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:7788", addr)
	})

	t.Run("child died before announcing", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		_ = pw.Close() // stand in for the child exiting

		start := time.Now()
		_, err = readGatewayAddr(pr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exited before listening")
		assert.Less(t, time.Since(start), gatewayStartTimeout/2,
			"EOF must fail fast, not wait out the start timeout")
	})

	t.Run("said something else", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		go func() {
			_, _ = io.WriteString(pw, "panic: something\n")
			_ = pw.Close()
		}()
		_, err = readGatewayAddr(pr)
		assert.ErrorContains(t, err, "panic: something")
	})
}

// shutdown must serialise with the lifecycle. Without it, an up in flight —
// holding gatewayOps with mu released to spawn — assigns its child after
// shutdown has already looked, leaving a listener that outlives the daemon.
func TestShutdown_WaitsForAnInFlightGatewayUp(t *testing.T) {
	s := quietSupervisor(t)

	started := make(chan struct{})
	finish := make(chan struct{})
	go func() {
		s.gatewayOps.Lock()
		defer s.gatewayOps.Unlock()
		close(started)
		<-finish // stand in for spawnGateway waiting on the child
	}()
	<-started

	acquired := make(chan struct{})
	go func() {
		s.gatewayOps.Lock()
		defer s.gatewayOps.Unlock()
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("shutdown took the lifecycle lock while an up held it")
	case <-time.After(50 * time.Millisecond):
	}

	close(finish)
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown never got the lifecycle lock")
	}
}

// The child names its own cause on stderr — "address already in use", a bad
// bind, a panic. Discarding it turned every start failure into the same
// contentless "it exited before listening".
func TestChildStderr(t *testing.T) {
	write := func(t *testing.T, c *childStderr, s string) {
		t.Helper()
		_, err := io.WriteString(c.f, s)
		require.NoError(t, err)
	}

	t.Run("quotes the cause without the child's own label", func(t *testing.T) {
		c, err := openChildStderr()
		require.NoError(t, err)
		t.Cleanup(c.discard)
		write(t, c, "gateway: listen on 127.0.0.1:7788: bind: address already in use\n")

		assert.Equal(t, "listen on 127.0.0.1:7788: bind: address already in use", c.firstLine())
	})

	t.Run("keeps the opening line, not the stack", func(t *testing.T) {
		c, err := openChildStderr()
		require.NoError(t, err)
		t.Cleanup(c.discard)
		write(t, c, "panic: nil map\n\ngoroutine 1 [running]:\nmain.run(...)\n")

		assert.Equal(t, "panic: nil map", c.firstLine())
	})

	t.Run("a silent child says nothing", func(t *testing.T) {
		c, err := openChildStderr()
		require.NoError(t, err)
		t.Cleanup(c.discard)

		assert.Empty(t, c.firstLine())
	})

	t.Run("discard removes the temp file", func(t *testing.T) {
		c, err := openChildStderr()
		require.NoError(t, err)
		require.True(t, c.temp)
		name := c.f.Name()
		c.closeParentCopy()
		c.discard()

		_, err = os.Stat(name)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	// DEVRUN_GATEWAY_LOG is appended to across restarts. Reading the whole file
	// would quote the *previous* child's failure at a child that is merely slow.
	t.Run("reads only what this child wrote to the log file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.err")
		require.NoError(t, os.WriteFile(path, []byte("gateway: an older failure\n"), 0o644))
		t.Setenv(gatewayLogEnv, path)

		c, err := openChildStderr()
		require.NoError(t, err)
		t.Cleanup(c.closeParentCopy)
		assert.Empty(t, c.firstLine(), "nothing written by this child yet")

		write(t, c, "gateway: the new failure\n")
		assert.Equal(t, "the new failure", c.firstLine())
	})

	// The point of setting it is to keep the output.
	t.Run("never removes the log file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.err")
		t.Setenv(gatewayLogEnv, path)

		c, err := openChildStderr()
		require.NoError(t, err)
		write(t, c, "gateway: kept\n")
		c.closeParentCopy()
		c.discard()

		kept, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(kept), "kept")
	})

	t.Run("a long stack does not read back unbounded", func(t *testing.T) {
		c, err := openChildStderr()
		require.NoError(t, err)
		t.Cleanup(c.discard)
		write(t, c, strings.Repeat("x", 4*stderrCap)+"\nlater\n")

		assert.Len(t, c.firstLine(), stderrCap, "capped, and never reaches the later line")
	})
}
