package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop all services defined in .devrun.yaml",
	Args:  cobra.NoArgs,
	RunE:  runDown,
}

func runDown(_ *cobra.Command, _ []string) error {
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
		return nil
	}

	socketPath := config.SocketPath()

	fmt.Printf("[%s] stopping %d service(s)\n", proj.Name, len(proj.Services))

	exitCode := 0
	for name := range proj.Services {
		// Fresh connection per service — daemon handles one request per connection.
		c, err := client.Connect(socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error stopping %s: connect: %v\n", name, err)
			exitCode = 1
			continue
		}
		if err := stopOne(c, name); err != nil {
			fmt.Fprintf(os.Stderr, "  error stopping %s: %v\n", name, err)
			exitCode = 1
		}
		c.Close()
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
