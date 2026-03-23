package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
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
	statePath  string
	registry   *config.Registry
}

func newSupervisor(socketPath string, logger *slog.Logger) *supervisor {
	return &supervisor{
		socketPath: socketPath,
		logger:     logger,
		services:   make(map[string]*managedService),
		statePath:  config.StatePath(),
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
	return s.saveStateLocked()
}

func (s *supervisor) saveStateLocked() error {
	state := &config.State{Version: 1, Services: make(map[string]*config.ServiceState)}
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
	case "remove":
		_ = ipc.WriteMessage(conn, s.handleRemove(req.Payload))
	case "list":
		_ = ipc.WriteMessage(conn, s.handleList())
	case "attach":
		s.handleAttach(conn, req.Payload)
	default:
		_ = ipc.WriteMessage(conn, errResp(fmt.Sprintf("unknown request type: %s", req.Type)))
	}
}

func (s *supervisor) handleStart(raw json.RawMessage) *ipc.Response {
	var p ipc.StartPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResp("invalid start payload")
	}

	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return errResp(fmt.Sprintf("load registry: %v", err))
	}
	cfg := reg.Services[p.Name]
	if cfg == nil {
		return errResp(fmt.Sprintf("service %q not registered. Run 'devrun add %s <cmd>' first.", p.Name, p.Name))
	}

	s.mu.Lock()
	existing := s.services[p.Name]
	if existing != nil && (existing.state.Status == config.StatusRunning || existing.state.Status == config.StatusStarting) {
		s.mu.Unlock()
		return errResp(fmt.Sprintf("%s is already running", p.Name))
	}
	s.mu.Unlock()

	proc, err := process.Start(cfg.Command, cfg.CWD, cfg.Env)
	if err != nil {
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

	s.mu.Lock()
	s.services[p.Name] = svc
	_ = s.saveStateLocked()
	s.mu.Unlock()

	go s.teeOutput(p.Name, svc, config.LogPath(p.Name))
	go s.watchExit(p.Name, svc)

	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		// Process already exited — watchExit may have already set crashed, or set it here.
		s.mu.Lock()
		if svc.state.Status == config.StatusStarting {
			svc.state.Status = config.StatusCrashed
			_ = s.saveStateLocked()
		}
		s.mu.Unlock()
		return errResp(fmt.Sprintf("process died immediately: PID %d", pid))
	}

	s.mu.Lock()
	// Only promote to running if still in starting state (watchExit hasn't fired yet).
	if svc.state.Status == config.StatusStarting {
		svc.state.Status = config.StatusRunning
		_ = s.saveStateLocked()
	}
	currentStatus := svc.state.Status
	s.mu.Unlock()

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
	if state != nil {
		exitCode = state.ExitCode()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if svc.state.Status == config.StatusStopping {
		svc.state.Status = config.StatusStopped
	} else {
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

	s.mu.Lock()
	svc := s.services[p.Name]
	if svc == nil || svc.state.Status == config.StatusStopped || svc.state.Status == config.StatusCrashed || svc.state.Status == config.StatusStopping {
		s.mu.Unlock()
		return errResp(fmt.Sprintf("%s is not running", p.Name))
	}
	svc.state.Status = config.StatusStopping
	_ = s.saveStateLocked()
	s.mu.Unlock()

	// Re-adopted services have nil proc (PTY master is gone after daemon restart).
	// Send SIGTERM directly to the PID and poll for exit.
	if svc.proc == nil {
		if svc.state.PID == nil {
			s.mu.Lock()
			svc.state.Status = config.StatusStopped
			_ = s.saveStateLocked()
			s.mu.Unlock()
			return &ipc.Response{OK: true}
		}
		pid := *svc.state.PID
		_ = syscall.Kill(pid, syscall.SIGTERM)
		// Poll up to 5s for the process to exit
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			if err := syscall.Kill(pid, 0); err != nil {
				break // process is gone
			}
		}
		// Force-kill if still alive
		_ = syscall.Kill(pid, syscall.SIGKILL)
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
	snaps := make([]snapshot, 0, len(s.services))
	for name, svc := range s.services {
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
		if snap.startedAt != nil {
			info.UptimeSec = int64(time.Since(*snap.startedAt).Seconds())
		}
		if snap.pid != nil {
			info.CPUPct, _ = process.CPUPercent(*snap.pid)
			info.MemBytes, _ = process.MemBytes(*snap.pid)
		}
		services = append(services, info)
	}

	payload, _ := json.Marshal(ipc.ListResponsePayload{Services: services})
	return &ipc.Response{OK: true, Payload: json.RawMessage(payload)}
}

func (s *supervisor) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, svc := range s.services {
		if svc.proc != nil && (svc.state.Status == config.StatusRunning || svc.state.Status == config.StatusStarting) {
			_ = svc.proc.Stop()
		}
	}
}


func errResp(msg string) *ipc.Response {
	return &ipc.Response{OK: false, Error: msg}
}
