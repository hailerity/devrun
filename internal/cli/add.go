package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ops"
)

var addCmd = &cobra.Command{
	Use:   "add <name> <command>",
	Short: "Register a new service",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		return config.ValidateName("service", args[0])
	},
	RunE: runAdd,
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

	scope, err := cliScope()
	if err != nil {
		return err
	}
	res, err := ops.AddService(scope, ops.NewService{
		Name:    name,
		Command: command,
		CWD:     addFlags.cwd,
		Group:   addFlags.group,
		Env:     envMap,
		// `devrun add` has always replaced a global service of the same name.
		Overwrite: true,
	})
	if err != nil {
		return err
	}

	// A project devrun.yaml, when present, is the config all commands write to.
	if res.Local {
		if res.GroupIgnored {
			fmt.Fprintln(os.Stderr, "note: --group is ignored for a project "+config.ProjectFileName+
				" (the group is its top-level name)")
		}
		fmt.Printf("added %s to %s\n", name, config.ProjectFileName)
		return nil
	}
	fmt.Printf("added %s\n", name)
	return nil
}
