package config

import (
	"fmt"
	"regexp"
	"strings"
)

// nameRe is the rule for a newly created service or target name: something that
// is unremarkable as a file name, a shell word and a YAML key. It is enforced
// when a name is created (devrun add, a new target, a rename in the TUI), not
// when a config is read, so a config written before the rule keeps working.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateName checks a new service or target name. kind ("service" or
// "target") only shapes the error message.
func ValidateName(kind, name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid %s name %q: use 1–64 letters, digits, '.', '_' or '-', starting with a letter or digit", kind, name)
	}
	return nil
}

// SafeFileName reports whether name can be used as the base of a file name —
// as a service's log file is — without escaping its directory. It is the
// looser, load-bearing check applied wherever a name becomes a path, so it
// holds even for names that never went through ValidateName (a hand-edited
// devrun.yaml, a config from before the rule).
func SafeFileName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, "/\\\x00")
}
