package gateway

import (
	"net"
	"net/http"
	"strings"
)

// Posture reports whether this request must be treated as coming from off the
// machine, which is what turns the allowlist and the token on.
//
// Deriving it from devrun's own state alone — "is a devrun-managed tunnel
// running, is the bind non-loopback" — fails open: `ngrok http 7788` satisfies
// neither, so the gateway would serve every running service unauthenticated
// while ngrok published it. The request's own Host is the third signal. A Host
// the gateway does not recognise as itself arrived through something, and that
// something is not on this machine.
//
// This is not airtight. A proxy configured to rewrite Host to localhost slips
// past it, which is why Config.Posture can force Published outright — the
// heuristic is the floor, not the guarantee.
func (s *Server) Posture(r *http.Request) Posture {
	if s.cfg.Posture == PostureForced {
		return Published
	}
	if s.Snapshot().Tunnelled {
		return Published
	}
	if !bindIsLoopback(s.cfg.Bind) {
		return Published
	}
	if r != nil && !hostIsSelf(r.Host, s.cfg.Bind) {
		return Published
	}
	return Local
}

// NeedsToken reports whether this request must carry the token.
func (s *Server) NeedsToken(r *http.Request) bool {
	switch s.cfg.Auth {
	case AuthNone:
		return false
	case AuthAlways:
		return s.cfg.Token != ""
	default:
		return s.cfg.Token != "" && s.Posture(r) == Published
	}
}

// bindIsLoopback reports whether the listen address reaches only this machine.
// An empty or wildcard host ("", "0.0.0.0", "::") listens everywhere, so it is
// not loopback; an unparseable address is treated as not loopback, because
// guessing wrong in that direction only ever adds protection.
func bindIsLoopback(bind string) bool {
	h := hostOnly(bind)
	if h == "" {
		return false
	}
	ip := net.ParseIP(h)
	if ip == nil {
		// A name rather than an address: only the reserved ones are ours.
		return strings.EqualFold(h, "localhost") || hasLocalhostSuffix(h)
	}
	return ip.IsLoopback()
}

// hostIsSelf reports whether a request's Host names this machine.
//
// `.localhost` counts: RFC 6761 reserves it for loopback and browsers resolve
// `web.localhost` to 127.0.0.1, which is how subdomain routing is reached
// locally. Treating it as foreign would demand a token on a purely local
// gateway.
func hostIsSelf(reqHost, bind string) bool {
	h := hostOnly(reqHost)
	if h == "" {
		// No Host at all (HTTP/1.0). Nothing to judge it by, so assume the
		// safer side.
		return false
	}
	if strings.EqualFold(h, "localhost") || hasLocalhostSuffix(h) {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	if b := hostOnly(bind); b != "" && strings.EqualFold(h, b) {
		return true
	}
	return false
}

func hasLocalhostSuffix(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), ".localhost")
}

// hostOnly strips a port and IPv6 brackets from a host[:port], leaving the host.
func hostOnly(hostport string) string {
	if hostport == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(hostport, "[]")
}
