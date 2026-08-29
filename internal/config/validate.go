package config

import (
	"fmt"
	"strings"
)

// Validate reports whether a service definition is runnable. The dominant
// failure is a blank command: the daemon would otherwise hand `sh -c` an empty
// script, which exits 0 immediately and is reported as a clean one-shot run.
func (c *ServiceConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("no service definition")
	}
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("command is empty")
	}
	return nil
}

// Validate mirrors ServiceConfig.Validate for a devrun.yaml service entry.
func (c *ProjectServiceConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("no service definition")
	}
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("command is empty")
	}
	return nil
}
