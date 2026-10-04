package gateway

import (
	"net/http"
	"sort"
	"strings"
)

// Target is the service a request resolved to, and how much of the path belongs
// to the gateway rather than to the service.
type Target struct {
	Service string
	Prefix  string // path segment to strip before proxying; "" strips nothing
}

// Outcome says why a request did not resolve to something proxyable.
type Outcome int

const (
	OK          Outcome = iota
	NoSuchRoute         // nothing of that name is configured
	NotRunning          // configured, but not up or no port known
	NotExposed          // up, but may not leave this machine
)

// Resolve picks the service a request is for.
//
// Both URL shapes are accepted whatever Config.Mode says — the first Host label,
// then the first path segment. Mode governs only which form the index
// advertises. Accepting both costs nothing (neither reaches a service the other
// could not, and the allowlist applies either way) and avoids a sharp edge:
// subdomain routing locally means web.localhost:7788, which browsers resolve but
// curl and some Linux resolvers do not, so a path URL has to keep working.
//
// An explicit routes table wins over both, since it was written on purpose.
func (s *Server) Resolve(r *http.Request) (Target, Outcome) {
	if t, ok := s.resolveRule(r.URL.Path); ok {
		return s.check(t, r)
	}
	if t, ok := s.resolveTemplate(r.Host); ok {
		return s.check(t, r)
	}
	if t, ok := s.resolveHost(r.Host); ok {
		return s.check(t, r)
	}
	// With an explicit routes table, those paths are the declared topology. A
	// name-based fallback would hand every service a second address that
	// bypasses its rule — including the Strip:false that buys prod parity.
	if len(s.cfg.Rules) == 0 {
		if t, ok := s.resolvePath(r.URL.Path); ok {
			return s.check(t, r)
		}
	}
	return Target{}, NoSuchRoute
}

// check applies the rules that depend on live state rather than on the URL.
func (s *Server) check(t Target, r *http.Request) (Target, Outcome) {
	route, ok := s.route(t.Service)
	if !ok {
		return Target{}, NoSuchRoute
	}
	// The allowlist is a statement about leaving the machine, so it only bites
	// once the request is coming from off it. The target is deliberately dropped
	// here: a withheld service's name must not reach a caller that could put it
	// in an error page.
	if s.Posture(r) == Published && !s.exposed(t.Service) {
		return Target{}, NotExposed
	}
	// NotRunning keeps its target — it passed the allowlist, so naming it in the
	// response tells the reader nothing they were not already entitled to.
	if !route.Reachable() {
		return t, NotRunning
	}
	return t, OK
}

// resolveRule matches the longest configured path prefix, so "/api" wins over
// "/" for /api/users however the map happens to be ordered.
func (s *Server) resolveRule(path string) (Target, bool) {
	if len(s.cfg.Rules) == 0 {
		return Target{}, false
	}
	prefixes := make([]string, 0, len(s.cfg.Rules))
	for p := range s.cfg.Rules {
		prefixes = append(prefixes, p)
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })

	for _, p := range prefixes {
		if !pathMatches(path, p) {
			continue
		}
		rule := s.cfg.Rules[p]
		t := Target{Service: rule.Service}
		// Normalised the same way pathMatches does, so a key written "api"
		// rather than "/api" is stripped as well as matched — otherwise it
		// matches here and then fails to strip, forwarding the whole path.
		if norm := "/" + strings.Trim(p, "/"); rule.Strip && norm != "/" {
			t.Prefix = norm
		}
		return t, true
	}
	return Target{}, false
}

// pathMatches reports whether path falls under the prefix. "/" matches
// everything; "/api" matches /api and /api/... but not /apiary.
func pathMatches(path, prefix string) bool {
	if prefix == "/" || prefix == "" {
		return true
	}
	prefix = "/" + strings.Trim(prefix, "/")
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}

// resolveHost takes the service from the first Host label — web.devrun.example.com.
// A bare host with no label to spare (localhost, an IP, a single-label name) names
// no service.
func (s *Server) resolveHost(reqHost string) (Target, bool) {
	h := hostOnly(reqHost)
	label, rest, found := strings.Cut(h, ".")
	if !found || label == "" || rest == "" {
		return Target{}, false
	}
	// "web.localhost" is a service; "127.0.0.1" is not, and neither is a bare
	// "devrun.example.com" asking for the index.
	if _, ok := s.route(label); !ok {
		return Target{}, false
	}
	return Target{Service: label}, true
}

// resolvePath takes the service from the first path segment and strips it,
// since the service knows nothing about being mounted under its own name.
func (s *Server) resolvePath(path string) (Target, bool) {
	seg := strings.Trim(path, "/")
	if seg == "" {
		return Target{}, false
	}
	if i := strings.Index(seg, "/"); i >= 0 {
		seg = seg[:i]
	}
	if _, ok := s.route(seg); !ok {
		return Target{}, false
	}
	return Target{Service: seg, Prefix: "/" + seg}, true
}

// exposed reports whether a service may leave this machine. Naming a service in
// the routes table counts: mentioning it there is the intent, and requiring it
// in two places would be a trap.
func (s *Server) exposed(name string) bool {
	for _, n := range s.Snapshot().Exposed {
		if n == name {
			return true
		}
	}
	for _, rule := range s.cfg.Rules {
		if rule.Service == name {
			return true
		}
	}
	return false
}

// Listing is what the index shows: the services this request is allowed to see,
// in name order.
func (s *Server) Listing(r *http.Request) []Route {
	snap := s.Snapshot()
	published := s.Posture(r) == Published

	out := make([]Route, 0, len(snap.Routes))
	for _, route := range snap.Routes {
		if published && !s.exposed(route.Name) {
			continue
		}
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// rulePath is the path the routes table mounts a service at, so the index can
// link to the address the user declared rather than inventing /<name>/.
// Shortest wins, ties by name, so the link does not depend on map order.
func (s *Server) rulePath(name string) (string, bool) {
	best, found := "", false
	for p, rule := range s.cfg.Rules {
		if rule.Service != name {
			continue
		}
		norm := "/" + strings.Trim(p, "/")
		if !found || len(norm) < len(best) || (len(norm) == len(best) && norm < best) {
			best, found = norm, true
		}
	}
	if !found {
		return "", false
	}
	if best == "/" {
		return "/", true
	}
	return best + "/", true
}
