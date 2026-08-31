package config

import "sort"

// SortedTargetNames returns the target names in targets, sorted alphabetically.
func SortedTargetNames(targets map[string][]string) []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TargetMemberConfigs resolves the members of target name to their service
// definitions, in the order listed. Members that are not a known service in the
// registry are skipped, and a name listed more than once is only returned once.
// An unknown target name yields nil.
func (r *Registry) TargetMemberConfigs(name string) []*ServiceConfig {
	members, ok := r.Targets[name]
	if !ok {
		return nil
	}
	out := make([]*ServiceConfig, 0, len(members))
	seen := make(map[string]bool, len(members))
	for _, m := range members {
		if seen[m] {
			continue
		}
		seen[m] = true
		if cfg := r.Services[m]; cfg != nil {
			out = append(out, cfg)
		}
	}
	return out
}
