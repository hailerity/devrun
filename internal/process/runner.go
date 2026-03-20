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

// Stop sends SIGTERM to the process. If the process does not exit within 5s,
// it sends SIGKILL. Stop does NOT call Process.Wait — watchExit in the daemon
// supervisor is the sole owner of Wait, preventing a double-waitpid deadlock.
func (p *Process) Stop() error {
	if p.Cmd.Process == nil {
		return nil
	}
	if err := p.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sigterm: %w", err)
	}
	done := make(chan struct{})
	go func() {
		// Poll via kill -0 (does not reap the process) for up to 5s.
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			if err := syscall.Kill(p.Cmd.Process.Pid, 0); err != nil {
				// Process is gone — watchExit will call Wait.
				close(done)
				return
			}
		}
		// Still alive after 5s — escalate to SIGKILL.
		_ = p.Cmd.Process.Signal(syscall.SIGKILL)
		close(done)
	}()
	<-done
	return nil
}
