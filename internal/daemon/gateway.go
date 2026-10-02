package daemon

import (
	"bufio"
	"encoding/json"
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
	defer func() { _ = pw.Close() }()

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
	if err != nil {
		_ = pr.Close()
		return nil, fmt.Errorf("start gateway: %w", err)
	}
	// Read the pid first: Release sets it to -1, and everything afterwards —
	// the liveness probe, stopping it, the status payload — is that number.
	pid := proc.Pid
	_ = proc.Release()

	addr, token2, err := readGatewayAddr(pr, token)
	_ = pr.Close()
	if err != nil {
		// It never got as far as listening; do not leave it behind.
		_, _ = process.TerminateGroup(pid, process.DefaultStopGrace)
		return nil, err
	}

	s.logger.Info("gateway started", "pid", pid, "addr", addr)
	return &gatewayChild{pid: pid, addr: addr, token: token2, cfg: cfg}, nil
}

// readGatewayAddr waits for the child's "gateway listening <addr>" line.
func readGatewayAddr(pr *os.File, token string) (addr, outToken string, err error) {
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
		ch <- line{err: sc.Err()}
	}()

	select {
	case l := <-ch:
		if l.err != nil {
			return "", "", fmt.Errorf("gateway did not start: %w", l.err)
		}
		const prefix = "gateway listening "
		if !strings.HasPrefix(l.s, prefix) {
			return "", "", fmt.Errorf("gateway said %q", l.s)
		}
		return strings.TrimSpace(strings.TrimPrefix(l.s, prefix)), token, nil
	case <-time.After(gatewayStartTimeout):
		return "", "", fmt.Errorf("gateway did not announce an address within %s", gatewayStartTimeout)
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
	return syscall.Kill(pid, 0) == nil
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
