package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/hailerity/devrun/internal/config"
)

// Run is the daemon entry point. Called from main when --_daemon flag is detected.
// socketPath is the Unix socket path to listen on.
func Run(socketPath string) error {
	return RunWithContext(context.Background(), socketPath)
}

// shutdownMode selects what happens to managed services when the daemon exits.
type shutdownMode int

const (
	modeStopServices shutdownMode = iota // SIGTERM/SIGINT: terminate everything
	modeKeepServices                     // `devrun daemon stop`: leave them running
	modeReexec                           // `devrun daemon restart`: a replacement has taken over
)

// RunWithContext is like Run but exits when ctx is cancelled. This is useful for
// in-process testing where the caller needs to stop the daemon programmatically.
func RunWithContext(ctx context.Context, socketPath string) error {
	if socketPath == "" {
		socketPath = config.SocketPath()
	}

	// Ensure data directory and socket parent directory exist.
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return fmt.Errorf("mkdir socket dir: %w", err)
	}

	logger := slog.Default()

	handoff, reexeced := reexecHandoff()

	sup := newSupervisor(socketPath, logger)
	if err := sup.loadState(); err != nil {
		logger.Error("failed to load state on startup", "err", err)
	}
	if reexeced {
		sup.adoptHandoff(handoff)
	}

	// Remove stale socket if present
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on socket %s: %w", socketPath, err)
	}

	// On a successful re-exec the replacement now owns the socket and pidfile —
	// leave both in place.
	var replaced bool
	defer func() {
		if !replaced {
			os.Remove(socketPath)
		}
	}()
	defer ln.Close()

	// Write pidfile so CLI can identify the daemon process.
	pidPath := config.DaemonPIDPath()
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	defer func() {
		if !replaced {
			os.Remove(pidPath)
		}
	}()

	logger.Info("daemon started", "socket", socketPath, "reexec", reexeced)

	shutdownReq := make(chan shutdownMode, 1)
	var shutdownOnce sync.Once
	doShutdown := func(m shutdownMode) {
		shutdownOnce.Do(func() {
			shutdownReq <- m
			ln.Close()
		})
	}

	// Wire the IPC-triggered shutdowns into the supervisor.
	sup.stopDaemon = func() { doShutdown(modeKeepServices) }
	sup.onReexec = func() { doShutdown(modeReexec) }

	// Create a context for graceful shutdown
	shutdownCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start port polling in background
	go sup.startPortPoller(shutdownCtx)

	// Handle SIGTERM/SIGINT: shut down and stop managed services.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigs:
			doShutdown(modeStopServices)
		case <-ctx.Done():
			doShutdown(modeKeepServices)
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			break // socket closed = shutdown
		}
		go sup.handleConn(conn)
	}

	cancel()
	switch <-shutdownReq {
	case modeStopServices:
		sup.shutdown()
	case modeReexec:
		replaced = true
	}
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
	self := os.Getenv("DEVRUN_DAEMON_BIN")
	if self == "" {
		var err error
		self, err = os.Executable()
		if err != nil {
			return fmt.Errorf("find executable: %w", err)
		}
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open /dev/null: %w", err)
	}
	defer devNull.Close()

	// If DEVRUN_DAEMON_LOG is set, redirect daemon stderr to that file for debugging.
	stderr := devNull
	if logFile := os.Getenv("DEVRUN_DAEMON_LOG"); logFile != "" {
		if f, err2 := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err2 == nil {
			stderr = f
			defer f.Close()
		}
	}

	proc, err := os.StartProcess(self, []string{self, "--_daemon", socketPath}, &os.ProcAttr{
		Env:   os.Environ(),
		Files: []*os.File{devNull, devNull, stderr},
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
