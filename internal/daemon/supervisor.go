package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/process"
)

type managedService struct {
	cfg      *config.ServiceConfig
	state    *config.ServiceState
	proc     *process.Process
	attached net.Conn
	mu       sync.Mutex
}

type supervisor struct {
	socketPath string
	logger     *slog.Logger
	mu         sync.RWMutex
	services   map[string]*managedService
	// activeTargets maps a started target to the member service names snapshotted
	// at target-start time. Guarded by mu. Persisted in state.json so it survives
	// a daemon restart or re-exec.
	activeTargets map[string][]string
	statePath     string
	registry      *config.Registry
	stopDaemon func() // called by IPC daemon-stop; set by server.go
	// onReexec stops this daemon after a replacement has been launched, leaving
	// managed services running. Set by server.go once the listener is up; a nil
	// value makes the daemon-reexec request report "unsupported".
	onReexec func()
}

func newSupervisor(socketPath string, logger *slog.Logger) *supervisor {
	return &supervisor{
		socketPath:    socketPath,
		logger:        logger,
		services:      make(map[string]*managedService),
		activeTargets: make(map[string][]string),
		statePath:     config.StatePath(),
	}
}

func (s *supervisor) loadState() error {
	state, err := config.LoadState(s.statePath)
	if err != nil {
		return err
	}
	config.ReAdoptServices(state.Services)

	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return err
	}
	s.registry = reg

	s.mu.Lock()
	defer s.mu.Unlock()
	for name, svcState := range state.Services {
		cfg := reg.Services[name]
		if cfg == nil {
			cfg = &config.ServiceConfig{Name: name}
		}
		// Re-adopted services have nil proc (PTY master fd is gone after daemon restart).
		// They appear in list as running but devrun fg is unavailable until restarted.
		s.services[name] = &managedService{cfg: cfg, state: svcState}
	}
	if state.ActiveTargets != nil {
		s.activeTargets = state.ActiveTargets
	}
	return s.saveStateLocked()
}

func (s *supervisor) saveStateLocked() error {
	state := &config.State{
		Version:       1,
		Services:      make(map[string]*config.ServiceState),
		ActiveTargets: s.activeTargets,
	}
	for name, svc := range s.services {
		state.Services[name] = svc.state
	}
	return config.SaveState(s.statePath, state)
}

func (s *supervisor) handleConn(conn net.Conn) {
	defer conn.Close()
	var req ipc.Request
	if err := ipc.ReadMessage(conn, &req); err != nil {
		return
	}

	switch req.Type {
	case "start":
		_ = ipc.WriteMessage(conn, s.handleStart(req.Payload))
	case "stop":
		_ = ipc.WriteMessage(conn, s.handleStop(req.Payload))
	case "target-start":
		_ = ipc.WriteMessage(conn, s.handleTargetStart(req.Payload))
	case "target-stop":
		_ = ipc.WriteMessage(conn, s.handleTargetStop(req.Payload))
	case "remove":
		_ = ipc.WriteMessage(conn, s.handleRemove(req.Payload))
	case "list":
		_ = ipc.WriteMessage(conn, s.handleList())
	case "attach":
		s.handleAttach(conn, req.Payload)
	case "daemon-stop":
		_ = ipc.WriteMessage(conn, &ipc.Response{OK: true})
		if s.stopDaemon != nil {
			go s.stopDaemon()
		}
		return
	case "daemon-reexec":
		if s.onReexec == nil {
			_ = ipc.WriteMessage(conn, errResp("daemon does not support graceful re-exec"))
			return
		}
		if err := s.reexec(); err != nil {
			_ = ipc.WriteMessage(conn, errResp(fmt.Sprintf("re-exec: %v", err)))
			return
		}
		// Reply and flush to the client *before* tearing this daemon down —
		// s.onReexec closes the listener, and the replacement now owns the
		// socket.
		_ = ipc.WriteMessage(conn, &ipc.Response{OK: true})
		_ = conn.Close()
		s.onReexec()
		return
	default:
		_ = ipc.WriteMessage(conn, errResp(fmt.Sprintf("unknown request type: %s", req.Type)))
	}
}

func (s *supervisor) handleStart(raw json.RawMessage) *ipc.Response {
	var p ipc.StartPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResp("invalid start payload")
	}
	return s.startService(p.Name, p.Config)
}

// startService starts one service. inlineCfg, when non-nil, carries the full
// definition (project devrun.yaml services and target members ship it this way);
// when nil the daemon resolves name from the global registry.
func (s *supervisor) startService(name string, inlineCfg *config.ServiceConfig) *ipc.Response {
	cfg := inlineCfg
	if cfg == nil {
		reg, err := config.LoadRegistry(config.RegistryPath())
		if err != nil {
			return errResp(fmt.Sprintf("load registry: %v", err))
		}
		cfg = reg.Services[name]
	}
	if cfg == nil {
		return errResp(fmt.Sprintf("service %q not registered. Run 'devrun add %s <cmd>' first.", name, name))
	}
	if cfg.Name == "" {
		cfg.Name = name
	}
	if err := cfg.Validate(); err != nil {
		return errResp(fmt.Sprintf("service %q: %v", name, err))
	}

	// Hold s.mu across the spawn so a second concurrent `start` for the same
	// name can't slip past the "already running" check before this one records
	// its process. Releasing the lock between the check and the map write let
	// both callers spawn; the loser's process was then dropped from the map and
	// leaked (unreachable by stop/list, unseen at shutdown). process.Start is a
	// local fork+exec and returns in a few ms.
	s.mu.Lock()
	existing := s.services[name]
	if existing != nil && (existing.state.Status == config.StatusRunning || existing.state.Status == config.StatusStarting) {
		s.mu.Unlock()
		return errResp(fmt.Sprintf("%s is already running", name))
	}

	proc, err := process.Start(cfg.Command, cfg.CWD, cfg.Env)
	if err != nil {
		s.mu.Unlock()
		return errResp(fmt.Sprintf("start process: %v", err))
	}

	pid := proc.Cmd.Process.Pid
	now := time.Now().UTC()
	svc := &managedService{
		cfg: cfg,
		state: &config.ServiceState{
			Status:    config.StatusStarting,
			PID:       &pid,
			StartedAt: &now,
		},
		proc: proc,
	}

	s.services[name] = svc
	_ = s.saveStateLocked()
	s.mu.Unlock()

	go s.teeOutput(name, svc, config.LogPath(name))
	go s.watchExit(name, svc)

	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		// Process already exited. Give watchExit a brief moment to record the
		// terminal status (exited vs crashed) so we can report it accurately.
		for i := 0; i < 20; i++ {
			s.mu.RLock()
			settled := svc.state.Status != config.StatusStarting
			s.mu.RUnlock()
			if settled {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		s.mu.Lock()
		if svc.state.Status == config.StatusStarting {
			svc.state.Status = config.StatusCrashed
			_ = s.saveStateLocked()
		}
		status := svc.state.Status
		exit := 0
		if svc.state.LastExitCode != nil {
			exit = *svc.state.LastExitCode
		}
		s.mu.Unlock()

		if status == config.StatusExited {
			// A one-shot command that finished cleanly — not a failure.
			payload, _ := json.Marshal(ipc.StartResponsePayload{PID: pid})
			return &ipc.Response{OK: true, Payload: json.RawMessage(payload)}
		}
		return errResp(fmt.Sprintf("%s exited immediately with code %d", name, exit))
	}

	s.mu.Lock()
	// Only promote to running if still in starting state (watchExit hasn't fired yet).
	if svc.state.Status == config.StatusStarting {
		svc.state.Status = config.StatusRunning
		_ = s.saveStateLocked()
	}
	currentStatus := svc.state.Status
	s.mu.Unlock()

	if currentStatus == config.StatusExited {
		payload, _ := json.Marshal(ipc.StartResponsePayload{PID: pid})
		return &ipc.Response{OK: true, Payload: json.RawMessage(payload)}
	}
	if currentStatus != config.StatusRunning {
		return errResp("process exited before confirming running state")
	}

	payload, _ := json.Marshal(ipc.StartResponsePayload{PID: pid})
	return &ipc.Response{OK: true, Payload: json.RawMessage(payload)}
}

func (s *supervisor) teeOutput(name string, svc *managedService, logPath string) {
	defer svc.proc.PTY.Close()
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		s.logger.Error("mkdir logs dir", "name", name, "err", err)
		return
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		s.logger.Error("open log file", "name", name, "err", err)
		return
	}
	defer logFile.Close()

	buf := make([]byte, 4096)
	for {
		n, err := svc.proc.PTY.Read(buf)
		if n > 0 {
			_, _ = logFile.Write(buf[:n])
			svc.mu.Lock()
			if svc.attached != nil {
				if _, writeErr := svc.attached.Write(buf[:n]); writeErr != nil {
					svc.attached = nil
				}
			}
			svc.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	// Service exited: notify any attached fg client so it can return to the
	// terminal instead of hanging on a blocked conn.Read.
	svc.mu.Lock()
	if svc.attached != nil {
		_ = svc.attached.Close()
		svc.attached = nil
	}
	svc.mu.Unlock()
}

func (s *supervisor) watchExit(name string, svc *managedService) {
	state, _ := svc.proc.Cmd.Process.Wait()
	exitCode := 0
	signaled := false
	if state != nil {
		exitCode = state.ExitCode()
		if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			signaled = true
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case svc.state.Status == config.StatusStopping:
		// Terminated by `devrun stop`.
		svc.state.Status = config.StatusStopped
	case svc.state.Status == config.StatusStopped ||
		svc.state.Status == config.StatusExited ||
		svc.state.Status == config.StatusCrashed:
		// A terminal state was already recorded by shutdown() or handleStop;
		// don't clobber it (e.g. flip a clean stop back to crashed).
	case !signaled && exitCode == 0:
		// Ran to completion on its own — a clean exit, not a crash.
		svc.state.Status = config.StatusExited
	default:
		// Non-zero exit code or killed by a signal.
		svc.state.Status = config.StatusCrashed
	}
	svc.state.LastExitCode = &exitCode
	svc.state.PID = nil
	_ = s.saveStateLocked()
}

func (s *supervisor) handleStop(raw json.RawMessage) *ipc.Response {
	var p ipc.StopPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResp("invalid stop payload")
	}
	return s.stopService(p.Name)
}

// stopService stops one running service by name, returning an error response if
// it is not running. Safe to call without holding s.mu.
func (s *supervisor) stopService(name string) *ipc.Response {
	s.mu.Lock()
	svc := s.services[name]
	if svc == nil || svc.state.Status == config.StatusStopped || svc.state.Status == config.StatusExited || svc.state.Status == config.StatusCrashed || svc.state.Status == config.StatusStopping {
		s.mu.Unlock()
		return errResp(fmt.Sprintf("%s is not running", name))
	}
	svc.state.Status = config.StatusStopping
	_ = s.saveStateLocked()
	s.mu.Unlock()

	// Re-adopted services have nil proc (PTY master is gone after daemon restart).
	// Terminate them by PID instead.
	if svc.proc == nil {
		if svc.state.PID == nil {
			s.mu.Lock()
			svc.state.Status = config.StatusStopped
			_ = s.saveStateLocked()
			s.mu.Unlock()
			return &ipc.Response{OK: true}
		}
		// Graceful SIGTERM to the process group, SIGKILL only after the grace
		// period. watchExit is gone for re-adopted services, so nothing else
		// will reap this — but the child was started in its own session by a
		// previous daemon, so it has no parent to zombie against.
		if _, err := process.TerminateGroup(*svc.state.PID, process.DefaultStopGrace); err != nil {
			s.logger.Warn("terminate re-adopted service", "name", name, "err", err)
		}
		s.mu.Lock()
		svc.state.Status = config.StatusStopped
		svc.state.PID = nil
		_ = s.saveStateLocked()
		s.mu.Unlock()
		return &ipc.Response{OK: true}
	}

	if err := svc.proc.Stop(); err != nil {
		return errResp(fmt.Sprintf("stop: %v", err))
	}
	return &ipc.Response{OK: true}
}

// handleTargetStart starts every service listed in the payload, then records the
// target as active with the member names as its snapshot. Members already
// running are left as-is; a member that fails to start is reported but does not
// abort the rest.
func (s *supervisor) handleTargetStart(raw json.RawMessage) *ipc.Response {
	var p ipc.TargetStartPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResp("invalid target-start payload")
	}
	if p.Name == "" {
		return errResp("target name is required")
	}
	if len(p.Services) == 0 {
		return errResp(fmt.Sprintf("target %q has no services", p.Name))
	}

	members := make([]string, 0, len(p.Services))
	seen := make(map[string]bool, len(p.Services))
	var failures []string
	for _, cfg := range p.Services {
		if cfg == nil || cfg.Name == "" || seen[cfg.Name] {
			continue
		}
		seen[cfg.Name] = true
		members = append(members, cfg.Name)

		resp := s.startService(cfg.Name, cfg)
		if !resp.OK && !strings.Contains(resp.Error, "already running") {
			failures = append(failures, fmt.Sprintf("%s: %s", cfg.Name, resp.Error))
		}
	}

	s.mu.Lock()
	s.activeTargets[p.Name] = members
	_ = s.saveStateLocked()
	s.mu.Unlock()

	if len(failures) > 0 {
		return errResp(fmt.Sprintf("target %q: %s", p.Name, strings.Join(failures, "; ")))
	}
	return &ipc.Response{OK: true}
}

// handleTargetStop stops the members of an active target, skipping any that are
// still listed under another active target, then clears the target. The member
// list comes from the snapshot taken at target-start time, not the live config.
func (s *supervisor) handleTargetStop(raw json.RawMessage) *ipc.Response {
	var p ipc.TargetStopPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResp("invalid target-stop payload")
	}
	if p.Name == "" {
		return errResp("target name is required")
	}

	s.mu.Lock()
	members, ok := s.activeTargets[p.Name]
	if !ok {
		s.mu.Unlock()
		return errResp(fmt.Sprintf("target %q is not active", p.Name))
	}
	// Names still held by another active target must keep running.
	held := make(map[string]bool)
	for other, otherMembers := range s.activeTargets {
		if other == p.Name {
			continue
		}
		for _, m := range otherMembers {
			held[m] = true
		}
	}
	toStop := make([]string, 0, len(members))
	for _, m := range members {
		if !held[m] {
			toStop = append(toStop, m)
		}
	}
	delete(s.activeTargets, p.Name)
	_ = s.saveStateLocked()
	s.mu.Unlock()

	var failures []string
	for _, name := range toStop {
		resp := s.stopService(name)
		if !resp.OK && !strings.Contains(resp.Error, "is not running") {
			failures = append(failures, fmt.Sprintf("%s: %s", name, resp.Error))
		}
	}
	if len(failures) > 0 {
		return errResp(fmt.Sprintf("target %q: %s", p.Name, strings.Join(failures, "; ")))
	}
	return &ipc.Response{OK: true}
}

func (s *supervisor) handleRemove(raw json.RawMessage) *ipc.Response {
	var p ipc.RemovePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResp("invalid remove payload")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	svc := s.services[p.Name]
	if svc != nil && (svc.state.Status == config.StatusRunning || svc.state.Status == config.StatusStarting) {
		return errResp(fmt.Sprintf("%s is running; stop it before removing", p.Name))
	}
	delete(s.services, p.Name)
	_ = s.saveStateLocked()
	return &ipc.Response{OK: true}
}

func (s *supervisor) handleList() *ipc.Response {
	// Fresh registry read so services added via `devrun add` appear immediately,
	// even before they have been started for the first time.
	reg, _ := config.LoadRegistry(config.RegistryPath())

	// Collect a snapshot of state fields under the read lock so that blocking
	// syscalls (CPUPercent, MemBytes) do not hold the lock and stall writers.
	type snapshot struct {
		name      string
		state     string
		pid       *int
		port      *int
		group     string
		startedAt *time.Time
	}

	s.mu.RLock()
	activeTargets := make([]string, 0, len(s.activeTargets))
	for name := range s.activeTargets {
		activeTargets = append(activeTargets, name)
	}
	snaps := make([]snapshot, 0, len(s.services))
	seen := make(map[string]bool, len(s.services))
	for name, svc := range s.services {
		seen[name] = true
		snap := snapshot{
			name:      name,
			state:     string(svc.state.Status),
			pid:       svc.state.PID,
			port:      svc.state.Port,
			startedAt: svc.state.StartedAt,
		}
		if svc.cfg != nil {
			snap.group = svc.cfg.Group
		}
		snaps = append(snaps, snap)
	}
	s.mu.RUnlock()

	// Append registry-only services (never started, so not in s.services).
	if reg != nil {
		for name, cfg := range reg.Services {
			if seen[name] {
				continue
			}
			snap := snapshot{
				name:  name,
				state: string(config.StatusStopped),
			}
			if cfg != nil {
				snap.group = cfg.Group
			}
			snaps = append(snaps, snap)
		}
	}

	// Compute CPU/mem outside the lock — these do blocking system calls.
	services := make([]ipc.ServiceInfo, 0, len(snaps))
	for _, snap := range snaps {
		info := ipc.ServiceInfo{
			Name:  snap.name,
			State: snap.state,
			PID:   snap.pid,
			Port:  snap.port,
			Group: snap.group,
		}
		if snap.startedAt != nil && config.ServiceStatus(snap.state).IsLive() {
			info.UptimeSec = int64(time.Since(*snap.startedAt).Seconds())
		}
		if snap.pid != nil {
			info.CPUPct, _ = process.CPUPercent(*snap.pid)
			info.MemBytes, _ = process.MemBytes(*snap.pid)
		}
		services = append(services, info)
	}

	sort.Strings(activeTargets)
	payload, _ := json.Marshal(ipc.ListResponsePayload{Services: services, ActiveTargets: activeTargets})
	return &ipc.Response{OK: true, Payload: json.RawMessage(payload)}
}

// shutdown gracefully terminates every managed service that is still running,
// all in parallel: SIGTERM to each process group, then SIGKILL only for the
// ones still alive after the grace period. It is invoked when the daemon
// receives SIGTERM/SIGINT — not on `devrun daemon stop`, which deliberately
// leaves services running for re-adoption.
func (s *supervisor) shutdown() {
	type target struct {
		name string
		proc *process.Process
		pid  int
	}

	s.mu.Lock()
	var targets []target
	for name, svc := range s.services {
		if svc.state.Status != config.StatusRunning && svc.state.Status != config.StatusStarting {
			continue
		}
		t := target{name: name, proc: svc.proc}
		if svc.state.PID != nil {
			t.pid = *svc.state.PID
		}
		targets = append(targets, t)
		// Mark stopping now so watchExit records a clean stop, not a crash.
		svc.state.Status = config.StatusStopping
	}
	_ = s.saveStateLocked()
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			switch {
			case t.proc != nil:
				_ = t.proc.Stop()
			case t.pid > 0:
				_, _ = process.TerminateGroup(t.pid, process.DefaultStopGrace)
			}
			s.logger.Info("stopped service on daemon shutdown", "name", t.name)
		}(t)
	}
	wg.Wait()

	// Record the terminal state synchronously — the daemon is about to exit and
	// per-service watchExit goroutines may not get to run.
	s.mu.Lock()
	for _, t := range targets {
		if svc := s.services[t.name]; svc != nil {
			svc.state.Status = config.StatusStopped
			svc.state.PID = nil
		}
	}
	_ = s.saveStateLocked()
	s.mu.Unlock()
}

func errResp(msg string) *ipc.Response {
	return &ipc.Response{OK: false, Error: msg}
}
