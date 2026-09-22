//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/mcpserver"
)

func mcpSession(t *testing.T, dir string) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := mcpserver.New("it", mcpserver.Options{Dir: dir}).Connect(context.Background(), st, nil)
	require.NoError(t, err)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "it"}, nil).Connect(context.Background(), ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close(); _ = ss.Wait() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	if res.IsError {
		var msg []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg = append(msg, tc.Text)
			}
		}
		t.Fatalf("%s failed: %s", name, strings.Join(msg, " "))
	}
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, out))
}

// TestMCP_AgentFlow is what an agent does end to end, through the MCP tools
// only: define a service in a project, start it, read its output, stop it.
func TestMCP_AgentFlow(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte("services: {}\n"), 0644))
	cs := mcpSession(t, dir)

	var added mcpserver.AddServiceOutput
	callTool(t, cs, "add_service", map[string]any{
		"name": "api", "command": `echo "api listening on $PORT"; sleep 30`, "env": map[string]any{"PORT": "4321"},
	}, &added)
	assert.Equal(t, "project", added.Scope)

	var started mcpserver.StartOutput
	callTool(t, cs, "start", map[string]any{"service": "api", "timeout_s": 10}, &started)
	require.Equal(t, "running", string(started.Outcome), "%+v", started)
	require.NotNil(t, started.Services[0].PID)

	var logs mcpserver.LogsOutput
	callTool(t, cs, "logs", map[string]any{"name": "api"}, &logs)
	assert.Contains(t, strings.Join(logs.Lines, "\n"), "api listening on 4321", "the env was applied and output captured")

	var list mcpserver.ListOutput
	callTool(t, cs, "list_services", nil, &list)
	require.Len(t, list.Services, 1)
	assert.Equal(t, "running", list.Services[0].State)

	var stopped mcpserver.StopOutput
	callTool(t, cs, "stop", map[string]any{"service": "api"}, &stopped)
	require.Len(t, stopped.Services, 1)
	assert.Contains(t, []string{"stopped", "stopping"}, stopped.Services[0].State)
}

// A service that fails to come up: start reports it, with the reason in the
// log tail, in one call — no follow-up list or logs needed.
func TestMCP_StartReportsACrashWithItsLog(t *testing.T) {
	testEnv(t)
	registerService(t, "broken", "echo 'Error: listen EADDRINUSE :3000'; exit 1", t.TempDir())
	cs := mcpSession(t, t.TempDir())

	var started mcpserver.StartOutput
	callTool(t, cs, "start", map[string]any{"service": "broken"}, &started)
	assert.Equal(t, "crashed", string(started.Outcome))
	s := started.Services[0]
	require.NotNil(t, s.ExitCode)
	assert.Equal(t, 1, *s.ExitCode)
	assert.Contains(t, strings.Join(s.LogTail, "\n"), "EADDRINUSE")
}

// Targets through the tools: group, start together, stop together.
func TestMCP_TargetFlow(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	registerService(t, "web", "sleep 30", dir)
	registerService(t, "worker", "sleep 30", dir)
	cs := mcpSession(t, t.TempDir())

	var grouped mcpserver.AddToTargetOutput
	callTool(t, cs, "add_to_target", map[string]any{"target": "dev", "services": []string{"web", "worker"}}, &grouped)
	assert.Equal(t, []string{"web", "worker"}, grouped.Members)

	var started mcpserver.StartOutput
	callTool(t, cs, "start", map[string]any{"target": "dev", "timeout_s": 10}, &started)
	require.Equal(t, "running", string(started.Outcome), "%+v", started)
	require.Len(t, started.Services, 2)

	var list mcpserver.ListOutput
	callTool(t, cs, "list_services", nil, &list)
	require.Len(t, list.Targets, 1)
	assert.True(t, list.Targets[0].Active)

	var stopped mcpserver.StopOutput
	callTool(t, cs, "stop", map[string]any{"target": "dev"}, &stopped)
	assert.Empty(t, stopped.Note)
	for _, s := range stopped.Services {
		assert.Contains(t, []string{"stopped", "stopping"}, s.State, s.Name)
	}
}
