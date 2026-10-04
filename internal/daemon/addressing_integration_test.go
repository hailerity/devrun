package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envDump starts a real service whose only job is to write its environment to
// a file, so the test reads what the process was actually given rather than
// what the supervisor meant to give it.
func envDump(t *testing.T, s *supervisor, name, out string, env map[string]string) map[string]string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the service is a shell command")
	}
	resp := s.startService(name, &config.ServiceConfig{
		Name:    name,
		Command: "env > " + out + "; sleep 30",
		Env:     env,
	})
	require.True(t, resp.OK, "start %s: %s", name, resp.Error)
	t.Cleanup(func() {
		s.mu.Lock()
		svc := s.services[name]
		s.mu.Unlock()
		if svc != nil && svc.proc != nil {
			_ = svc.proc.Stop()
		}
	})

	require.Eventually(t, func() bool {
		st, err := os.Stat(out)
		return err == nil && st.Size() > 0
	}, 10*time.Second, 50*time.Millisecond, "the service never wrote its environment")

	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	got := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			got[k] = v
		}
	}
	return got
}

func integrationSupervisor(t *testing.T) *supervisor {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	s := quietSupervisor(t)
	s.statePath = filepath.Join(dir, "state.json")
	return s
}

// The case group 5 exists for, end to end: a frontend reads its sibling's
// address through a VITE_-prefixed variable, because Vite exposes nothing
// else to client code.
func TestIntegration_ServiceReadsASiblingsURL(t *testing.T) {
	s := integrationSupervisor(t)
	dir := t.TempDir()

	// The backend, with a declared port so its address is known at once
	// rather than after a detection poll.
	s.services["api"] = svc("api", config.StatusRunning, 0, 3000)

	env := envDump(t, s, "web", filepath.Join(dir, "web.env"), map[string]string{
		"VITE_API_URL": "${DEVRUN_URL_API}",
	})

	assert.Equal(t, "http://localhost:3000", env["DEVRUN_URL_API"],
		"the sibling's address is injected")
	assert.Equal(t, "http://localhost:3000", env["VITE_API_URL"],
		"and bridged into the prefix the framework will actually expose")
}

// Published, the same config has to yield the public address — one variable
// resolving in both worlds is the whole point.
func TestIntegration_TheSameConfigPublished(t *testing.T) {
	s := integrationSupervisor(t)
	dir := t.TempDir()

	s.services["api"] = svc("api", config.StatusRunning, 0, 3000)
	// A gateway and a devrun-run tunnel: this process stands in for both
	// children, since only their liveness and config are read here.
	s.gateway = &gatewayChild{pid: os.Getpid(), addr: "127.0.0.1:7788", cfg: config.GatewayConfig{
		PublicHostname: "{service}-devrun.example.com",
		Expose:         []string{"api", "web"},
	}}
	s.tunnel = &tunnelChild{pid: os.Getpid(), kind: KindNamed, publicURL: "https://devrun.example.com"}

	env := envDump(t, s, "web", filepath.Join(dir, "web.env"), map[string]string{
		"VITE_API_URL": "${DEVRUN_URL_API}",
	})

	assert.Equal(t, "https://api-devrun.example.com", env["VITE_API_URL"],
		"the same env: line, the public address")
}

// A withheld service has no public address to advertise, even to a sibling
// that is published.
func TestIntegration_WithheldSiblingHasNoURL(t *testing.T) {
	s := integrationSupervisor(t)
	dir := t.TempDir()

	s.services["admin"] = svc("admin", config.StatusRunning, 0, 9000)
	s.gateway = &gatewayChild{pid: os.Getpid(), addr: "127.0.0.1:7788", cfg: config.GatewayConfig{
		PublicHostname: "{service}-devrun.example.com",
		Expose:         []string{"web"}, // admin is not exposed
	}}
	s.tunnel = &tunnelChild{pid: os.Getpid(), kind: KindNamed, publicURL: "https://devrun.example.com"}

	env := envDump(t, s, "web", filepath.Join(dir, "web.env"), map[string]string{
		"VITE_ADMIN_URL": "[${DEVRUN_URL_ADMIN}]",
	})

	assert.NotContains(t, env, "DEVRUN_URL_ADMIN")
	assert.Equal(t, "[]", env["VITE_ADMIN_URL"],
		"an unset name expands to empty, which breaks visibly rather than plausibly")
}

// The inherited environment still reaches the service; injection adds to it.
func TestIntegration_InheritedEnvironmentSurvives(t *testing.T) {
	t.Setenv("DEVRUN_TEST_MARKER", "inherited-ok")
	s := integrationSupervisor(t)
	dir := t.TempDir()

	env := envDump(t, s, "web", filepath.Join(dir, "web.env"), nil)
	assert.Equal(t, "inherited-ok", env["DEVRUN_TEST_MARKER"])
}
