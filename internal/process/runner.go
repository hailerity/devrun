package process

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Process represents a running child process with a PTY.
type Process struct {
	PTY *os.File
	Cmd *exec.Cmd
}

// Start forks the command with a PTY. CWD defaults to current dir if empty.
// The caller (daemon supervisor) is responsible for reading PTY output.
func Start(command, cwd string, env map[string]string) (*Process, error) {
	cmd := exec.Command("sh", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}
	return &Process{PTY: ptmx, Cmd: cmd}, nil
}

// Stop sends SIGTERM and waits up to 5 seconds, then sends SIGKILL.
func (p *Process) Stop() error {
	if err := p.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sigterm: %w", err)
	}
	done := make(chan error, 1)
	go func() { _, err := p.Cmd.Process.Wait(); done <- err }()
	select {
	case <-done:
		p.PTY.Close()
		return nil
	case <-time.After(5 * time.Second):
		_ = p.Cmd.Process.Signal(syscall.SIGKILL)
		<-done
		p.PTY.Close()
		return nil
	}
}
