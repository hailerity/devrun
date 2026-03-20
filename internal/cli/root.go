package cli

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "procet",
	Short: "A lightweight process manager for developers",
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
