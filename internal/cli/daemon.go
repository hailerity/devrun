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
	Short: "Restart the daemon (running services keep running, uninterrupted)",
	Long: `Restart the daemon.

Normally the daemon re-execs itself in place and hands every running
service — its definition and its live log capture — to the replacement,
so nothing is interrupted.

If that in-place handoff can't be used (the running daemon predates it,
or the re-exec fails) devrun falls back to stopping and relaunching the
daemon. Services keep running, but the new daemon re-adopts them by PID
only: log capture is paused and, for a project service, its command is
forgotten until you re-run 'devrun up' in that directory.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		socketPath := config.SocketPath()

		if !isDaemonRunning(socketPath) {
			if err := daemon.EnsureDaemon(socketPath); err != nil {
				return fmt.Errorf("start daemon: %w", err)
			}
			fmt.Println("daemon started")
			return nil
		}

		oldPID := readDaemonPID()

		// Preferred path: the daemon re-execs itself in place, handing the live
		// PTY masters to the replacement so log capture never gaps.
		c, err := client.Connect(socketPath)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		resp, sendErr := c.Send("daemon-reexec", struct{}{})
		c.Close()

		if sendErr == nil && resp != nil && resp.OK {
			if err := waitForDaemonReexec(socketPath, oldPID, 5*time.Second); err != nil {
				return err
			}
			fmt.Println("daemon restarted")
			return nil
		}

		// Fallback: an older daemon that doesn't understand daemon-reexec, or a
		// re-exec that failed. Stop and relaunch (services are re-adopted).
		c2, err := client.Connect(socketPath)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		c2.Send("daemon-stop", struct{}{}) //nolint
		c2.Close()
		if err := waitForDaemonStop(socketPath, 5*time.Second); err != nil {
			return err
		}
		if err := daemon.EnsureDaemon(socketPath); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		fmt.Println("daemon restarted")
		return nil
	},
}

// readDaemonPID returns the PID recorded in the daemon pidfile, or 0.
func readDaemonPID() int {
	data, err := os.ReadFile(config.DaemonPIDPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// waitForDaemonReexec blocks until the pidfile names a live daemon other than
// oldPID and its socket answers.
func waitForDaemonReexec(socketPath string, oldPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid := readDaemonPID(); pid > 0 && pid != oldPID &&
			syscall.Kill(pid, 0) == nil && isDaemonRunning(socketPath) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("replacement daemon did not come up within %s", timeout)
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
