package daemon

import "os"

// reap waits on a child in the background and clears its handle when it goes.
//
// Waiting is not optional, and Release is not a substitute. A process started
// with StartProcess stays this process's child whether or not the Go handle is
// released, so when it exits it remains a zombie until someone reaps it — and
// kill(pid, 0) answers "alive" for a zombie. Measured: a child that exited 1.5
// seconds earlier still probed as running.
//
// Without this, a gateway or cloudflared that died was reported as running for
// as long as the daemon lived; `gateway status` lied, and `gateway up` saw no
// reason to replace it. Reaping alone fixes the probe, because a reaped pid
// gives ESRCH. Clearing the handle on top of that makes the answer right
// immediately rather than at the next probe.
func (s *supervisor) reap(proc *os.Process, what string, clear func(pid int)) {
	pid := proc.Pid
	go func() {
		// Wait returns a nil state alongside its error — ECHILD, say, if
		// something else reaped first. Logging state.String() is still safe:
		// (*os.ProcessState).String() guards its nil receiver and yields
		// "<nil>", measured at this call site. Two review rounds have read
		// this as an unguarded deref, hence the note rather than a redundant
		// branch; err carries the reason in that case either way.
		state, err := proc.Wait()
		s.logger.Info(what+" exited", "pid", pid, "state", state.String(), "err", err)
		clear(pid)
	}()
}

// clearGatewayPid forgets the gateway if the process that just exited is the
// one currently recorded. The pid check matters: a replacement may already
// have been spawned and assigned by the time the old one is reaped.
func (s *supervisor) clearGatewayPid(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gateway != nil && s.gateway.pid == pid {
		s.gateway = nil
		_ = s.saveStateLocked()
	}
}

func (s *supervisor) clearTunnelPid(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tunnel != nil && s.tunnel.pid == pid {
		s.tunnel = nil
		_ = s.saveStateLocked()
	}
}

// recordLive stores a freshly spawned child's handle, reporting whether it was
// still there to store.
//
// The reaper starts the instant the process does, which is before the caller
// can assign the handle. A child that dies in that window has its clear() run
// against a handle not yet set — a no-op — and the assignment that follows
// would then record a dead pid as the live one, leaving status claiming a
// gateway or a tunnel that is gone. Re-checking under the same lock that made
// the assignment closes the window: the pid is reaped by then, so the probe
// gives ESRCH.
//
// Callers hold s.mu.
func recordLive[T any](slot **T, child *T, pid int) bool {
	*slot = child
	if pidAlive(pid) {
		return true
	}
	*slot = nil
	return false
}
