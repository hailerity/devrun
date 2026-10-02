package daemon

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/process"
)

// gatewayChild is the supervisor's handle on the running gateway process.
type gatewayChild struct {
	pid   int
	addr  string
	token string
	cfg   config.GatewayConfig
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
		token = newGatewayToken()
	}
	s.mu.Unlock()

	child, err := s.spawnGateway(cfg, token)
	if err != nil {
		return errResp(err.Error())
	}

	s.mu.Lock()
	s.gateway = child
	resp := s.gatewayStatusLocked()
	_ = s.saveStateLocked()
	s.mu.Unlock()
	return okResp(resp)
}

func (s *supervisor) handleGatewayDown() *ipc.Response {
	s.gatewayOps.Lock()
	defer s.gatewayOps.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
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
	resp := s.gatewayStatusLocked()
	_ = s.saveStateLocked()
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

	stderr := devNull
	if logFile := os.Getenv("DEVRUN_GATEWAY_LOG"); logFile != "" {
		if f, err2 := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err2 == nil {
			stderr = f
			defer func() { _ = f.Close() }()
		}
	}

	env := append(os.Environ(),
		"DEVRUN_GATEWAY_CONFIG="+string(blob),
	)
	if token != "" {
		env = append(env, "DEVRUN_GATEWAY_TOKEN="+token)
	}

	proc, err := os.StartProcess(self, []string{self, "--_gateway", s.socketPath}, &os.ProcAttr{
		Env:   env,
		Files: []*os.File{devNull, pw, stderr},
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
	// Read the pid first: Release sets it to -1, and everything afterwards —
	// the liveness probe, stopping it, the status payload — is that number.
	pid := proc.Pid
	_ = proc.Release()

	addr, err := readGatewayAddr(pr)
	_ = pr.Close()
	if err != nil {
		// It never got as far as listening; do not leave it behind.
		_, _ = process.TerminateGroup(pid, process.DefaultStopGrace)
		return nil, err
	}

	s.logger.Info("gateway started", "pid", pid, "addr", addr)
	return &gatewayChild{pid: pid, addr: addr, token: token, cfg: cfg}, nil
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

// newGatewayToken mints a key for the gateway: 128 bits of randomness, hex so it
// survives a URL, a shell and a copy-paste without escaping.
func newGatewayToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A predictable key is worse than no gateway.
		panic("devrun: cannot mint a gateway token: " + err.Error())
	}
	return "k_" + hex.EncodeToString(b[:])
}
