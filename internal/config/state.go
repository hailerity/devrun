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
)

type ServiceState struct {
	Status       ServiceStatus `json:"state"`
	PID          *int          `json:"pid"`
	Port         *int          `json:"port"`
	StartedAt    *time.Time    `json:"started_at"`
	LastExitCode *int          `json:"last_exit_code"`
	ReAdopted    bool          `json:"re_adopted"`
}

type State struct {
	Version  int                      `json:"version"`
	Services map[string]*ServiceState `json:"services"`
}

// LoadState reads state.json. Returns an empty state if the file does not exist.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Version: 1, Services: make(map[string]*ServiceState)}, nil
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
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp state: %w", err)
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
