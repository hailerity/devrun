package config

// ServiceConfig is the shared vocabulary across CLI, daemon, and config layer.
// YAML tags match .procet.yaml and services.yaml schema.
type ServiceConfig struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	CWD     string            `yaml:"cwd,omitempty"`
	Group   string            `yaml:"group,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Desc    string            `yaml:"desc,omitempty"`
}

// Registry is the top-level structure of services.yaml.
type Registry struct {
	Version  string                    `yaml:"version"`
	Services map[string]*ServiceConfig `yaml:"services"`
}
