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

// DefaultGatewayPort is where the gateway listens when the config says nothing.
// A stable default matters: it is what makes a shared link keep working across
// restarts, so it is the behaviour of an absent key rather than of port 0.
const DefaultGatewayPort = 7788

// GatewayPort is a port literal as GatewayConfig.Port wants it. `port: 0` and
// an absent `port:` mean different things, so the field is a pointer and a
// literal needs somewhere to live.
func GatewayPort(n int) *int { return &n }

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
	// Port distinguishes three cases, which an int could not: absent is
	// DefaultGatewayPort, 0 is "any free port, the kernel picks", and anything
	// else is itself. Port 0 is how you run two projects at once without
	// choosing numbers by hand.
	Port       *int                    `yaml:"port,omitempty" json:"port,omitempty"`
	Bind       string                  `yaml:"bind,omitempty" json:"bind,omitempty"`
	Posture    string                  `yaml:"posture,omitempty" json:"posture,omitempty"`
	Mode       string                  `yaml:"mode,omitempty" json:"mode,omitempty"`
	Auth       string                  `yaml:"auth,omitempty" json:"auth,omitempty"`
	HostHeader string                  `yaml:"host_header,omitempty" json:"host_header,omitempty"`
	// PublicHostname is how a published service is addressed, as a template
	// with one {service} placeholder — "{service}-devrun.example.com". Unset,
	// the gateway puts the service's name in front of whatever host it was
	// reached on, which is what *.localhost needs locally.
	//
	// It exists because that prepend cannot be used when published. A
	// Cloudflare Universal SSL certificate covers the apex and one wildcard
	// level, so reaching the gateway at devrun.example.com and linking to
	// web.devrun.example.com produces a name no certificate covers — the TLS
	// handshake is refused outright, not merely distrusted. A template lets
	// the service names live wherever the certificate reaches.
	PublicHostname string `yaml:"public_hostname,omitempty" json:"public_hostname,omitempty"`
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
	// Always a fresh pointer, so a caller's config cannot be changed through the
	// copy this returns.
	port := DefaultGatewayPort
	if out.Port != nil {
		port = *out.Port
	}
	out.Port = &port
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

// Addr is the address the gateway listens on. Call it on the result of
// Defaults, which is what guarantees Port is set.
func (g GatewayConfig) Addr() string {
	port := DefaultGatewayPort
	if g.Port != nil {
		port = *g.Port
	}
	return net.JoinHostPort(g.Bind, itoa(port))
}

// Validate rejects a gateway block that cannot mean anything, naming the key at
// fault. It is deliberately strict about the enums: a typo in `posture` would
// otherwise silently fall back to a weaker setting than the user asked for.
func (g *GatewayConfig) Validate() error {
	if g == nil {
		return nil
	}
	if g.Port != nil && (*g.Port < 0 || *g.Port > 65535) {
		return fmt.Errorf("gateway.port %d is out of range", *g.Port)
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
	return validateHostnameTemplate(g.PublicHostname)
}

// ServicePlaceholder is what gateway.public_hostname substitutes a service name
// for. Exactly one, so recovering the name from a request's Host is a prefix
// and suffix strip with nothing to disambiguate.
const ServicePlaceholder = "{service}"

// validateHostnameTemplate rejects a template that cannot produce a hostname.
// Strictly, because the failure it prevents is remote: a template with a scheme
// or a port in it yields links that are wrong only once published, by which
// point the person debugging them is not at this machine.
func validateHostnameTemplate(t string) error {
	if t == "" {
		return nil
	}
	switch strings.Count(t, ServicePlaceholder) {
	case 1:
	case 0:
		return fmt.Errorf("gateway.public_hostname %q has no %s — every service would share one hostname", t, ServicePlaceholder)
	default:
		return fmt.Errorf("gateway.public_hostname %q has more than one %s", t, ServicePlaceholder)
	}
	if i := strings.IndexAny(t, "/: \t"); i >= 0 {
		return fmt.Errorf("gateway.public_hostname %q is a hostname, not a URL: remove %q", t, string(t[i]))
	}
	// Without a dot outside the placeholder there is no domain, only a label —
	// which resolves nowhere and silently produces links that cannot work.
	if !strings.Contains(strings.ReplaceAll(t, ServicePlaceholder, ""), ".") {
		return fmt.Errorf("gateway.public_hostname %q names no domain", t)
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

// SaveGatewayExpose adds or removes services from the gateway allowlist in
// whichever config src points at — the project devrun.yaml when src.IsLocal(),
// otherwise the global registry. It mirrors SaveTargetEdit: an allowlist is a
// named set of services, and `gateway expose` is as much a config edit as
// `target add` is.
//
// Persisting matters beyond convenience. The allowlist is what may leave the
// machine; if it lived only in the running gateway, the next `gateway up` would
// read the file, see something different, and silently restart without it.
func SaveGatewayExpose(src Source, names []string, exposed bool) error {
	if src.IsLocal() {
		proj, err := LoadProject(src.Dir)
		if err != nil {
			return err
		}
		if proj == nil {
			return fmt.Errorf("no %s in %s", ProjectFileName, src.Dir)
		}
		if proj.Gateway == nil {
			proj.Gateway = &GatewayConfig{}
		}
		if !proj.Gateway.SetExposed(names, exposed) {
			return nil
		}
		return SaveProject(src.Dir, proj)
	}

	path := RegistryPath()
	reg, err := LoadRegistry(path)
	if err != nil {
		return err
	}
	if reg.Gateway == nil {
		reg.Gateway = &GatewayConfig{}
	}
	if !reg.Gateway.SetExposed(names, exposed) {
		return nil
	}
	if reg.Version == "" {
		reg.Version = "1"
	}
	return SaveRegistry(path, reg)
}
