package ops

import (
	"encoding/json"
	"fmt"
	"sort"
	"syscall"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/ipc"
)

// ListResult is the live state of every service in a scope.
type ListResult struct {
	// Services is every service the resolved config defines, sorted by name,
	// with a never-started service reported as "stopped".
	Services []ipc.ServiceInfo
	// ActiveTargets names the targets the daemon currently records as started.
	ActiveTargets map[string]bool
	// Offline is true when the daemon could not be reached and Services was
	// read from the last-saved state file instead.
	Offline bool
}

// List reports every service in the resolved scope with its live state. It
// starts the daemon when needed so the list reflects live state; if the daemon
// cannot be reached anyway, it falls back to the last-saved state file and
// sets Offline.
func List(r *Resolved) (*ListResult, error) {
	socketPath := config.SocketPath()
	_ = daemon.EnsureDaemon(socketPath)

	c, err := client.Connect(socketPath)
	if err != nil {
		return listOffline(r.Registry)
	}
	defer c.Close()

	resp, err := c.Send("list", struct{}{})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var payload ipc.ListResponsePayload
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return nil, fmt.Errorf("parse list response: %w", err)
	}
	active := make(map[string]bool, len(payload.ActiveTargets))
	for _, n := range payload.ActiveTargets {
		active[n] = true
	}
	return &ListResult{Services: ScopeToRegistry(payload.Services, r.Registry), ActiveTargets: active}, nil
}

// ScopeToRegistry filters daemon-reported services down to the names present in
// reg, appending registry-only services as "stopped", sorted by name.
func ScopeToRegistry(svcs []ipc.ServiceInfo, reg *config.Registry) []ipc.ServiceInfo {
	byName := make(map[string]ipc.ServiceInfo, len(svcs))
	for _, s := range svcs {
		byName[s.Name] = s
	}

	names := make([]string, 0, len(reg.Services))
	for name := range reg.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ipc.ServiceInfo, 0, len(names))
	for _, name := range names {
		info, ok := byName[name]
		if !ok {
			info = ipc.ServiceInfo{Name: name, State: string(config.StatusStopped)}
		}
		if cfg := reg.Services[name]; cfg != nil && cfg.Group != "" {
			info.Group = cfg.Group
		}
		out = append(out, info)
	}
	return out
}

// listOffline reads the resolved registry and the last-saved state file
// directly, for when the daemon is not running. A service the state file says
// is running but whose process is gone is reported as crashed.
func listOffline(reg *config.Registry) (*ListResult, error) {
	state, err := config.LoadState(config.StatePath())
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	names := make([]string, 0, len(reg.Services))
	for name := range reg.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	var svcs []ipc.ServiceInfo
	for _, name := range names {
		svcState := state.Services[name]
		info := ipc.ServiceInfo{Name: name}

		if reg.Services[name] != nil {
			info.Group = reg.Services[name].Group
		}

		if svcState == nil {
			info.State = string(config.StatusStopped)
		} else {
			status := svcState.Status
			// If the state file says running/starting, verify the process is still alive.
			if (status == config.StatusRunning || status == config.StatusStarting) && svcState.PID != nil {
				if syscall.Kill(*svcState.PID, 0) != nil {
					status = config.StatusCrashed
				} else {
					info.PID = svcState.PID
					info.Port = svcState.Port
				}
			}
			info.State = string(status)
		}
		svcs = append(svcs, info)
	}
	return &ListResult{Services: svcs, ActiveTargets: map[string]bool{}, Offline: true}, nil
}

// ActiveTargets asks a running daemon which targets are active. Best effort: an
// unreachable daemon yields an empty set rather than an error, and it never
// starts a daemon just to answer.
func ActiveTargets() map[string]bool {
	out := map[string]bool{}
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return out
	}
	defer c.Close()
	resp, err := c.Send("list", struct{}{})
	if err != nil || !resp.OK {
		return out
	}
	var payload ipc.ListResponsePayload
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return out
	}
	for _, n := range payload.ActiveTargets {
		out[n] = true
	}
	return out
}

// ServiceState returns the last-saved state of a service from the state file,
// or nil when it has never been started. It does not contact the daemon.
func ServiceState(name string) (*config.ServiceState, error) {
	state, err := config.LoadState(config.StatePath())
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	return state.Services[name], nil
}
