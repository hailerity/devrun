package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func targetTestSupervisor(t *testing.T) *supervisor {
	t.Helper()
	sup := newSupervisor("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	sup.statePath = filepath.Join(t.TempDir(), "state.json")
	return sup
}

func svcCfg(name string) *config.ServiceConfig {
	return &config.ServiceConfig{Name: name, Command: "sleep 30"}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// stopEverything terminates every process the supervisor still tracks so the
// test does not leak `sleep` children.
func stopEverything(t *testing.T, sup *supervisor) {
	t.Helper()
	sup.mu.RLock()
	procs := make(map[string]int)
	for name, svc := range sup.services {
		if svc.proc != nil && svc.proc.Cmd != nil && svc.proc.Cmd.Process != nil {
			procs[name] = svc.proc.Cmd.Process.Pid
		}
	}
	sup.mu.RUnlock()
	for name := range procs {
		_ = sup.stopService(name)
	}
	for _, pid := range procs {
		assert.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil },
			5*time.Second, 20*time.Millisecond)
	}
}

func serviceState(sup *supervisor, name string) config.ServiceStatus {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	if svc := sup.services[name]; svc != nil {
		return svc.state.Status
	}
	return ""
}

func TestHandleTargetStart_StartsMembersAndRecordsActive(t *testing.T) {
	sup := targetTestSupervisor(t)
	defer stopEverything(t, sup)

	resp := sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Name:     "project-1",
		Services: []*config.ServiceConfig{svcCfg("web"), svcCfg("api")},
	}))
	require.True(t, resp.OK, resp.Error)

	assert.Equal(t, config.StatusRunning, serviceState(sup, "web"))
	assert.Equal(t, config.StatusRunning, serviceState(sup, "api"))

	sup.mu.RLock()
	assert.Equal(t, []string{"web", "api"}, sup.activeTargets["project-1"])
	sup.mu.RUnlock()

	// list reports the active target.
	listResp := sup.handleList()
	require.True(t, listResp.OK)
	var lp ipc.ListResponsePayload
	require.NoError(t, json.Unmarshal(listResp.Payload, &lp))
	assert.Equal(t, []string{"project-1"}, lp.ActiveTargets)

	// active targets are persisted.
	st, err := config.LoadState(sup.statePath)
	require.NoError(t, err)
	assert.Equal(t, []string{"web", "api"}, st.ActiveTargets["project-1"])
}

func TestHandleTargetStart_RejectsEmpty(t *testing.T) {
	sup := targetTestSupervisor(t)

	resp := sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{Name: "t1"}))
	assert.False(t, resp.OK)

	resp = sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Services: []*config.ServiceConfig{svcCfg("web")},
	}))
	assert.False(t, resp.OK)
}

func TestHandleTargetStop_StopsMembersAndClearsActive(t *testing.T) {
	sup := targetTestSupervisor(t)
	defer stopEverything(t, sup)

	require.True(t, sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Name:     "project-1",
		Services: []*config.ServiceConfig{svcCfg("web"), svcCfg("api")},
	})).OK)

	resp := sup.handleTargetStop(mustMarshal(t, ipc.TargetStopPayload{Name: "project-1"}))
	require.True(t, resp.OK, resp.Error)

	assert.Eventually(t, func() bool {
		return serviceState(sup, "web") == config.StatusStopped &&
			serviceState(sup, "api") == config.StatusStopped
	}, 5*time.Second, 20*time.Millisecond)

	sup.mu.RLock()
	_, stillActive := sup.activeTargets["project-1"]
	sup.mu.RUnlock()
	assert.False(t, stillActive)
}

// A member shared with another still-active target must keep running when one
// target is stopped.
func TestHandleTargetStop_KeepsServiceHeldByAnotherTarget(t *testing.T) {
	sup := targetTestSupervisor(t)
	defer stopEverything(t, sup)

	require.True(t, sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Name:     "t1",
		Services: []*config.ServiceConfig{svcCfg("web"), svcCfg("api")},
	})).OK)
	require.True(t, sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Name:     "t2",
		Services: []*config.ServiceConfig{svcCfg("api"), svcCfg("db")},
	})).OK)

	resp := sup.handleTargetStop(mustMarshal(t, ipc.TargetStopPayload{Name: "t1"}))
	require.True(t, resp.OK, resp.Error)

	assert.Eventually(t, func() bool {
		return serviceState(sup, "web") == config.StatusStopped
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, config.StatusRunning, serviceState(sup, "api"), "api held by t2")
	assert.Equal(t, config.StatusRunning, serviceState(sup, "db"), "db held by t2")

	sup.mu.RLock()
	assert.Equal(t, []string{"api", "db"}, sup.activeTargets["t2"])
	sup.mu.RUnlock()
}

// If every member fails to start, the target is not recorded active.
func TestHandleTargetStart_AllMembersFailNotRecorded(t *testing.T) {
	sup := targetTestSupervisor(t)
	defer stopEverything(t, sup)

	bad := func(name string) *config.ServiceConfig {
		return &config.ServiceConfig{Name: name, Command: "   "} // rejected: empty command
	}
	resp := sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Name:     "project-1",
		Services: []*config.ServiceConfig{bad("web"), bad("api")},
	}))
	assert.False(t, resp.OK)

	sup.mu.RLock()
	_, recorded := sup.activeTargets["project-1"]
	sup.mu.RUnlock()
	assert.False(t, recorded, "a target with no started services is not active")

	listResp := sup.handleList()
	var lp ipc.ListResponsePayload
	require.NoError(t, json.Unmarshal(listResp.Payload, &lp))
	assert.Empty(t, lp.ActiveTargets)
}

// A partial start (one member up, one down) still records the target active so
// `target stop` can clean it up.
func TestHandleTargetStart_PartialFailureStillRecorded(t *testing.T) {
	sup := targetTestSupervisor(t)
	defer stopEverything(t, sup)

	resp := sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Name: "project-1",
		Services: []*config.ServiceConfig{
			svcCfg("web"),
			{Name: "api", Command: "   "}, // fails
		},
	}))
	assert.False(t, resp.OK, "reports the failed member")

	sup.mu.RLock()
	assert.Equal(t, []string{"web", "api"}, sup.activeTargets["project-1"])
	sup.mu.RUnlock()
	assert.Equal(t, config.StatusRunning, serviceState(sup, "web"))
}

func TestReconcileActiveTargets_PrunesTargetsWithNoLiveMember(t *testing.T) {
	sup := targetTestSupervisor(t)
	sup.services["dead"] = &managedService{
		cfg:   &config.ServiceConfig{Name: "dead"},
		state: &config.ServiceState{Status: config.StatusCrashed},
	}
	sup.services["alive"] = &managedService{
		cfg:   &config.ServiceConfig{Name: "alive"},
		state: &config.ServiceState{Status: config.StatusRunning},
	}
	sup.activeTargets = map[string][]string{
		"gone": {"dead", "missing"},
		"kept": {"dead", "alive"},
	}

	sup.mu.Lock()
	changed := sup.reconcileActiveTargetsLocked()
	sup.mu.Unlock()

	assert.True(t, changed)
	_, gone := sup.activeTargets["gone"]
	assert.False(t, gone, "target with only dead/missing members is pruned")
	_, kept := sup.activeTargets["kept"]
	assert.True(t, kept, "target with a running member is kept")
}

// loadState drops an active target whose services are no longer alive after a
// daemon restart.
func TestLoadState_ReconcilesStaleActiveTargets(t *testing.T) {
	sup := targetTestSupervisor(t)

	deadPID := 999999999
	seed := &config.State{
		Version: 1,
		Services: map[string]*config.ServiceState{
			"web": {Status: config.StatusRunning, PID: &deadPID},
		},
		ActiveTargets: map[string][]string{"project-1": {"web"}},
	}
	require.NoError(t, config.SaveState(sup.statePath, seed))

	require.NoError(t, sup.loadState())

	sup.mu.RLock()
	_, active := sup.activeTargets["project-1"]
	sup.mu.RUnlock()
	assert.False(t, active, "web's PID is dead → target pruned on load")
}

func TestHandleTargetStop_NotActive(t *testing.T) {
	sup := targetTestSupervisor(t)

	resp := sup.handleTargetStop(mustMarshal(t, ipc.TargetStopPayload{Name: "ghost"}))
	assert.False(t, resp.OK)
	assert.Contains(t, resp.Error, "not active")
}

// Restarting an already-running member as part of a target start is not an error.
func TestHandleTargetStart_MemberAlreadyRunningIsOK(t *testing.T) {
	sup := targetTestSupervisor(t)
	defer stopEverything(t, sup)

	require.True(t, sup.startService("web", svcCfg("web")).OK)

	resp := sup.handleTargetStart(mustMarshal(t, ipc.TargetStartPayload{
		Name:     "project-1",
		Services: []*config.ServiceConfig{svcCfg("web"), svcCfg("api")},
	}))
	require.True(t, resp.OK, resp.Error)
	assert.Equal(t, config.StatusRunning, serviceState(sup, "api"))
}
