package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/ipc"
)

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Group services into named targets you can start and stop as a unit",
}

var targetCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new, empty target",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		if err := editTargets(func(targets map[string][]string, _ map[string]bool) error {
			if _, ok := targets[name]; ok {
				return fmt.Errorf("target %q already exists", name)
			}
			targets[name] = []string{}
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("created target %s\n", name)
		return nil
	},
}

var targetAddCmd = &cobra.Command{
	Use:   "add <name> <service>...",
	Short: "Add one or more services to a target (creating it if needed)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		name, svcs := args[0], args[1:]
		if err := editTargets(func(targets map[string][]string, known map[string]bool) error {
			for _, s := range svcs {
				if !known[s] {
					return fmt.Errorf("service %q is not defined in this config", s)
				}
			}
			targets[name] = mergeMembers(targets[name], svcs)
			return nil
		}); err != nil {
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
		if err := editTargets(func(targets map[string][]string, _ map[string]bool) error {
			if _, ok := targets[name]; !ok {
				return fmt.Errorf("target %q does not exist", name)
			}
			if len(svcs) == 0 {
				delete(targets, name)
				return nil
			}
			targets[name] = dropMembers(targets[name], svcs)
			return nil
		}); err != nil {
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

		active := activeTargetSet()

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
		if _, ok := reg.Targets[name]; !ok {
			return fmt.Errorf("target %q does not exist", name)
		}
		members := reg.TargetMemberConfigs(name)
		if len(members) == 0 {
			return fmt.Errorf("target %q has no runnable services", name)
		}

		socketPath := config.SocketPath()
		if err := daemon.EnsureDaemon(socketPath); err != nil {
			return fmt.Errorf("could not start daemon: %w", err)
		}
		c, err := client.Connect(socketPath)
		if err != nil {
			return fmt.Errorf("connect to daemon: %w", err)
		}
		defer c.Close()

		resp, err := c.Send("target-start", ipc.TargetStartPayload{Name: name, Services: members})
		if err != nil {
			return fmt.Errorf("target-start request: %w", err)
		}
		if !resp.OK {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Printf("started target %s (%d service(s))\n", name, len(members))
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
		c, err := client.Connect(config.SocketPath())
		if err != nil {
			// No daemon means nothing is running, so nothing to stop. Match
			// `devrun stop`: don't auto-start a daemon just to stop a target.
			return fmt.Errorf("no daemon running — target %q is not running", name)
		}
		defer c.Close()

		resp, err := c.Send("target-stop", ipc.TargetStopPayload{Name: name})
		if err != nil {
			return fmt.Errorf("target-stop request: %w", err)
		}
		if !resp.OK {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Printf("stopped target %s\n", name)
		return nil
	},
}

// mergeMembers returns existing with each name in add appended once, preserving
// order and skipping names already present.
func mergeMembers(existing, add []string) []string {
	out := append([]string(nil), existing...)
	has := make(map[string]bool, len(out))
	for _, m := range out {
		has[m] = true
	}
	for _, s := range add {
		if !has[s] {
			out = append(out, s)
			has[s] = true
		}
	}
	return out
}

// dropMembers returns existing with every name in drop removed, preserving order.
func dropMembers(existing, drop []string) []string {
	rm := make(map[string]bool, len(drop))
	for _, s := range drop {
		rm[s] = true
	}
	out := make([]string, 0, len(existing))
	for _, m := range existing {
		if !rm[m] {
			out = append(out, m)
		}
	}
	return out
}

// editTargets loads the active config (project devrun.yaml or the global
// registry), hands its target map and the set of service names known in that
// scope to fn, and writes the config back when fn returns nil. fn mutates the
// map in place.
func editTargets(fn func(targets map[string][]string, known map[string]bool) error) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	_, src, err := config.Resolve(cwd, globalFlag)
	if err != nil {
		return err
	}

	if src.IsLocal() {
		proj, err := config.LoadProject(src.Dir)
		if err != nil {
			return err
		}
		if proj == nil {
			return fmt.Errorf("no %s in %s", config.ProjectFileName, src.Dir)
		}
		if proj.Targets == nil {
			proj.Targets = make(map[string][]string)
		}
		if err := fn(proj.Targets, keySet(proj.Services)); err != nil {
			return err
		}
		return config.SaveProject(src.Dir, proj)
	}

	greg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if greg.Targets == nil {
		greg.Targets = make(map[string][]string)
	}
	if greg.Version == "" {
		greg.Version = "1"
	}
	if err := fn(greg.Targets, keySet(greg.Services)); err != nil {
		return err
	}
	return config.SaveRegistry(config.RegistryPath(), greg)
}

// keySet returns the set of keys of m as a bool map — used to validate target
// members against the service names in the same file being edited.
func keySet[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// activeTargetSet asks a running daemon which targets are active. Best effort:
// an unreachable daemon yields an empty set rather than an error.
func activeTargetSet() map[string]bool {
	out := map[string]bool{}
	c, err := client.Connect(config.SocketPath())
	if err != nil {
		return out
	}
	defer c.Close()
	resp, err := c.Send("list", struct{}{})
	if err != nil || !resp.OK {
		return out
	}
	var payload ipc.ListResponsePayload
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return out
	}
	for _, n := range payload.ActiveTargets {
		out[n] = true
	}
	return out
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
