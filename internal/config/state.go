package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type ServiceStatus string

const (
	StatusStopped  ServiceStatus = "stopped"
	StatusStarting ServiceStatus = "starting"
	StatusRunning  ServiceStatus = "running"
	StatusCrashed  ServiceStatus = "crashed"
	StatusStopping ServiceStatus = "stopping"
	// StatusExited marks a process that terminated on its own with exit code 0
	// (e.g. a one-shot command), as opposed to StatusCrashed (non-zero exit or
	// killed by signal) or StatusStopped (terminated by `devrun stop`).
	StatusExited ServiceStatus = "exited"
)

// IsLive reports whether the process is (or should be) running — i.e. uptime,
// CPU, and memory are meaningful. Terminal states (stopped, exited, crashed)
// are not live.
func (s ServiceStatus) IsLive() bool {
	switch s {
	case StatusStarting, StatusRunning, StatusStopping:
		return true
	default:
		return false
	}
}

type ServiceState struct {
	Status       ServiceStatus `json:"state"`
	PID          *int          `json:"pid"`
	Port         *int          `json:"port"`
	StartedAt    *time.Time    `json:"started_at"`
	LastExitCode *int          `json:"last_exit_code"`
	ReAdopted    bool          `json:"re_adopted"`
}

// GatewayState is the running gateway, recorded so a replacement daemon can
// take it back.
//
// The gateway is spawned into its own session and never waited on, so it
// outlives the daemon on every path except SIGTERM — which is deliberate, the
// same as for services: `devrun daemon restart` should not drop a published
// URL. Without this record the replacement daemon had no handle on it, and the
// gateway became unstoppable and invisible while still holding its port.
//
// Token is here because it cannot be re-derived: the running child was given
// it at startup and a new daemon cannot change its mind. It is why state.json
// is written 0600.
type GatewayState struct {
	PID    int           `json:"pid"`
	Addr   string        `json:"addr"`
	Token  string        `json:"token"`
	Config GatewayConfig `json:"config"`
}

type State struct {
	Version  int                      `json:"version"`
	Services map[string]*ServiceState `json:"services"`
	Gateway  *GatewayState            `json:"gateway,omitempty"`
	// ActiveTargets maps a currently-started target to the member service names
	// captured when it was started. `devrun target stop` consults this snapshot —
	// not the live config — so editing a target's membership while it runs does
	// not change what a later stop releases. A service is left running on stop if
	// it still appears under another active target.
	ActiveTargets map[string][]string `json:"active_targets,omitempty"`
}

// LoadState reads state.json. Returns an empty state if the file does not exist.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{
			Version:       1,
			Services:      make(map[string]*ServiceState),
			ActiveTargets: make(map[string][]string),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Services == nil {
		s.Services = make(map[string]*ServiceState)
	}
	if s.ActiveTargets == nil {
		s.ActiveTargets = make(map[string][]string)
	}
	return &s, nil
}

// SaveState writes state atomically: write to .tmp, then rename.
func SaveState(path string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	tmp := path + ".tmp"
	// 0600: the gateway's token lives here, and it is the only thing between a
	// stranger and the services once the gateway is published.
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write tmp state: %w", err)
	}
	// WriteFile applies the mode only when it creates the file, so a .tmp left
	// behind by an earlier crash would keep whatever mode it had.
	if err := os.Chmod(tmp, 0600); err != nil {
		return fmt.Errorf("chmod tmp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// ReAdoptServices checks each service's PID to determine if it is still alive.
// Alive → StatusRunning + ReAdopted=true. Dead → StatusCrashed, PID cleared.
// Modifies the map in place. Only checks services that have a non-nil PID.
func ReAdoptServices(services map[string]*ServiceState) {
	for _, svc := range services {
		if svc.PID == nil {
			continue
		}
		err := syscall.Kill(*svc.PID, 0)
		if err == nil {
			svc.Status = StatusRunning
			svc.ReAdopted = true
		} else {
			svc.Status = StatusCrashed
			svc.PID = nil
		}
	}
}
