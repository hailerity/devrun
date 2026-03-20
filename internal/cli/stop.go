package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/procet/internal/client"
	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/ipc"
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

	socketPath := config.SocketPath()
	c, err := client.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer c.Close()

	if stopFlags.all {
		return stopAll(c)
	}
	return stopOne(c, args[0])
}

func stopOne(c *client.Client, name string) error {
	resp, err := c.Send("stop", ipc.StopPayload{Name: name})
	if err != nil {
		return err
	}
	if !resp.OK {
		fmt.Println(resp.Error)
		return nil
	}
	fmt.Printf("stopped %s\n", name)
	return nil
}

func stopAll(c *client.Client) error {
	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return err
	}
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
		if err := stopOne(c, name); err != nil {
			fmt.Fprintf(os.Stderr, "error stopping %s: %v\n", name, err)
			exitCode = 1
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
