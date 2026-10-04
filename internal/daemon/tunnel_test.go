package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCloudflared puts a stand-in on PATH and points the data dir at a temp
// directory, so no test here reaches the real binary — it publishes services
// to the internet — or writes to the real log.
func fakeCloudflared(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cloudflared"),
		[]byte("#!/bin/sh\n"+script), 0o700))
	// Prepended, not replaced: the script needs `cat` and `sleep`, and the
	// stand-in still wins because dir comes first.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The banner cloudflared prints for a quick tunnel, boxed as it really is.
const quickBanner = `2026-10-04T00:00:00Z INF Requesting new quick Tunnel on trycloudflare.com...
2026-10-04T00:00:01Z INF +----------------------------------------------------+
2026-10-04T00:00:01Z INF |  Your quick Tunnel has been created! Visit it at:  |
2026-10-04T00:00:01Z INF |  https://odd-mountain-4821.trycloudflare.com       |
2026-10-04T00:00:01Z INF +----------------------------------------------------+`

func TestSpawnTunnel_QuickScrapesTheAssignedURL(t *testing.T) {
	fakeCloudflared(t, "cat <<'EOF'\n"+quickBanner+"\nEOF\nsleep 30\n")
	s := quietSupervisor(t)

	child, err := s.spawnTunnel(config.TunnelConfig{}, "http://127.0.0.1:7788")
	require.NoError(t, err)
	t.Cleanup(func() { s.mu.Lock(); s.tunnel = child; s.stopTunnelLocked(); s.mu.Unlock() })

	assert.Equal(t, KindQuick, child.kind)
	assert.Equal(t, "https://odd-mountain-4821.trycloudflare.com", child.publicURL)
	assert.True(t, pidAlive(child.pid))
}

// A named tunnel's hostname is config, not output: nothing is scraped, so
// cloudflared changing its banner cannot break it.
func TestSpawnTunnel_NamedTakesItsURLFromConfig(t *testing.T) {
	fakeCloudflared(t, "sleep 30\n")
	s := quietSupervisor(t)
	cfg := config.TunnelConfig{Name: "devrun", Hostname: "devrun.example.com"}

	child, err := s.spawnTunnel(cfg, "http://127.0.0.1:7788")
	require.NoError(t, err)
	t.Cleanup(func() { s.mu.Lock(); s.tunnel = child; s.stopTunnelLocked(); s.mu.Unlock() })

	assert.Equal(t, KindNamed, child.kind)
	assert.Equal(t, "https://devrun.example.com", child.publicURL)
}

// cloudflared's output is not an API. A tunnel that is up but whose URL could
// not be read still works, so it degrades rather than being torn down.
func TestSpawnTunnel_RunningWithoutAReadableURL(t *testing.T) {
	fakeCloudflared(t, "echo 'INF connection registered'\nsleep 30\n")
	s := quietSupervisor(t)
	quickURLFast(t)

	child, err := s.spawnTunnel(config.TunnelConfig{}, "http://127.0.0.1:7788")
	require.NoError(t, err, "running but unreadable is not a failure")
	t.Cleanup(func() { s.mu.Lock(); s.tunnel = child; s.stopTunnelLocked(); s.mu.Unlock() })

	assert.Empty(t, child.publicURL)
	assert.True(t, pidAlive(child.pid))
}

// Dying is a failure, and cloudflared said why.
func TestSpawnTunnel_ReportsWhyItDied(t *testing.T) {
	fakeCloudflared(t, "echo 'ERR tunnel credentials file not found' >&2\nexit 1\n")
	s := quietSupervisor(t)

	_, err := s.spawnTunnel(config.TunnelConfig{}, "http://127.0.0.1:7788")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials file not found")
}

func TestSpawnTunnel_MissingBinaryNamesBothForks(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	s := quietSupervisor(t)

	_, err := s.spawnTunnel(config.TunnelConfig{}, "http://127.0.0.1:7788")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "posture: published")
}

// The tunnel outlives the daemon, so a replacement must be able to take it
// back — and must not mistake a reused pid for it.
func TestAdoptTunnel(t *testing.T) {
	log := quietSupervisor(t).logger

	t.Run("takes back a running cloudflared", func(t *testing.T) {
		cmd := exec.Command("sh", "-c", ": cloudflared; sleep 30")
		require.NoError(t, cmd.Start())
		t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

		got := adoptTunnel(&config.TunnelState{
			PID: cmd.Process.Pid, Kind: KindQuick,
			PublicURL: "https://odd-mountain-4821.trycloudflare.com",
		}, log)
		require.NotNil(t, got)
		assert.Equal(t, "https://odd-mountain-4821.trycloudflare.com", got.publicURL)
	})

	t.Run("refuses a live pid that is somebody else", func(t *testing.T) {
		cmd := exec.Command("sleep", "30")
		require.NoError(t, cmd.Start())
		t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

		assert.Nil(t, adoptTunnel(&config.TunnelState{PID: cmd.Process.Pid}, log))
	})

	t.Run("nothing recorded", func(t *testing.T) {
		assert.Nil(t, adoptTunnel(nil, log))
	})
}

// A wildcard bind is where the gateway listens, not an address anything can
// connect to.
func TestOriginURL(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:7788": "http://127.0.0.1:7788",
		"0.0.0.0:7788":   "http://127.0.0.1:7788",
		"[::]:7788":      "http://127.0.0.1:7788",
		"[::1]:7788":     "http://[::1]:7788",
	} {
		assert.Equalf(t, want, originURL(addr), "%s", addr)
	}
}

func TestFirstLogLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnel.log")
	require.NoError(t, os.WriteFile(path, []byte("older run\nERR the new failure\nstack\n"), 0o644))

	// Only what this run wrote: the log is appended to across restarts.
	assert.Equal(t, "ERR the new failure", firstLogLine(path, int64(len("older run\n"))))
	assert.Equal(t, "nothing was logged", firstLogLine(path, 9999))
}

// Keep the no-URL case from waiting out the full timeout.
func quickURLFast(t *testing.T) {
	t.Helper()
	orig := quickURLTimeout
	quickURLTimeout = 600 * time.Millisecond
	t.Cleanup(func() { quickURLTimeout = orig })
}

var _ = strings.TrimSpace
