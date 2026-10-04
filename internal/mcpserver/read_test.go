package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A read tool must not hand out the gateway's token. An agent has no use for
// it, and a transcript carrying it would be enough to reach everything
// published.
func TestGatewayInfo_OmitsTheToken(t *testing.T) {
	gw := gatewayInfo(&ipc.GatewayStatusPayload{
		Running: true, Addr: "127.0.0.1:7788", Posture: "published",
		Token: "k_secret", Exposed: []string{"web"},
		Tunnel: &ipc.TunnelStatusPayload{Running: true, Kind: "named", PublicURL: "https://devrun.example.com"},
	})
	require.NotNil(t, gw)

	blob, err := json.Marshal(gw)
	require.NoError(t, err)
	assert.NotContains(t, string(blob), "k_secret")
	assert.NotContains(t, strings.ToLower(string(blob)), "token")

	assert.Equal(t, "published", gw.Posture)
	assert.Equal(t, "https://devrun.example.com", gw.PublicURL)
	assert.Equal(t, "named", gw.TunnelKind)
}

// Absent, not a zero struct: every caller reads the same answer about what
// "no gateway" means.
func TestGatewayInfo_NilWhenNotRunning(t *testing.T) {
	assert.Nil(t, gatewayInfo(nil))
	assert.Nil(t, gatewayInfo(&ipc.GatewayStatusPayload{Running: false}))
}
