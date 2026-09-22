//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fast keeps the tests quick while still exercising the settle period.
var fast = ops.WaitOptions{Timeout: 8 * time.Second, Settle: 700 * time.Millisecond, Poll: 50 * time.Millisecond}

func globalScope(t *testing.T) *ops.Resolved {
	t.Helper()
	work, err := os.MkdirTemp("", "ag-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	r, err := ops.Resolve(ops.Scope{Dir: work})
	require.NoError(t, err)
	return r
}

func stopAfter(t *testing.T, names ...string) {
	t.Cleanup(func() {
		for _, n := range names {
			_, _ = ops.Stop(n)
		}
	})
}

func TestStartAndWait_Running(t *testing.T) {
	testEnv(t)
	registerService(t, "up", "echo booting; sleep 30", t.TempDir())
	stopAfter(t, "up")

	res, err := ops.StartAndWait(globalScope(t), "up", "", fast)
	require.NoError(t, err)
	require.Len(t, res.Services, 1)
	s := res.Services[0]
	assert.Equal(t, ops.OutcomeRunning, res.Outcome)
	assert.Equal(t, "running", s.State)
	require.NotNil(t, s.PID)
	assert.Empty(t, s.LogTail, "no log tail on success")
	assert.GreaterOrEqual(t, res.Waited, fast.Settle, "it waited out the settle period")

	// Asking again is not an error — it is already in the wanted state.
	again, err := ops.StartAndWait(globalScope(t), "up", "", fast)
	require.NoError(t, err)
	assert.Equal(t, ops.OutcomeAlreadyRunning, again.Outcome)
	assert.Less(t, again.Waited, fast.Settle, "already running needs no settle wait")
}

// A command that dies at once: the daemon refuses the start ("exited
// immediately"), but a process did run, so it is a crash with its output.
func TestStartAndWait_ImmediateCrashCarriesItsOutput(t *testing.T) {
	testEnv(t)
	registerService(t, "boom", "echo 'fatal: config missing'; exit 3", t.TempDir())

	res, err := ops.StartAndWait(globalScope(t), "boom", "", fast)
	require.NoError(t, err, "a failed start is an outcome, not an error")
	s := res.Services[0]
	assert.Equal(t, ops.OutcomeCrashed, res.Outcome)
	require.NotNil(t, s.ExitCode)
	assert.Equal(t, 3, *s.ExitCode)
	assert.Contains(t, strings.Join(s.LogTail, "\n"), "fatal: config missing")

	// Crashing again straight away is still recognised as this start's crash.
	res, err = ops.StartAndWait(globalScope(t), "boom", "", fast)
	require.NoError(t, err)
	assert.Equal(t, ops.OutcomeCrashed, res.Outcome)
}

// The settle period is what catches a service that comes up and dies shortly
// after — the case a bare start reports as success.
func TestStartAndWait_SettleCatchesALateCrash(t *testing.T) {
	testEnv(t)
	registerService(t, "late", "echo listening; sleep 0.3; echo 'panic: nil map'; exit 2", t.TempDir())

	res, err := ops.StartAndWait(globalScope(t), "late", "", fast)
	require.NoError(t, err)
	assert.Equal(t, ops.OutcomeCrashed, res.Outcome)
	assert.Contains(t, strings.Join(res.Services[0].LogTail, "\n"), "panic: nil map")
}

func TestStartAndWait_CleanExitIsExited(t *testing.T) {
	testEnv(t)
	registerService(t, "once", "sleep 0.2; echo migrated", t.TempDir())

	res, err := ops.StartAndWait(globalScope(t), "once", "", fast)
	require.NoError(t, err)
	assert.Equal(t, ops.OutcomeExited, res.Outcome)
	require.NotNil(t, res.Services[0].ExitCode)
	assert.Equal(t, 0, *res.Services[0].ExitCode)
	assert.Contains(t, strings.Join(res.Services[0].LogTail, "\n"), "migrated")
}

// A service whose process cannot be spawned at all never gets a StartedAt; it
// is "failed", with the daemon's reason.
func TestStartAndWait_UnspawnableIsFailedWithReason(t *testing.T) {
	testEnv(t)
	registerService(t, "nodir", "sleep 30", "/definitely/not/a/dir")

	res, err := ops.StartAndWait(globalScope(t), "nodir", "", fast)
	require.NoError(t, err)
	assert.Equal(t, ops.OutcomeFailed, res.Outcome)
	assert.NotEmpty(t, res.Services[0].Error)
}

func TestStartAndWait_NoWaitReturnsAtOnce(t *testing.T) {
	testEnv(t)
	registerService(t, "bg", "sleep 30", t.TempDir())
	stopAfter(t, "bg")

	opts := fast
	opts.NoWait = true
	res, err := ops.StartAndWait(globalScope(t), "bg", "", opts)
	require.NoError(t, err)
	assert.Equal(t, ops.OutcomeStarted, res.Outcome)
	assert.Less(t, res.Waited, fast.Settle)
}

// A target reports each member: one up, one crashed, one already running.
func TestStartAndWait_TargetReportsEachMember(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	registerService(t, "api", "sleep 30", dir)
	registerService(t, "worker", "echo 'worker: redis refused'; exit 1", dir)
	registerService(t, "db", "sleep 30", dir)
	stopAfter(t, "api", "db")

	reg, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	reg.Targets = map[string][]string{"stack": {"db", "api", "worker"}}
	require.NoError(t, config.SaveRegistry(config.RegistryPath(), reg))

	pre, err := ops.StartAndWait(globalScope(t), "db", "", fast)
	require.NoError(t, err)
	require.Equal(t, ops.OutcomeRunning, pre.Outcome)

	res, err := ops.StartAndWait(globalScope(t), "", "stack", fast)
	require.NoError(t, err)
	by := map[string]ops.ServiceOutcome{}
	for _, s := range res.Services {
		by[s.Name] = s
	}
	assert.Equal(t, ops.OutcomeAlreadyRunning, by["db"].Outcome)
	assert.Equal(t, ops.OutcomeRunning, by["api"].Outcome)
	assert.Equal(t, ops.OutcomeCrashed, by["worker"].Outcome)
	assert.Contains(t, strings.Join(by["worker"].LogTail, "\n"), "redis refused")
	assert.Equal(t, ops.OutcomeCrashed, res.Outcome, "the target is up only when every member is")
	assert.Equal(t, []string{"db", "api", "worker"}, []string{res.Services[0].Name, res.Services[1].Name, res.Services[2].Name},
		"members are reported in the target's order")
}

// A project service is started from its devrun.yaml definition, shipped inline.
func TestStartAndWait_ProjectScope(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("services:\n  app:\n    command: echo app-up; sleep 30\n"), 0644))
	stopAfter(t, "app")

	r, err := ops.Resolve(ops.Scope{Dir: dir})
	require.NoError(t, err)
	res, err := ops.StartAndWait(r, "app", "", fast)
	require.NoError(t, err)
	assert.Equal(t, ops.OutcomeRunning, res.Outcome)

	logs, err := ops.Logs("app", ops.LogQuery{Lines: 10, Plain: true})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(logs.Lines, "\n"), "app-up")
}
