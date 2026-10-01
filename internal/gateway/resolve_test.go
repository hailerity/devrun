package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func running(name string, port int) Route {
	return Route{Name: name, State: "running", Port: port}
}

// get builds a request for host+path. An empty host means localhost, which keeps
// the posture local unless a test asks otherwise.
func get(host, path string) *http.Request {
	if host == "" {
		host = "localhost:7788"
	}
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Host = host
	return r
}

func server(t *testing.T, cfg Config, snap Snapshot) *Server {
	t.Helper()
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1:7788"
	}
	s := New(cfg)
	s.SetSnapshot(snap)
	return s
}

func TestResolve_BothUrlShapesAlwaysWork(t *testing.T) {
	// Mode governs what the index advertises, not what is accepted: subdomain
	// routing locally means web.localhost, which curl and some Linux resolvers
	// do not resolve, so the path form has to keep working either way.
	snap := Snapshot{Routes: []Route{running("web", 4200)}}

	for _, mode := range []Mode{Subdomain, Path} {
		s := server(t, Config{Mode: mode}, snap)

		got, outcome := s.Resolve(get("web.localhost:7788", "/"))
		require.Equal(t, OK, outcome, "subdomain under mode %s", mode)
		assert.Equal(t, Target{Service: "web"}, got, "a Host label strips nothing")

		got, outcome = s.Resolve(get("", "/web/assets/app.js"))
		require.Equal(t, OK, outcome, "path under mode %s", mode)
		assert.Equal(t, Target{Service: "web", Prefix: "/web"}, got,
			"the service knows nothing about being mounted under its own name")
	}
}

func TestResolve_Misses(t *testing.T) {
	s := server(t, Config{}, Snapshot{Routes: []Route{
		running("web", 4200),
		{Name: "db", State: "stopped"},
		{Name: "tauri", State: "running", Port: 0}, // running, port not detected
	}})

	for _, tc := range []struct {
		name string
		req  *http.Request
		want Outcome
	}{
		{"bare host is the index, not a service", get("", "/"), NoSuchRoute},
		{"unknown name", get("", "/nope/"), NoSuchRoute},
		{"unknown subdomain", get("nope.localhost", "/"), NoSuchRoute},
		{"not running", get("", "/db/"), NotRunning},
		{"running but no port known", get("", "/tauri/"), NotRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, outcome := s.Resolve(tc.req)
			assert.Equal(t, tc.want, outcome)
		})
	}
}

// The allowlist is a statement about leaving the machine, so it must not bite
// while the request came from this one.
func TestResolve_AllowlistOnlyAppliesWhenPublished(t *testing.T) {
	snap := Snapshot{Routes: []Route{running("web", 4200), running("admin", 9000)}, Exposed: []string{"web"}}

	local := server(t, Config{}, snap)
	_, outcome := local.Resolve(get("", "/admin/"))
	assert.Equal(t, OK, outcome, "on loopback the gateway offers nothing localhost:9000 did not")

	published := server(t, Config{Posture: PostureForced}, snap)
	_, outcome = published.Resolve(get("", "/admin/"))
	assert.Equal(t, NotExposed, outcome)
	_, outcome = published.Resolve(get("", "/web/"))
	assert.Equal(t, OK, outcome)
}

func TestResolve_RoutesTable(t *testing.T) {
	cfg := Config{Rules: map[string]Rule{
		"/":     {Service: "web", Strip: false},
		"/api":  {Service: "api", Strip: false},
		"/docs": {Service: "docs", Strip: true},
	}}
	snap := Snapshot{Routes: []Route{running("web", 4200), running("api", 3000), running("docs", 8080)}}
	s := server(t, cfg, snap)

	for _, tc := range []struct {
		path string
		want Target
		why  string
	}{
		{"/api/users", Target{Service: "api"}, "longest prefix wins over /"},
		{"/api", Target{Service: "api"}, "the prefix itself matches"},
		{"/docs/intro", Target{Service: "docs", Prefix: "/docs"}, "strip true removes the prefix"},
		{"/anything", Target{Service: "web"}, "/ is the catch-all"},
		{"/apiary/x", Target{Service: "web"}, "/api must not match /apiary"},
	} {
		got, outcome := s.Resolve(get("", tc.path))
		require.Equalf(t, OK, outcome, "%s (%s)", tc.path, tc.why)
		assert.Equalf(t, tc.want, got, "%s: %s", tc.path, tc.why)
	}
}

// A rules table wins over the URL shapes, since it was written on purpose — and
// naming a service there is enough to expose it.
func TestResolve_RoutesTableBeatsHostAndImpliesExposure(t *testing.T) {
	s := server(t,
		Config{
			Posture: PostureForced,
			Rules:   map[string]Rule{"/": {Service: "web"}},
		},
		Snapshot{Routes: []Route{running("web", 4200), running("api", 3000)}}, // nothing in Exposed
	)

	got, outcome := s.Resolve(get("api.devrun.example.com", "/"))
	require.Equal(t, OK, outcome)
	assert.Equal(t, "web", got.Service, "the catch-all rule decides, not the Host label")

	_, outcome = s.Resolve(get("", "/api/x"))
	assert.Equal(t, OK, outcome, "no rule matched /api, but / did")
}

func TestPathMatches(t *testing.T) {
	for _, tc := range []struct {
		path, prefix string
		want         bool
	}{
		{"/anything", "/", true},
		{"/api", "/api", true},
		{"/api/users", "/api", true},
		{"/api/users", "api", true},
		{"/apiary", "/api", false},
		{"/", "/api", false},
	} {
		assert.Equalf(t, tc.want, pathMatches(tc.path, tc.prefix), "%s under %s", tc.path, tc.prefix)
	}
}

func TestListing(t *testing.T) {
	snap := Snapshot{
		Routes:  []Route{running("web", 4200), {Name: "db", State: "stopped"}, running("api", 3000)},
		Exposed: []string{"web"},
	}

	local := server(t, Config{}, snap)
	names := func(rs []Route) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			out[i] = r.Name
		}
		return out
	}
	assert.Equal(t, []string{"api", "db", "web"}, names(local.Listing(get("", "/"))),
		"everything running or not, in name order")

	published := server(t, Config{Posture: PostureForced}, snap)
	assert.Equal(t, []string{"web"}, names(published.Listing(get("", "/"))),
		"only what may leave the machine")
}

func TestRoute_Reachable(t *testing.T) {
	assert.True(t, running("web", 4200).Reachable())
	assert.False(t, Route{Name: "web", State: "running"}.Reachable(), "no port, nowhere to proxy")
	assert.False(t, Route{Name: "web", State: "stopped", Port: 4200}.Reachable())
	assert.False(t, Route{Name: "web", State: "crashed", Port: 4200}.Reachable())
}

// pathMatches normalises the prefix but resolveRule used to take Target.Prefix
// from the raw map key, so a rule written "api" instead of "/api" matched the
// request and then failed to strip, forwarding the whole path upstream.
func TestResolve_RuleKeyWithoutLeadingSlashStillStrips(t *testing.T) {
	s := server(t,
		Config{Rules: map[string]Rule{"api": {Service: "api", Strip: true}}},
		Snapshot{Routes: []Route{running("api", 3000)}},
	)

	got, outcome := s.Resolve(get("", "/api/users"))
	require.Equal(t, OK, outcome)
	assert.Equal(t, Target{Service: "api", Prefix: "/api"}, got)
	assert.Equal(t, "/users", stripPrefix("/api/users", got.Prefix),
		"a matched prefix must also be a strippable one")
}

// A routes table is the declared topology. Without this, a rule mounting
// `backend` at /api with Strip:false left the service reachable at a second,
// prefix-stripped /backend/ — bypassing the very rule that was written.
func TestResolve_RoutesTableIsTheOnlyPathTopology(t *testing.T) {
	s := server(t,
		Config{Rules: map[string]Rule{"/api": {Service: "backend", Strip: false}}},
		Snapshot{Routes: []Route{running("backend", 3000)}},
	)

	got, outcome := s.Resolve(get("", "/api/users"))
	require.Equal(t, OK, outcome)
	assert.Equal(t, Target{Service: "backend"}, got, "Strip:false keeps /api on the request")

	_, outcome = s.Resolve(get("", "/backend/users"))
	assert.Equal(t, NoSuchRoute, outcome, "the name-based path must not be a second address")
}

func TestRulePath(t *testing.T) {
	s := server(t, Config{Rules: map[string]Rule{
		"/":       {Service: "web"},
		"/api":    {Service: "backend"},
		"/api/v2": {Service: "backend"},
		"bare":    {Service: "odd"},
	}}, Snapshot{})

	for name, want := range map[string]string{"web": "/", "backend": "/api/", "odd": "/bare/"} {
		got, ok := s.rulePath(name)
		require.Truef(t, ok, "%s should be mounted", name)
		assert.Equalf(t, want, got, "%s: shortest mount wins, normalised", name)
	}

	_, ok := s.rulePath("nothing")
	assert.False(t, ok)
}
