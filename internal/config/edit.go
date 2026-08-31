package config

import (
	"fmt"
	"path/filepath"
	"sort"
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

	// A project service's identity is its map key — ProjectServiceConfig has no
	// Name field — so the rekey below is the rename; there is nothing on the
	// value to update (unlike editRegistryService, which sets ServiceConfig.Name).
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

// SaveTargetEdit renames a target and/or replaces its member list, persisting to
// whichever config src points at: the project devrun.yaml when src.IsLocal(),
// otherwise the global registry.
//
// oldName identifies the target to edit. newName must be non-empty (after
// trimming); it may equal oldName but must not collide with a different target.
// members is stored sorted and de-duplicated; an empty list is allowed.
func SaveTargetEdit(src Source, oldName, newName string, members []string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("name is empty")
	}
	members = sortedUnique(members)
	if src.IsLocal() {
		return editProjectTarget(src.Dir, oldName, newName, members)
	}
	return editRegistryTarget(RegistryPath(), oldName, newName, members)
}

func editRegistryTarget(path, oldName, newName string, members []string) error {
	reg, err := LoadRegistry(path)
	if err != nil {
		return err
	}
	if _, ok := reg.Targets[oldName]; !ok {
		return fmt.Errorf("target %q not found", oldName)
	}
	if newName != oldName {
		if _, taken := reg.Targets[newName]; taken {
			return fmt.Errorf("target %q already exists", newName)
		}
		delete(reg.Targets, oldName)
	}
	reg.Targets[newName] = members
	if reg.Version == "" {
		reg.Version = "1"
	}
	return SaveRegistry(path, reg)
}

func editProjectTarget(dir, oldName, newName string, members []string) error {
	proj, err := LoadProject(dir)
	if err != nil {
		return err
	}
	if proj == nil {
		return fmt.Errorf("no %s in %s", ProjectFileName, dir)
	}
	if _, ok := proj.Targets[oldName]; !ok {
		return fmt.Errorf("target %q not found in %s", oldName, ProjectFileName)
	}
	if newName != oldName {
		if _, taken := proj.Targets[newName]; taken {
			return fmt.Errorf("target %q already exists in %s", newName, ProjectFileName)
		}
		delete(proj.Targets, oldName)
	}
	proj.Targets[newName] = members
	return SaveProject(dir, proj)
}

// sortedUnique returns a sorted copy of in with duplicates and empty strings removed.
func sortedUnique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
