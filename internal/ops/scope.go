// Package ops is devrun's operations layer: resolve the active config, talk to
// the daemon, read and write service and target definitions. Every function
// returns data and errors and prints nothing, so the CLI and the MCP server
// share one implementation and render it their own way — a table for a person,
// JSON for an agent.
package ops

import (
	"fmt"
	"path/filepath"

	"github.com/hailerity/devrun/internal/config"
)

// Scope says which config an operation acts on: the project devrun.yaml in Dir
// when there is one (unless Global), otherwise the global registry. It is the
// ops-level form of the CLI's working directory plus --global flag.
type Scope struct {
	Dir    string
	Global bool
}

// Resolved is a Scope with its config loaded.
type Resolved struct {
	Scope
	Registry *config.Registry
	Source   config.Source
}

// Resolve loads the config the scope points at. It re-reads from disk on every
// call — nothing is cached — so an edit made meanwhile by the TUI, the CLI or a
// person is always seen.
//
// When a project devrun.yaml exists but fails to parse, the returned Resolved
// still carries its Source (and a nil Registry) alongside the error, so callers
// can name the broken file.
func Resolve(s Scope) (*Resolved, error) {
	reg, src, err := config.Resolve(s.Dir, s.Global)
	return &Resolved{Scope: s, Registry: reg, Source: src}, err
}

// IsLocal reports whether the resolved config is a project devrun.yaml.
func (r *Resolved) IsLocal() bool { return r.Source.IsLocal() }

// SourcePath is the file the resolved config was read from: the project
// devrun.yaml, or the global services.yaml.
func (r *Resolved) SourcePath() string {
	if r.IsLocal() {
		return r.Source.Local
	}
	return config.RegistryPath()
}

// ScopeName is "project" or "global".
func (r *Resolved) ScopeName() string {
	if r.IsLocal() {
		return "project"
	}
	return "global"
}

// InlineConfig returns the definition to ship in a start request. The daemon
// resolves global services by name from services.yaml itself, but it has no way
// to see a project devrun.yaml — so for a project service the full definition
// travels inline, and for a global one this is nil.
func (r *Resolved) InlineConfig(name string) *config.ServiceConfig {
	if !r.IsLocal() || r.Registry == nil {
		return nil
	}
	return r.Registry.Services[name]
}

// Service returns the named service's definition, or an error naming the file
// that was searched.
func (r *Resolved) Service(name string) (*config.ServiceConfig, error) {
	if r.Registry != nil {
		if svc := r.Registry.Services[name]; svc != nil {
			return svc, nil
		}
	}
	return nil, fmt.Errorf("service %q not found", name)
}

// absUnder makes p absolute, interpreting a relative path against dir rather
// than the process's working directory — so a relative cwd means the same thing
// whether it came from the CLI (dir is the cwd) or an agent (dir is its
// project_dir).
func absUnder(dir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}
