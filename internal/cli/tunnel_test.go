package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetTunnelFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { tunnelUpFlags.hostname, tunnelUpFlags.name, tunnelUpFlags.quick = "", "", false })
	tunnelUpFlags.hostname, tunnelUpFlags.name, tunnelUpFlags.quick = "", "", false
}

// Precedence is the whole of the resolution rule: flags, then config, then a
// prompt, then a quick tunnel. Getting the order wrong means a flag someone
// typed loses to a file they forgot about.
func TestResolveTunnel_Precedence(t *testing.T) {
	configured := &config.Registry{Tunnel: &config.TunnelConfig{Name: "fromfile", Hostname: "fromfile.example.com"}}

	t.Run("flags win over config", func(t *testing.T) {
		resetTunnelFlags(t)
		tunnelUpFlags.hostname = "flag.example.com"

		got, err := resolveTunnel(&cobra.Command{}, config.Source{}, configured)
		require.NoError(t, err)
		assert.Equal(t, "flag.example.com", got.Hostname)
		assert.Equal(t, "flag", got.Name, "the name is filled from the first label")
	})

	t.Run("--quick wins over both", func(t *testing.T) {
		resetTunnelFlags(t)
		tunnelUpFlags.quick = true

		got, err := resolveTunnel(&cobra.Command{}, config.Source{}, configured)
		require.NoError(t, err)
		assert.True(t, got.IsQuick())
	})

	t.Run("--quick with a hostname is a contradiction", func(t *testing.T) {
		resetTunnelFlags(t)
		tunnelUpFlags.quick = true
		tunnelUpFlags.hostname = "x.example.com"

		_, err := resolveTunnel(&cobra.Command{}, config.Source{}, configured)
		assert.ErrorContains(t, err, "--quick takes no")
	})

	t.Run("config when no flags", func(t *testing.T) {
		resetTunnelFlags(t)
		got, err := resolveTunnel(&cobra.Command{}, config.Source{}, configured)
		require.NoError(t, err)
		assert.Equal(t, "fromfile", got.Name)
	})

	// The branch that must never block: an agent or a script gets a quick
	// tunnel and a printed note, never a hung call waiting on stdin.
	t.Run("nothing configured, not a terminal", func(t *testing.T) {
		resetTunnelFlags(t)
		got, err := resolveTunnel(&cobra.Command{}, config.Source{}, &config.Registry{})
		require.NoError(t, err)
		assert.True(t, got.IsQuick())
	})

	t.Run("an empty tunnel block is not configuration", func(t *testing.T) {
		resetTunnelFlags(t)
		got, err := resolveTunnel(&cobra.Command{}, config.Source{}, &config.Registry{Tunnel: &config.TunnelConfig{}})
		require.NoError(t, err)
		assert.True(t, got.IsQuick(), "it falls through rather than validating as half a pair")
	})
}

func TestFirstLabel(t *testing.T) {
	for in, want := range map[string]string{
		"devrun.example.com": "devrun",
		"example.com":        "example",
		"devrun":             "devrun",
		"":                   "",
	} {
		assert.Equalf(t, want, firstLabel(in), "%q", in)
	}
}

func TestPublicHostFor(t *testing.T) {
	templated := ipc.GatewayStatusPayload{PublicHostname: "{service}-devrun.example.com"}
	assert.Equal(t, "web-devrun.example.com", publicHostFor(templated, "web"))

	assert.Empty(t, publicHostFor(ipc.GatewayStatusPayload{}, "web"),
		"no template means services are addressed by path")
}

// With a template each service has a hostname of its own; without one they
// hang off the tunnel's single URL as paths.
func TestServiceURLFor(t *testing.T) {
	running := func(tpl string) ipc.GatewayStatusPayload {
		return ipc.GatewayStatusPayload{
			PublicHostname: tpl,
			Tunnel:         &ipc.TunnelStatusPayload{Running: true, PublicURL: "https://devrun.example.com"},
		}
	}
	assert.Equal(t, "https://web-devrun.example.com/",
		serviceURLFor(running("{service}-devrun.example.com"), "web"))
	assert.Equal(t, "https://devrun.example.com/web/", serviceURLFor(running(""), "web"))

	assert.Equal(t, "(URL unknown)", serviceURLFor(ipc.GatewayStatusPayload{}, "web"),
		"a quick tunnel whose banner could not be read has no base to build on")
}

// A missing DNS record fails at Cloudflare with error 1033 and nothing in
// devrun explains why, so the command devrun will not run for itself is at
// least printed.
func TestWarnMissingDNS_OnlyWhenItCanHelp(t *testing.T) {
	// A quick tunnel owns its hostname; there is no record to create.
	quick := ipc.GatewayStatusPayload{
		Exposed: []string{"web"},
		Tunnel:  &ipc.TunnelStatusPayload{Running: true, Kind: "quick", PublicURL: "https://x.trycloudflare.com"},
	}
	assert.Empty(t, warned(t, quick))
}

// Path mode needs no per-service record, but it does need the tunnel's own:
// it is the only way in, and every service is a path under it. devrun used to
// check the optional names and skip the mandatory one.
func TestWarnMissingDNS_PathModeStillNeedsTheTunnelHostname(t *testing.T) {
	out := warned(t, ipc.GatewayStatusPayload{
		Exposed: []string{"web"},
		Tunnel: &ipc.TunnelStatusPayload{
			Running: true, Kind: "named", Name: "devrun",
			PublicURL: "https://devrun.invalid",
		},
	})
	assert.Contains(t, out, "cloudflared tunnel route dns devrun devrun.invalid")
	assert.NotContains(t, out, "web", "a service addressed by path has no record of its own")
}

// With a template both are needed: one per service, plus the tunnel's own for
// the index page. The tunnel's comes first — it is the one that is forgotten.
func TestWarnMissingDNS_SubdomainModeAlsoNeedsTheTunnelHostname(t *testing.T) {
	out := warned(t, ipc.GatewayStatusPayload{
		Exposed:        []string{"web", "api"},
		PublicHostname: "{service}-devrun.invalid",
		Tunnel: &ipc.TunnelStatusPayload{
			Running: true, Kind: "named", Name: "devrun",
			PublicURL: "https://devrun.invalid",
		},
	})
	assert.Contains(t, out, "3 hostnames do not resolve yet")
	assert.Contains(t, out, "cloudflared tunnel route dns devrun web-devrun.invalid")
	assert.Contains(t, out, "cloudflared tunnel route dns devrun api-devrun.invalid")
	assert.Less(t, strings.Index(out, "dns devrun devrun.invalid"), strings.Index(out, "web-devrun.invalid"))
}

// A template with no {service} in it resolves to the same name for every
// service. That is a misconfiguration, but printing one command four times is
// not how to report it.
func TestWarnMissingDNS_NamesEachHostOnce(t *testing.T) {
	out := warned(t, ipc.GatewayStatusPayload{
		Exposed:        []string{"web", "api"},
		PublicHostname: "devrun.invalid",
		Tunnel: &ipc.TunnelStatusPayload{
			Running: true, Kind: "named", Name: "devrun",
			PublicURL: "https://devrun.invalid",
		},
	})
	assert.Contains(t, out, "1 hostname does not resolve yet")
	assert.Equal(t, 1, strings.Count(out, "route dns"))
}

// The prompt is the one moment devrun knows someone has not published before,
// so it is where `tunnel create` gets named. It names only what is left to do.
func TestSetupSteps(t *testing.T) {
	assert.Equal(t, []string{
		"cloudflared tunnel login",
		"cloudflared tunnel create pimatix",
		"cloudflared tunnel route dns pimatix devrun.thesys.link",
	}, setupSteps(config.TunnelConfig{Name: "pimatix", Hostname: "devrun.thesys.link"}),
		"the name and the hostname are separate arguments, and route takes both")
}

// The same three lines are shown above the question as examples and below it
// with the answers filled in. One function renders both, so they cannot drift
// into teaching one sequence and printing another.
func TestExampleTunnel_RendersTheWholeSequence(t *testing.T) {
	steps := setupSteps(exampleTunnel)
	assert.Len(t, steps, 3)
	assert.Equal(t, "cloudflared tunnel login", steps[0],
		"shown even to an account already logged in: the sequence is the point")
	assert.Contains(t, steps[2], "devrun.example.com")
}

// warned captures what warnMissingDNS printed. The hostnames are all .invalid,
// which no resolver will answer, so every one of them counts as missing.
func warned(t *testing.T, s ipc.GatewayStatusPayload) string {
	t.Helper()
	var buf bytes.Buffer
	printTo(&buf, func() { warnMissingDNS(s, config.TunnelConfig{}) })
	return buf.String()
}

// printTo captures what fn writes to stdout. These commands print there
// directly, as the rest of this package does.
func printTo(w *bytes.Buffer, fn func()) {
	old := os.Stdout
	r, pw, err := os.Pipe()
	if err != nil {
		return
	}
	os.Stdout = pw
	done := make(chan struct{})
	go func() { defer close(done); _, _ = w.ReadFrom(r) }()
	fn()
	_ = pw.Close()
	<-done
	_ = r.Close()
	os.Stdout = old
}

// The point of printing the command is that it can be pasted. With no tunnel
// name there is no command to give, and one with a hole in it is worse than
// none.
func TestWarnMissingDNS_WithoutATunnelName(t *testing.T) {
	s := ipc.GatewayStatusPayload{
		Exposed:        []string{"web"},
		PublicHostname: "{service}-devrun.invalid",
		Tunnel:         &ipc.TunnelStatusPayload{Running: true, Kind: "named"}, // no Name
	}
	var buf bytes.Buffer
	printTo(&buf, func() { warnMissingDNS(s, config.TunnelConfig{}) })

	out := buf.String()
	assert.Contains(t, out, "web-devrun.invalid", "the hostname is still named")
	assert.Contains(t, out, "needs a CNAME")
	assert.NotContains(t, out, "route dns  ", "never a command with a missing argument")
}

// With the name, the command is complete and copy-pasteable.
func TestWarnMissingDNS_PrintsTheCommand(t *testing.T) {
	s := ipc.GatewayStatusPayload{
		Exposed:        []string{"web"},
		PublicHostname: "{service}-devrun.invalid",
		Tunnel:         &ipc.TunnelStatusPayload{Running: true, Kind: "named", Name: "devrun"},
	}
	var buf bytes.Buffer
	printTo(&buf, func() { warnMissingDNS(s, config.TunnelConfig{}) })

	assert.Contains(t, buf.String(), "cloudflared tunnel route dns devrun web-devrun.invalid")
}

// Both ways of asking for a named tunnel must produce the same config.
func TestResolveTunnel_FlagsAndPromptAgreeOnProvider(t *testing.T) {
	resetTunnelFlags(t)
	tunnelUpFlags.hostname = "devrun.example.com"

	got, err := resolveTunnel(&cobra.Command{}, config.Source{}, nil)
	require.NoError(t, err)
	assert.Equal(t, config.ProviderCloudflare, got.Provider)
}
