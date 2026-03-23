package cli

import (
	"fmt"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

var removeCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a registered service",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemove,
}

func runRemove(_ *cobra.Command, args []string) error {
	name := args[0]

	// Check state.json directly — no daemon required
	state, err := config.LoadState(config.StatePath())
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	if svcState, ok := state.Services[name]; ok {
		if svcState.Status == config.StatusRunning || svcState.Status == config.StatusStarting {
			return fmt.Errorf("%s is running. Stop it first with 'devrun stop %s'", name, name)
		}
		// Double-check: if PID recorded, make sure it's actually dead
		if svcState.PID != nil {
			if err := syscall.Kill(*svcState.PID, 0); err == nil {
				return fmt.Errorf("%s appears to be running (PID %d). Stop it before removing", name, *svcState.PID)
			}
		}
		// Clean up state entry
		delete(state.Services, name)
		if err := config.SaveState(config.StatePath(), state); err != nil {
			return fmt.Errorf("save state: %w", err)
		}
	}

	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if _, ok := reg.Services[name]; !ok {
		return fmt.Errorf("service %q not registered", name)
	}
	delete(reg.Services, name)
	if err := config.SaveRegistry(config.RegistryPath(), reg); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}

	// Notify a running daemon so it evicts the service from its in-memory map.
	if c, err := client.Connect(config.SocketPath()); err == nil {
		_, _ = c.Send("remove", ipc.RemovePayload{Name: name})
		c.Close()
	}

	fmt.Printf("removed %s\n", name)
	return nil
}
