package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SaveServiceEdit changes an existing service's name, command, and working
// directory, then persists the result to whichever config src points at: the
// project devrun.yaml when src.IsLocal(), otherwise the global registry.
//
// oldName identifies the service to edit. newName and command must be non-empty
// (after trimming); newName may equal oldName but must not collide with a
// different existing service. For a project file cwd is stored relative to
// src.Dir (and dropped when it is the project root); for the global registry it
// is stored as given. All other fields (group, env, desc) are preserved.
func SaveServiceEdit(src Source, oldName, newName, command, cwd string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("name is empty")
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command is empty")
	}
	if src.IsLocal() {
		return editProjectService(src.Dir, oldName, newName, command, cwd)
	}
	return editRegistryService(RegistryPath(), oldName, newName, command, cwd)
}

func editRegistryService(path, oldName, newName, command, cwd string) error {
	reg, err := LoadRegistry(path)
	if err != nil {
		return err
	}
	cur, ok := reg.Services[oldName]
	if !ok {
		return fmt.Errorf("service %q not found", oldName)
	}
	if newName != oldName {
		if _, taken := reg.Services[newName]; taken {
			return fmt.Errorf("service %q already exists", newName)
		}
	}

	updated := *cur // preserve group / env / desc
	updated.Name = newName
	updated.Command = command
	updated.CWD = cwd

	if newName != oldName {
		delete(reg.Services, oldName)
	}
	reg.Services[newName] = &updated
	return SaveRegistry(path, reg)
}

func editProjectService(dir, oldName, newName, command, cwd string) error {
	proj, err := LoadProject(dir)
	if err != nil {
		return err
	}
	if proj == nil {
		return fmt.Errorf("no %s in %s", ProjectFileName, dir)
	}
	cur, ok := proj.Services[oldName]
	if !ok {
		return fmt.Errorf("service %q not found in %s", oldName, ProjectFileName)
	}
	if newName != oldName {
		if _, taken := proj.Services[newName]; taken {
			return fmt.Errorf("service %q already exists in %s", newName, ProjectFileName)
		}
	}

	updated := *cur // preserve env / desc
	updated.Command = command
	updated.CWD = relProjectCWD(dir, cwd)

	if newName != oldName {
		delete(proj.Services, oldName)
	}
	proj.Services[newName] = &updated
	return SaveProject(dir, proj)
}

// relProjectCWD mirrors `devrun add`: a project file stores cwd relative to the
// project directory, and empty when it is the project root.
func relProjectCWD(dir, cwd string) string {
	if cwd == "" {
		return ""
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		if rel, err := filepath.Rel(dir, abs); err == nil {
			cwd = rel
		}
	}
	if cwd == "." {
		return ""
	}
	return cwd
}
