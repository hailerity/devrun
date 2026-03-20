package daemon

import (
	"time"

	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/process"
)

// startPortPoller polls each running service's port every 5 seconds.
// Call this as a goroutine after daemon startup.
func (s *supervisor) startPortPoller() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.pollPorts()
	}
}

func (s *supervisor) pollPorts() {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, svc := range s.services {
		if svc.state.Status != config.StatusRunning || svc.state.PID == nil {
			continue
		}
		port, err := process.DetectPort(*svc.state.PID)
		if err != nil || port == 0 {
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
