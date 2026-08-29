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
}

// Registry is the top-level structure of services.yaml.
type Registry struct {
	Version  string                    `yaml:"version"`
	Services map[string]*ServiceConfig `yaml:"services"`
}
