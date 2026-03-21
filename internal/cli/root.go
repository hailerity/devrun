package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/daemon"
	"github.com/hailerity/procet/internal/tui"
)

var rootCmd = &cobra.Command{
	Use:   "procet",
	Short: "A lightweight process manager for developers",
	RunE: func(cmd *cobra.Command, args []string) error {
		socketPath := config.SocketPath()
		if err := daemon.EnsureDaemon(socketPath); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}

		reg, err := config.LoadRegistry(config.RegistryPath())
		if err != nil {
			// Empty registry is fine — TUI shows placeholder
			reg = &config.Registry{Services: map[string]*config.ServiceConfig{}}
		}

		logDir := config.DataDir()
		return tui.Run(socketPath, reg, logDir)
	},
}

// Execute is the CLI entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		addCmd,
		removeCmd,
		startCmd,
		stopCmd,
		listCmd,
		logsCmd,
		fgCmd,
	)
}
