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

// TunnelState is the running cloudflared, recorded for the same reason
// GatewayState is: the process outlives the daemon on every exit path but
// SIGTERM, so without a record a replacement daemon could neither see it nor
// stop it, while it went on publishing.
type TunnelState struct {
	PID  int    `json:"pid"`
	Kind string `json:"kind"` // "named" | "quick"
	// Origin is the gateway address cloudflared was pointed at, so a new
	// gateway can tell whether it has actually moved.
	Origin    string       `json:"origin"`
	PublicURL string       `json:"public_url"`
	Config    TunnelConfig `json:"config"`
}

type State struct {
	Version  int                      `json:"version"`
	Services map[string]*ServiceState `json:"services"`
	Gateway  *GatewayState            `json:"gateway,omitempty"`
	Tunnel   *TunnelState             `json:"tunnel,omitempty"`
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

// ReAdoptServices decides, for each service with a recorded pid, whether that
// process is still the one devrun started. Alive and recognised →
// StatusRunning with ReAdopted set; anything else → StatusCrashed with the
// pid cleared. Modifies the map in place.
//
// recognise is asked only about pids that are alive, and a nil recognise
// keeps the liveness-only behaviour. It must answer true whenever it cannot
// tell: see the daemon's recogniseService for why a false negative is the
// more expensive mistake.
//
// The liveness probe treats EPERM as dead, unlike the one used for devrun's
// own children. That is deliberate here. EPERM means the process belongs to
// another user, which a service devrun spawned never does — so it is a
// stranger who inherited the pid, and refusing to adopt it is the point.
func ReAdoptServices(services map[string]*ServiceState, recognise func(name string, pid int) bool) {
	for name, svc := range services {
		if svc.PID == nil {
			continue
		}
		if syscall.Kill(*svc.PID, 0) == nil && (recognise == nil || recognise(name, *svc.PID)) {
			svc.Status = StatusRunning
			svc.ReAdopted = true
			continue
		}
		svc.Status = StatusCrashed
		svc.PID = nil
	}
}
