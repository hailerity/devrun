package ops

import (
	"net/http/httptest"
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
