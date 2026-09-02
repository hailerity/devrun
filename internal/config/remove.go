package config

import "fmt"

// RemoveService deletes a service from whichever config src points at: the
// project devrun.yaml when src.IsLocal(), otherwise the global registry.
//
// It errors when the named service is not defined in that config. Notifying a
// running daemon (so it evicts the service from its in-memory map) is the
// caller's responsibility.
func RemoveService(src Source, name string) error {
	if src.IsLocal() {
		return removeProjectService(src.Dir, name)
	}
	return removeRegistryService(RegistryPath(), name)
}

func removeRegistryService(path, name string) error {
	reg, err := LoadRegistry(path)
	if err != nil {
		return err
	}
	if _, ok := reg.Services[name]; !ok {
		return fmt.Errorf("service %q not found", name)
	}
	delete(reg.Services, name)
	return SaveRegistry(path, reg)
}

func removeProjectService(dir, name string) error {
	proj, err := LoadProject(dir)
	if err != nil {
		return err
	}
	if proj == nil {
		return fmt.Errorf("no %s in %s", ProjectFileName, dir)
	}
	if _, ok := proj.Services[name]; !ok {
		return fmt.Errorf("service %q not found in %s", name, ProjectFileName)
	}
	delete(proj.Services, name)
	return SaveProject(dir, proj)
}
