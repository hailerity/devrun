package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ops"
)

// Bounds on how long start may wait.
const (
	defaultStartTimeout = 15
	maxStartTimeout     = 120
)

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type AddServiceInput struct {
	Scoped
	Name    string            `json:"name" jsonschema:"Name for the new service: letters, digits, '.', '_' or '-'."`
	Command string            `json:"command" jsonschema:"Shell command that runs the service in the foreground, e.g. 'npm run dev'. It is run with sh -c."`
	CWD     string            `json:"cwd,omitempty" jsonschema:"Working directory. A relative path is taken against the project directory. Defaults to the project directory."`
	Group   string            `json:"group,omitempty" jsonschema:"Section to file the service under in the dashboard, e.g. 'backend'. One line, at most 128 characters; a service belongs to exactly one group. Use list_services to see the groups in use and match one rather than inventing a synonym. Omit it to leave the service in the default section, which is the project's name for a devrun.yaml and no group at all for the global registry."`
	Env     map[string]string `json:"env,omitempty" jsonschema:"Environment variables to set for the service."`
}

type ServiceDef struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	CWD     string   `json:"cwd"`
	Group   string   `json:"group,omitempty"`
	EnvKeys []string `json:"env_keys,omitempty"`
}

type AddServiceOutput struct {
	Origin
	Service ServiceDef `json:"service"`
}

type AddToTargetInput struct {
	Scoped
	Target   string   `json:"target" jsonschema:"The target to add to. It is created if it does not exist."`
	Services []string `json:"services" jsonschema:"Services to add. Each must already be defined in the same config."`
}

type AddToTargetOutput struct {
	Origin
	Target  string   `json:"target"`
	Members []string `json:"members" jsonschema:"The target's members after the change."`
}

type StartInput struct {
	Scoped
	Service  string `json:"service,omitempty" jsonschema:"The service to start. Give this or target, not both."`
	Target   string `json:"target,omitempty" jsonschema:"The target whose services to start. Give this or service, not both."`
	Wait     *bool  `json:"wait,omitempty" jsonschema:"Wait to see whether each service comes up (default true). With false, return as soon as the start is accepted."`
	TimeoutS int    `json:"timeout_s,omitempty" jsonschema:"How long to wait, in seconds. Default 15, at most 120."`
}

type StartOutput struct {
	Origin
	Outcome  ops.Outcome          `json:"outcome" jsonschema:"running or already_running (up), started (accepted, not waited for), or crashed, exited, stopped, failed or timeout. For a target, running only if every member is."`
	Services []ops.ServiceOutcome `json:"services" jsonschema:"Each service's outcome; one that did not come up carries its exit code and the end of its log."`
	WaitedMS int64                `json:"waited_ms"`
}

type StopInput struct {
	Scoped
	Service string `json:"service,omitempty" jsonschema:"The service to stop. Give this or target, not both."`
	Target  string `json:"target,omitempty" jsonschema:"The target to stop. Members still held by another running target keep running."`
}

type StopState struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type StopOutput struct {
	Origin
	Services []StopState `json:"services" jsonschema:"State of each affected service after the stop."`
	Note     string      `json:"note,omitempty"`
}

func registerWriteTools(s *mcp.Server, h *handlers) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "add_service",
		Title: "Add a service",
		Description: "Define a new service in the project's devrun.yaml (or the global registry when the project has none), " +
			"optionally in a group — the section it is filed under in the dashboard. " +
			"It does not start it — call start next. Refuses a name that already exists.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, h.addService)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_to_target",
		Title:       "Add services to a target",
		Description: "Add services to a target — a named group that starts and stops together — creating the target if needed.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)},
	}, h.addToTarget)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "start",
		Title: "Start a service or target",
		Description: "Start a service, or every service in a target, in the background, and wait to see whether it comes up: " +
			"running (and still running after a short settle period), or crashed / exited / failed — then with the end of its log so you can see why. " +
			"Starting something already running is fine and changes nothing.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)},
	}, h.start)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "stop",
		Title:       "Stop a service or target",
		Description: "Stop a running service, or a target's services. Stopping something that is not running is fine and changes nothing.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(false)},
	}, h.stop)
}

func (h *handlers) addService(_ context.Context, _ *mcp.CallToolRequest, in AddServiceInput) (*mcp.CallToolResult, AddServiceOutput, error) {
	if err := checkName("service", in.Name); err != nil {
		return nil, AddServiceOutput{}, err
	}
	for k := range in.Env {
		if !envKeyRe.MatchString(k) {
			return nil, AddServiceOutput{}, fmt.Errorf("invalid environment variable name %q", k)
		}
	}
	// Trimmed once, here, and everything downstream sees the trimmed value:
	// validating the raw string refused " "+128 chars+" " for being 130 when
	// what would be written is exactly at the limit, and let a whitespace-only
	// group through to be normalised to "" — after which the echo reported the
	// project's name as the group just set, matching neither the request nor
	// "no group".
	group := strings.TrimSpace(in.Group)
	if err := checkGroup(group); err != nil {
		return nil, AddServiceOutput{}, err
	}
	s, err := h.scope(in.Scoped)
	if err != nil {
		return nil, AddServiceOutput{}, err
	}
	if _, err := ops.AddService(s, ops.NewService{
		Name: in.Name, Command: in.Command, CWD: in.CWD,
		Group: h.ownGroup(in.Scoped, group), Env: in.Env,
	}); err != nil {
		return nil, AddServiceOutput{}, err
	}

	// Read it back as the daemon will see it — with the working directory
	// resolved — rather than echoing the input.
	r, err := h.resolve(in.Scoped)
	if err != nil {
		return nil, AddServiceOutput{}, err
	}
	cfg, err := r.Service(in.Name)
	if err != nil {
		return nil, AddServiceOutput{}, err
	}
	// cfg.Group, not in.Group: for a project service an omitted group has been
	// resolved to the project's name by now, and echoing the input back would
	// report an empty group for a service that has one.
	def := ServiceDef{Name: in.Name, Command: cfg.Command, CWD: cfg.CWD, Group: cfg.Group}
	for k := range cfg.Env {
		def.EnvKeys = append(def.EnvKeys, k)
	}
	sort.Strings(def.EnvKeys)
	return nil, AddServiceOutput{Origin: sourceOf(r), Service: def}, nil
}

// ownGroup is the group to *store* for a service an agent asked to put in
// `group`: the value itself, unless it is the group the service would inherit
// anyway, in which case nothing is stored and it inherits.
//
// Both read tools report a project service's group after the project's name has
// been applied as a default, and nothing in that output says whether a group was
// the service's own or the default. An agent told to match an existing group
// therefore sees `shop` and passes `shop` — which pins the service to that name,
// so renaming the project leaves it behind while its siblings follow, splitting
// one group into two. The TUI editor avoids the same trap by prefilling
// config.StoredGroup instead of the resolved group.
//
// Normalising rather than refusing, because the agent's intent — put it in the
// section called shop — is honoured exactly either way: the resolved group is
// `shop` whichever is stored. Only the brittle encoding is dropped. Deliberately
// not done in ops, which `devrun add` shares: a human typing the flag is making
// a choice, and the CLI has always stored it literally.
// Keyed on whether the config being written is a project file, via
// Resolved.IsLocal — not on the `global` flag. A directory with no devrun.yaml
// resolves to the global registry with `global` false, and there a group that
// happens to match the directory's name is a group like any other: nothing
// inherits, so there is nothing to normalise away.
//
// Takes `group` already trimmed — its caller trims once, before validating, so
// that what is checked, compared here and stored are the same string. Trimming
// again here would only hide a caller that did not.
func (h *handlers) ownGroup(sc Scoped, group string) string {
	if group == "" {
		return ""
	}
	r, err := h.resolve(sc)
	if err != nil || !r.IsLocal() {
		return group
	}
	if group == config.ProjectGroupName(r.Dir) {
		return ""
	}
	return group
}

func (h *handlers) addToTarget(_ context.Context, _ *mcp.CallToolRequest, in AddToTargetInput) (*mcp.CallToolResult, AddToTargetOutput, error) {
	if err := checkName("target", in.Target); err != nil {
		return nil, AddToTargetOutput{}, err
	}
	if len(in.Services) == 0 {
		return nil, AddToTargetOutput{}, fmt.Errorf("give at least one service to add")
	}
	for _, s := range in.Services {
		if err := checkName("service", s); err != nil {
			return nil, AddToTargetOutput{}, err
		}
	}
	r, err := h.resolve(in.Scoped)
	if err != nil {
		return nil, AddToTargetOutput{}, err
	}
	members, err := ops.AddToTarget(r.Scope, in.Target, in.Services)
	if err != nil {
		return nil, AddToTargetOutput{}, fmt.Errorf("%w (in %s); %s", err, r.SourcePath(), definedServices(r))
	}
	return nil, AddToTargetOutput{Origin: sourceOf(r), Target: in.Target, Members: members}, nil
}

func (h *handlers) start(_ context.Context, _ *mcp.CallToolRequest, in StartInput) (*mcp.CallToolResult, StartOutput, error) {
	if err := oneOf(in.Service, in.Target); err != nil {
		return nil, StartOutput{}, err
	}
	if err := checkTargetOrService(in.Service, in.Target); err != nil {
		return nil, StartOutput{}, err
	}
	timeout := in.TimeoutS
	switch {
	case timeout == 0:
		timeout = defaultStartTimeout
	case timeout < 0 || timeout > maxStartTimeout:
		return nil, StartOutput{}, fmt.Errorf("timeout_s must be between 1 and %d, got %d", maxStartTimeout, timeout)
	}
	r, err := h.resolve(in.Scoped)
	if err != nil {
		return nil, StartOutput{}, err
	}
	opts := ops.WaitOptions{Timeout: time.Duration(timeout) * time.Second, NoWait: in.Wait != nil && !*in.Wait}
	res, err := ops.StartAndWait(r, in.Service, in.Target, opts)
	if err != nil {
		return nil, StartOutput{}, err
	}
	return nil, StartOutput{Origin: sourceOf(r), Outcome: res.Outcome, Services: res.Services, WaitedMS: res.Waited.Milliseconds()}, nil
}

func (h *handlers) stop(_ context.Context, _ *mcp.CallToolRequest, in StopInput) (*mcp.CallToolResult, StopOutput, error) {
	if err := oneOf(in.Service, in.Target); err != nil {
		return nil, StopOutput{}, err
	}
	r, err := h.resolve(in.Scoped)
	if err != nil {
		return nil, StopOutput{}, err
	}
	out := StopOutput{Origin: sourceOf(r)}

	var names []string
	if in.Service != "" {
		if _, _, err := h.service(in.Scoped, in.Service); err != nil {
			return nil, StopOutput{}, err
		}
		names = []string{in.Service}
		// Not running, or no daemon at all, is the state the caller wanted.
		if _, err := ops.Stop(in.Service); err != nil && !errors.Is(err, ops.ErrNoDaemon) {
			return nil, StopOutput{}, err
		}
	} else {
		if err := checkName("target", in.Target); err != nil {
			return nil, StopOutput{}, err
		}
		members, ok := r.Registry.Targets[in.Target]
		if !ok {
			return nil, StopOutput{}, fmt.Errorf("target %q not found in %s (%s scope)", in.Target, r.SourcePath(), r.ScopeName())
		}
		names = members
		// The daemon only stops a target it started as one. Ask first rather
		// than read its refusal's wording: a target that is not active has
		// members that may be running on their own or under another target,
		// so they are left alone and the note says so.
		if !ops.ActiveTargets()[in.Target] {
			out.Note = fmt.Sprintf("target %q was not started as a target, so nothing was stopped; stop its services individually if you mean to", in.Target)
		} else if err := ops.StopTarget(in.Target); err != nil && !errors.Is(err, ops.ErrNoDaemon) {
			return nil, StopOutput{}, err
		}
	}

	live, err := ops.List(r)
	if err != nil {
		return nil, StopOutput{}, err
	}
	state := map[string]string{}
	for _, s := range live.Services {
		state[s.Name] = s.State
	}
	out.Services = []StopState{}
	for _, n := range names {
		st := state[n]
		if st == "" {
			st = string(config.StatusStopped)
		}
		out.Services = append(out.Services, StopState{Name: n, State: st})
	}
	return nil, out, nil
}

// checkTargetOrService validates whichever of the two names was given.
func checkTargetOrService(service, target string) error {
	if service != "" {
		return checkName("service", service)
	}
	return checkName("target", target)
}

// oneOf checks that exactly one of a service and a target was given.
func oneOf(service, target string) error {
	if (service == "") == (target == "") {
		return fmt.Errorf("give exactly one of service or target")
	}
	return nil
}
