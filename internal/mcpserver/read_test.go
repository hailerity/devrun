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

// An agent that can set a group has to be able to read one, or it cannot match
// an existing group and will invent synonyms of it instead. list_services is
// the tool the server's own instructions point at first.
func TestListServices_ReportsTheGroup(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n" +
		"  web:\n    command: npm start\n    group: frontend\n" +
		"  api:\n    command: go run .\n    group: backend\n" +
		"  legacy:\n    command: ./run.sh\n")

	var out ListOutput
	require.Empty(t, e.call("list_services", map[string]any{"project_dir": dir}, &out))

	got := map[string]string{}
	for _, s := range out.Services {
		got[s.Name] = s.Group
	}
	assert.Equal(t, map[string]string{
		"web":    "frontend",
		"api":    "backend",
		"legacy": "shop", // inherits the project's name
	}, got)
}

// A group and a target are different things, and the same service can carry one
// of each — which is the distinction an agent most needs the list to make.
func TestListServices_GroupAndTargetsAreSeparate(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n" +
		"  web:\n    command: npm start\n    group: frontend\n" +
		"  api:\n    command: go run .\n    group: backend\n" +
		"targets:\n  dev: [web, api]\n")

	var out ListOutput
	require.Empty(t, e.call("list_services", map[string]any{"project_dir": dir}, &out))

	// Compared as a whole rather than asserted inside a switch: a switch with no
	// default passes vacuously if the list comes back empty or the names change,
	// which is a test that cannot fail.
	type gt struct {
		group   string
		targets []string
	}
	got := map[string]gt{}
	for _, s := range out.Services {
		got[s.Name] = gt{s.Group, s.Targets}
	}
	assert.Equal(t, map[string]gt{
		"web": {"frontend", []string{"dev"}},
		"api": {"backend", []string{"dev"}},
	}, got)
}
