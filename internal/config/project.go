package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const ProjectFileName = ".devrun.yaml"

// ProjectServiceConfig is a service entry in a .devrun.yaml file.
type ProjectServiceConfig struct {
	Command string            `yaml:"command"`
	CWD     string            `yaml:"cwd,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Desc    string            `yaml:"desc,omitempty"`
}

// ProjectConfig is the top-level structure of .devrun.yaml.
type ProjectConfig struct {
	Name     string                          `yaml:"name,omitempty"`
	Services map[string]*ProjectServiceConfig `yaml:"services"`
}

// LoadProject reads .devrun.yaml from dir.
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
	if p.Name == "" {
		p.Name = sanitizeName(filepath.Base(dir))
	}
	return &p, nil
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
		out[name] = &ServiceConfig{
			Name:    name,
			Command: svc.Command,
			CWD:     cwd,
			Group:   p.Name,
			Env:     svc.Env,
			Desc:    svc.Desc,
		}
	}
	return out
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
