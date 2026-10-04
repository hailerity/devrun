package cli

import (
	"bytes"
	"os"
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
	out := func(s ipc.GatewayStatusPayload) string {
		var buf bytes.Buffer
		printTo(&buf, func() { warnMissingDNS(s, config.TunnelConfig{}) })
		return buf.String()
	}

	// A quick tunnel owns its hostname; there is no record to create.
	quick := ipc.GatewayStatusPayload{
		Exposed: []string{"web"},
		Tunnel:  &ipc.TunnelStatusPayload{Running: true, Kind: "quick", PublicURL: "https://x.trycloudflare.com"},
	}
	assert.Empty(t, out(quick))

	// Path mode needs no per-service record either.
	pathMode := ipc.GatewayStatusPayload{
		Exposed: []string{"web"},
		Tunnel:  &ipc.TunnelStatusPayload{Running: true, Kind: "named", Name: "devrun"},
	}
	assert.Empty(t, out(pathMode))
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
