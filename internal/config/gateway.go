package config

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Gateway posture, auth and Host-header settings. The zero value of each means
// "unset", and Defaults fills it in — so an absent gateway: block and an empty
// one behave identically.
const (
	PostureAuto      = "auto"
	PosturePublished = "published"

	AuthAuto   = "auto"
	AuthAlways = "always"
	AuthNone   = "none"

	ModeSubdomain = "subdomain"
	ModePath      = "path"

	HostUpstream = "upstream"
	HostPreserve = "preserve"
)

// DefaultGatewayPort is where the gateway listens unless told otherwise. 0 in
// config means "pick a free one".
const DefaultGatewayPort = 7788

// GatewayRoute mounts a service at a path. It accepts two spellings in YAML:
//
//	"/api": api                       # shorthand
//	"/api": {service: api, strip: false}
//
// Strip defaults to true, matching what path routing does without a table — the
// service knows nothing about being mounted under a prefix. strip: false is the
// production-parity opt-in: the backend keeps seeing /api/..., so a frontend's
// relative URLs behave the same in dev, published and in production.
type GatewayRoute struct {
	Service string `yaml:"service" json:"service"`
	Strip   bool   `yaml:"strip" json:"strip"`
}

// UnmarshalYAML accepts the shorthand as well as the full mapping.
func (r *GatewayRoute) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		r.Service = value.Value
		r.Strip = true
		return nil
	}
	// An alias type, so decoding the mapping does not call this method again.
	type plain struct {
		Service string `yaml:"service"`
		Strip   *bool  `yaml:"strip"`
	}
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	r.Service = p.Service
	r.Strip = p.Strip == nil || *p.Strip
	return nil
}

// GatewayConfig is the gateway: block of a devrun.yaml or services.yaml.
//
// Every field has a working default, so `devrun gateway up` with no block at all
// serves every running service on 127.0.0.1:7788.
type GatewayConfig struct {
	Port       int                     `yaml:"port,omitempty" json:"port,omitempty"`
	Bind       string                  `yaml:"bind,omitempty" json:"bind,omitempty"`
	Posture    string                  `yaml:"posture,omitempty" json:"posture,omitempty"`
	Mode       string                  `yaml:"mode,omitempty" json:"mode,omitempty"`
	Auth       string                  `yaml:"auth,omitempty" json:"auth,omitempty"`
	HostHeader string                  `yaml:"host_header,omitempty" json:"host_header,omitempty"`
	Expose     []string                `yaml:"expose,omitempty" json:"expose,omitempty"`
	Routes     map[string]GatewayRoute `yaml:"routes,omitempty" json:"routes,omitempty"`
}

// Defaults returns a copy with every unset field filled in. A nil receiver is
// a valid input and yields the all-defaults gateway.
func (g *GatewayConfig) Defaults() GatewayConfig {
	out := GatewayConfig{}
	if g != nil {
		out = *g
	}
	if out.Port == 0 {
		out.Port = DefaultGatewayPort
	}
	if out.Bind == "" {
		out.Bind = "127.0.0.1"
	}
	if out.Posture == "" {
		out.Posture = PostureAuto
	}
	if out.Mode == "" {
		out.Mode = ModeSubdomain
	}
	if out.Auth == "" {
		out.Auth = AuthAuto
	}
	if out.HostHeader == "" {
		out.HostHeader = HostUpstream
	}
	return out
}

// Addr is the address the gateway listens on.
func (g GatewayConfig) Addr() string {
	return net.JoinHostPort(g.Bind, itoa(g.Port))
}

// Validate rejects a gateway block that cannot mean anything, naming the key at
// fault. It is deliberately strict about the enums: a typo in `posture` would
// otherwise silently fall back to a weaker setting than the user asked for.
func (g *GatewayConfig) Validate() error {
	if g == nil {
		return nil
	}
	if g.Port < 0 || g.Port > 65535 {
		return fmt.Errorf("gateway.port %d is out of range", g.Port)
	}
	if g.Bind != "" && net.ParseIP(g.Bind) == nil && !strings.EqualFold(g.Bind, "localhost") {
		return fmt.Errorf("gateway.bind %q is not an IP address", g.Bind)
	}
	for key, allowed := range map[string][]string{
		"posture":     {PostureAuto, PosturePublished},
		"mode":        {ModeSubdomain, ModePath},
		"auth":        {AuthAuto, AuthAlways, AuthNone},
		"host_header": {HostUpstream, HostPreserve},
	} {
		var got string
		switch key {
		case "posture":
			got = g.Posture
		case "mode":
			got = g.Mode
		case "auth":
			got = g.Auth
		case "host_header":
			got = g.HostHeader
		}
		if got == "" || contains(allowed, got) {
			continue
		}
		return fmt.Errorf("gateway.%s %q: want one of %s", key, got, strings.Join(allowed, ", "))
	}
	for path, route := range g.Routes {
		if route.Service == "" {
			return fmt.Errorf("gateway.routes[%q] names no service", path)
		}
		if !strings.HasPrefix(path, "/") {
			// Accepted — matching normalises it — but worth not silently
			// tolerating in a file someone will read back.
			return fmt.Errorf("gateway.routes[%q] must start with /", path)
		}
	}
	for _, name := range g.Expose {
		if err := ValidateName("exposed service", name); err != nil {
			return fmt.Errorf("gateway.expose: %w", err)
		}
	}
	return nil
}

// ExposedSet is the allowlist as a set, sorted for a stable status output.
func (g GatewayConfig) ExposedSet() []string {
	seen := map[string]bool{}
	for _, n := range g.Expose {
		seen[n] = true
	}
	// A service named in the routes table is exposed by that fact: mentioning it
	// there is the intent, and requiring it in two places would be a trap.
	for _, r := range g.Routes {
		seen[r.Service] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SetExposed adds or removes names from the allowlist, reporting whether
// anything changed. Removing a service the routes table mounts is refused by
// the caller, not here — this only edits the list.
func (g *GatewayConfig) SetExposed(names []string, exposed bool) bool {
	has := map[string]bool{}
	for _, n := range g.Expose {
		has[n] = true
	}
	changed := false
	for _, n := range names {
		if has[n] == exposed {
			continue
		}
		has[n] = exposed
		changed = true
	}
	if !changed {
		return false
	}
	out := make([]string, 0, len(has))
	for n, on := range has {
		if on {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	g.Expose = out
	return true
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
