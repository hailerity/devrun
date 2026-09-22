package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With no daemon running and none startable, no tool may hang: the read tools
// answer from the last saved state and say so, stop succeeds (nothing is
// running), and start fails fast with a reason the agent can act on.
func TestNoDaemon_EveryToolAnswersPromptly(t *testing.T) {
	e := newEnvWithoutDaemon(t)
	e.registry(map[string]string{"web": "sleep 30"}, map[string][]string{"stack": {"web"}})

	timed := func(name string, args map[string]any) (*mcp.CallToolResult, time.Duration) {
		start := time.Now()
		res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err, name)
		return res, time.Since(start)
	}

	var list ListOutput
	require.Empty(t, e.call("list_services", nil, &list))
	assert.False(t, list.DaemonRunning, "the result says the state is not live")
	assert.Equal(t, "stopped", list.Services[0].State)

	var status StatusOutput
	require.Empty(t, e.call("service_status", map[string]any{"name": "web"}, &status))
	assert.Equal(t, "stopped", status.State)

	var stop StopOutput
	require.Empty(t, e.call("stop", map[string]any{"service": "web"}, &stop))
	assert.Equal(t, "stopped", stop.Services[0].State)
	require.Empty(t, e.call("stop", map[string]any{"target": "stack"}, &stop))

	res, took := timed("start", map[string]any{"service": "web"})
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, "could not start daemon")
	assert.Less(t, took, 5*time.Second, "bounded by the daemon launch wait, not the start timeout")
}

// Every out-of-range or malformed input is refused with a message naming the
// problem, before anything is written or started.
func TestInputs_RejectedWithAReason(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "sleep 30"}, map[string][]string{"stack": {"web"}})

	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"logs", map[string]any{"name": "web", "lines": -1}, "lines must be between 1 and 1000"},
		{"logs", map[string]any{"name": "web", "lines": 1001}, "lines must be between 1 and 1000"},
		{"start", map[string]any{"service": "web", "timeout_s": -5}, "timeout_s must be between 1 and 120"},
		{"start", map[string]any{"service": "web", "timeout_s": 121}, "timeout_s must be between 1 and 120"},
		{"start", map[string]any{"target": "../stack"}, "invalid"},
		{"stop", map[string]any{"target": "a b"}, "invalid target name"},
		{"add_to_target", map[string]any{"target": "t", "services": []string{"../web"}}, "invalid service name"},
		{"add_to_target", map[string]any{"target": "/abs", "services": []string{"web"}}, "invalid target name"},
		{"list_services", map[string]any{"project_dir": "."}, "must be an absolute path"},
		{"add_service", map[string]any{"name": "x", "command": "y", "project_dir": "rel"}, "must be an absolute path"},
	}
	for _, c := range cases {
		assert.Contains(t, e.call(c.tool, c.args, nil), c.want, "%s %v", c.tool, c.args)
	}
}
