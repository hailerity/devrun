package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/process"
)

// Environment keys used to hand state from a daemon to the replacement process
// it exec's during `devrun daemon restart`.
const (
	reexecEnvActive = "DEVRUN_REEXEC"       // "1" in the replacement
	reexecEnvState  = "DEVRUN_REEXEC_STATE" // JSON-encoded handoffState
)

// firstHandoffFD is the fd number the first inherited PTY master lands on in the
// replacement: 0/1/2 are stdio, PTY masters follow.
const firstHandoffFD = 3

// handoffService is one managed service carried across a graceful re-exec.
type handoffService struct {
	Name      string                `json:"name"`
	PID       int                   `json:"pid"`
	PTYFD     int                   `json:"pty_fd"` // fd number in the replacement
	Status    config.ServiceStatus  `json:"status"`
	StartedAt *time.Time            `json:"started_at,omitempty"`
	Port      *int                  `json:"port,omitempty"`
	Config    *config.ServiceConfig `json:"config,omitempty"`
}

type handoffState struct {
	Version  int              `json:"version"`
	Services []handoffService `json:"services"`
}

// reexecHandoff returns the decoded handoff state when this process was started
// as the replacement in a graceful daemon re-exec.
func reexecHandoff() (*handoffState, bool) {
	if os.Getenv(reexecEnvActive) != "1" {
		return nil, false
	}
	hs := &handoffState{}
	if raw := os.Getenv(reexecEnvState); raw != "" {
		if err := json.Unmarshal([]byte(raw), hs); err != nil {
			return &handoffState{}, true
		}
	}
	return hs, true
}

// reexec launches a fresh copy of the binary as the replacement daemon, handing
// over the live PTY master for every managed service so log capture and
// interactive attach continue without a gap. It does not stop this daemon —
// the caller does that once reexec returns nil. On any failure it returns an
// error and the caller keeps serving.
func (s *supervisor) reexec() error {
	self := os.Getenv("DEVRUN_DAEMON_BIN")
	if self == "" {
		var err error
		if self, err = os.Executable(); err != nil {
			return fmt.Errorf("find executable: %w", err)
		}
	}

	s.mu.RLock()
	extra := make([]*os.File, 0, len(s.services))
	handed := make([]*process.Process, 0, len(s.services))
	hs := &handoffState{Version: 1}
	for name, svc := range s.services {
		if svc.proc == nil || svc.proc.PTY == nil {
			continue
		}
		if svc.state.Status != config.StatusRunning && svc.state.Status != config.StatusStarting {
			continue
		}
		hs.Services = append(hs.Services, handoffService{
			Name:      name,
			PID:       svc.proc.Pid,
			PTYFD:     firstHandoffFD + len(extra),
			Status:    svc.state.Status,
			StartedAt: svc.state.StartedAt,
			Port:      svc.state.Port,
			Config:    svc.cfg,
		})
		extra = append(extra, svc.proc.PTY)
		handed = append(handed, svc.proc)
	}
	s.mu.RUnlock()

	stateJSON, err := json.Marshal(hs)
	if err != nil {
		return fmt.Errorf("marshal handoff: %w", err)
	}

	files := append([]*os.File{os.Stdin, os.Stdout, os.Stderr}, extra...)
	env := append(os.Environ(), reexecEnvActive+"=1", reexecEnvState+"="+string(stateJSON))

	proc, err := os.StartProcess(self, []string{self, "--_daemon", s.socketPath}, &os.ProcAttr{
		Env:   env,
		Files: files,
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		return fmt.Errorf("start replacement daemon: %w", err)
	}
	_ = proc.Release()

	// The replacement holds its own dup of every PTY master now. Close ours so
	// this daemon's teeOutput goroutines stop draining the same streams — two
	// readers on one PTY master would split the output between them during the
	// brief overlap.
	for _, p := range handed {
		_ = p.PTY.Close()
	}

	s.logger.Info("re-exec: replacement daemon launched", "pid", proc.Pid, "services", len(hs.Services))
	return nil
}

// adoptHandoff wires up services inherited from a predecessor daemon during a
// graceful re-exec. Each keeps its live PTY master, so teeOutput resumes
// appending to the log file and attach works immediately. These processes are
// NOT children of this daemon, so exit is detected by polling the PID rather
// than by Wait().
func (s *supervisor) adoptHandoff(hs *handoffState) {
	if hs == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range hs.Services {
		if h.PID <= 0 || syscall.Kill(h.PID, 0) != nil {
			s.logger.Warn("re-exec: handed-off service is gone", "name", h.Name, "pid", h.PID)
			continue
		}
		pty := process.AdoptPTY(uintptr(h.PTYFD), "pty:"+h.Name)
		if pty == nil {
			s.logger.Warn("re-exec: missing PTY fd", "name", h.Name, "fd", h.PTYFD)
			continue
		}
		cfg := h.Config
		if cfg == nil {
			cfg = &config.ServiceConfig{Name: h.Name}
		}
		pid := h.PID
		status := h.Status
		if status != config.StatusRunning && status != config.StatusStarting {
			status = config.StatusRunning
		}
		svc := &managedService{
			cfg:  cfg,
			proc: &process.Process{PTY: pty, Pid: pid},
			state: &config.ServiceState{
				Status:    status,
				PID:       &pid,
				StartedAt: h.StartedAt,
				Port:      h.Port,
			},
		}
		s.services[h.Name] = svc
		go s.teeOutput(h.Name, svc, config.LogPath(h.Name))
		go s.watchHandoffExit(h.Name, svc)
	}
	_ = s.saveStateLocked()
}

// watchHandoffExit replaces watchExit for services adopted across a re-exec:
// the process is not our child, so its exit is observed by polling the PID.
func (s *supervisor) watchHandoffExit(name string, svc *managedService) {
	pid := svc.proc.Pid
	for {
		time.Sleep(500 * time.Millisecond)
		if syscall.Kill(pid, 0) != nil {
			break
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch svc.state.Status {
	case config.StatusStopping:
		svc.state.Status = config.StatusStopped
	case config.StatusStopped, config.StatusExited, config.StatusCrashed:
		// Terminal state already recorded elsewhere; keep it.
	default:
		// No exit code is available for a process that is not our child.
		svc.state.Status = config.StatusCrashed
	}
	svc.state.PID = nil
	_ = s.saveStateLocked()
}
