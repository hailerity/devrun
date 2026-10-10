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

// MaxGroupLen caps a group label, for every writer that validates one. Generous
// — a project directory name can be long — but bounded: this is a label drawn on
// one row of a pane at most 47 columns wide, so an unbounded one only makes a
// config file unreadable.
//
// Counted in **characters, not bytes**, and every enforcer has to agree on that:
// the TUI's input limit counts runes, so a byte comparison elsewhere made the
// same string pass in one place and fail in the other — 128 Cyrillic characters
// are 256 bytes. Characters is also the unit the rationale is in, since what
// bounds this is how much fits on a row.
//
// Shared so the MCP server's refusal and the TUI editor's input limit are the
// same number. They were 128 and 512, which made the same field's limit depend
// on who was writing it.
const MaxGroupLen = 128

// ProjectGroupName is the group a service in dir's devrun.yaml inherits when it
// names none of its own: the file's `name:`, or the sanitised directory name
// when it does not set one. The same value ToServiceConfigs would apply, for
// callers that hold only the derived ServiceConfigs and need the default back —
// the TUI's editor, mirroring a cleared group field without re-resolving the
// whole config.
//
// A read error also yields the directory name. That is LoadProject's default
// for a *missing* `name:` but not its behaviour on a parse or permission error,
// which it propagates — so on a broken file this answers with a plausible group
// rather than the real one. Acceptable only because every caller runs against a
// file that has just loaded successfully; it is not a general-purpose reader.
func ProjectGroupName(dir string) string {
	if p, err := LoadProject(dir); err == nil && p != nil && p.Name != "" {
		return p.Name
	}
	return sanitizeName(filepath.Base(dir))
}

// StoredGroup is the group the service called name sets for *itself* in the
// config src points at, or "" when it sets none.
//
// Distinct from ServiceConfig.Group, which for a project service has already had
// the project's name applied as a default by ToServiceConfigs. Anything that
// needs to tell "inherits the project's name" from "explicitly set to the
// project's name" — the editor, so it can round-trip the field rather than
// rewriting it — has to read the file, because the derived value cannot
// distinguish them.
//
// A service that is not in the config, or a config that will not load, reads as
// "" the same way a service with no group does: the caller is prefilling a form,
// and an empty field is the safe answer.
func StoredGroup(src Source, name string) string {
	if !src.IsLocal() {
		reg, err := LoadRegistry(RegistryPath())
		if err != nil {
			return ""
		}
		if svc := reg.Services[name]; svc != nil {
			return svc.Group
		}
		return ""
	}
	proj, err := LoadProject(src.Dir)
	if err != nil || proj == nil {
		return ""
	}
	if svc := proj.Services[name]; svc != nil {
		return svc.Group
	}
	return ""
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
