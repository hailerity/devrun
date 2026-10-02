package config_test

import (
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGatewayConfig_Defaults(t *testing.T) {
	// An absent block and an empty one must behave identically: `devrun gateway
	// up` with no configuration at all is the zero-friction path.
	var absent *config.GatewayConfig
	empty := &config.GatewayConfig{}

	for name, got := range map[string]config.GatewayConfig{
		"absent": absent.Defaults(),
		"empty":  empty.Defaults(),
	} {
		assert.Equal(t, config.DefaultGatewayPort, got.Port, name)
		assert.Equal(t, "127.0.0.1", got.Bind, name)
		assert.Equal(t, config.PostureAuto, got.Posture, name)
		assert.Equal(t, config.ModeSubdomain, got.Mode, name)
		assert.Equal(t, config.AuthAuto, got.Auth, name)
		assert.Equal(t, config.HostUpstream, got.HostHeader, name)
		assert.Equal(t, "127.0.0.1:7788", got.Addr(), name)
	}
}

func TestGatewayConfig_DefaultsKeepWhatWasSet(t *testing.T) {
	g := (&config.GatewayConfig{Port: 9000, Bind: "0.0.0.0", Auth: config.AuthNone}).Defaults()
	assert.Equal(t, 9000, g.Port)
	assert.Equal(t, "0.0.0.0", g.Bind)
	assert.Equal(t, config.AuthNone, g.Auth)
	assert.Equal(t, config.ModeSubdomain, g.Mode, "unset fields still get a default")
	assert.Equal(t, "0.0.0.0:9000", g.Addr())
}

// Both spellings of a route, and the default for the shorthand.
func TestGatewayRoute_UnmarshalsBothSpellings(t *testing.T) {
	var g config.GatewayConfig
	require.NoError(t, yaml.Unmarshal([]byte(`
routes:
  "/": web
  "/api": {service: api, strip: false}
  "/docs": {service: docs}
`), &g))

	assert.Equal(t, config.GatewayRoute{Service: "web", Strip: true}, g.Routes["/"],
		"the shorthand strips, like path routing without a table")
	assert.Equal(t, config.GatewayRoute{Service: "api", Strip: false}, g.Routes["/api"],
		"strip: false is the production-parity opt-in")
	assert.Equal(t, config.GatewayRoute{Service: "docs", Strip: true}, g.Routes["/docs"],
		"an explicit mapping without strip still defaults to stripping")
}

func TestGatewayConfig_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		in      config.GatewayConfig
		wantErr string
	}{
		"ok":                     {config.GatewayConfig{Port: 7788, Bind: "127.0.0.1", Mode: config.ModePath}, ""},
		"empty is fine":          {config.GatewayConfig{}, ""},
		"port range":             {config.GatewayConfig{Port: 70000}, "out of range"},
		"bad bind":               {config.GatewayConfig{Bind: "not-an-ip"}, "not an IP"},
		"localhost bind is fine": {config.GatewayConfig{Bind: "localhost"}, ""},
		// A typo here would otherwise fall back to a weaker setting than asked for.
		"bad posture":  {config.GatewayConfig{Posture: "publshed"}, "gateway.posture"},
		"bad mode":     {config.GatewayConfig{Mode: "subdomian"}, "gateway.mode"},
		"bad auth":     {config.GatewayConfig{Auth: "alwys"}, "gateway.auth"},
		"bad host hdr": {config.GatewayConfig{HostHeader: "rewrite"}, "gateway.host_header"},
		"route without a service": {
			config.GatewayConfig{Routes: map[string]config.GatewayRoute{"/api": {}}}, "names no service"},
		"route path without a slash": {
			config.GatewayConfig{Routes: map[string]config.GatewayRoute{"api": {Service: "api"}}}, "must start with /"},
		"bad exposed name": {config.GatewayConfig{Expose: []string{"../etc"}}, "gateway.expose"},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	var nilCfg *config.GatewayConfig
	assert.NoError(t, nilCfg.Validate())
}

// Naming a service in the routes table is enough to expose it — requiring it in
// two places would be a trap.
func TestGatewayConfig_ExposedSet(t *testing.T) {
	g := config.GatewayConfig{
		Expose: []string{"web", "web"},
		Routes: map[string]config.GatewayRoute{"/api": {Service: "api"}},
	}
	assert.Equal(t, []string{"api", "web"}, g.ExposedSet(), "deduplicated and sorted")
}

func TestGatewayConfig_SetExposed(t *testing.T) {
	g := config.GatewayConfig{Expose: []string{"web"}}

	assert.True(t, g.SetExposed([]string{"api"}, true))
	assert.Equal(t, []string{"api", "web"}, g.Expose)

	assert.False(t, g.SetExposed([]string{"api"}, true), "already exposed, nothing to write")

	assert.True(t, g.SetExposed([]string{"web"}, false))
	assert.Equal(t, []string{"api"}, g.Expose)

	assert.False(t, g.SetExposed([]string{"gone"}, false), "removing what is absent is a no-op")
}

func TestServiceConfig_PortIsValidated(t *testing.T) {
	ok := &config.ServiceConfig{Name: "web", Command: "yarn dev", Port: 4200}
	assert.NoError(t, ok.Validate())

	bad := &config.ServiceConfig{Name: "web", Command: "yarn dev", Port: 70000}
	assert.ErrorContains(t, bad.Validate(), "out of range")

	// 0 means "not declared"; detection stays in charge.
	none := &config.ServiceConfig{Name: "web", Command: "yarn dev"}
	assert.NoError(t, none.Validate())
}

func TestProjectConfig_CarriesPortAndGateway(t *testing.T) {
	var p config.ProjectConfig
	require.NoError(t, yaml.Unmarshal([]byte(`
name: myapp
services:
  web:
    command: yarn dev
    port: 4200
gateway:
  mode: path
  expose: [web]
`), &p))

	require.NotNil(t, p.Gateway)
	assert.Equal(t, config.ModePath, p.Gateway.Mode)
	assert.Equal(t, []string{"web"}, p.Gateway.Expose)

	svcs := p.ToServiceConfigs("/tmp/proj")
	require.Contains(t, svcs, "web")
	assert.Equal(t, 4200, svcs["web"].Port, "a declared port must survive the trip to the daemon")
}
