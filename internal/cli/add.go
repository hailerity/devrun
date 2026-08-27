package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
)

var addCmd = &cobra.Command{
	Use:   "add <name> <command>",
	Short: "Register a new service",
	Args:  cobra.ExactArgs(2),
	RunE:  runAdd,
}

var addFlags struct {
	cwd   string
	group string
	env   []string
}

func init() {
	addCmd.Flags().StringVar(&addFlags.cwd, "cwd", "", "Working directory (default: current dir)")
	addCmd.Flags().StringVar(&addFlags.group, "group", "", "Assign to a group")
	addCmd.Flags().StringArrayVar(&addFlags.env, "env", nil, "Set environment variable (KEY=VALUE)")
}

func runAdd(cmd *cobra.Command, args []string) error {
	name, command := args[0], args[1]

	envMap := make(map[string]string)
	for _, e := range addFlags.env {
		// Parse KEY=VALUE
		for i, c := range e {
			if c == '=' {
				envMap[e[:i]] = e[i+1:]
				break
			}
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// A project devrun.yaml, when present, is the config all commands write to.
	if _, src, err := config.Resolve(cwd, globalFlag); err != nil {
		return err
	} else if src.IsLocal() {
		return addToProject(src.Dir, name, command, envMap)
	}

	svcCWD := addFlags.cwd
	if svcCWD == "" {
		svcCWD = cwd
	}

	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	reg.Services[name] = &config.ServiceConfig{
		Name:    name,
		Command: command,
		CWD:     svcCWD,
		Group:   addFlags.group,
		Env:     envMap,
	}
	if reg.Version == "" {
		reg.Version = "1"
	}

	if err := config.SaveRegistry(config.RegistryPath(), reg); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}

	fmt.Printf("added %s\n", name)
	return nil
}

// addToProject appends a service to the project devrun.yaml in dir. --group is
// ignored here because a project file's group is its top-level name.
func addToProject(dir, name, command string, env map[string]string) error {
	if addFlags.group != "" {
		fmt.Fprintln(os.Stderr, "note: --group is ignored for a project "+config.ProjectFileName+
			" (the group is its top-level name)")
	}

	proj, err := config.LoadProject(dir)
	if err != nil {
		return err
	}
	if proj == nil || proj.Services == nil {
		return fmt.Errorf("no %s in %s", config.ProjectFileName, dir)
	}
	if _, exists := proj.Services[name]; exists {
		return fmt.Errorf("service %q already defined in %s", name, config.ProjectFileName)
	}

	// Store cwd relative to the project dir; leave empty when it is the project root.
	svcCWD := addFlags.cwd
	if svcCWD != "" {
		if abs, err := filepath.Abs(svcCWD); err == nil {
			if rel, err := filepath.Rel(dir, abs); err == nil {
				svcCWD = rel
			}
		}
	}
	if svcCWD == "." {
		svcCWD = ""
	}

	proj.Services[name] = &config.ProjectServiceConfig{
		Command: command,
		CWD:     svcCWD,
		Env:     env,
	}
	if err := config.SaveProject(dir, proj); err != nil {
		return err
	}

	fmt.Printf("added %s to %s\n", name, config.ProjectFileName)
	return nil
}
