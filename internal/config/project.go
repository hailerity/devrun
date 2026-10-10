package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const ProjectFileName = "devrun.yaml"

// ProjectServiceConfig is a service entry in a devrun.yaml file.
type ProjectServiceConfig struct {
	Command string            `yaml:"command"`
	CWD     string            `yaml:"cwd,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Desc    string            `yaml:"desc,omitempty"`
	// Group sections this service in the dashboard. Empty inherits the
	// project's name, which is what every service in a devrun.yaml got before
	// this field existed — so a file that says nothing about groups behaves
	// exactly as it did.
	//
	// An explicit group replaces that default rather than nesting inside it:
	// groups are a flat set, so a project that groups some of its services
	// simply shows the project's name as the header for the rest.
	Group string `yaml:"group,omitempty"`
	// Port overrides detection; see ServiceConfig.Port.
	Port int `yaml:"port,omitempty"`
}

// ProjectConfig is the top-level structure of devrun.yaml.
type ProjectConfig struct {
	Name     string                           `yaml:"name,omitempty"`
	Services map[string]*ProjectServiceConfig `yaml:"services"`
	// Targets maps a target name to the service names it groups. See
	// Registry.Targets — the semantics are identical for a project file.
	Targets map[string][]string `yaml:"targets,omitempty"`
	// Gateway configures the local HTTP gateway for this project.
	Gateway *GatewayConfig `yaml:"gateway,omitempty"`
	// Tunnel configures publishing. Absent means cloudflared is never looked
	// for; publishing is optional, and so is the dependency.
	Tunnel *TunnelConfig `yaml:"tunnel,omitempty"`
}

// LoadProject reads devrun.yaml from dir.
// Returns nil, nil if the file does not exist.
// If Name is empty it defaults to the sanitised base name of dir.
func LoadProject(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, ProjectFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ProjectFileName, err)
	}
	var p ProjectConfig
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ProjectFileName, err)
	}
	if p.Services == nil {
		p.Services = make(map[string]*ProjectServiceConfig)
	}
	if p.Targets == nil {
		p.Targets = make(map[string][]string)
	}
	for name, svc := range p.Services {
		if err := svc.Validate(); err != nil {
			return nil, fmt.Errorf("%s service %q: %w", ProjectFileName, name, err)
		}
	}
	if p.Name == "" {
		p.Name = sanitizeName(filepath.Base(dir))
	}
	return &p, nil
}

// SaveProject writes p to devrun.yaml in dir. The file is re-marshalled from the
// in-memory structure, so hand-written comments and key ordering are not
// preserved.
func SaveProject(dir string, p *ProjectConfig) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", ProjectFileName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProjectFileName), data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", ProjectFileName, err)
	}
	return nil
}

// ToServiceConfigs converts a ProjectConfig to ServiceConfig entries ready for
// the global registry. Relative CWD values are resolved against dir.
func (p *ProjectConfig) ToServiceConfigs(dir string) map[string]*ServiceConfig {
	out := make(map[string]*ServiceConfig, len(p.Services))
	for name, svc := range p.Services {
		cwd := svc.CWD
		if cwd == "" {
			cwd = dir
		} else if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(dir, cwd)
		}
		// The project's name is the default group, not an override: a service
		// that names its own group keeps it, so one devrun.yaml can section
		// itself instead of arriving as a single block.
		//
		// Trimmed here rather than only at the writers, because this is the one
		// path every reader goes through and the file can be hand-written: a
		// `group: '  '` would otherwise be a group, filing the service under a
		// blank-looking header of its own instead of falling back.
		group := strings.TrimSpace(svc.Group)
		if group == "" {
			group = p.Name
		}
		out[name] = &ServiceConfig{
			Name:    name,
			Command: svc.Command,
			CWD:     cwd,
			Group:   group,
			Env:     svc.Env,
			Desc:    svc.Desc,
			Port:    svc.Port,
		}
	}
	return out
}

// ProjectGroupName is the group a service in dir's devrun.yaml inherits when it
// names none of its own: the file's `name:`, or the sanitised directory name
// when it does not set one. The same value ToServiceConfigs would apply, for
// callers that hold only the derived ServiceConfigs and need the default back —
// the TUI's editor, mirroring a cleared group field without re-resolving the
// whole config.
//
// Falls back to the directory name on any read error, which is also what
// LoadProject would have defaulted to.
func ProjectGroupName(dir string) string {
	if p, err := LoadProject(dir); err == nil && p != nil && p.Name != "" {
		return p.Name
	}
	return sanitizeName(filepath.Base(dir))
}

// sanitizeName replaces characters that are not safe in a project/group name
// with hyphens.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
