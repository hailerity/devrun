package cli

import (
	"fmt"
	"os"

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

	cwd := addFlags.cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

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

	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	reg.Services[name] = &config.ServiceConfig{
		Name:    name,
		Command: command,
		CWD:     cwd,
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
