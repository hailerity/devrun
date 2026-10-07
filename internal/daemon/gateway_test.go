package daemon

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

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
	a := (&config.GatewayConfig{Port: config.GatewayPort(7788)}).Defaults()
	b := (&config.GatewayConfig{Port: config.GatewayPort(7788)}).Defaults()
	assert.True(t, sameGatewayConfig(a, b), "an unchanged config must not restart the child")

	c := (&config.GatewayConfig{Port: config.GatewayPort(7799)}).Defaults()
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
	// Each subtest gets its own data dir, so GatewayLogPath lands somewhere
	// disposable instead of in the developer's real log directory.
	fresh := func(t *testing.T) *childStderr {
		t.Helper()
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		c, err := openChildStderr(quietLogger())
		require.NoError(t, err)
		t.Cleanup(c.closeParentCopy)
		return c
	}

	t.Run("quotes the cause without the child's own label", func(t *testing.T) {
		c := fresh(t)
		write(t, c, "gateway: listen on 127.0.0.1:7788: bind: address already in use\n")

		assert.Equal(t, "listen on 127.0.0.1:7788: bind: address already in use", c.firstLine())
	})

	t.Run("keeps the opening line, not the stack", func(t *testing.T) {
		c := fresh(t)
		write(t, c, "panic: nil map\n\ngoroutine 1 [running]:\nmain.run(...)\n")

		assert.Equal(t, "panic: nil map", c.firstLine())
	})

	t.Run("a silent child says nothing", func(t *testing.T) {
		c := fresh(t)

		assert.Empty(t, c.firstLine())
	})

	// The log outlives the child, and the next child picks up after it. It
	// used to be a temp file unlinked the moment the gateway announced its
	// address, so anything written while *serving* — a handler panic and its
	// stack, a body-copy error — went to an inode nothing could open.
	t.Run("one child's output survives into the next child's run", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		first, err := openChildStderr(quietLogger())
		require.NoError(t, err)
		write(t, first, "gateway: panic: nil map\n")
		first.closeParentCopy()

		second, err := openChildStderr(quietLogger())
		require.NoError(t, err)
		t.Cleanup(second.closeParentCopy)

		assert.Equal(t, int64(len("gateway: panic: nil map\n")), second.from,
			"the second child reads from where the first stopped")
		assert.Empty(t, second.firstLine(), "and is not blamed for the first's panic")

		kept, err := os.ReadFile(GatewayLogPath())
		require.NoError(t, err)
		assert.Contains(t, string(kept), "panic: nil map", "the first child's output is still there")
	})

	// A log is a debugging aid, not a prerequisite for serving: an unopenable
	// path must not be the difference between a gateway and no gateway.
	t.Run("an unopenable log does not stop the gateway", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dir)
		// A directory where the log file goes: O_CREATE|O_WRONLY cannot open it.
		require.NoError(t, os.MkdirAll(GatewayLogPath(), 0o755))

		c, err := openChildStderr(quietLogger())
		require.NoError(t, err, "it falls back rather than failing")
		t.Cleanup(c.closeParentCopy)
		assert.Equal(t, os.DevNull, c.f.Name())
		assert.Empty(t, c.firstLine(), "nowhere to read a cause from, so none is invented")
	})

	// The file is appended to across restarts, so reading the whole of it would
	// quote the *previous* child's failure at a child that is merely slow.
	t.Run("reads only what this child wrote", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		require.NoError(t, os.MkdirAll(filepath.Dir(GatewayLogPath()), 0o755))
		require.NoError(t, os.WriteFile(GatewayLogPath(), []byte("gateway: an older failure\n"), 0o644))

		c, err := openChildStderr(quietLogger())
		require.NoError(t, err)
		t.Cleanup(c.closeParentCopy)
		assert.Empty(t, c.firstLine(), "nothing written by this child yet")

		write(t, c, "gateway: the new failure\n")
		assert.Equal(t, "the new failure", c.firstLine())
	})

	t.Run("a long stack does not read back unbounded", func(t *testing.T) {
		c := fresh(t)
		write(t, c, strings.Repeat("x", 4*stderrCap)+"\nlater\n")

		assert.Len(t, c.firstLine(), stderrCap, "capped, and never reaches the later line")
	})
}

// A process whose argv carries the marker, standing in for a gateway left
// behind by a previous daemon.
func fakeGateway(t *testing.T) int {
	t.Helper()
	return standIn(t, gatewayArgMarker)
}

// standIn starts a long-lived process whose argv carries marker, and does not
// return until the marker is actually readable from that pid.
//
// The wait is load-bearing, not caution. cmd.Start returns once the fork has
// happened, which on Linux is before the exec: /proc/<pid>/cmdline is empty
// in that window, so CommandLine reports nothing and a re-adoption check
// reads the stand-in as somebody else. macOS hides it — ps shows the parent's
// argv immediately — so it passes locally and fails in CI.
func standIn(t *testing.T, marker string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", ": "+marker+"; sleep 30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	require.Eventually(t, func() bool {
		return strings.Contains(process.CommandLine(cmd.Process.Pid), marker)
	}, 5*time.Second, 50*time.Millisecond, "the stand-in never showed %q in its argv", marker)
	return cmd.Process.Pid
}

// The gateway outlives the daemon on purpose. Without re-adoption a
// `daemon stop` or `restart` left it running and holding its port, with
// nothing able to see or stop it ever again.
func TestAdoptGateway(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("takes back a gateway that is still running", func(t *testing.T) {
		pid := fakeGateway(t)
		got := adoptGateway(&config.GatewayState{
			PID:   pid,
			Addr:  "127.0.0.1:7788",
			Token: "k_abc",
			Config: config.GatewayConfig{
				Port: config.GatewayPort(7788), Posture: config.PostureAuto, Expose: []string{"web"},
			},
		}, log)

		require.NotNil(t, got)
		assert.Equal(t, pid, got.pid)
		assert.Equal(t, "127.0.0.1:7788", got.addr)
		assert.Equal(t, "k_abc", got.token, "a link already shared has to keep working")
		assert.Equal(t, []string{"web"}, got.cfg.Expose)
	})

	// state.json can name a pid from before a reboot. Whatever holds that
	// number now would be reported as the gateway, and then signalled by
	// `devrun gateway down`.
	t.Run("refuses a live pid that is somebody else", func(t *testing.T) {
		cmd := exec.Command("sleep", "30")
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		})

		assert.Nil(t, adoptGateway(&config.GatewayState{PID: cmd.Process.Pid}, log))
	})

	t.Run("nothing recorded, nothing to take back", func(t *testing.T) {
		assert.Nil(t, adoptGateway(nil, log))
	})

	t.Run("the recorded gateway is gone", func(t *testing.T) {
		cmd := exec.Command("true")
		require.NoError(t, cmd.Start())
		pid := cmd.Process.Pid
		_ = cmd.Wait()

		assert.Nil(t, adoptGateway(&config.GatewayState{PID: pid}, log))
	})
}

// The round trip that makes a restart survivable: what `gateway up` recorded
// is what the replacement daemon reads back.
func TestLoadState_TakesBackTheRecordedGateway(t *testing.T) {
	pid := fakeGateway(t)
	dir := t.TempDir()

	first := quietSupervisor(t)
	first.statePath = filepath.Join(dir, "state.json")
	first.gateway = &gatewayChild{
		pid: pid, addr: "127.0.0.1:7788", token: "k_shared",
		cfg: config.GatewayConfig{Port: config.GatewayPort(7788)},
	}
	require.NoError(t, first.saveStateLocked())

	second := quietSupervisor(t)
	second.statePath = first.statePath
	require.NoError(t, second.loadState())

	require.NotNil(t, second.gateway, "the replacement daemon must find it")
	assert.Equal(t, pid, second.gateway.pid)
	assert.Equal(t, "k_shared", second.gateway.token)

	st := second.gatewayStatusLocked()
	assert.True(t, st.Running)
	assert.Equal(t, "127.0.0.1:7788", st.Addr)
}

// Stopping it must clear the record, or the next daemon re-adopts a corpse —
// or worse, whatever pid the kernel hands out next.
func TestHandleGatewayDown_ClearsTheRecord(t *testing.T) {
	dir := t.TempDir()
	s := quietSupervisor(t)
	s.statePath = filepath.Join(dir, "state.json")
	s.gateway = &gatewayChild{pid: fakeGateway(t), addr: "127.0.0.1:7788"}
	require.NoError(t, s.saveStateLocked())

	require.True(t, s.handleGatewayDown().OK)

	state, err := config.LoadState(s.statePath)
	require.NoError(t, err)
	assert.Nil(t, state.Gateway)
}
