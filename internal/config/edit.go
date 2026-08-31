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

// relProjectCWD returns the value to store for a project service's cwd: relative
// to the project directory, and empty when it is the project root. An absolute
// input is made relative to dir; a relative input is already interpreted
// relative to dir, so it is kept as given. (Unlike `devrun add`, there is no
// meaningful process working directory to resolve against here.)
func relProjectCWD(dir, cwd string) string {
	if cwd == "" {
		return ""
	}
	if filepath.IsAbs(cwd) {
		if rel, err := filepath.Rel(dir, cwd); err == nil {
			cwd = rel
		}
	}
	if cwd = filepath.Clean(cwd); cwd == "." {
		return ""
	}
	return cwd
}
