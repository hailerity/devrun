//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain builds the devrun binary once and sets DEVRUN_DAEMON_BIN so that
// EnsureDaemon re-execs the real binary rather than the test binary.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "pt-bin-")
	if err != nil {
		panic("create temp dir for binary: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	binPath := filepath.Join(tmp, "devrun")
	out, err := exec.Command("go", "build", "-o", binPath, "github.com/hailerity/devrun/cmd/devrun").CombinedOutput()
	if err != nil {
		panic("build devrun binary: " + err.Error() + "\n" + string(out))
	}
	os.Setenv("DEVRUN_DAEMON_BIN", binPath)

	os.Exit(m.Run())
}

// testEnv sets XDG dirs to a short temp directory, starts the daemon in-process
// in a background goroutine via RunWithContext, and returns the socket path.
//
// We use os.MkdirTemp with a short prefix to avoid exceeding the macOS Unix
// socket path limit of 103 characters (sun_path is 104 bytes incl. null terminator).
// t.TempDir() embeds the full test name in the path which can exceed the limit.
func testEnv(t *testing.T) (socketPath string, cleanup func()) {
	tmp, err := os.MkdirTemp("", "pt-")
	if err != nil {
		t.Fatal("create temp dir:", err)
	}
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	sockPath := config.SocketPath()

	// Start the daemon in a background goroutine (in-process) to avoid subprocess
	// path-length and env-inheritance issues.
	ctx, cancel := context.WithCancel(context.Background())
	daemonDone := make(chan error, 1)
	go func() {
		daemonDone <- daemon.RunWithContext(ctx, sockPath)
	}()

	// Wait until the socket is available (up to 3s).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.Dial("unix", sockPath)
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cleanup = func() {
		cancel()       // signal daemon goroutine to stop (closes ln via ctx.Done)
		<-daemonDone   // wait for daemon goroutine to finish
		os.RemoveAll(tmp)
	}
	t.Cleanup(cleanup)
	return sockPath, cleanup
}

func registerService(t *testing.T, name, command, cwd string) {
	reg, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	reg.Services[name] = &config.ServiceConfig{Name: name, Command: command, CWD: cwd}
	if reg.Version == "" {
		reg.Version = "1"
	}
	require.NoError(t, config.SaveRegistry(config.RegistryPath(), reg))
}

// send is a helper that opens a fresh connection, sends one request, reads the
// response, and closes the connection. The daemon handles exactly one request
// per connection (per its handleConn design).
func send(t *testing.T, socketPath, reqType string, payload interface{}) *ipc.Response {
	t.Helper()
	c, err := client.Connect(socketPath)
	require.NoError(t, err)
	defer c.Close()
	resp, err := c.Send(reqType, payload)
	require.NoError(t, err)
	return resp
}

func TestLifecycle_StartListStop(t *testing.T) {
	socketPath, _ := testEnv(t)
	registerService(t, "echo", "while true; do echo hello; sleep 1; done", t.TempDir())

	// Start
	resp := send(t, socketPath, "start", ipc.StartPayload{Name: "echo"})
	assert.True(t, resp.OK, resp.Error)

	// List — should show running
	resp = send(t, socketPath, "list", struct{}{})
	assert.True(t, resp.OK)
	var payload ipc.ListResponsePayload
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	require.Len(t, payload.Services, 1)
	assert.Equal(t, "running", payload.Services[0].State)

	// Log file should exist and have content
	time.Sleep(200 * time.Millisecond)
	logPath := config.LogPath("echo")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "hello")

	// Stop
	resp = send(t, socketPath, "stop", ipc.StopPayload{Name: "echo"})
	assert.True(t, resp.OK, resp.Error)

	// List — should show stopped (give the watchExit goroutine a moment)
	time.Sleep(200 * time.Millisecond)
	resp = send(t, socketPath, "list", struct{}{})
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	assert.Equal(t, "stopped", payload.Services[0].State)
}

func TestLifecycle_ProcessCrash(t *testing.T) {
	socketPath, _ := testEnv(t)
	// Command starts, passes the 100ms alive check, then crashes.
	registerService(t, "crasher", "sleep 0.2 && exit 1", t.TempDir())

	// Start should succeed (process is alive at the 100ms check)
	resp := send(t, socketPath, "start", ipc.StartPayload{Name: "crasher"})
	require.True(t, resp.OK, "start should succeed: %s", resp.Error)

	// Wait for the process to crash
	time.Sleep(500 * time.Millisecond)

	resp = send(t, socketPath, "list", struct{}{})
	var payload ipc.ListResponsePayload
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))
	require.Len(t, payload.Services, 1)
	assert.Equal(t, "crashed", payload.Services[0].State)
}

func TestLifecycle_DaemonCrashReAdoption(t *testing.T) {
	// Use a short temp dir with two subprocess daemons (via EnsureDaemon).
	// Subprocess daemons are killed by removing the socket, which doesn't kill
	// child processes — simulating a real daemon crash cleanly.
	tmp, err := os.MkdirTemp("", "pt-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmp) })

	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	socketPath := config.SocketPath()

	serviceDir := filepath.Join(tmp, "svcdir")
	require.NoError(t, os.MkdirAll(serviceDir, 0755))
	registerService(t, "persistent", "sleep 300", serviceDir)

	// Start first daemon (subprocess) via EnsureDaemon.
	require.NoError(t, daemon.EnsureDaemon(socketPath))

	resp := send(t, socketPath, "start", ipc.StartPayload{Name: "persistent"})
	require.True(t, resp.OK, resp.Error)

	// Simulate daemon crash by removing the socket. The subprocess daemon will
	// continue running but clients can no longer connect. The child "persistent"
	// process (sleep 300) remains alive since the daemon was not gracefully stopped.
	os.Remove(socketPath)

	// Brief pause to let the first daemon notice the socket is gone and
	// write its final state.json before the second daemon starts.
	time.Sleep(100 * time.Millisecond)

	// Re-start daemon — it should re-adopt the still-running "persistent" service.
	require.NoError(t, daemon.EnsureDaemon(socketPath))
	time.Sleep(200 * time.Millisecond) // let loadState + re-adoption settle

	resp = send(t, socketPath, "list", struct{}{})
	var payload ipc.ListResponsePayload
	require.NoError(t, json.Unmarshal(resp.Payload, &payload))

	// "persistent" should be re-adopted and show as running
	require.Len(t, payload.Services, 1)
	assert.Equal(t, "running", payload.Services[0].State)

	// Clean up: remove socket to signal the second daemon; first daemon
	// is an orphan but will exit when its socket is cleaned up by the OS.
	t.Cleanup(func() { os.Remove(socketPath) })
}

// waitForSocket polls until the unix socket at path is connectable (up to 3s).
func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s did not become available within 3s", path)
}


func TestLifecycle_DaemonAutoStart(t *testing.T) {
	// Use a fresh short temp dir WITHOUT starting an in-process daemon.
	tmp, err := os.MkdirTemp("", "pt-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmp) })

	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	socketPath := config.SocketPath()
	registerService(t, "web", "sleep 60", tmp)

	// Socket does not exist yet — EnsureDaemon should create it
	_, err = os.Stat(socketPath)
	assert.True(t, os.IsNotExist(err))

	require.NoError(t, daemon.EnsureDaemon(socketPath))

	_, err = os.Stat(socketPath)
	assert.NoError(t, err, "socket should exist after EnsureDaemon")

	// Clean up the subprocess daemon
	t.Cleanup(func() { os.Remove(socketPath) })
}

func TestLifecycle_LogsWithoutDaemon(t *testing.T) {
	_, _ = testEnv(t)
	// Write a fake log file
	logPath := config.LogPath("web")
	require.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0755))
	require.NoError(t, os.WriteFile(logPath, []byte("line1\nline2\n"), 0644))

	// Read directly — no daemon needed
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "line1")
}
