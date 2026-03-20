package daemon

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hailerity/procet/internal/config"
)

// Run is the daemon entry point. Called from main when --_daemon flag is detected.
// socketPath is the Unix socket path to listen on.
func Run(socketPath string) error {
	if socketPath == "" {
		socketPath = config.SocketPath()
	}

	// Ensure data directory exists
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}

	logger := slog.Default()

	sup := newSupervisor(socketPath, logger)
	if err := sup.loadState(); err != nil {
		logger.Error("failed to load state on startup", "err", err)
	}

	// Remove stale socket if present
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on socket %s: %w", socketPath, err)
	}
	defer os.Remove(socketPath)
	defer ln.Close()

	logger.Info("daemon started", "socket", socketPath)

	// Start port polling in background
	go sup.startPortPoller()

	// Handle SIGTERM/SIGINT for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			break // socket closed = shutdown
		}
		go sup.handleConn(conn)
	}

	sup.shutdown()
	return nil
}

// EnsureDaemon checks if the daemon socket is alive. If not, launches the daemon
// by re-execing the current binary with --_daemon and waits up to 3s.
func EnsureDaemon(socketPath string) error {
	if isSocketAlive(socketPath) {
		return nil
	}
	return launchDaemon(socketPath)
}

func isSocketAlive(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func launchDaemon(socketPath string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open /dev/null: %w", err)
	}
	defer devNull.Close()

	proc, err := os.StartProcess(self, []string{self, "--_daemon", socketPath}, &os.ProcAttr{
		Files: []*os.File{devNull, devNull, devNull},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}
	proc.Release() // detach; don't wait for it

	return waitForSocket(socketPath, 3*time.Second)
}

func waitForSocket(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isSocketAlive(socketPath) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon to start")
}
