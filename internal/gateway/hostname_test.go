package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three shapes a published hostname can take, and what each costs, are the
// whole point of the template: a Cloudflare Universal SSL certificate covers
// one wildcard level, so the nested shape needs a paid certificate and the
// other two do not.
func TestHostTemplate_RoundTrip(t *testing.T) {
	for name, tc := range map[string]struct {
		template, service, host string
	}{
		"namespaced": {"{service}-devrun.example.com", "web", "web-devrun.example.com"},
		"flat":       {"{service}.example.com", "web", "web.example.com"},
		"nested":     {"{service}.devrun.example.com", "web", "web.devrun.example.com"},
		// Service names carry hyphens of their own; the suffix is anchored at
		// the end, so there is nothing to confuse.
		"hyphenated service": {"{service}-devrun.example.com", "shop-web", "shop-web-devrun.example.com"},
		// And a name that ends in the suffix's own word still round-trips.
		"service named like the suffix": {"{service}-devrun.example.com", "foo-devrun", "foo-devrun-devrun.example.com"},
	} {
		s := server(t, Config{PublicHostname: tc.template}, Snapshot{})
		h, ok := s.hostTemplate()
		require.Truef(t, ok, "%s: template did not parse", name)

		assert.Equalf(t, tc.host, h.hostFor(tc.service), "%s: outbound", name)

		got, ok := h.serviceIn(tc.host)
		require.Truef(t, ok, "%s: inbound did not match", name)
		assert.Equalf(t, tc.service, got, "%s: inbound", name)
	}
}

func TestHostTemplate_RejectsWhatItDidNotProduce(t *testing.T) {
	s := server(t, Config{PublicHostname: "{service}-devrun.example.com"}, Snapshot{})
	h, ok := s.hostTemplate()
	require.True(t, ok)

	for _, host := range []string{
		"-devrun.example.com",       // both affixes, no service between them
		"devrun.example.com",        // the index's own hostname
		"web-devrun.example.net",    // another domain
		"web.example.com",           // the suffix is not there
		"example.com",               // shorter than the affixes
		"",                          // no Host at all
		"web-devrun.example.com.ev", // suffix must be anchored at the end
	} {
		_, ok := h.serviceIn(host)
		assert.Falsef(t, ok, "%q should not name a service", host)
	}
}

// Hostnames are case-insensitive, but a service name is matched against the
// route table as written.
func TestHostTemplate_MatchesHostCaseInsensitively(t *testing.T) {
	s := server(t, Config{PublicHostname: "{service}-devrun.Example.COM"}, Snapshot{})
	h, _ := s.hostTemplate()

	got, ok := h.serviceIn("web-DEVRUN.example.com:443")
	require.True(t, ok, "the domain's case must not matter, nor the port")
	assert.Equal(t, "web", got)
}

// The reason the template is consulted before the first-label rule: that rule
// would read "web-devrun" out of the Host and find no such service.
func TestResolve_TemplateBeatsTheFirstLabelRule(t *testing.T) {
	// Exposed, because a Host the gateway does not recognise as itself means
	// the request came from off the machine: the allowlist applies.
	s := server(t, Config{PublicHostname: "{service}-devrun.example.com"},
		Snapshot{Routes: []Route{running("web", 4200)}, Exposed: []string{"web"}})

	target, outcome := s.Resolve(get("web-devrun.example.com", "/assets/app.js"))
	require.Equal(t, OK, outcome)
	assert.Equal(t, "web", target.Service)
	assert.Empty(t, target.Prefix, "a hostname is the whole address; nothing is stripped")
}

// The gateway's own hostname must stay the index, not become a 404.
func TestResolve_TheBareDomainIsStillTheIndex(t *testing.T) {
	s := server(t, Config{PublicHostname: "{service}-devrun.example.com"},
		Snapshot{Routes: []Route{running("web", 4200)}})

	_, outcome := s.Resolve(get("devrun.example.com", "/"))
	assert.Equal(t, NoSuchRoute, outcome, "which ServeHTTP renders as the index")
}

// Both URL shapes stay acceptable, as they already did: a template adds an
// address, it does not remove the path one.
func TestResolve_PathStillWorksAlongsideATemplate(t *testing.T) {
	s := server(t, Config{PublicHostname: "{service}-devrun.example.com"},
		Snapshot{Routes: []Route{running("web", 4200)}, Exposed: []string{"web"}})

	target, outcome := s.Resolve(get("devrun.example.com", "/web/assets/app.js"))
	require.Equal(t, OK, outcome)
	assert.Equal(t, "web", target.Service)
	assert.Equal(t, "/web", target.Prefix)
}

// Locally the template must stay out of the way: browsing at localhost should
// link to localhost, not to a public name that only resolves through a tunnel.
func TestServiceHost_TemplateOnlyAppliesOnceOffThisMachine(t *testing.T) {
	cfg := Config{Bind: "127.0.0.1:7788", PublicHostname: "{service}-devrun.example.com"}
	s := server(t, cfg, Snapshot{Routes: []Route{running("web", 4200)}})

	assert.Equal(t, "web.localhost:7788", s.serviceHost("localhost:7788", "web"),
		"reached at itself: the prepend, which is what *.localhost needs")
	assert.Equal(t, "web-devrun.example.com", s.serviceHost("devrun.example.com", "web"),
		"reached from off the machine: the template")
}

// Unset, nothing about today's behaviour changes.
func TestServiceHost_WithoutATemplate(t *testing.T) {
	s := server(t, Config{Bind: "127.0.0.1:7788"}, Snapshot{Routes: []Route{running("web", 4200)}})

	assert.Equal(t, "web.localhost:7788", s.serviceHost("localhost:7788", "web"))
	// A label in front of an IP resolves nowhere, so there is no hostname.
	assert.Empty(t, s.serviceHost("127.0.0.1:7788", "web"))
}

// A template gives every service a hostname, so an IP-reached gateway is no
// longer forced into path links — that was only ever true of the prepend.
func TestIndex_TemplateLinksSurviveBeingReachedByIP(t *testing.T) {
	cfg := Config{Bind: "0.0.0.0:7788", PublicHostname: "{service}-devrun.example.com"}
	s := server(t, cfg, Snapshot{Routes: []Route{running("web", 4200)}, Exposed: []string{"web"}})

	out := body(t, s, get("203.0.113.9:7788", "/"))
	assert.Contains(t, out, "web-devrun.example.com")
	assert.NotContains(t, out, `href="/web/"`, "a path link would be the degraded shape")
}

func TestIndex_LinksAndRowsAgreeOnTheHostname(t *testing.T) {
	cfg := Config{Bind: "127.0.0.1:7788", PublicHostname: "{service}-devrun.example.com"}
	s := server(t, cfg, Snapshot{Routes: []Route{running("web", 4200)}, Exposed: []string{"web"}})

	out := body(t, s, get("devrun.example.com", "/"))
	assert.Contains(t, out, `href="https://web-devrun.example.com/`,
		"published, so https — the scheme the tunnel terminates")
	assert.Contains(t, out, ">web-devrun.example.com<", "the row names the same host it links to")
}

// The commonest way into path mode is not configuring it — opening the gateway
// at an IP silently selects the one shape that breaks root-absolute asset
// URLs. A caveat that does not name the cause leaves the reader guessing which
// of several things went wrong.
func TestPathExplain(t *testing.T) {
	t.Run("reached by IP", func(t *testing.T) {
		s := server(t, Config{Mode: Subdomain}, Snapshot{})
		reason, fix := s.pathExplain("127.0.0.1:7788")
		assert.Contains(t, reason, "reached at 127.0.0.1")
		assert.Contains(t, reason, "cannot be put in front of an IP")
		assert.NotEmpty(t, fix, "this one has a specific remedy: open it by name")
	})

	t.Run("configured", func(t *testing.T) {
		s := server(t, Config{Mode: Path}, Snapshot{})
		reason, _ := s.pathExplain("localhost:7788")
		assert.Contains(t, reason, "mode: path")
	})

	t.Run("routes table", func(t *testing.T) {
		s := server(t, Config{Rules: map[string]Rule{"/": {Service: "web"}}}, Snapshot{})
		reason, _ := s.pathExplain("localhost:7788")
		assert.Contains(t, reason, "routes table")
	})

	t.Run("no Host at all", func(t *testing.T) {
		s := server(t, Config{Mode: Subdomain}, Snapshot{})
		reason, _ := s.pathExplain("")
		assert.Contains(t, reason, "no Host header")
	})
}

// The caveat must name a fix that exists. "Set a base path" named none, which
// is how a reader ends up debugging the proxy instead of their mount point.
func TestIndex_CaveatNamesTheRoutesFix(t *testing.T) {
	s := server(t, Config{Mode: Path}, Snapshot{Routes: []Route{running("web", 4200)}})

	out := body(t, s, get("localhost:7788", "/"))
	assert.Contains(t, out, "/@vite/client", "the symptom, so it is searchable")
	assert.Contains(t, out, `routes: {"/": name}`, "the config that fixes it")
}

// And no caveat at all when services have hostnames of their own.
func TestIndex_NoCaveatWithATemplate(t *testing.T) {
	cfg := Config{Bind: "127.0.0.1:7788", PublicHostname: "{service}-devrun.example.com"}
	s := server(t, cfg, Snapshot{Routes: []Route{running("web", 4200)}, Exposed: []string{"web"}})

	out := body(t, s, get("devrun.example.com", "/"))
	assert.NotContains(t, out, "addressed by path")
}

// A setting the user wrote down must not be silently overridden. Before this,
// a template plus mode: path gave hostname links off-machine and said nothing,
// which is the failure Validate is otherwise careful to avoid — its comment
// warns that a typo must not "fall back to a weaker setting than the user
// asked for".
func TestPathLinks_ConfiguredModeBeatsTheTemplate(t *testing.T) {
	cfg := Config{Bind: "127.0.0.1:7788", Mode: Path, PublicHostname: "{service}-devrun.example.com"}
	s := server(t, cfg, Snapshot{Routes: []Route{running("web", 4200)}, Exposed: []string{"web"}})

	assert.True(t, s.pathLinks("devrun.example.com"), "mode: path was asked for")
	assert.Empty(t, s.serviceHost("devrun.example.com", "web"), "so there is no hostname to show")

	out := body(t, s, get("devrun.example.com", "/"))
	assert.Contains(t, out, `href="/web/"`)
	assert.NotContains(t, out, "web-devrun.example.com", "the template must not appear in a link")
	assert.Contains(t, out, "mode: path is configured", "and the caveat names the real reason")
}

// Honouring the mode costs nothing inbound: Mode has only ever governed which
// shape the index advertises, and both still resolve.
func TestResolve_TemplateStillRoutesUnderModePath(t *testing.T) {
	cfg := Config{Bind: "127.0.0.1:7788", Mode: Path, PublicHostname: "{service}-devrun.example.com"}
	s := server(t, cfg, Snapshot{Routes: []Route{running("web", 4200)}, Exposed: []string{"web"}})

	target, outcome := s.Resolve(get("web-devrun.example.com", "/assets/app.js"))
	require.Equal(t, OK, outcome, "a templated hostname still reaches its service")
	assert.Equal(t, "web", target.Service)
}

// A configured reason outranks an accidental one: told it was reached at an IP,
// a reader would go and change the URL rather than the setting that decided it.
func TestPathExplain_ReportsTheSettingOverTheAccident(t *testing.T) {
	s := server(t, Config{Mode: Path}, Snapshot{})
	reason, _ := s.pathExplain("127.0.0.1:7788")
	assert.Contains(t, reason, "mode: path is configured")

	table := server(t, Config{Mode: Path, Rules: map[string]Rule{"/": {Service: "web"}}}, Snapshot{})
	reason, _ = table.pathExplain("127.0.0.1:7788")
	assert.Contains(t, reason, "routes table", "the table is more specific still")
}
