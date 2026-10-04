package config_test

import (
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTunnelConfig_Defaults(t *testing.T) {
	var absent *config.TunnelConfig
	assert.Equal(t, config.ProviderCloudflare, absent.Defaults().Provider,
		"a nil block is valid and means the default provider")

	kept := (&config.TunnelConfig{Name: "devrun", Hostname: "devrun.example.com"}).Defaults()
	assert.Equal(t, "devrun", kept.Name)
	assert.Equal(t, "devrun.example.com", kept.Hostname)
}

// No name means nothing to run but a throwaway tunnel, which needs no account,
// no DNS and no prior login.
func TestTunnelConfig_IsQuick(t *testing.T) {
	assert.True(t, config.TunnelConfig{}.IsQuick())
	assert.False(t, config.TunnelConfig{Name: "devrun", Hostname: "devrun.example.com"}.IsQuick())
}

// Name and hostname travel together. Conflating them is the mistake this
// design already made once — cloudflared takes a name where a hostname looks
// like it would do — so supplying half is an error rather than a guess.
func TestTunnelConfig_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		in      config.TunnelConfig
		wantErr string
	}{
		"empty is a quick tunnel":  {config.TunnelConfig{}, ""},
		"named":                    {config.TunnelConfig{Name: "devrun", Hostname: "devrun.example.com"}, ""},
		"provider spelled out":     {config.TunnelConfig{Provider: "cloudflare"}, ""},
		"another provider":         {config.TunnelConfig{Provider: "ngrok"}, "only \"cloudflare\" is supported"},
		"hostname without a name":  {config.TunnelConfig{Hostname: "devrun.example.com"}, "needs tunnel.name beside it"},
		"name without a hostname":  {config.TunnelConfig{Name: "devrun"}, "needs tunnel.hostname beside it"},
		"hostname is a URL":        {config.TunnelConfig{Name: "devrun", Hostname: "https://devrun.example.com"}, "not a URL"},
		"hostname carries a port":  {config.TunnelConfig{Name: "devrun", Hostname: "devrun.example.com:443"}, "not a URL"},
		"hostname names no domain": {config.TunnelConfig{Name: "devrun", Hostname: "devrun"}, "names no domain"},
		"unusable tunnel name":     {config.TunnelConfig{Name: "a/b", Hostname: "devrun.example.com"}, "invalid tunnel name"},
	} {
		err := tc.in.Validate()
		if tc.wantErr == "" {
			assert.NoErrorf(t, err, "%s", name)
			continue
		}
		assert.ErrorContainsf(t, err, tc.wantErr, "%s", name)
	}

	var nilBlock *config.TunnelConfig
	assert.NoError(t, nilBlock.Validate(), "absent means never involve cloudflared, not invalid")
}

// The block has to survive the file it is written in, in both config shapes.
func TestTunnelConfig_FromYAML(t *testing.T) {
	const doc = `
version: "1"
services:
  web:
    command: npm run dev
tunnel:
  provider: cloudflare
  name: devrun
  hostname: devrun.example.com
`
	var reg config.Registry
	require.NoError(t, yaml.Unmarshal([]byte(doc), &reg))
	require.NotNil(t, reg.Tunnel)
	assert.Equal(t, "devrun", reg.Tunnel.Name)
	assert.Equal(t, "devrun.example.com", reg.Tunnel.Hostname)
	assert.NoError(t, reg.Tunnel.Validate())

	var proj config.ProjectConfig
	require.NoError(t, yaml.Unmarshal([]byte(doc), &proj))
	require.NotNil(t, proj.Tunnel)
	assert.Equal(t, "devrun", proj.Tunnel.Name)
}

// The tunnel name is often derived from the hostname, so validating the name
// first turns a mistyped hostname into a complaint about a name the user
// never typed.
func TestTunnelConfig_ValidateBlamesTheHostnameFirst(t *testing.T) {
	// What `devrun tunnel up --hostname https://x.example.com` produces:
	// firstLabel gives "https://x" as the name.
	cfg := config.TunnelConfig{Name: "https://x", Hostname: "https://x.example.com"}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel.hostname")
	assert.NotContains(t, err.Error(), "invalid tunnel name",
		"the hostname is what the user typed; the name was derived from it")
}
