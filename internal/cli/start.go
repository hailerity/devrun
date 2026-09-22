package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ops"
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

	if err := ops.EnsureDaemon(); err != nil {
		return err
	}

	r := &ops.Resolved{Registry: reg, Source: src}
	socketPath := config.SocketPath()
	if startFlags.all {
		return startAll(socketPath, r)
	}
	return startOne(socketPath, args[0], r.InlineConfig(args[0]), startFlags.fg)
}

// startOne starts one service and prints the outcome. "Already running" is
// reported but is not an error. With attach it then hands the terminal to the
// service, like `devrun fg`.
func startOne(socketPath, name string, cfg *config.ServiceConfig, attach bool) error {
	res, err := ops.Start(name, cfg)
	if err != nil {
		return err
	}
	if res.AlreadyRunning {
		fmt.Println(res.Message)
		return nil
	}
	fmt.Printf("started %s\n", name)

	if attach {
		return runFgByName(socketPath, name)
	}
	return nil
}

func startAll(socketPath string, r *ops.Resolved) error {
	exitCode := 0
	for name := range r.Registry.Services {
		if err := startOne(socketPath, name, r.InlineConfig(name), false); err != nil {
			fmt.Fprintf(os.Stderr, "error starting %s: %v\n", name, err)
			exitCode = 1
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
