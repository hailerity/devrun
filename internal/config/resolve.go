package config

import "path/filepath"

// Source describes where the active service registry for a command came from.
type Source struct {
	// Local is the path to the project devrun.yaml when one is active, or "" when
	// the global registry is in use.
	Local string
	// Dir is the directory the project devrun.yaml lives in. Empty for the global
	// registry.
	Dir string
}

// IsLocal reports whether a project devrun.yaml is the active config.
func (s Source) IsLocal() bool { return s.Local != "" }

// Resolve determines which service registry is active for a command run in dir.
//
// When global is false and dir contains a devrun.yaml, that file is loaded and
// converted to an in-memory Registry, and the returned Source points at it.
// Otherwise the global registry (~/.config/devrun/services.yaml) is returned with
// an empty Source. Project services are never written to the global registry, so
// it contains only services registered with `devrun add`.
func Resolve(dir string, global bool) (*Registry, Source, error) {
	if !global {
		proj, err := LoadProject(dir)
		if err != nil {
			return nil, Source{}, err
		}
		if proj != nil {
			reg := &Registry{
				Version:  "1",
				Services: proj.ToServiceConfigs(dir),
				Targets:  proj.Targets,
			}
			return reg, Source{Local: filepath.Join(dir, ProjectFileName), Dir: dir}, nil
		}
	}
	reg, err := LoadRegistry(RegistryPath())
	if err != nil {
		return nil, Source{}, err
	}
	return reg, Source{}, nil
}
