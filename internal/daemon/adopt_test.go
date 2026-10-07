package daemon

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func regWith(name, command string) *config.Registry {
	return &config.Registry{Services: map[string]*config.ServiceConfig{
		name: {Name: name, Command: command},
	}}
}

// The service really is the one devrun started: its argv carries the command.
func TestRecogniseService_MatchesTheRecordedCommand(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap '' HUP\nsleep 31")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	pid := cmd.Process.Pid
	require.Eventually(t, func() bool {
		return strings.Contains(process.CommandLine(pid), "sleep 31")
	}, 5*time.Second, 50*time.Millisecond)

	assert.True(t, recogniseService(regWith("web", "sleep 31"))("web", pid))
}

// The pid was reused by something else entirely — the case kill(pid, 0)
// cannot see, and the one that gets a stranger SIGTERMed by `devrun stop`.
func TestRecogniseService_RefusesAReusedPid(t *testing.T) {
	cmd := exec.Command("sleep", "32")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	pid := cmd.Process.Pid
	require.Eventually(t, func() bool {
		return process.CommandLine(pid) != ""
	}, 5*time.Second, 50*time.Millisecond)

	assert.False(t, recogniseService(regWith("web", "npm run dev"))("web", pid))
}

// An argv that cannot be read is not evidence of anything. Reporting a
// running dev server as crashed invites a second copy of it, and two servers
// fighting over one port is worse than adopting a stranger.
func TestRecogniseService_SaysYesWhenItCannotTell(t *testing.T) {
	reg := regWith("web", "npm run dev")

	assert.True(t, recogniseService(reg)("web", 0), "no such pid, no argv")
	assert.True(t, recogniseService(reg)("web", 1<<30), "nor this one")
}

// Nothing recorded to compare against is also not evidence.
func TestRecogniseService_SaysYesWithNothingToCompare(t *testing.T) {
	pid := os.Getpid()
	assert.True(t, recogniseService(nil)("web", pid))
	assert.True(t, recogniseService(&config.Registry{})("web", pid), "service not in the registry")
	assert.True(t, recogniseService(regWith("web", ""))("web", pid), "no command recorded")
}

// A shell may exec-replace itself with a simple command, leaving an argv of
// the resolved program rather than `sh -c <command>` — and it may not. Which
// it does varies by shell: locally this exec'd and the argv was "sleep 35",
// while on CI it did not and the whole `sh -c …` line was there, on both
// Linux and macOS.
//
// So the recognition has to hold either way, which is the reason it compares
// the program name and nothing longer. The test asserts that and deliberately
// not which shape the argv took.
func TestRecogniseService_HoldsWhicheverShapeTheArgvTakes(t *testing.T) {
	recorded := "sleep 35 # " + strings.Repeat("x", 200)
	cmd := exec.Command("sh", "-c", "trap '' HUP\n"+recorded)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	pid := cmd.Process.Pid
	require.Eventually(t, func() bool {
		return strings.Contains(process.CommandLine(pid), "sleep 35")
	}, 5*time.Second, 50*time.Millisecond)

	assert.True(t, recogniseService(regWith("web", recorded))("web", pid),
		"recognised whether or not the shell exec'd")
}

func TestProgramName(t *testing.T) {
	for in, want := range map[string]string{
		"npm run dev":                   "npm",
		"./scripts/dev.sh --port 3000":  "dev.sh",
		"/usr/local/bin/node server.js": "node",
		"  spaced   out  ":              "spaced",
		"":                              "",
	} {
		assert.Equalf(t, want, programName(in), "%q", in)
	}
}
