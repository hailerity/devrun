package ops

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// internal/gateway does not import internal/config — GatewayServerConfig is
// what translates between them — so the {service} placeholder is spelled on
// both sides of that boundary with nothing in the compiler holding the two
// equal. This is that something, and it lives here because ops is the one
// package that imports both.
//
// Drift would not fail loudly. config would accept a template the gateway
// cannot split, so the gateway would report no template at all: every
// published link would quietly fall back to the prepend, and no request to a
// templated hostname would resolve. That is the exact shape public_hostname
// exists to avoid, arrived at silently.
//
// The assertion is behavioural rather than a constant comparison, so it also
// covers GatewayServerConfig forgetting to carry the field at all.
func TestServicePlaceholderSurvivesConfigToGateway(t *testing.T) {
	cfg := config.GatewayConfig{PublicHostname: "{service}-devrun.example.com"}
	require.NoError(t, cfg.Validate(), "config accepts this template")

	srv := gateway.New(GatewayServerConfig(cfg.Defaults(), "test"))
	srv.SetSnapshot(gateway.Snapshot{
		Routes:  []gateway.Route{{Name: "web", State: "running", Port: 4200}},
		Exposed: []string{"web"},
	})

	r := httptest.NewRequest("GET", "/assets/app.js", nil)
	r.Host = "web-devrun.example.com"
	target, outcome := srv.Resolve(r)

	require.Equal(t, gateway.OK, outcome,
		"the gateway must split the same template config just validated")
	assert.Equal(t, "web", target.Service)
}

// config.GatewayConfig.LocalLinksByPath decides the shape of the URLs devrun
// prints — `gateway status`, `list`, the TUI's details pane. The gateway's own
// pathLinks decides the shape of the links on its index page. They are in
// packages that cannot import each other, so nothing in the compiler holds
// them equal.
//
// Drift would not fail loudly. devrun would print a subdomain URL for a
// gateway serving paths, or a path URL for one serving subdomains — both
// reach the service, so nothing errors; the second just arrives at a page
// whose root-absolute assets 404, which is the failure public_hostname and
// mode exist to avoid.
//
// Asserted through the rendered index, since pathLinks is unexported: a
// subdomain link carries the service's own host, a path link does not.
func TestLocalLinksMatchTheGatewaysOwnChoice(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.GatewayConfig
		host string
	}{
		{"subdomain at a name", config.GatewayConfig{}, "localhost:7788"},
		{"subdomain at an IP", config.GatewayConfig{}, "127.0.0.1:7788"},
		{"mode path", config.GatewayConfig{Mode: config.ModePath}, "localhost:7788"},
		{"routes table", config.GatewayConfig{
			Routes: map[string]config.GatewayRoute{"/admin": {Service: "web"}},
		}, "localhost:7788"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			full := tc.cfg.Defaults()
			srv := gateway.New(GatewayServerConfig(full, "test"))
			srv.SetSnapshot(gateway.Snapshot{
				Routes: []gateway.Route{{Name: "web", State: "running", Port: 4200}},
			})

			r := httptest.NewRequest("GET", "/", nil)
			r.Host = tc.host
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)

			servedByPath := !strings.Contains(w.Body.String(), "web."+tc.host)
			assert.Equal(t, servedByPath, full.LocalLinksByPath(tc.host),
				"the URL devrun prints must have the shape the index links to")
		})
	}
}

// Starting a tunnel makes the gateway withhold a non-exposed service from
// requests on this machine too — Posture checks Snapshot().Tunnelled before
// it looks at the request's Host. The daemon has to know that to decide
// whether it has a local URL to offer, and it cannot ask the gateway, so
// config.GatewayConfig.LocalAllowlistApplies says it a second time.
//
// Drift would hand out a local URL for a service the gateway 404s, which is
// the worse direction: a link that looks fine and is not.
func TestLocalAllowlistMatchesTheGatewaysPosture(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cfg       config.GatewayConfig
		tunnelled bool
	}{
		{"plain local gateway", config.GatewayConfig{}, false},
		{"tunnel running", config.GatewayConfig{}, true},
		{"posture published", config.GatewayConfig{Posture: config.PosturePublished}, false},
		{"bound to the LAN", config.GatewayConfig{Bind: "0.0.0.0"}, false},
		{"bound to the LAN with a tunnel", config.GatewayConfig{Bind: "192.168.1.8"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			full := tc.cfg.Defaults()
			srv := gateway.New(GatewayServerConfig(full, "test"))
			srv.SetSnapshot(gateway.Snapshot{Tunnelled: tc.tunnelled})

			r := httptest.NewRequest("GET", "/", nil)
			r.Host = "localhost:7788"
			assert.Equal(t, srv.Posture(r) == gateway.Published,
				full.LocalAllowlistApplies(tc.tunnelled),
				"a local URL may only be offered when the gateway would serve it")
		})
	}
}
