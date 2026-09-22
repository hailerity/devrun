package mcpserver

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
)

// env is a sandboxed devrun: its own XDG dirs, a daemon running in-process on
// a socket there, and an MCP client session connected to a fresh server over
// the in-memory transport.
type env struct {
	t    *testing.T
	root string // sandbox root; also a directory with no devrun.yaml
	cs   *mcp.ClientSession
}

func newEnv(t *testing.T) *env {
	t.Helper()
	// Short path: a Unix socket path must fit in ~104 bytes on macOS, and
	// t.TempDir embeds the whole test name.
	root, err := os.MkdirTemp("", "mcp-")
	require.NoError(t, err)
	root, _ = filepath.EvalSymlinks(root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_DATA_HOME", root)
	// Never let ops fall back to spawning the test binary as a daemon.
	t.Setenv("DEVRUN_DAEMON_BIN", "/nonexistent/devrun")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.RunWithContext(ctx, config.SocketPath()) }()
	for i := 0; i < 150; i++ {
		if c, err := net.Dial("unix", config.SocketPath()); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	st, ct := mcp.NewInMemoryTransports()
	srv := New("test", Options{Dir: root})
	ss, err := srv.Connect(context.Background(), st, nil)
	require.NoError(t, err)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(context.Background(), ct, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cs.Close()
		_ = ss.Wait()
		cancel()
		<-done
		_ = os.RemoveAll(root)
	})
	return &env{t: t, root: root, cs: cs}
}

// call invokes a tool and decodes its structured result into out. It returns
// the tool-error text when the call failed as a tool error ("" otherwise).
func (e *env) call(name string, args map[string]any, out any) string {
	e.t.Helper()
	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(e.t, err, "protocol-level error calling %s", name)
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String()
	}
	if out != nil {
		raw, err := json.Marshal(res.StructuredContent)
		require.NoError(e.t, err)
		require.NoError(e.t, json.Unmarshal(raw, out), "decode %s result: %s", name, raw)
	}
	return ""
}

func (e *env) registry(services map[string]string, targets map[string][]string) {
	e.t.Helper()
	reg := &config.Registry{Version: "1", Services: map[string]*config.ServiceConfig{}, Targets: targets}
	for name, cmd := range services {
		reg.Services[name] = &config.ServiceConfig{Name: name, Command: cmd, CWD: e.root, Env: map[string]string{"SECRET_TOKEN": "hunter2", "PORT": "8080"}}
	}
	require.NoError(e.t, config.SaveRegistry(config.RegistryPath(), reg))
}

func (e *env) project(yaml string) string {
	e.t.Helper()
	dir := filepath.Join(e.root, "proj")
	require.NoError(e.t, os.MkdirAll(dir, 0755))
	require.NoError(e.t, os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte(yaml), 0644))
	return dir
}

func TestTools_ListedWithSchemasAndAnnotations(t *testing.T) {
	e := newEnv(t)
	res, err := e.cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"list_services", "service_status", "logs"} {
		tool := byName[name]
		require.NotNil(t, tool, name)
		assert.NotEmpty(t, tool.Description, name)
		require.NotNil(t, tool.Annotations, name)
		assert.True(t, tool.Annotations.ReadOnlyHint, "%s is read-only", name)

		schema, _ := json.Marshal(tool.InputSchema)
		assert.Contains(t, string(schema), `"project_dir"`, "%s takes project_dir", name)
		assert.NotNil(t, tool.OutputSchema, "%s declares its output", name)
	}

	logs, _ := json.Marshal(byName["logs"].InputSchema)
	var s struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(logs, &s))
	assert.Equal(t, []string{"name"}, s.Required, "only name is required; project_dir, lines and grep are optional")
}

func TestListServices_ReportsStateTargetsAndSource(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "yarn dev", "api": "go run ."}, map[string][]string{"stack": {"web", "api"}})

	var out ListOutput
	require.Empty(t, e.call("list_services", nil, &out))
	assert.Equal(t, "global", out.Scope)
	assert.Equal(t, config.RegistryPath(), out.Source)
	assert.True(t, out.DaemonRunning)
	require.Len(t, out.Services, 2)
	assert.Equal(t, "api", out.Services[0].Name)
	assert.Equal(t, "stopped", out.Services[0].State)
	assert.Equal(t, []string{"stack"}, out.Services[0].Targets)
	require.Len(t, out.Targets, 1)
	assert.Equal(t, TargetInfo{Name: "stack", Members: []string{"web", "api"}}, out.Targets[0])
}

// project_dir picks the config per call; with none, the server's own directory.
func TestListServices_ProjectDirSelectsTheConfig(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"global-svc": "x"}, nil)
	dir := e.project("services:\n  app:\n    command: npm start\n")

	var out ListOutput
	require.Empty(t, e.call("list_services", map[string]any{"project_dir": dir}, &out))
	assert.Equal(t, "project", out.Scope)
	assert.Equal(t, filepath.Join(dir, config.ProjectFileName), out.Source)
	require.Len(t, out.Services, 1)
	assert.Equal(t, "app", out.Services[0].Name)

	require.Empty(t, e.call("list_services", map[string]any{"project_dir": dir, "global": true}, &out))
	assert.Equal(t, "global", out.Scope, "global overrides the project file")
	assert.Equal(t, "global-svc", out.Services[0].Name)

	require.Empty(t, e.call("list_services", nil, &out))
	assert.Equal(t, "global", out.Scope, "the server's own directory has no devrun.yaml")
}

// The config is read on every call, so an edit made meanwhile — by the TUI,
// the CLI or a person — is seen at once.
func TestListServices_SeesEditsBetweenCalls(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"one": "x"}, nil)
	var out ListOutput
	require.Empty(t, e.call("list_services", nil, &out))
	require.Len(t, out.Services, 1)

	e.registry(map[string]string{"one": "x", "two": "y"}, nil)
	require.Empty(t, e.call("list_services", nil, &out))
	assert.Len(t, out.Services, 2)
}

func TestServiceStatus_ReturnsDefinitionButNotEnvValues(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"api": "go run ./cmd/api"}, nil)

	var out StatusOutput
	require.Empty(t, e.call("service_status", map[string]any{"name": "api"}, &out))
	assert.Equal(t, "go run ./cmd/api", out.Command)
	assert.Equal(t, e.root, out.CWD)
	assert.Equal(t, []string{"PORT", "SECRET_TOKEN"}, out.EnvKeys)
	assert.Equal(t, config.LogPath("api"), out.LogPath)

	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "service_status", Arguments: map[string]any{"name": "api"}})
	require.NoError(t, err)
	raw, _ := json.Marshal(res)
	assert.NotContains(t, string(raw), "hunter2", "environment values never reach the agent")
}

func TestServiceStatus_UnknownServiceNamesWhatExists(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "x", "api": "y"}, nil)
	msg := e.call("service_status", map[string]any{"name": "wbe"}, nil)
	assert.Contains(t, msg, `service "wbe" not found in `+config.RegistryPath()+" (global scope)")
	assert.Contains(t, msg, "defined services: [api web]")
	assert.Contains(t, msg, "pass project_dir")
}

func TestLogs_BoundedPlainAndFiltered(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"api": "x"}, nil)
	require.NoError(t, os.MkdirAll(filepath.Dir(config.LogPath("api")), 0755))
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("\x1b[32mINFO\x1b[0m tick\n")
	}
	b.WriteString("\x1b[31mERROR\x1b[0m redis refused\r\n")
	require.NoError(t, os.WriteFile(config.LogPath("api"), []byte(b.String()), 0644))

	var out LogsOutput
	require.Empty(t, e.call("logs", map[string]any{"name": "api"}, &out))
	assert.Len(t, out.Lines, defaultLogLines)
	assert.True(t, out.Truncated)
	assert.Equal(t, "ERROR redis refused", out.Lines[len(out.Lines)-1], "plain text: no escape codes, no CR")

	require.Empty(t, e.call("logs", map[string]any{"name": "api", "grep": "error"}, &out))
	assert.Equal(t, []string{"ERROR redis refused"}, out.Lines)
	assert.False(t, out.Truncated)

	assert.Contains(t, e.call("logs", map[string]any{"name": "api", "lines": 5000}, nil), "lines must be between 1 and 1000")
}

func TestLogs_NeverStartedSaysSo(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"api": "x"}, nil)
	assert.Contains(t, e.call("logs", map[string]any{"name": "api"}, nil), `no logs found for "api". Has it been started before?`)
}

func TestScope_RejectsBadProjectDir(t *testing.T) {
	e := newEnv(t)
	assert.Contains(t, e.call("list_services", map[string]any{"project_dir": "relative/path"}, nil), "must be an absolute path")
	assert.Contains(t, e.call("list_services", map[string]any{"project_dir": "/definitely/not/here"}, nil), "no such file")
	f := filepath.Join(e.root, "file")
	require.NoError(t, os.WriteFile(f, nil, 0644))
	assert.Contains(t, e.call("list_services", map[string]any{"project_dir": f}, nil), "is not a directory")
}

// A service's log file is named after it: a name must never be able to
// escape the logs directory.
func TestNames_CannotEscapeTheLogsDirectory(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"../../etc/passwd", "a/b", "", ".hidden", "-flag", "with space", strings.Repeat("x", 65)} {
		msg := e.call("logs", map[string]any{"name": bad}, nil)
		assert.Contains(t, msg, "invalid service name", "%q", bad)
	}
}

func TestBrokenProjectFileIsNamed(t *testing.T) {
	e := newEnv(t)
	dir := e.project("services: [unclosed\n")
	msg := e.call("list_services", map[string]any{"project_dir": dir}, nil)
	assert.Contains(t, msg, filepath.Join(dir, config.ProjectFileName))
}
