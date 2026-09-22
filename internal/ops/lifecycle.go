package ops

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/ipc"
)

// EnsureDaemon starts the daemon if it is not already running.
func EnsureDaemon() error {
	if err := daemon.EnsureDaemon(config.SocketPath()); err != nil {
		return fmt.Errorf("could not start daemon: %w", err)
	}
	return nil
}

// ErrNoDaemon means the daemon is not running. Stop operations return an error
// matching it (errors.Is) rather than starting a daemon just to find nothing
// running.
var ErrNoDaemon = errors.New("no daemon running")

// noDaemonError keeps the connection error's own text — which the CLI has
// always printed — while still matching ErrNoDaemon.
type noDaemonError struct{ err error }

func (e noDaemonError) Error() string        { return "connect to daemon: " + e.err.Error() }
func (e noDaemonError) Unwrap() error        { return e.err }
func (e noDaemonError) Is(target error) bool { return target == ErrNoDaemon }

// StartResult is the daemon's answer to a start request.
type StartResult struct {
	// AlreadyRunning is true when the service was running before the request.
	// That is not an error: the service is in the state the caller wanted.
	AlreadyRunning bool
	// Message is the daemon's own wording when AlreadyRunning.
	Message string
}

// Start asks the daemon to start one service. cfg is the definition to ship
// inline — Resolved.InlineConfig — and nil for a global service, which the
// daemon resolves from services.yaml itself. The daemon must already be
// running; see EnsureDaemon.
func Start(name string, cfg *config.ServiceConfig) (*StartResult, error) {
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()

	resp, err := c.Send("start", ipc.StartPayload{Name: name, Config: cfg})
	if err != nil {
		return nil, fmt.Errorf("start request: %w", err)
	}
	if !resp.OK {
		if resp.Error != "" && strings.HasSuffix(resp.Error, "is already running") {
			return &StartResult{AlreadyRunning: true, Message: resp.Error}, nil
		}
		// The definition went inline, so "not registered" can only mean the
		// running daemon is an older build that ignores it.
		if cfg != nil && strings.Contains(resp.Error, "not registered") {
			return nil, fmt.Errorf("the running daemon is from an older devrun and does not understand project %s services; run 'devrun daemon restart' and try again", config.ProjectFileName)
		}
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return &StartResult{}, nil
}

// StartTarget asks the daemon to start every service in a target, recording it
// as an active target. It returns how many services the target has. It starts
// the daemon when needed.
func StartTarget(r *Resolved, name string) (int, error) {
	if _, ok := r.Registry.Targets[name]; !ok {
		return 0, fmt.Errorf("target %q does not exist", name)
	}
	members := r.Registry.TargetMemberConfigs(name)
	if len(members) == 0 {
		return 0, fmt.Errorf("target %q has no runnable services", name)
	}
	if err := EnsureDaemon(); err != nil {
		return 0, err
	}
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return 0, fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()

	resp, err := c.Send("target-start", ipc.TargetStartPayload{Name: name, Services: members})
	if err != nil {
		return 0, fmt.Errorf("target-start request: %w", err)
	}
	if !resp.OK {
		return 0, fmt.Errorf("%s", resp.Error)
	}
	return len(members), nil
}

// StopResult is the daemon's answer to a stop request.
type StopResult struct {
	// NotStopped is true when the daemon declined, typically because the
	// service was not running. Message carries its reason.
	NotStopped bool
	Message    string
}

// Stop asks the daemon to stop one service. It does not start a daemon:
// without one, nothing is running, and it returns ErrNoDaemon.
func Stop(name string) (*StopResult, error) {
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return nil, noDaemonError{err}
	}
	defer c.Close()
	resp, err := c.Send("stop", ipc.StopPayload{Name: name})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return &StopResult{NotStopped: true, Message: resp.Error}, nil
	}
	return &StopResult{}, nil
}

// StopTarget asks the daemon to stop a target. The daemon keeps any member still
// held by another active target. Without a daemon it returns ErrNoDaemon.
func StopTarget(name string) error {
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return noDaemonError{err}
	}
	defer c.Close()

	resp, err := c.Send("target-stop", ipc.TargetStopPayload{Name: name})
	if err != nil {
		return fmt.Errorf("target-stop request: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
