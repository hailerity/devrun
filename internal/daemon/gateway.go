package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/hailerity/devrun/internal/cloudflared"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/gateway"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/process"
)

// gatewayChild is the supervisor's handle on the running gateway process.
//
// It is recorded in state.json and re-adopted by adoptGateway on startup. The
// gateway outlives the daemon on every exit path but SIGTERM — it is spawned
// into its own session and never waited on — so without that record a
// `devrun daemon stop` or `restart` left it running, holding its port, with
// nothing able to see or stop it ever again.
type gatewayChild struct {
	pid   int
	addr  string
	token string
	cfg   config.GatewayConfig
}

// gatewayArgMarker is how a gateway process is recognised in its own argv.
const gatewayArgMarker = "--_gateway"

// adoptGateway takes back the gateway a previous daemon started, or returns nil
// when there is nothing to take back.
//
// Liveness alone is not enough to go on. state.json may name a pid from before
// a reboot, and whatever holds that number now would be reported as the gateway
// and then signalled by `devrun gateway down`. The argv has to agree that it is
// one of ours; when it cannot be read, the answer is no.
func adoptGateway(gs *config.GatewayState, log *slog.Logger) *gatewayChild {
	if gs == nil || !pidAlive(gs.PID) {
		return nil
	}
	if !strings.Contains(process.CommandLine(gs.PID), gatewayArgMarker) {
		log.Info("not re-adopting the recorded gateway: pid is something else now", "pid", gs.PID)
		return nil
	}
	log.Info("gateway re-adopted", "pid", gs.PID, "addr", gs.Addr)
	return &gatewayChild{pid: gs.PID, addr: gs.Addr, token: gs.Token, cfg: gs.Config}
}

// gatewayStartTimeout bounds how long we wait for the child to announce its
// address. It is generous: the child's first act is to ask this daemon for the
// service list, and that goes through the same lock everything else does.
const gatewayStartTimeout = 10 * time.Second

func (s *supervisor) handleGatewayUp(raw json.RawMessage) *ipc.Response {
	var p ipc.GatewayUpPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return errResp(fmt.Sprintf("bad payload: %v", err))
		}
	}
	if err := p.Config.Validate(); err != nil {
		return errResp(err.Error())
	}
	cfg := p.Config.Defaults()

	// One lifecycle operation at a time. Without this, two concurrent ups each
	// spawn a child and the second assignment orphans the first — a listener
	// holding a port with nothing left to stop it.
	s.gatewayOps.Lock()
	defer s.gatewayOps.Unlock()

	s.mu.Lock()
	// Already up with the same configuration: idempotent, so `tunnel up` can
	// call this without caring whether the gateway is running.
	if s.gateway != nil && sameGatewayConfig(s.gateway.cfg, cfg) && pidAlive(s.gateway.pid) {
		resp := s.gatewayStatusLocked()
		s.mu.Unlock()
		return okResp(resp)
	}
	// Configuration changed, so the child has to be replaced — it reads its
	// config once at startup. Keep the token: a link already shared must
	// keep working.
	token := ""
	if s.gateway != nil {
		token = s.gateway.token
		s.stopGatewayLocked()
	}
	if token == "" {
		// Minted here, not in the child: the daemon is what reports it to the
		// CLI, and what must hand the same one back on every restart so a link
		// already shared keeps working.
		token = gateway.NewToken()
	}
	s.mu.Unlock()

	child, err := s.spawnGateway(cfg, token)
	if err != nil {
		return errResp(err.Error())
	}

	s.mu.Lock()
	s.gateway = child
	// Against the origin the tunnel actually holds, not against whatever the
	// gateway's address was before. With no gateway running, "before" is the
	// empty string and every up would look like a move — tearing down a
	// working tunnel and handing a quick one a new URL for nothing.
	moved := s.tunnel != nil && s.tunnel.origin != originURL(child.addr)
	tunnelCfg := config.TunnelConfig{}
	if moved {
		tunnelCfg = s.tunnel.cfg
		s.stopTunnelLocked()
	}
	// Saved once both handles are settled, so the record never describes a
	// tunnel that has just been stopped. Recording it at all is what lets a
	// daemon that is stopped or re-execs hand these same children back.
	_ = s.saveStateLocked()
	s.mu.Unlock()

	// A tunnel carries one origin, fixed at spawn. If the gateway just moved —
	// a new port, a new bind — cloudflared would go on publishing the old
	// address, which now answers nothing. Following it is the only option that
	// keeps the tunnel true; a quick tunnel gets a new URL in the process,
	// which is why the CLI reports the URL after every up.
	if moved {
		if err := s.followGateway(tunnelCfg, child.addr); err != nil {
			// The gateway is up; publishing is not. Reported rather than
			// swallowed, since the two halves now disagree with what the user
			// had before the command.
			return errResp(err.Error())
		}
	}

	s.mu.Lock()
	resp := s.gatewayStatusLocked()
	s.mu.Unlock()
	return okResp(resp)
}

// followGateway restarts the tunnel against the gateway's new address, and
// reports whether it managed to.
//
// A failure here leaves the gateway up and nothing publishing, which the
// caller has to say out loud: the user asked to move the gateway, not to stop
// publishing, and a log line nobody reads is not an answer.
func (s *supervisor) followGateway(cfg config.TunnelConfig, addr string) error {
	child, err := s.spawnTunnel(cfg, originURL(addr))
	if err != nil {
		s.logger.Warn("gateway moved; the tunnel could not follow it", "addr", addr, "err", err)
		// The stop is already persisted, so the record matches reality —
		// nothing is publishing — without another save.
		return fmt.Errorf("the gateway moved to %s but the tunnel could not follow: %w", addr, err)
	}
	s.mu.Lock()
	s.tunnel = child
	_ = s.saveStateLocked()
	s.mu.Unlock()
	return nil
}

func (s *supervisor) handleGatewayDown() *ipc.Response {
	s.gatewayOps.Lock()
	defer s.gatewayOps.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// The tunnel first. It is pointed at the gateway as its only origin, so
	// stopping them the other way round leaves cloudflared publishing an
	// address nothing answers on.
	s.stopTunnelLocked()
	s.stopGatewayLocked()
	_ = s.saveStateLocked()
	return &ipc.Response{OK: true}
}

func (s *supervisor) handleGatewayStatus() *ipc.Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	return okResp(s.gatewayStatusLocked())
}

// handleGatewayExpose edits the allowlist and restarts the child, which reads it
// once at startup. Restarting is the honest option: the alternative is a
// gateway serving a list it was told to stop serving.
func (s *supervisor) handleGatewayExpose(raw json.RawMessage) *ipc.Response {
	var p ipc.GatewayExposePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return errResp(fmt.Sprintf("bad payload: %v", err))
	}

	s.gatewayOps.Lock()
	defer s.gatewayOps.Unlock()

	s.mu.Lock()
	if s.gateway == nil {
		s.mu.Unlock()
		return errResp("gateway is not running")
	}
	cfg := s.gateway.cfg
	token := s.gateway.token
	if !cfg.SetExposed(p.Names, p.Exposed) {
		resp := s.gatewayStatusLocked()
		s.mu.Unlock()
		return okResp(resp) // nothing changed; no need to restart
	}
	s.stopGatewayLocked()
	s.mu.Unlock()

	child, err := s.spawnGateway(cfg, token)
	if err != nil {
		return errResp(err.Error())
	}

	s.mu.Lock()
	s.gateway = child
	_ = s.saveStateLocked()
	resp := s.gatewayStatusLocked()
	s.mu.Unlock()
	return okResp(resp)
}

// spawnGateway re-execs this binary as the gateway and waits for it to announce
// the address it bound. The address cannot be assumed: a configured port of 0
// means the kernel chooses.
func (s *supervisor) spawnGateway(cfg config.GatewayConfig, token string) (*gatewayChild, error) {
	self := os.Getenv("DEVRUN_DAEMON_BIN")
	if self == "" {
		var err error
		self, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("find executable: %w", err)
		}
	}

	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode gateway config: %w", err)
	}

	// The child writes its address to stdout, so that end is a pipe rather than
	// /dev/null.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		_ = pr.Close()
		return nil, fmt.Errorf("open /dev/null: %w", err)
	}
	defer func() { _ = devNull.Close() }()

	// The child's stderr is where it says why it could not start: a port already
	// taken, a bind it cannot have, a panic. Sending it to /dev/null turned every
	// one of those into "it exited before listening", which names no cause at
	// all and leaves nothing to act on.
	said, err := openChildStderr()
	if err != nil {
		_ = pr.Close()
		return nil, err
	}
	defer said.closeParentCopy()

	env := append(os.Environ(),
		"DEVRUN_GATEWAY_CONFIG="+string(blob),
	)
	if token != "" {
		env = append(env, "DEVRUN_GATEWAY_TOKEN="+token)
	}

	proc, err := os.StartProcess(self, []string{self, "--_gateway", s.socketPath}, &os.ProcAttr{
		Env:   env,
		Files: []*os.File{devNull, pw, said.f},
		// Its own session, so a signal aimed at the daemon's group does not
		// also take the gateway down behind our back.
		Sys: &syscall.SysProcAttr{Setsid: true},
	})
	// The child holds the write end now, so close the parent's copy. That is
	// what lets the scanner below see EOF when the child dies before
	// announcing, instead of waiting out the whole start timeout.
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return nil, fmt.Errorf("start gateway: %w", err)
	}
	// Read the pid first: everything afterwards — the liveness probe, stopping
	// it, the status payload — is that number.
	pid := proc.Pid
	s.reap(proc, "gateway", s.clearGatewayPid)

	addr, err := readGatewayAddr(pr)
	_ = pr.Close()
	if err != nil {
		// It never got as far as listening; do not leave it behind.
		_, _ = process.TerminateGroup(pid, process.DefaultStopGrace)
		if line := said.firstLine(); line != "" {
			err = fmt.Errorf("%w: %s", err, line)
		}
		said.discard()
		return nil, err
	}
	said.discard()

	s.logger.Info("gateway started", "pid", pid, "addr", addr)
	return &gatewayChild{pid: pid, addr: addr, token: token, cfg: cfg}, nil
}

// stderrCap bounds how much of the child's stderr is read back when it fails.
const stderrCap = 4096

// gatewayLogEnv names a file to keep the gateway's stderr in, for anyone
// debugging the child itself. Unset, it goes to a temp file that is read back
// only on failure and then removed.
const gatewayLogEnv = "DEVRUN_GATEWAY_LOG"

// childStderr is where the gateway child's stderr goes, and how to read back
// what this particular child wrote to it.
//
// A file, deliberately, not a pipe. The gateway is meant to outlive this
// daemon, and a write to a pipe whose read end has gone raises SIGPIPE — which
// on fd 2 the Go runtime turns into a fatal signal. Tying the child's stderr
// to the daemon's lifetime would mean a surviving gateway dies the next time
// anything logs. A file cannot kill it, and needs no draining goroutine.
type childStderr struct {
	f *os.File
	// from is the size the file had before this child started. With
	// DEVRUN_GATEWAY_LOG the file is appended to across restarts, and quoting a
	// previous child's failure would be worse than quoting none.
	from int64
	temp bool
}

func openChildStderr() (*childStderr, error) {
	if path := os.Getenv(gatewayLogEnv); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", gatewayLogEnv, err)
		}
		var from int64
		if st, err := f.Stat(); err == nil {
			from = st.Size()
		}
		return &childStderr{f: f, from: from}, nil
	}
	f, err := os.CreateTemp("", "devrun-gateway-*.err")
	if err != nil {
		return nil, fmt.Errorf("gateway stderr file: %w", err)
	}
	return &childStderr{f: f, temp: true}, nil
}

// closeParentCopy drops this process's descriptor. The child holds its own.
func (c *childStderr) closeParentCopy() { _ = c.f.Close() }

// discard removes the temp file once its contents are no longer wanted. The
// child keeps writing to the open descriptor either way; unlinked, that costs
// nothing once it exits. A file named by DEVRUN_GATEWAY_LOG is left alone —
// the point of setting it is to keep the output.
func (c *childStderr) discard() {
	if c.temp {
		_ = os.Remove(c.f.Name())
	}
}

// firstLine is the one line worth putting in an error: "address already in
// use", or "panic: ...". What follows is the stack, or the fallout.
func (c *childStderr) firstLine() string {
	f, err := os.Open(c.f.Name())
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, stderrCap)
	n, _ := f.ReadAt(buf, c.from)
	// The child labels its own messages "gateway: ", which would read twice
	// over inside an error the caller also labels.
	return strings.TrimPrefix(cloudflared.FirstLine(string(buf[:n])), "gateway: ")
}

// readGatewayAddr waits for the child's "gateway listening <addr>" line.
func readGatewayAddr(pr *os.File) (string, error) {
	type line struct {
		s   string
		err error
	}
	ch := make(chan line, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		if sc.Scan() {
			ch <- line{s: sc.Text()}
			return
		}
		if err := sc.Err(); err != nil {
			ch <- line{err: err}
			return
		}
		// EOF with nothing read. The child holds the only write end, so this
		// means it exited before it managed to listen.
		ch <- line{err: errors.New("it exited before listening")}
	}()

	select {
	case l := <-ch:
		if l.err != nil {
			return "", fmt.Errorf("gateway did not start: %w", l.err)
		}
		const prefix = "gateway listening "
		if !strings.HasPrefix(l.s, prefix) {
			return "", fmt.Errorf("gateway said %q", l.s)
		}
		return strings.TrimSpace(strings.TrimPrefix(l.s, prefix)), nil
	case <-time.After(gatewayStartTimeout):
		return "", fmt.Errorf("gateway did not announce an address within %s", gatewayStartTimeout)
	}
}

// stopGatewayLocked stops the child if there is one. Caller holds s.mu.
func (s *supervisor) stopGatewayLocked() {
	if s.gateway == nil {
		return
	}
	pid := s.gateway.pid
	s.gateway = nil
	if pid <= 0 {
		return
	}
	// Outside the lock would be nicer, but the window is a SIGTERM and a short
	// wait, and holding it keeps the "is there a gateway" answer consistent.
	if _, err := process.TerminateGroup(pid, process.DefaultStopGrace); err != nil {
		s.logger.Warn("stopping gateway", "pid", pid, "err", err)
	}
}

// gatewayStatusLocked builds the status payload. Caller holds s.mu.
func (s *supervisor) gatewayStatusLocked() ipc.GatewayStatusPayload {
	if s.gateway == nil || !pidAlive(s.gateway.pid) {
		return ipc.GatewayStatusPayload{}
	}
	pid := s.gateway.pid
	return ipc.GatewayStatusPayload{
		Running: true,
		Addr:    s.gateway.addr,
		Posture: s.gateway.cfg.Posture,
		Mode:    s.gateway.cfg.Mode,
		Token:   s.gateway.token,
		Exposed: s.gateway.cfg.ExposedSet(),
		PID:     &pid,
	}
}

// pidAlive reports whether a pid is still a live process, the same kill(pid, 0)
// probe used to re-adopt services.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	// EPERM means the process is there but not ours to signal, which is still
	// alive. Only ESRCH means gone.
	return err == nil || errors.Is(err, syscall.EPERM)
}

func sameGatewayConfig(a, b config.GatewayConfig) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(x) == string(y)
}

func okResp(v any) *ipc.Response {
	blob, err := json.Marshal(v)
	if err != nil {
		return errResp(fmt.Sprintf("encode response: %v", err))
	}
	return &ipc.Response{OK: true, Payload: blob}
}
