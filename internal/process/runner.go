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

// DefaultStopGrace is how long a process is given to exit after SIGTERM before
// it is force-killed with SIGKILL.
const DefaultStopGrace = 5 * time.Second

// Stop terminates the process gracefully: SIGTERM first, then SIGKILL only if it
// is still alive after DefaultStopGrace. Signals go to the whole process group —
// Start runs each command in its own session via the PTY, so the child is a
// process-group leader — which also reaches services that spawn their own
// children (e.g. a dev server behind a shell wrapper).
//
// Stop does NOT call Process.Wait — watchExit in the daemon supervisor is the
// sole owner of Wait, preventing a double-waitpid deadlock.
func (p *Process) Stop() error {
	if p.Cmd.Process == nil {
		return nil
	}
	_, err := TerminateGroup(p.Cmd.Process.Pid, DefaultStopGrace)
	return err
}

// TerminateGroup gracefully stops the process group led by pid: it sends SIGTERM
// to the group, polls for the leader to exit for up to grace, and only then
// sends SIGKILL to the group. It never reaps the process — a caller that owns
// Wait must do that. It reports whether SIGKILL had to be sent.
//
// pid must be a process-group leader (pgid == pid), which holds for every
// process started by Start. Signals target the negative pid so the leader and
// all of its descendants receive them; if the group is already gone the bare
// pid is used as a fallback.
//
// The leader-exit poll uses kill(pid, 0), so if pid is a child of the current
// process it needs a concurrent reaper (the daemon's watchExit, or an eventual
// Wait) — an exited-but-unreaped zombie keeps the poll alive until the grace
// period elapses. Re-adopted processes are reaped by init, so this is a
// non-issue there.
func TerminateGroup(pid int, grace time.Duration) (killed bool, err error) {
	if pid <= 1 {
		return false, nil
	}
	sig := func(s syscall.Signal) error {
		e := syscall.Kill(-pid, s)
		if e == syscall.ESRCH {
			e = syscall.Kill(pid, s) // group gone; try the lone process
		}
		if e == syscall.ESRCH {
			return nil // already gone
		}
		return e
	}

	if err := sig(syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("sigterm: %w", err)
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if syscall.Kill(pid, 0) != nil {
			return false, nil // leader exited within the grace period
		}
	}

	_ = sig(syscall.SIGKILL)
	return true, nil
}
