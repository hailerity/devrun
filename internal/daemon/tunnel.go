package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/hailerity/devrun/internal/cloudflared"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/gateway"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/process"
)

// tunnelChild is the supervisor's handle on the running cloudflared.
//
// The second supervised child, and mostly the gateway's machinery used twice:
// its own session, never waited on, stopped by process group, recorded in
// state.json and re-adopted by pid with its argv checked.
type tunnelChild struct {
	pid  int
	kind string // KindNamed | KindQuick
	// origin is the gateway address this cloudflared was pointed at, fixed
	// when it started. It is what decides whether a gateway that just came up
	// has actually moved: comparing against "wherever the gateway was before"
	// calls it a move when there was no gateway before, and needlessly
	// restarts a working tunnel.
	origin    string
	publicURL string
	cfg       config.TunnelConfig
}

// How a tunnel was obtained, which decides what its URL is worth: a named
// tunnel's hostname is yours and stable, a quick one's is assigned per run and
// gone when it stops.
const (
	KindNamed = "named"
	KindQuick = "quick"
)

// namedSettle is how long a named tunnel is given to fail before it is
// called started. Long enough for an immediate exit — bad credentials, an
// unknown name — to have happened, short enough not to be felt.
var namedSettle = 750 * time.Millisecond

// quickURLTimeout bounds the wait for a quick tunnel to report its hostname.
// Generous: cloudflared has to reach the edge and register before it knows.
var quickURLTimeout = 30 * time.Second

// quickURLRe matches the hostname cloudflared prints for a quick tunnel. It
// sits inside an ASCII box in the log, so the URL is found rather than parsed
// out of a known position.
var quickURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// TunnelLogPath is where cloudflared's output is kept. A file, not a pipe: the
// tunnel outlives this daemon on the same paths the gateway does, and a write
// to a pipe whose reader has gone raises SIGPIPE, which on fd 2 the Go runtime
// turns into a fatal signal.
func TunnelLogPath() string { return config.InternalLogPath("tunnel") }

// spawnTunnel starts cloudflared against the gateway as its single origin.
//
// No config file is written and ~/.cloudflared is never touched: `tunnel run
// --url` does everything a catch-all ingress file would, so there is no file
// to own and nothing to collide with a setup the user already has.
func (s *supervisor) spawnTunnel(cfg config.TunnelConfig, origin string) (*tunnelChild, error) {
	bin, err := cloudflared.Find()
	if err != nil {
		return nil, err
	}

	// --http-host-header must never appear here. cloudflared knows only one
	// origin, so it would stamp a single fixed Host on every request and
	// destroy the label the gateway routes on. Host rewriting belongs in the
	// gateway, per service, after routing.
	argv := []string{bin, "tunnel", "--url", origin}
	kind := KindQuick
	if !cfg.IsQuick() {
		argv = []string{bin, "tunnel", "run", "--url", origin, cfg.Name}
		kind = KindNamed
	}

	if err := os.MkdirAll(filepath.Dir(TunnelLogPath()), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir log dir: %w", err)
	}
	logFile, err := os.OpenFile(TunnelLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open tunnel log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	// Where this run's output starts, so a quick URL scraped below belongs to
	// this cloudflared and not to one that ran an hour ago.
	var from int64
	if st, err := logFile.Stat(); err == nil {
		from = st.Size()
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/null: %w", err)
	}
	defer func() { _ = devNull.Close() }()

	proc, err := os.StartProcess(bin, argv, &os.ProcAttr{
		Env:   os.Environ(),
		Files: []*os.File{devNull, logFile, logFile},
		// Its own session, as the gateway gets, so a signal aimed at the
		// daemon's group does not take the tunnel down behind our back.
		Sys: &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}
	pid := proc.Pid
	s.reap(proc, "tunnel", s.clearTunnelPid)

	child := &tunnelChild{pid: pid, kind: kind, origin: origin, cfg: cfg}
	if kind == KindNamed {
		child.publicURL = "https://" + cfg.Hostname
		// Nothing in a named tunnel's output is parsed, so there is no wait
		// that would incidentally notice it failing. The common failures —
		// missing credentials, an unknown tunnel name — happen at once, so a
		// short settle catches them instead of reporting a URL for a process
		// that is already gone.
		time.Sleep(namedSettle)
	} else {
		child.publicURL = waitForQuickURL(TunnelLogPath(), from, pid)
	}

	// Liveness regardless of what it managed to print. A tunnel that exited is
	// a failure even if it announced a hostname on the way out: recording that
	// URL would have status claiming devrun publishes somewhere it does not.
	if !pidAlive(pid) {
		return nil, fmt.Errorf("cloudflared exited: %s", firstLogLine(TunnelLogPath(), from))
	}
	s.logger.Info("tunnel started", "pid", pid, "kind", kind, "url", child.publicURL)
	return child, nil
}

// waitForQuickURL watches the log for the hostname cloudflared was assigned.
//
// An empty result is not a failure. cloudflared's output is not an API and the
// banner's shape can change between releases, so a tunnel that is running but
// whose URL could not be read degrades to "running, URL unknown" rather than
// being torn down — the tunnel works, devrun just cannot print the address.
func waitForQuickURL(path string, from int64, pid int) string {
	deadline := time.Now().Add(quickURLTimeout)
	for time.Now().Before(deadline) {
		if url := scanQuickURL(path, from); url != "" {
			return url
		}
		if !pidAlive(pid) {
			// One last look: it may have printed the URL and then died.
			return scanQuickURL(path, from)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return ""
}

func scanQuickURL(path string, from int64) string {
	return quickURLRe.FindString(readLogFrom(path, from))
}

// logTail bounds how much of the log is read back, for a URL or an error.
const logTail = 16 << 10

func readLogFrom(path string, from int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, logTail)
	n, _ := f.ReadAt(buf, from)
	return string(buf[:n])
}

// firstLogLine is the opening of what this run wrote, which is where
// cloudflared says why it could not start.
func firstLogLine(path string, from int64) string {
	if l := cloudflared.FirstLine(readLogFrom(path, from)); l != "" {
		return l
	}
	return "nothing was logged"
}

// stopTunnelLocked stops the tunnel if there is one. Caller holds s.mu.
func (s *supervisor) stopTunnelLocked() {
	if s.tunnel == nil {
		return
	}
	pid := s.tunnel.pid
	s.tunnel = nil
	if pid <= 0 {
		return
	}
	if _, err := process.TerminateGroup(pid, process.DefaultStopGrace); err != nil {
		s.logger.Warn("stopping tunnel", "pid", pid, "err", err)
	}
}

// adoptTunnel takes back the cloudflared a previous daemon started.
//
// As with the gateway, liveness alone is not enough: state.json may name a pid
// from before a reboot, and signalling whatever holds that number now would be
// worse than concluding the tunnel is gone.
func adoptTunnel(ts *config.TunnelState, log *slog.Logger) *tunnelChild {
	if ts == nil || !pidAlive(ts.PID) {
		return nil
	}
	if !strings.Contains(process.CommandLine(ts.PID), cloudflared.Binary) {
		log.Info("not re-adopting the recorded tunnel: pid is something else now", "pid", ts.PID)
		return nil
	}
	log.Info("tunnel re-adopted", "pid", ts.PID, "url", ts.PublicURL)
	return &tunnelChild{pid: ts.PID, kind: ts.Kind, origin: ts.Origin, publicURL: ts.PublicURL, cfg: ts.Config}
}

// originURL is the gateway as cloudflared should reach it. A wildcard bind is
// not an address to connect to, so it is dialled on loopback.
func originURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// handleTunnelUp publishes the gateway, starting it first if it is not up.
//
// Lifecycle flows one way: the dangerous command may imply the safe one, so
// `tunnel up` starts a gateway, while `gateway up` never starts a tunnel.
func (s *supervisor) handleTunnelUp(raw json.RawMessage) *ipc.Response {
	var p ipc.TunnelUpPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return errResp(fmt.Sprintf("bad payload: %v", err))
		}
	}
	if err := p.Config.Validate(); err != nil {
		return errResp(err.Error())
	}
	if err := p.Gateway.Validate(); err != nil {
		return errResp(err.Error())
	}
	cfg := p.Config.Defaults()
	gwCfg := p.Gateway.Defaults()
	// Naming a service here is what makes it publishable, so the allowlist is
	// widened before anything can be reached rather than after.
	gwCfg.SetExposed(p.Expose, true)

	// The same lock the gateway's lifecycle takes: these two cascade, so one
	// order for both and no pair to deadlock.
	s.gatewayOps.Lock()
	defer s.gatewayOps.Unlock()

	gw, err := s.ensureGateway(gwCfg)
	if err != nil {
		return errResp(err.Error())
	}

	s.mu.Lock()
	// Already publishing the same way: idempotent, so this is safe to repeat.
	if s.tunnel != nil && sameTunnelConfig(s.tunnel.cfg, cfg) &&
		s.tunnel.origin == originURL(gw.addr) && pidAlive(s.tunnel.pid) {
		resp := s.gatewayStatusLocked()
		s.mu.Unlock()
		return okResp(resp)
	}
	s.stopTunnelLocked()
	_ = s.saveStateLocked()
	s.mu.Unlock()

	child, err := s.spawnTunnel(cfg, originURL(gw.addr))
	if err != nil {
		return errResp(err.Error())
	}

	s.mu.Lock()
	live := recordLive(&s.tunnel, child, child.pid)
	_ = s.saveStateLocked()
	resp := s.gatewayStatusLocked()
	s.mu.Unlock()
	if !live {
		return errResp("cloudflared exited immediately after starting; see " + TunnelLogPath())
	}
	return okResp(resp)
}

func (s *supervisor) handleTunnelDown() *ipc.Response {
	s.gatewayOps.Lock()
	defer s.gatewayOps.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// Only the tunnel. Teardown cascades downward — a gateway without a
	// tunnel is the ordinary local case, so there is nothing to take with it.
	s.stopTunnelLocked()
	_ = s.saveStateLocked()
	return &ipc.Response{OK: true}
}

// handleTunnelList answers with the account's tunnel names so the CLI can
// catch a typo before cloudflared does. Knowing cloudflared lives here rather
// than in the CLI, which keeps that knowledge in one package.
func (s *supervisor) handleTunnelList() *ipc.Response {
	bin, err := cloudflared.Find()
	if err != nil {
		// Not installed is a legitimate answer to "which tunnels exist", not
		// an error: the caller degrades to unverified either way.
		return okResp(ipc.TunnelListPayload{Known: false})
	}
	names, err := cloudflared.List(context.Background(), bin)
	if err != nil {
		s.logger.Info("tunnel list unavailable", "err", err)
		return okResp(ipc.TunnelListPayload{Known: false})
	}
	return okResp(ipc.TunnelListPayload{Names: names, Known: true})
}

// ensureGateway returns the running gateway, starting it if needed. Caller
// holds gatewayOps.
func (s *supervisor) ensureGateway(cfg config.GatewayConfig) (*gatewayChild, error) {
	s.mu.Lock()
	if s.gateway != nil && sameGatewayConfig(s.gateway.cfg, cfg) && pidAlive(s.gateway.pid) {
		gw := s.gateway
		s.mu.Unlock()
		return gw, nil
	}
	token := ""
	if s.gateway != nil {
		token = s.gateway.token
		s.stopGatewayLocked()
	}
	if token == "" {
		token = gateway.NewToken()
	}
	s.mu.Unlock()

	child, err := s.spawnGateway(cfg, token)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !recordLive(&s.gateway, child, child.pid) {
		_ = s.saveStateLocked()
		return nil, errors.New("the gateway exited immediately after starting; see the daemon log")
	}
	_ = s.saveStateLocked()
	return child, nil
}

// tunnelStatusLocked builds the tunnel half of the status. Caller holds s.mu.
func (s *supervisor) tunnelStatusLocked() *ipc.TunnelStatusPayload {
	if s.tunnel == nil || !pidAlive(s.tunnel.pid) {
		return nil
	}
	pid := s.tunnel.pid
	return &ipc.TunnelStatusPayload{
		Running:   true,
		Kind:      s.tunnel.kind,
		Name:      s.tunnel.cfg.Name,
		PublicURL: s.tunnel.publicURL,
		PID:       &pid,
	}
}

func sameTunnelConfig(a, b config.TunnelConfig) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(x) == string(y)
}
