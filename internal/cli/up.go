package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start all services defined in .devrun.yaml",
	Args:  cobra.NoArgs,
	RunE:  runUp,
}

func runUp(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	proj, err := config.LoadProject(cwd)
	if err != nil {
		return err
	}
	if proj == nil {
		return fmt.Errorf("no %s found in current directory", config.ProjectFileName)
	}
	if len(proj.Services) == 0 {
		return fmt.Errorf("%s defines no services", config.ProjectFileName)
	}

	socketPath := config.SocketPath()
	if err := daemon.EnsureDaemon(socketPath); err != nil {
		return fmt.Errorf("could not start daemon: %w", err)
	}

	// Register/update all project services in the global registry so the
	// daemon can look them up by name.
	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	for name, svcCfg := range proj.ToServiceConfigs(cwd) {
		reg.Services[name] = svcCfg
	}
	if reg.Version == "" {
		reg.Version = "1"
	}
	if err := config.SaveRegistry(config.RegistryPath(), reg); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}

	fmt.Printf("[%s] starting %d service(s)\n", proj.Name, len(proj.Services))

	exitCode := 0
	for name := range proj.Services {
		if err := startOne(socketPath, name, false); err != nil {
			fmt.Fprintf(os.Stderr, "  error starting %s: %v\n", name, err)
			exitCode = 1
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
