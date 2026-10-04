package gateway

import "strings"

// servicePlaceholder is what Config.PublicHostname substitutes a service name
// for. Exactly one is allowed, which is what makes the reverse direction a
// plain prefix and suffix strip with nothing to disambiguate.
//
// It is spelled again here rather than imported: this package does not depend
// on internal/config — ops.GatewayServerConfig is what translates between the
// two — so the literal has to exist on both sides of that boundary. Nothing in
// the compiler holds them equal, so TestServicePlaceholderSurvivesConfigToGateway in
// internal/ops does, that being the one package importing both. Were they to
// drift, config would accept a template this package cannot split, and every
// published link would quietly fall back to the prepend.
const servicePlaceholder = "{service}"

// hostTemplate is Config.PublicHostname split around its placeholder. It runs
// in both directions: building the index's links, and recovering the service
// name from the Host of a request that arrives.
type hostTemplate struct{ prefix, suffix string }

// hostTemplate parses the configured template, or reports that there is none.
func (s *Server) hostTemplate() (hostTemplate, bool) {
	before, after, found := strings.Cut(s.cfg.PublicHostname, servicePlaceholder)
	if !found {
		return hostTemplate{}, false
	}
	return hostTemplate{prefix: before, suffix: after}, true
}

// hostFor is the hostname this service is published at.
func (h hostTemplate) hostFor(service string) string {
	return h.prefix + service + h.suffix
}

// serviceIn recovers the service name from a Host this template produced, or
// reports that the Host is not one of ours.
//
// The affixes are compared case-insensitively, since a hostname is, but the
// name is cut from the original so a service whose name has capitals still
// matches the route table. The length check is what rejects the bare domain —
// under "{service}-devrun.example.com", a request to "-devrun.example.com"
// carries both affixes and no service between them.
func (h hostTemplate) serviceIn(reqHost string) (string, bool) {
	host := hostOnly(reqHost)
	if len(host) <= len(h.prefix)+len(h.suffix) {
		return "", false
	}
	lower := strings.ToLower(host)
	if !strings.HasPrefix(lower, strings.ToLower(h.prefix)) {
		return "", false
	}
	if !strings.HasSuffix(lower, strings.ToLower(h.suffix)) {
		return "", false
	}
	return host[len(h.prefix) : len(host)-len(h.suffix)], true
}

// resolveTemplate takes the service from a Host built by the configured
// template. It runs before the first-label rule, which would read
// "web-devrun" out of "web-devrun.example.com" and find no such service.
func (s *Server) resolveTemplate(reqHost string) (Target, bool) {
	h, ok := s.hostTemplate()
	if !ok {
		return Target{}, false
	}
	name, ok := h.serviceIn(reqHost)
	if !ok {
		return Target{}, false
	}
	if _, known := s.route(name); !known {
		return Target{}, false
	}
	return Target{Service: name}, true
}

// pathLinks reports whether the index will address services by path rather
// than by a hostname of their own. It is the one place that decision is made,
// so the links, the per-row hostname and the caveat cannot disagree.
func (s *Server) pathLinks(reqHost string) bool {
	// Asked for, so honoured — ahead of the template, which would otherwise
	// override a setting the user wrote down and say nothing about it. Nothing
	// is lost by obeying: Mode has only ever governed which shape the index
	// advertises, and Resolve still accepts a templated hostname inbound.
	// linkMode is Path for an explicit routes table too, that being a set of
	// paths already.
	if s.linkMode() == Path {
		return true
	}
	// A template carries its own domain, so it works however the gateway was
	// reached — but only once the request is actually coming from off this
	// machine. Browsing at localhost:7788 should link to localhost, not to a
	// public name that only resolves through a tunnel.
	if _, ok := s.hostTemplate(); ok && !hostIsSelf(reqHost, s.cfg.Bind) {
		return false
	}
	return !canSubdomain(reqHost)
}

// serviceHost is the hostname a service is reached at, or "" when it is
// reached by path instead.
func (s *Server) serviceHost(reqHost, name string) string {
	if s.pathLinks(reqHost) {
		return ""
	}
	if h, ok := s.hostTemplate(); ok && !hostIsSelf(reqHost, s.cfg.Bind) {
		return h.hostFor(name)
	}
	return subdomainHost(reqHost, name)
}

// pathExplain says why services are addressed by path, and what to do about
// it when there is something specific to do.
//
// It exists because the commonest way to end up here is not configuring it:
// opening the gateway at an IP address silently selects the one shape that
// breaks a frontend's root-absolute asset URLs. A caveat that does not name
// the cause leaves the reader to guess which of several things went wrong.
func (s *Server) pathExplain(reqHost string) (reason, fix string) {
	// The order matches pathLinks. A configured reason is the reason, even
	// when the request also arrived somewhere that would have forced paths
	// anyway — reporting the accident over the setting would send the reader
	// to change the wrong thing.
	switch {
	case len(s.cfg.Rules) > 0:
		return "an explicit routes table is in use", ""
	case s.cfg.Mode == Path:
		return "mode: path is configured", ""
	case !canSubdomain(reqHost):
		host := hostOnly(reqHost)
		if host == "" {
			return "this request carried no Host header", ""
		}
		return "this gateway was reached at " + host +
				", and a label cannot be put in front of an IP address",
			"Opening it by name instead gives each service its own origin."
	default:
		return "no hostname could be formed for a service", ""
	}
}
