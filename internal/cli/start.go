package cli

import (
	"fmt"
	"os"

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

	socketPath := config.SocketPath()
	if err := daemon.EnsureDaemon(socketPath); err != nil {
		return fmt.Errorf("could not start daemon: %w", err)
	}

	if startFlags.all {
		return startAll(socketPath)
	}
	return startOne(socketPath, args[0], startFlags.fg)
}

func startOne(socketPath, name string, attach bool) error {
	c, err := client.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()

	resp, err := c.Send("start", ipc.StartPayload{Name: name})
	if err != nil {
		return fmt.Errorf("start request: %w", err)
	}
	if !resp.OK {
		if resp.Error != "" && containsAlreadyRunning(resp.Error) {
			fmt.Println(resp.Error)
			return nil
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

func startAll(socketPath string) error {
	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	exitCode := 0
	for name := range reg.Services {
		if err := startOne(socketPath, name, false); err != nil {
			fmt.Fprintf(os.Stderr, "error starting %s: %v\n", name, err)
			exitCode = 1
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
