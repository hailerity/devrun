package mcpserver

import (
	"context"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/ops"
)

// Limits on what a single logs call may return, so one call cannot flood the
// agent's context.
const (
	defaultLogLines = 100
	maxLogLines     = 1000
	maxLogBytes     = 64 * 1024
)

// ServiceInfo is one service's live state.
type ServiceInfo struct {
	Name     string   `json:"name"`
	State    string   `json:"state" jsonschema:"running, starting, stopping, stopped, crashed or exited."`
	PID      *int     `json:"pid,omitempty"`
	Port     *int     `json:"port,omitempty" jsonschema:"The TCP port it listens on, once detected."`
	UptimeS  int64    `json:"uptime_s,omitempty"`
	CPUPct   float64  `json:"cpu_pct,omitempty"`
	MemBytes int64    `json:"mem_bytes,omitempty"`
	Targets  []string `json:"targets,omitempty" jsonschema:"Targets this service belongs to."`
}

// TargetInfo is one target and whether it is active.
type TargetInfo struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Active  bool     `json:"active" jsonschema:"Started as a target and not stopped since."`
}

type ListInput struct {
	Scoped
}

type ListOutput struct {
	Origin
	DaemonRunning bool          `json:"daemon_running" jsonschema:"False when the state shown is the last saved one because the daemon could not be reached."`
	Services      []ServiceInfo `json:"services"`
	Targets       []TargetInfo  `json:"targets"`
}

type StatusInput struct {
	Scoped
	Name string `json:"name" jsonschema:"The service name."`
}

type StatusOutput struct {
	Origin
	ServiceInfo
	ExitCode *int     `json:"exit_code,omitempty" jsonschema:"Exit code of the last run, when it crashed or exited."`
	Command  string   `json:"command"`
	CWD      string   `json:"cwd"`
	EnvKeys  []string `json:"env_keys,omitempty" jsonschema:"Names of the environment variables it sets. Values are not returned."`
	LogPath  string   `json:"log_path"`
}

type LogsInput struct {
	Scoped
	Name  string `json:"name" jsonschema:"The service name."`
	Lines int    `json:"lines,omitempty" jsonschema:"How many lines to return, from the end of the log. Default 100, at most 1000."`
	Grep  string `json:"grep,omitempty" jsonschema:"Only return lines containing this text, case-insensitively; lines then counts matches."`
}

type LogsOutput struct {
	Origin
	Name      string   `json:"name"`
	Lines     []string `json:"lines" jsonschema:"Oldest first, as plain text with terminal control sequences removed."`
	Truncated bool     `json:"truncated" jsonschema:"True when earlier lines (or matches) exist that were not returned, or a line was cut to fit 64 KB."`
	LogPath   string   `json:"log_path"`
}

func registerReadTools(s *mcp.Server, h *handlers) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_services",
		Title:       "List services",
		Description: "List every service and target defined for the project, with each service's live state (running, crashed, …), PID, port, uptime and resource use.",
		Annotations: read,
	}, h.list)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "service_status",
		Title:       "Service status",
		Description: "One service's live state plus its definition: command, working directory, environment variable names (not values), last exit code and log path.",
		Annotations: read,
	}, h.status)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "logs",
		Title:       "Read service logs",
		Description: "Return the last lines of a service's output as plain text — 100 by default, at most 1000 and 64 KB — optionally only lines containing some text. This is a snapshot: call it again to see new output.",
		Annotations: read,
	}, h.logs)
}

func (h *handlers) list(_ context.Context, _ *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListOutput, error) {
	r, err := h.resolve(in.Scoped)
	if err != nil {
		return nil, ListOutput{}, err
	}
	res, err := ops.List(r)
	if err != nil {
		return nil, ListOutput{}, err
	}
	memberOf := targetsByService(r.Registry)
	out := ListOutput{Origin: sourceOf(r), DaemonRunning: !res.Offline, Services: []ServiceInfo{}, Targets: []TargetInfo{}}
	for _, s := range res.Services {
		out.Services = append(out.Services, serviceInfo(s, memberOf[s.Name]))
	}
	for _, name := range config.SortedTargetNames(r.Registry.Targets) {
		members := r.Registry.Targets[name]
		if members == nil {
			members = []string{}
		}
		out.Targets = append(out.Targets, TargetInfo{Name: name, Members: members, Active: res.ActiveTargets[name]})
	}
	return nil, out, nil
}

func (h *handlers) status(_ context.Context, _ *mcp.CallToolRequest, in StatusInput) (*mcp.CallToolResult, StatusOutput, error) {
	r, cfg, err := h.service(in.Scoped, in.Name)
	if err != nil {
		return nil, StatusOutput{}, err
	}
	res, err := ops.List(r)
	if err != nil {
		return nil, StatusOutput{}, err
	}
	var live ipc.ServiceInfo
	for _, s := range res.Services {
		if s.Name == in.Name {
			live = s
		}
	}
	out := StatusOutput{
		Origin:      sourceOf(r),
		ServiceInfo: serviceInfo(live, targetsByService(r.Registry)[in.Name]),
		Command:     cfg.Command,
		CWD:         cfg.CWD,
		LogPath:     config.LogPath(in.Name),
	}
	for k := range cfg.Env {
		out.EnvKeys = append(out.EnvKeys, k)
	}
	sort.Strings(out.EnvKeys)
	if live.State == string(config.StatusCrashed) || live.State == string(config.StatusExited) {
		if ss, err := ops.ServiceState(in.Name); err == nil && ss != nil {
			out.ExitCode = ss.LastExitCode
		}
	}
	return nil, out, nil
}

func (h *handlers) logs(_ context.Context, _ *mcp.CallToolRequest, in LogsInput) (*mcp.CallToolResult, LogsOutput, error) {
	r, _, err := h.service(in.Scoped, in.Name)
	if err != nil {
		return nil, LogsOutput{}, err
	}
	lines := in.Lines
	switch {
	case lines == 0:
		lines = defaultLogLines
	case lines < 0 || lines > maxLogLines:
		return nil, LogsOutput{}, fmt.Errorf("lines must be between 1 and %d, got %d", maxLogLines, lines)
	}
	res, err := ops.Logs(in.Name, ops.LogQuery{Lines: lines, Grep: in.Grep, MaxBytes: maxLogBytes, Plain: true})
	if err != nil {
		return nil, LogsOutput{}, err
	}
	out := LogsOutput{Origin: sourceOf(r), Name: in.Name, Lines: res.Lines, Truncated: res.Truncated, LogPath: res.Path}
	if out.Lines == nil {
		out.Lines = []string{}
	}
	return nil, out, nil
}

// service resolves the scope and looks the named service up in it, with an
// error that lists what is defined — enough for an agent to correct itself.
func (h *handlers) service(in Scoped, name string) (*ops.Resolved, *config.ServiceConfig, error) {
	if err := checkName("service", name); err != nil {
		return nil, nil, err
	}
	r, err := h.resolve(in)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := r.Service(name)
	if err != nil {
		return nil, nil, fmt.Errorf("%w in %s (%s scope); %s — pass project_dir if you meant another project",
			err, r.SourcePath(), r.ScopeName(), definedServices(r))
	}
	return r, cfg, nil
}

func serviceInfo(s ipc.ServiceInfo, targets []string) ServiceInfo {
	return ServiceInfo{
		Name: s.Name, State: s.State, PID: s.PID, Port: s.Port,
		UptimeS: s.UptimeSec, CPUPct: s.CPUPct, MemBytes: s.MemBytes, Targets: targets,
	}
}

// targetsByService inverts the target map: service name → targets it is in.
func targetsByService(reg *config.Registry) map[string][]string {
	out := map[string][]string{}
	for _, t := range config.SortedTargetNames(reg.Targets) {
		for _, m := range reg.Targets[t] {
			out[m] = append(out[m], t)
		}
	}
	return out
}

func definedServices(r *ops.Resolved) string {
	if r.Registry == nil || len(r.Registry.Services) == 0 {
		return "it defines no services"
	}
	names := make([]string, 0, len(r.Registry.Services))
	for n := range r.Registry.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	return fmt.Sprintf("defined services: %v", names)
}
