package cli

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
)

func plainLines(st ipc.GatewayStatusPayload) string {
	out := ""
	for _, l := range childInfoLines(st) {
		out += ansi.Strip(l) + "\n"
	}
	return out
}

// `devrun info` is the output people paste into a bug report, and the gateway
// token is a credential that unlocks every published service. It is in the
// payload this renders from, so nothing but this test stops it being printed
// by someone adding a field later.
func TestChildInfo_NeverPrintsTheToken(t *testing.T) {
	const secret = "tok_Sh0uldNeverAppear"
	out := plainLines(ipc.GatewayStatusPayload{
		Running: true, Addr: "127.0.0.1:7788", Mode: "subdomain", Posture: "auto",
		Token:   secret,
		Exposed: []string{"web"},
		Tunnel:  &ipc.TunnelStatusPayload{Running: true, Kind: "named", PublicURL: "https://x.example.com"},
	})
	assert.NotContains(t, out, secret)
	assert.Contains(t, out, "https://x.example.com", "the rest of the payload is rendered")
}

// Both children are reported whether or not they are up: "not running" is an
// answer, and a missing block reads as a command that forgot to look.
func TestChildInfo_ReportsBothEvenWhenDown(t *testing.T) {
	down := plainLines(ipc.GatewayStatusPayload{})
	assert.Contains(t, down, "gateway  not running")
	assert.NotContains(t, down, "tunnel", "no gateway means there is no tunnel over it")

	gatewayOnly := plainLines(ipc.GatewayStatusPayload{
		Running: true, Addr: "127.0.0.1:7788", Mode: "subdomain", Posture: "auto",
	})
	assert.Contains(t, gatewayOnly, "gateway  running")
	assert.Contains(t, gatewayOnly, "tunnel   not running")
}

// A quick tunnel whose banner could not be read is running and unaddressable.
// Printing an empty url row would read as "no tunnel".
func TestChildInfo_SaysWhenTheTunnelURLIsUnknown(t *testing.T) {
	out := plainLines(ipc.GatewayStatusPayload{
		Running: true, Addr: "127.0.0.1:7788",
		Tunnel: &ipc.TunnelStatusPayload{Running: true, Kind: "quick"},
	})
	assert.Contains(t, out, "tunnel   running")
	assert.Contains(t, out, "banner could not be read")
}
