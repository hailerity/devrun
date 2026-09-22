package ops

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hailerity/devrun/internal/config"
)

// NewService is a service definition to add.
type NewService struct {
	Name    string
	Command string
	// CWD is the service's working directory. Relative paths are taken against
	// the scope's Dir. Empty means the scope's Dir.
	CWD   string
	Group string // global registry only; a project file's group is its name
	Env   map[string]string
	// Overwrite replaces an existing global service of the same name. A project
	// devrun.yaml always refuses a duplicate. The CLI's `devrun add` sets this
	// for the global registry, which is its long-standing behaviour; agents
	// never do.
	Overwrite bool
}

// AddResult reports where a service was written.
type AddResult struct {
	// Local is true when it went into the project devrun.yaml.
	Local bool
	// GroupIgnored is true when a Group was given for a project file, which has
	// no per-service group.
	GroupIgnored bool
}

// AddService writes a new service into the config the scope resolves to: the
// project devrun.yaml when one is in scope, otherwise the global registry.
func AddService(s Scope, svc NewService) (*AddResult, error) {
	if strings.TrimSpace(svc.Command) == "" {
		return nil, fmt.Errorf("command cannot be empty")
	}

	// Resolve only to learn which file is in scope; the write below re-reads
	// that file itself so it edits exactly what is on disk.
	_, src, err := config.Resolve(s.Dir, s.Global)
	if err != nil {
		return nil, err
	}
	if src.IsLocal() {
		return addToProject(src.Dir, s.Dir, svc)
	}

	svcCWD := absUnder(s.Dir, svc.CWD)
	if svcCWD == "" {
		svcCWD = s.Dir
	}

	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	if _, exists := reg.Services[svc.Name]; exists && !svc.Overwrite {
		return nil, fmt.Errorf("service %q already registered", svc.Name)
	}

	reg.Services[svc.Name] = &config.ServiceConfig{
		Name:    svc.Name,
		Command: svc.Command,
		CWD:     svcCWD,
		Group:   svc.Group,
		Env:     svc.Env,
	}
	if reg.Version == "" {
		reg.Version = "1"
	}
	if err := config.SaveRegistry(config.RegistryPath(), reg); err != nil {
		return nil, fmt.Errorf("save registry: %w", err)
	}
	return &AddResult{}, nil
}

// addToProject appends a service to the devrun.yaml in projDir. base is what a
// relative svc.CWD is interpreted against.
func addToProject(projDir, base string, svc NewService) (*AddResult, error) {
	proj, err := config.LoadProject(projDir)
	if err != nil {
		return nil, err
	}
	if proj == nil || proj.Services == nil {
		return nil, fmt.Errorf("no %s in %s", config.ProjectFileName, projDir)
	}
	if _, exists := proj.Services[svc.Name]; exists {
		return nil, fmt.Errorf("service %q already defined in %s", svc.Name, config.ProjectFileName)
	}

	// Store cwd relative to the project dir; leave empty when it is the project root.
	svcCWD := absUnder(base, svc.CWD)
	if svcCWD != "" {
		if rel, err := filepath.Rel(projDir, svcCWD); err == nil {
			svcCWD = rel
		}
	}
	if svcCWD == "." {
		svcCWD = ""
	}

	env := svc.Env
	if env == nil {
		env = map[string]string{}
	}
	proj.Services[svc.Name] = &config.ProjectServiceConfig{
		Command: svc.Command,
		CWD:     svcCWD,
		Env:     env,
	}
	if err := config.SaveProject(projDir, proj); err != nil {
		return nil, err
	}
	return &AddResult{Local: true, GroupIgnored: svc.Group != ""}, nil
}

// EditTargets loads the config the scope resolves to (project devrun.yaml or
// the global registry), hands its target map and the set of service names known
// in that file to fn, and writes the config back when fn returns nil. fn mutates
// the map in place.
func EditTargets(s Scope, fn func(targets map[string][]string, known map[string]bool) error) error {
	_, src, err := config.Resolve(s.Dir, s.Global)
	if err != nil {
		return err
	}

	if src.IsLocal() {
		proj, err := config.LoadProject(src.Dir)
		if err != nil {
			return err
		}
		if proj == nil {
			return fmt.Errorf("no %s in %s", config.ProjectFileName, src.Dir)
		}
		if proj.Targets == nil {
			proj.Targets = make(map[string][]string)
		}
		if err := fn(proj.Targets, keySet(proj.Services)); err != nil {
			return err
		}
		return config.SaveProject(src.Dir, proj)
	}

	greg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if greg.Targets == nil {
		greg.Targets = make(map[string][]string)
	}
	if greg.Version == "" {
		greg.Version = "1"
	}
	if err := fn(greg.Targets, keySet(greg.Services)); err != nil {
		return err
	}
	return config.SaveRegistry(config.RegistryPath(), greg)
}

// CreateTarget adds a new, empty target.
func CreateTarget(s Scope, name string) error {
	return EditTargets(s, func(targets map[string][]string, _ map[string]bool) error {
		if _, ok := targets[name]; ok {
			return fmt.Errorf("target %q already exists", name)
		}
		targets[name] = []string{}
		return nil
	})
}

// AddToTarget adds services to a target, creating it if needed. Every service
// must be defined in the same config. It returns the target's members after the
// change.
func AddToTarget(s Scope, name string, svcs []string) ([]string, error) {
	var members []string
	err := EditTargets(s, func(targets map[string][]string, known map[string]bool) error {
		for _, svc := range svcs {
			if !known[svc] {
				return fmt.Errorf("service %q is not defined in this config", svc)
			}
		}
		targets[name] = MergeMembers(targets[name], svcs)
		members = targets[name]
		return nil
	})
	return members, err
}

// RemoveFromTarget drops services from a target, or deletes the target when
// svcs is empty.
func RemoveFromTarget(s Scope, name string, svcs []string) error {
	return EditTargets(s, func(targets map[string][]string, _ map[string]bool) error {
		if _, ok := targets[name]; !ok {
			return fmt.Errorf("target %q does not exist", name)
		}
		if len(svcs) == 0 {
			delete(targets, name)
			return nil
		}
		targets[name] = DropMembers(targets[name], svcs)
		return nil
	})
}

// MergeMembers returns existing with each name in add appended once, preserving
// order and skipping names already present.
func MergeMembers(existing, add []string) []string {
	out := append([]string(nil), existing...)
	has := make(map[string]bool, len(out))
	for _, m := range out {
		has[m] = true
	}
	for _, s := range add {
		if !has[s] {
			out = append(out, s)
			has[s] = true
		}
	}
	return out
}

// DropMembers returns existing with every name in drop removed, preserving order.
func DropMembers(existing, drop []string) []string {
	rm := make(map[string]bool, len(drop))
	for _, s := range drop {
		rm[s] = true
	}
	out := make([]string, 0, len(existing))
	for _, m := range existing {
		if !rm[m] {
			out = append(out, m)
		}
	}
	return out
}

// keySet returns the set of keys of m as a bool map — used to validate target
// members against the service names in the same file being edited.
func keySet[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}
