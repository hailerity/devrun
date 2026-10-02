package config

// ServiceConfig is the shared vocabulary across CLI, daemon, and config layer.
// YAML tags match the devrun.yaml and services.yaml schema. JSON tags are load
// bearing too: ServiceConfig crosses the wire as JSON in ipc.StartPayload and in
// the daemon re-exec handoff, so the field names must be pinned explicitly
// rather than left to default to the Go identifiers.
type ServiceConfig struct {
	Name    string            `yaml:"name" json:"name"`
	Command string            `yaml:"command" json:"command"`
	CWD     string            `yaml:"cwd,omitempty" json:"cwd,omitempty"`
	Group   string            `yaml:"group,omitempty" json:"group,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Desc    string            `yaml:"desc,omitempty" json:"desc,omitempty"`
	// Port overrides what the daemon detects. Detection is correct but not
	// instant — it lags a service's start by up to one poll, and picks the
	// lowest when a service holds several listeners — so this is the answer for
	// a service the gateway must reach immediately or that runs a debugger
	// alongside its app port.
	Port int `yaml:"port,omitempty" json:"port,omitempty"`
}

// Registry is the top-level structure of services.yaml.
type Registry struct {
	Version  string                    `yaml:"version"`
	Services map[string]*ServiceConfig `yaml:"services"`
	// Targets maps a target name to the service names it groups. A target is a
	// named subset of services that can be started or stopped as a unit. Members
	// that do not resolve to a known service are ignored at use time rather than
	// rejected on load, so removing a service never breaks config parsing.
	Targets map[string][]string `yaml:"targets,omitempty"`
	// Gateway configures the local HTTP gateway. Absent means all defaults.
	Gateway *GatewayConfig `yaml:"gateway,omitempty"`
}
