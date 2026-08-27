package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/ipc"
)

var startCmd = &cobra.Command{
	Use:   "start <name|--all>",
	Short: "Start one or all services",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStart,
}

var startFlags struct {
	all bool
	fg  bool
}

func init() {
	startCmd.Flags().BoolVar(&startFlags.all, "all", false, "Start all registered services")
	startCmd.Flags().BoolVar(&startFlags.fg, "fg", false, "Attach terminal after starting")
}

func runStart(cmd *cobra.Command, args []string) error {
	if startFlags.fg && startFlags.all {
		return fmt.Errorf("--fg cannot be used with --all")
	}
	if !startFlags.all && len(args) == 0 {
		return fmt.Errorf("specify a service name or --all")
	}

	reg, src, err := activeRegistry()
	if err != nil {
		return err
	}

	socketPath := config.SocketPath()
	if err := daemon.EnsureDaemon(socketPath); err != nil {
		return fmt.Errorf("could not start daemon: %w", err)
	}

	if startFlags.all {
		return startAll(socketPath, reg, src.IsLocal())
	}
	return startOne(socketPath, args[0], inlineConfig(reg, args[0], src.IsLocal()), startFlags.fg)
}

// inlineConfig returns the ServiceConfig to ship in a start request. For a
// project devrun.yaml service it is the resolved definition (the daemon has no
// other way to see it); for a globally registered service it is nil, leaving the
// daemon to resolve the name from ~/.config/devrun/services.yaml.
func inlineConfig(reg *config.Registry, name string, local bool) *config.ServiceConfig {
	if !local || reg == nil {
		return nil
	}
	return reg.Services[name]
}

func startOne(socketPath, name string, cfg *config.ServiceConfig, attach bool) error {
	c, err := client.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()

	resp, err := c.Send("start", ipc.StartPayload{Name: name, Config: cfg})
	if err != nil {
		return fmt.Errorf("start request: %w", err)
	}
	if !resp.OK {
		if resp.Error != "" && containsAlreadyRunning(resp.Error) {
			fmt.Println(resp.Error)
			return nil
		}
		// We sent the full definition inline, so "not registered" can only mean
		// the running daemon is an older build that ignores it.
		if cfg != nil && containsNotRegistered(resp.Error) {
			return fmt.Errorf("the running daemon is from an older devrun and does not understand project %s services; run 'devrun daemon restart' and try again", config.ProjectFileName)
		}
		return fmt.Errorf("%s", resp.Error)
	}
	fmt.Printf("started %s\n", name)

	if attach {
		c.Close()
		return runFgByName(socketPath, name)
	}
	return nil
}

func containsAlreadyRunning(msg string) bool {
	suffix := "is already running"
	return len(msg) >= len(suffix) && msg[len(msg)-len(suffix):] == suffix
}

func containsNotRegistered(msg string) bool {
	return strings.Contains(msg, "not registered")
}

func startAll(socketPath string, reg *config.Registry, local bool) error {
	exitCode := 0
	for name := range reg.Services {
		if err := startOne(socketPath, name, inlineConfig(reg, name, local), false); err != nil {
			fmt.Fprintf(os.Stderr, "error starting %s: %v\n", name, err)
			exitCode = 1
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
