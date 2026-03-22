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
	// creack/pty opens /dev/ptmx without O_NONBLOCK, so os.NewFile creates a
	// non-pollable *os.File and Go falls back to a blocking OS thread for every
	// PTY read.  On macOS the thread scheduler can lag several milliseconds
	// between a PTY echo arriving and the thread waking up, causing visible
	// input lag when typing.  Dup the fd, set O_NONBLOCK, and re-wrap with
	// os.NewFile so Go registers it with kqueue and wakes the goroutine the
	// instant the kernel signals readability.
	if nb := makePollable(ptmx); nb != nil {
		return &Process{PTY: nb, Cmd: cmd}, nil
	}
	return &Process{PTY: ptmx, Cmd: cmd}, nil
}

// makePollable converts a blocking *os.File (e.g., a PTY master) into a
// non-blocking, kqueue/epoll-pollable file.  It dups the underlying fd, sets
// O_NONBLOCK on the dup, and wraps it with os.NewFile — which detects the
// non-blocking flag and registers the fd with Go's netpoller.  The original
// file is closed only after a valid replacement is ready; on any failure nil
// is returned and the original file is left untouched.
func makePollable(f *os.File) *os.File {
	rawConn, err := f.SyscallConn()
	if err != nil {
		return nil
	}
	var rawFD int
	_ = rawConn.Control(func(fd uintptr) { rawFD = int(fd) })

	dupFD, err := syscall.Dup(rawFD)
	if err != nil {
		return nil
	}
	syscall.CloseOnExec(dupFD)

	if err := syscall.SetNonblock(dupFD, true); err != nil {
		_ = syscall.Close(dupFD)
		return nil
	}

	nb := os.NewFile(uintptr(dupFD), f.Name())
	if nb == nil {
		_ = syscall.Close(dupFD)
		return nil
	}
	// Close the original blocking file; the dup keeps the file description alive.
	_ = f.Close()
	return nb
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
