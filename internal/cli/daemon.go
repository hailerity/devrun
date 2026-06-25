package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Control the devrun daemon",
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		socketPath := config.SocketPath()
		running := isDaemonRunning(socketPath)

		if running {
			fmt.Printf("%s  %s\n", styleBold.Render("daemon"), styleGreen.Render("running"))
		} else {
			fmt.Printf("%s  %s\n", styleBold.Render("daemon"), styleRed.Render("stopped"))
		}

		if data, err := os.ReadFile(config.DaemonPIDPath()); err == nil {
			pidStr := strings.TrimSpace(string(data))
			if pid, err := strconv.Atoi(pidStr); err == nil && syscall.Kill(pid, 0) == nil {
				fmt.Printf("  %s  %s\n", styleLabel.Render("pid"), styleValue.Render(pidStr))
			}
		}
		fmt.Printf("  %s  %s\n", styleLabel.Render("socket"), styleValue.Render(socketPath))

		return nil
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the daemon (managed services keep running)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		socketPath := config.SocketPath()
		if !isDaemonRunning(socketPath) {
			fmt.Println("daemon is not running")
			return nil
		}

		c, err := client.Connect(socketPath)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		defer c.Close()

		resp, err := c.Send("daemon-stop", struct{}{})
		if err != nil {
			return fmt.Errorf("send stop: %w", err)
		}
		if !resp.OK {
			return fmt.Errorf("%s", resp.Error)
		}

		if err := waitForDaemonStop(socketPath, 5*time.Second); err != nil {
			return err
		}
		fmt.Println("daemon stopped")
		return nil
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the daemon",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		socketPath := config.SocketPath()

		if isDaemonRunning(socketPath) {
			c, err := client.Connect(socketPath)
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			c.Send("daemon-stop", struct{}{}) //nolint
			c.Close()

			if err := waitForDaemonStop(socketPath, 5*time.Second); err != nil {
				return err
			}
		}

		if err := daemon.EnsureDaemon(socketPath); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		fmt.Println("daemon restarted")
		return nil
	},
}

func waitForDaemonStop(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isDaemonRunning(socketPath) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not stop within %s", timeout)
}

func init() {
	daemonCmd.AddCommand(daemonStatusCmd, daemonStopCmd, daemonRestartCmd)
}
