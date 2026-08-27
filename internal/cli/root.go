package cli

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/tui"
)

var rootCmd = &cobra.Command{
	Use:     "devrun",
	Short:   "A lightweight process manager for developers",
	Version: Version,
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := activeRegistry()
		if err != nil {
			// Empty registry is fine — TUI shows placeholder
			reg = &config.Registry{Services: map[string]*config.ServiceConfig{}}
		}

		socketPath := config.SocketPath()
		if err := daemon.EnsureDaemon(socketPath); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}

		logDir := config.DataDir()
		return tui.Run(socketPath, reg, logDir)
	},
}

// globalFlag forces use of the global registry even when a devrun.yaml is present
// in the working directory. Bound to the persistent --global flag.
var globalFlag bool

// activeRegistry resolves the service registry for the current command, honoring
// --global and a devrun.yaml in the working directory.
func activeRegistry() (*config.Registry, config.Source, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, config.Source{}, fmt.Errorf("get working directory: %w", err)
	}
	return config.Resolve(cwd, globalFlag)
}

// Execute is the CLI entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Fallback for `go install`: ldflags are not applied, but Go embeds module
	// metadata we can read via debug.ReadBuildInfo.
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if v := info.Main.Version; v != "" && v != "(devel)" {
				Version = strings.TrimPrefix(v, "v")
				rootCmd.Version = Version
			}
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					if len(s.Value) >= 7 {
						Commit = s.Value[:7]
					}
				case "vcs.time":
					Date = s.Value
				}
			}
		}
	}
	rootCmd.SetVersionTemplate("devrun {{.Version}} (commit: " + Commit + ", built: " + Date + ")\n")
	rootCmd.PersistentFlags().BoolVarP(&globalFlag, "global", "g", false,
		"Use the global registry even when a "+config.ProjectFileName+" is present")
	rootCmd.AddCommand(
		upCmd,
		downCmd,
		addCmd,
		removeCmd,
		startCmd,
		stopCmd,
		listCmd,
		logsCmd,
		fgCmd,
		infoCmd,
		daemonCmd,
	)
}
