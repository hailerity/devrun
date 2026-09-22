package ops

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

// Outcome is how a start ended, for one service or overall.
type Outcome string

const (
	// OutcomeRunning: up, and stayed up for the settle period.
	OutcomeRunning Outcome = "running"
	// OutcomeAlreadyRunning: it was running before the request. Success.
	OutcomeAlreadyRunning Outcome = "already_running"
	// OutcomeStarted: the start was accepted and the caller chose not to wait.
	OutcomeStarted Outcome = "started"
	// OutcomeCrashed: it died with a non-zero code or a signal.
	OutcomeCrashed Outcome = "crashed"
	// OutcomeExited: it finished on its own with code 0 — a one-shot command.
	OutcomeExited Outcome = "exited"
	// OutcomeStopped: something stopped it while we were waiting.
	OutcomeStopped Outcome = "stopped"
	// OutcomeFailed: no process was started at all (bad command, bad cwd, …).
	OutcomeFailed Outcome = "failed"
	// OutcomeTimeout: neither up nor down when the timeout expired.
	OutcomeTimeout Outcome = "timeout"
)

// OK reports whether the outcome means the service is running.
func (o Outcome) OK() bool {
	return o == OutcomeRunning || o == OutcomeAlreadyRunning || o == OutcomeStarted
}

// WaitOptions controls StartAndWait. Zero values take the defaults.
type WaitOptions struct {
	// NoWait returns as soon as the daemon has accepted the start.
	NoWait bool
	// Timeout bounds the whole wait. Default 15s.
	Timeout time.Duration
	// Settle is how long a service must stay running to count as up, which is
	// what catches a crash a moment after boot. Default 2s.
	Settle time.Duration
	// Poll is the interval between state checks. Default 250ms.
	Poll time.Duration
	// TailLines is how many log lines to attach when a service did not come up.
	// Default 50.
	TailLines int
}

func (o WaitOptions) withDefaults() WaitOptions {
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	if o.Settle <= 0 {
		o.Settle = 2 * time.Second
	}
	if o.Poll <= 0 {
		o.Poll = 250 * time.Millisecond
	}
	if o.TailLines <= 0 {
		o.TailLines = 50
	}
	return o
}

// ServiceOutcome is how one service's start ended.
type ServiceOutcome struct {
	Name     string   `json:"name"`
	Outcome  Outcome  `json:"outcome"`
	State    string   `json:"state"`
	PID      *int     `json:"pid,omitempty"`
	Port     *int     `json:"port,omitempty"`
	ExitCode *int     `json:"exit_code,omitempty"`
	Error    string   `json:"error,omitempty"`    // the daemon's reason, for OutcomeFailed
	LogTail  []string `json:"log_tail,omitempty"` // when it did not come up
}

// StartWaitResult is the outcome of StartAndWait.
type StartWaitResult struct {
	// Outcome is the overall result: for a target, running only when every
	// member is running, otherwise the worst member outcome.
	Outcome  Outcome          `json:"outcome"`
	Services []ServiceOutcome `json:"services"`
	Waited   time.Duration    `json:"-"`
}

// logTailBytes caps an attached log tail, so a failed start cannot flood the
// caller with output.
const logTailBytes = 16 * 1024

// StartAndWait starts a service — or, with target set, every service in a
// target — and waits until each one is up (running for the settle period),
// down (crashed, exited, stopped, never started), or the timeout expires. A
// service that did not come up has the end of its log attached.
//
// Exactly one of service and target must be non-empty. A failure to start is
// an outcome, not an error: the error return is for the request itself going
// wrong — an unknown name, or a daemon that cannot be reached.
func StartAndWait(r *Resolved, service, target string, opts WaitOptions) (*StartWaitResult, error) {
	opts = opts.withDefaults()
	began := time.Now()

	var names []string
	switch {
	case (service == "") == (target == ""):
		return nil, fmt.Errorf("give exactly one of a service or a target")
	case service != "":
		if _, err := r.Service(service); err != nil {
			return nil, fmt.Errorf("%w in %s (%s scope); %s", err, r.SourcePath(), r.ScopeName(), definedNames(r))
		}
		names = []string{service}
	default:
		if _, ok := r.Registry.Targets[target]; !ok {
			return nil, fmt.Errorf("target %q not found in %s (%s scope); %s", target, r.SourcePath(), r.ScopeName(), definedTargets(r))
		}
		for _, cfg := range r.Registry.TargetMemberConfigs(target) {
			names = append(names, cfg.Name)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("target %q has no runnable services", target)
		}
	}

	if err := EnsureDaemon(); err != nil {
		return nil, err
	}

	// Which services are already running decides "already_running" for target
	// members; a single service learns it from the daemon's reply.
	before, err := liveServices()
	if err != nil {
		return nil, err
	}
	// Anything the daemon spawns from here on records a StartedAt at or after
	// this instant. That is how a member that was started is told apart from
	// one that never was — and an old crash is never mistaken for a new one.
	requested := time.Now()

	outcomes := make(map[string]*ServiceOutcome, len(names))
	var refusal string
	if service != "" {
		res, err := startRaw(service, r.InlineConfig(service))
		if err != nil {
			return nil, err
		}
		if res.alreadyRunning {
			outcomes[service] = &ServiceOutcome{Name: service, Outcome: OutcomeAlreadyRunning}
		}
		refusal = res.refused
	} else {
		for _, n := range names {
			if before[n].State == string(config.StatusRunning) {
				outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeAlreadyRunning}
			}
		}
		refusal, err = targetStartRaw(target, r.Registry.TargetMemberConfigs(target))
		if err != nil {
			return nil, err
		}
	}

	// Wait until every service has an outcome.
	deadline := began.Add(opts.Timeout)
	runningSince := map[string]time.Time{}
	var now map[string]ipc.ServiceInfo
	for {
		now, err = liveServices()
		if err != nil {
			return nil, err
		}
		pending := 0
		for _, n := range names {
			if outcomes[n] != nil {
				continue
			}
			info, spawned := now[n], spawnedSince(n, requested)
			switch {
			case !spawned && refusal != "":
				outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeFailed, Error: refusal}
			case !spawned:
				pending++ // not in the list yet
			case opts.NoWait:
				outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeStarted}
			case info.State == string(config.StatusRunning):
				if runningSince[n].IsZero() {
					runningSince[n] = time.Now()
				}
				if time.Since(runningSince[n]) >= opts.Settle {
					outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeRunning}
				} else {
					pending++
				}
			case info.State == string(config.StatusCrashed):
				outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeCrashed}
			case info.State == string(config.StatusExited):
				outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeExited}
			case info.State == string(config.StatusStopped) || info.State == string(config.StatusStopping):
				outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeStopped}
			default: // starting
				delete(runningSince, n)
				pending++
			}
		}
		if pending == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			for _, n := range names {
				if outcomes[n] != nil {
					continue
				}
				// Up but not yet for the full settle period: it is running,
				// which is what the caller asked about.
				if now[n].State == string(config.StatusRunning) {
					outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeRunning}
				} else {
					outcomes[n] = &ServiceOutcome{Name: n, Outcome: OutcomeTimeout}
				}
			}
			break
		}
		time.Sleep(opts.Poll)
	}

	res := &StartWaitResult{Waited: time.Since(began)}
	for _, n := range names {
		o := outcomes[n]
		info := now[n]
		o.State = info.State
		if o.State == "" {
			o.State = string(config.StatusStopped)
		}
		o.PID, o.Port = info.PID, info.Port
		if o.Outcome == OutcomeCrashed || o.Outcome == OutcomeExited {
			if ss, err := ServiceState(n); err == nil && ss != nil {
				o.ExitCode = ss.LastExitCode
			}
		}
		if !o.Outcome.OK() {
			if tail, err := Logs(n, LogQuery{Lines: opts.TailLines, MaxBytes: logTailBytes, Plain: true}); err == nil {
				o.LogTail = tail.Lines
			}
		}
		res.Services = append(res.Services, *o)
	}
	res.Outcome = overall(res.Services)
	return res, nil
}

// spawnedSince reports whether the daemon started a process for the service at
// or after t, from the StartedAt it records in the state file on every spawn.
// PIDs cannot answer this: the daemon clears a PID when its process exits, so a
// service that crashed before and crashes again at once looks unchanged.
func spawnedSince(name string, t time.Time) bool {
	ss, err := ServiceState(name)
	if err != nil || ss == nil || ss.StartedAt == nil {
		return false
	}
	return !ss.StartedAt.Before(t)
}

// overall folds member outcomes into one: all running (or already running) is
// running; otherwise the most serious member outcome wins.
func overall(svcs []ServiceOutcome) Outcome {
	rank := map[Outcome]int{
		OutcomeFailed: 7, OutcomeCrashed: 6, OutcomeStopped: 5, OutcomeTimeout: 4, OutcomeExited: 3,
		OutcomeStarted: 2, OutcomeRunning: 1, OutcomeAlreadyRunning: 0,
	}
	worst := OutcomeAlreadyRunning
	for _, s := range svcs {
		if rank[s.Outcome] > rank[worst] {
			worst = s.Outcome
		}
	}
	return worst
}

// startRawResult is the daemon's reply to a single start, unpacked.
type startRawResult struct {
	alreadyRunning bool
	// refused is the daemon's reason when it did not accept the start. The
	// process may still have been spawned and died at once ("exited
	// immediately"), which the PID snapshot tells apart.
	refused string
}

func startRaw(name string, cfg *config.ServiceConfig) (*startRawResult, error) {
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()
	resp, err := c.Send("start", ipc.StartPayload{Name: name, Config: cfg})
	if err != nil {
		return nil, fmt.Errorf("start request: %w", err)
	}
	switch {
	case resp.OK:
		return &startRawResult{}, nil
	case strings.HasSuffix(resp.Error, "is already running"):
		return &startRawResult{alreadyRunning: true}, nil
	default:
		return &startRawResult{refused: resp.Error}, nil
	}
}

// targetStartRaw sends target-start and returns the daemon's refusal, if any.
// The daemon starts what it can and reports the members that failed, so a
// refusal is not fatal: each member's own state is checked afterwards.
func targetStartRaw(name string, members []*config.ServiceConfig) (string, error) {
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return "", fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()
	resp, err := c.Send("target-start", ipc.TargetStartPayload{Name: name, Services: members})
	if err != nil {
		return "", fmt.Errorf("target-start request: %w", err)
	}
	if !resp.OK {
		return resp.Error, nil
	}
	return "", nil
}

// liveServices asks the daemon for every service it knows, by name.
func liveServices() (map[string]ipc.ServiceInfo, error) {
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()
	resp, err := c.Send("list", struct{}{})
	if err != nil {
		return nil, fmt.Errorf("list request: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var payload ipc.ListResponsePayload
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return nil, fmt.Errorf("parse list response: %w", err)
	}
	out := make(map[string]ipc.ServiceInfo, len(payload.Services))
	for _, s := range payload.Services {
		out[s.Name] = s
	}
	return out, nil
}

// definedNames lists the services a scope defines, for an error message that
// tells an agent what it could have asked for.
func definedNames(r *Resolved) string {
	if r.Registry == nil || len(r.Registry.Services) == 0 {
		return "it defines no services"
	}
	names := make([]string, 0, len(r.Registry.Services))
	for n := range r.Registry.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	return "defined services: " + strings.Join(names, ", ")
}

func definedTargets(r *Resolved) string {
	if r.Registry == nil || len(r.Registry.Targets) == 0 {
		return "it defines no targets"
	}
	return "defined targets: " + strings.Join(config.SortedTargetNames(r.Registry.Targets), ", ")
}
