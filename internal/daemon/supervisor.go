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

	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/ipc"
	"github.com/hailerity/procet/internal/process"
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
		return errResp(fmt.Sprintf("service %q not registered. Run 'procet add %s <cmd>' first.", p.Name, p.Name))
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
		s.mu.Lock()
		svc.state.Status = config.StatusCrashed
		_ = s.saveStateLocked()
		s.mu.Unlock()
		return errResp("process died immediately after start")
	}

	s.mu.Lock()
	svc.state.Status = config.StatusRunning
	_ = s.saveStateLocked()
	s.mu.Unlock()

	payload, _ := json.Marshal(ipc.StartResponsePayload{PID: pid})
	return &ipc.Response{OK: true, Payload: json.RawMessage(payload)}
}

func (s *supervisor) teeOutput(name string, svc *managedService, logPath string) {
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
	if svc == nil || svc.state.Status == config.StatusStopped || svc.state.Status == config.StatusCrashed {
		s.mu.Unlock()
		return errResp(fmt.Sprintf("%s is not running", p.Name))
	}
	svc.state.Status = config.StatusStopping
	_ = s.saveStateLocked()
	s.mu.Unlock()

	if err := svc.proc.Stop(); err != nil {
		return errResp(fmt.Sprintf("stop %s: %v", p.Name, err))
	}
	return &ipc.Response{OK: true}
}

func (s *supervisor) handleList() *ipc.Response {
	s.mu.RLock()
	defer s.mu.RUnlock()

	services := make([]ipc.ServiceInfo, 0, len(s.services))
	for name, svc := range s.services {
		info := ipc.ServiceInfo{
			Name:  name,
			State: string(svc.state.Status),
			PID:   svc.state.PID,
			Port:  svc.state.Port,
		}
		if svc.cfg != nil {
			info.Group = svc.cfg.Group
		}
		if svc.state.StartedAt != nil {
			info.UptimeSec = int64(time.Since(*svc.state.StartedAt).Seconds())
		}
		if svc.state.PID != nil {
			info.CPUPct, _ = process.CPUPercent(*svc.state.PID)
			info.MemBytes, _ = process.MemBytes(*svc.state.PID)
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

// startPortPoller stub — implemented in recovery.go (Task 12)
func (s *supervisor) startPortPoller() {}

func errResp(msg string) *ipc.Response {
	return &ipc.Response{OK: false, Error: msg}
}
