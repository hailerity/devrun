package daemon

import (
	"io"
	"log/slog"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSupervisorShutdown_StopsRunningAndReAdopted verifies that a daemon
// SIGTERM/SIGINT shutdown gracefully stops both normally-managed services (which
// hold a *process.Process) and re-adopted ones (PID only, proc == nil).
func TestSupervisorShutdown_StopsRunningAndReAdopted(t *testing.T) {
	sup := newSupervisor("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	sup.statePath = filepath.Join(t.TempDir(), "state.json")

	p1, err := process.Start("sleep 120", "", nil)
	require.NoError(t, err)
	pid1 := p1.Cmd.Process.Pid
	sup.services["managed"] = &managedService{
		cfg:   &config.ServiceConfig{Name: "managed"},
		state: &config.ServiceState{Status: config.StatusRunning, PID: &pid1},
		proc:  p1,
	}

	p2, err := process.Start("sleep 120", "", nil)
	require.NoError(t, err)
	pid2 := p2.Cmd.Process.Pid
	sup.services["readopted"] = &managedService{
		cfg:   &config.ServiceConfig{Name: "readopted"},
		state: &config.ServiceState{Status: config.StatusRunning, PID: &pid2},
	}

	// Stand in for watchExit / init: concurrent reapers so the leader-exit poll
	// in TerminateGroup sees the processes go away promptly.
	done := make(chan struct{}, 2)
	go func() { p1.Cmd.Wait(); done <- struct{}{} }() //nolint:errcheck
	go func() { p2.Cmd.Wait(); done <- struct{}{} }() //nolint:errcheck

	sup.shutdown()

	<-done
	<-done

	assert.Error(t, syscall.Kill(pid1, 0), "managed service should be stopped")
	assert.Error(t, syscall.Kill(pid2, 0), "re-adopted service should be stopped")

	assert.Equal(t, config.StatusStopped, sup.services["managed"].state.Status)
	assert.Equal(t, config.StatusStopped, sup.services["readopted"].state.Status)
	assert.Nil(t, sup.services["managed"].state.PID)
	assert.Nil(t, sup.services["readopted"].state.PID)
}

// TestSupervisorShutdown_SkipsTerminalServices makes sure a service that already
// exited is left untouched (no spurious status change).
func TestSupervisorShutdown_SkipsTerminalServices(t *testing.T) {
	sup := newSupervisor("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	sup.statePath = filepath.Join(t.TempDir(), "state.json")

	sup.services["done"] = &managedService{
		cfg:   &config.ServiceConfig{Name: "done"},
		state: &config.ServiceState{Status: config.StatusCrashed},
	}

	sup.shutdown()

	assert.Equal(t, config.StatusCrashed, sup.services["done"].state.Status)
}
