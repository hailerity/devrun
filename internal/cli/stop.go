package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ops"
)

var stopCmd = &cobra.Command{
	Use:   "stop <name|--all>",
	Short: "Stop one or all services",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStop,
}

var stopFlags struct{ all bool }

func init() {
	stopCmd.Flags().BoolVar(&stopFlags.all, "all", false, "Stop all running services")
}

func runStop(_ *cobra.Command, args []string) error {
	if !stopFlags.all && len(args) == 0 {
		return fmt.Errorf("specify a service name or --all")
	}

	if stopFlags.all {
		reg, _, err := activeRegistry()
		if err != nil {
			return err
		}
		return stopAll(reg)
	}
	return stopOne(args[0])
}

// stopOne stops one service and prints the outcome. A service the daemon
// declines to stop — typically one that is not running — is reported, not an
// error.
func stopOne(name string) error {
	res, err := ops.Stop(name)
	if err != nil {
		return err
	}
	if res.NotStopped {
		fmt.Println(res.Message)
		return nil
	}
	fmt.Printf("stopped %s\n", name)
	return nil
}

// stopErrDetail is the per-service reason `stop --all` and `down` print. A
// daemon that cannot be reached reads "connect: <why>", as it always has.
func stopErrDetail(err error) string {
	if errors.Is(err, ops.ErrNoDaemon) {
		if inner := errors.Unwrap(err); inner != nil {
			return "connect: " + inner.Error()
		}
	}
	return err.Error()
}

func stopAll(reg *config.Registry) error {
	names := make([]string, 0, len(reg.Services))
	for name := range reg.Services {
		names = append(names, name)
	}
	// Reverse order (last registered stopped first)
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	exitCode := 0
	for _, name := range names {
		// ops.Stop opens a fresh connection per service: the daemon handles one
		// request per connection.
		if err := stopOne(name); err != nil {
			fmt.Fprintf(os.Stderr, "error stopping %s: %s\n", name, stopErrDetail(err))
			exitCode = 1
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
