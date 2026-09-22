package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ops"
)

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Group services into named targets you can start and stop as a unit",
}

var targetCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new, empty target",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		return config.ValidateName("target", args[0])
	},
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		scope, err := cliScope()
		if err != nil {
			return err
		}
		if err := ops.CreateTarget(scope, name); err != nil {
			return err
		}
		fmt.Printf("created target %s\n", name)
		return nil
	},
}

var targetAddCmd = &cobra.Command{
	Use:   "add <name> <service>...",
	Short: "Add one or more services to a target (creating it if needed)",
	Args:  newTargetNameArgs(cobra.MinimumNArgs(2)),
	RunE: func(_ *cobra.Command, args []string) error {
		name, svcs := args[0], args[1:]
		scope, err := cliScope()
		if err != nil {
			return err
		}
		if _, err := ops.AddToTarget(scope, name, svcs); err != nil {
			return err
		}
		fmt.Printf("added %s to target %s\n", strings.Join(svcs, ", "), name)
		return nil
	},
}

var targetRemoveCmd = &cobra.Command{
	Use:     "rm <name> [service]...",
	Aliases: []string{"remove"},
	Short:   "Remove services from a target, or the whole target when no service is given",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name, svcs := args[0], args[1:]
		scope, err := cliScope()
		if err != nil {
			return err
		}
		if err := ops.RemoveFromTarget(scope, name, svcs); err != nil {
			return err
		}
		if len(svcs) == 0 {
			fmt.Printf("removed target %s\n", name)
		} else {
			fmt.Printf("removed %s from target %s\n", strings.Join(svcs, ", "), name)
		}
		return nil
	},
}

var targetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List targets, their members, and which are running",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		reg, _, err := activeRegistry()
		if err != nil {
			return err
		}
		names := config.SortedTargetNames(reg.Targets)
		if len(names) == 0 {
			fmt.Println(styleLabel.Render("no targets defined — create one with 'devrun target create <name>'"))
			return nil
		}

		active := ops.ActiveTargets()

		width := 0
		for _, n := range names {
			if len(n) > width {
				width = len(n)
			}
		}
		for _, n := range names {
			marker := "  "
			if active[n] {
				marker = styleGreen.Render("● ")
			}
			members := reg.Targets[n]
			cells := make([]string, 0, len(members))
			for _, m := range members {
				if reg.Services[m] == nil {
					cells = append(cells, styleLabel.Render(m+" (unknown)"))
				} else {
					cells = append(cells, styleValue.Render(m))
				}
			}
			list := strings.Join(cells, styleLabel.Render(", "))
			if list == "" {
				list = styleLabel.Render("(empty)")
			}
			fmt.Printf("%s%s  %s\n", marker, styleBold.Render(fmt.Sprintf("%-*s", width, n)), list)
		}
		return nil
	},
}

var targetStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start every service in a target",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		reg, _, err := activeRegistry()
		if err != nil {
			return err
		}
		n, err := ops.StartTarget(&ops.Resolved{Registry: reg}, name)
		if err != nil {
			return err
		}
		fmt.Printf("started target %s (%d service(s))\n", name, n)
		return nil
	},
}

var targetStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop a target's services, keeping any still held by another running target",
	Long: `Stop a target.

Stops every service in the snapshot taken when the target was started —
including any that were already running at that point — except services
still listed under another active target, which keep running.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		// No daemon means nothing is running, so nothing to stop. Match
		// `devrun stop`: don't auto-start a daemon just to stop a target.
		if err := ops.StopTarget(name); errors.Is(err, ops.ErrNoDaemon) {
			return fmt.Errorf("no daemon running — target %q is not running", name)
		} else if err != nil {
			return err
		}
		fmt.Printf("stopped target %s\n", name)
		return nil
	},
}

func init() {
	targetCmd.AddCommand(
		targetCreateCmd,
		targetAddCmd,
		targetRemoveCmd,
		targetListCmd,
		targetStartCmd,
		targetStopCmd,
	)
}

// newTargetNameArgs wraps an argument-count check with the name rule for a
// target that does not exist yet. An existing target keeps whatever name it
// already has, so a config written before the rule still works.
func newTargetNameArgs(count cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := count(cmd, args); err != nil {
			return err
		}
		if reg, _, err := activeRegistry(); err == nil {
			if _, exists := reg.Targets[args[0]]; exists {
				return nil
			}
		}
		return config.ValidateName("target", args[0])
	}
}
