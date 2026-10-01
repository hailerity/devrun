// Package gateway serves devrun's services over HTTP — an index at / and a
// reverse proxy for everything below it.
//
// It is deliberately ignorant of how it is reached. It holds a Snapshot of what
// the daemon last told it and answers requests from that; it knows nothing about
// cloudflared, the Unix socket, or who refreshes the snapshot. That keeps the
// routing and access rules testable with httptest alone.
package gateway

import (
	"net/http"
	"sync"
)

// Posture says whether a request must be treated as coming from off this
// machine. It decides whether the allowlist and the auth token apply: a gateway
// reachable only from loopback grants nothing that localhost:<port> did not
// already grant the same caller, so it asks for nothing.
type Posture string

const (
	Local     Posture = "local"
	Published Posture = "published"
)

// Mode is how the index advertises a service's URL. Both shapes are always
// accepted — see Resolve.
type Mode string

const (
	Subdomain Mode = "subdomain"
	Path      Mode = "path"
)

// Auth modes. Auto asks for a token only once the posture is Published.
const (
	AuthAuto   = "auto"
	AuthAlways = "always"
	AuthNone   = "none"
)

// Posture settings. PostureAuto derives it per request; PostureForced is for a
// gateway someone else publishes, where devrun cannot tell from its own state.
const (
	PostureAuto   = "auto"
	PostureForced = "published"
)

// Host header handling for the upstream request.
const (
	HostUpstream = "upstream" // rewrite to localhost:<port>
	HostPreserve = "preserve" // pass the eyeball's Host through
)

// Route is one service as the gateway sees it.
type Route struct {
	Name  string
	State string
	Port  int // 0 when the daemon has not detected one yet
}

// Reachable reports whether a request can actually be proxied to this service.
// A route that is not reachable is still listed on the index, with the reason —
// a link that 503s is worse than a row that explains itself.
func (r Route) Reachable() bool { return r.State == "running" && r.Port > 0 }

// Rule is one entry of an explicit routes table: which service a path prefix
// belongs to, and whether the prefix is the service's or the gateway's.
//
// Strip false is what buys production parity — the backend keeps seeing
// /api/..., so a frontend's relative URLs behave the same in dev, published and
// in production.
type Rule struct {
	Service string
	Strip   bool
}

// Config is the gateway's static configuration, fixed for the life of the
// process. Everything that changes as services come and go lives in Snapshot.
type Config struct {
	Bind       string // "127.0.0.1:7788" — the address actually listened on
	Mode       Mode
	Posture    string // PostureAuto | PostureForced
	Auth       string // AuthAuto | AuthAlways | AuthNone
	HostHeader string // HostUpstream | HostPreserve
	Rules      map[string]Rule
	Token      string
	Version    string
}

// Snapshot is what the gateway last learned from the daemon. It is replaced
// wholesale rather than mutated, so a request always sees one coherent view.
type Snapshot struct {
	Routes    []Route
	Exposed   []string // may leave this machine; applies only when Published
	Tunnelled bool     // a devrun-managed tunnel is running
}

// Server answers requests from a Snapshot it does not own.
type Server struct {
	cfg Config

	mu   sync.RWMutex
	snap Snapshot
}

func New(cfg Config) *Server {
	if cfg.Mode == "" {
		cfg.Mode = Subdomain
	}
	if cfg.Posture == "" {
		cfg.Posture = PostureAuto
	}
	if cfg.Auth == "" {
		cfg.Auth = AuthAuto
	}
	if cfg.HostHeader == "" {
		cfg.HostHeader = HostUpstream
	}
	return &Server{cfg: cfg}
}

// Config returns the static configuration. It is a copy: Rules is shared, and
// callers must not write to it.
func (s *Server) Config() Config { return s.cfg }

// SetSnapshot replaces what the gateway knows. Safe to call from the refresh
// loop while requests are being served.
func (s *Server) SetSnapshot(snap Snapshot) {
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

// Snapshot returns the current view.
func (s *Server) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// route finds a service by name in the current snapshot.
func (s *Server) route(name string) (Route, bool) {
	for _, r := range s.Snapshot().Routes {
		if r.Name == name {
			return r, true
		}
	}
	return Route{}, false
}

// ServeHTTP is filled in by the proxy and index commits; resolution and the
// access rules land first so they can be tested on their own.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
