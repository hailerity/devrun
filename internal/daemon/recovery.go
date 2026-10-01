package daemon

import (
	"context"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/process"
)

// startPortPoller polls each running service's port every 5 seconds.
// Call this as a goroutine after daemon startup.
func (s *supervisor) startPortPoller(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollPorts()
		}
	}
}

func (s *supervisor) pollPorts() {
	// Snapshot the pids under the lock and detect without holding it. Detection
	// forks ps and lsof; this used to keep s.mu for the whole walk, so any list
	// request landing mid-poll waited behind one exec per running service.
	s.mu.RLock()
	pids := make([]int, 0, len(s.services))
	owner := make(map[int]string, len(s.services))
	for name, svc := range s.services {
		if svc.state.Status != config.StatusRunning || svc.state.PID == nil {
			continue
		}
		pids = append(pids, *svc.state.PID)
		owner[*svc.state.PID] = name
	}
	s.mu.RUnlock()

	if len(pids) == 0 {
		return
	}
	ports := process.DetectPorts(pids)
	if len(ports) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for pid, port := range ports {
		svc, ok := s.services[owner[pid]]
		// A service can have been stopped or restarted while the lock was down,
		// so only apply a port to the pid it was actually read from.
		if !ok || svc.state.PID == nil || *svc.state.PID != pid {
			continue
		}
		if svc.state.Port == nil || *svc.state.Port != port {
			p := port
			svc.state.Port = &p
			changed = true
		}
	}
	if changed {
		_ = s.saveStateLocked()
	}
}
