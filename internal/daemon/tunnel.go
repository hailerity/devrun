package daemon

import (
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
	"github.com/hailerity/devrun/internal/process"
)

// tunnelChild is the supervisor's handle on the running cloudflared.
//
// The second supervised child, and mostly the gateway's machinery used twice:
// its own session, never waited on, stopped by process group, recorded in
// state.json and re-adopted by pid with its argv checked.
type tunnelChild struct {
	pid       int
	kind      string // KindNamed | KindQuick
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
func TunnelLogPath() string { return config.LogPath("tunnel") }

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

	child := &tunnelChild{pid: pid, kind: kind, cfg: cfg}
	if kind == KindNamed {
		child.publicURL = "https://" + cfg.Hostname
		s.logger.Info("tunnel started", "pid", pid, "kind", kind, "url", child.publicURL)
		return child, nil
	}

	child.publicURL = waitForQuickURL(TunnelLogPath(), from, pid)
	if child.publicURL == "" && !pidAlive(pid) {
		// It died rather than went quiet, so there is a cause to report.
		return nil, fmt.Errorf("cloudflared exited before publishing: %s",
			firstLogLine(TunnelLogPath(), from))
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
	for _, line := range strings.Split(readLogFrom(path, from), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l
		}
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
	return &tunnelChild{pid: ts.PID, kind: ts.Kind, publicURL: ts.PublicURL, cfg: ts.Config}
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
