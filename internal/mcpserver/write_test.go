package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
)

func TestAddService_ProjectFile(t *testing.T) {
	e := newEnv(t)
	dir := e.project("services:\n  web:\n    command: npm start\n")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"project_dir": dir, "name": "api", "command": "go run ./cmd/api", "cwd": "backend",
		"env": map[string]any{"PORT": "8080"},
	}, &out))
	assert.Equal(t, "project", out.Scope)
	assert.Equal(t, filepath.Join(dir, config.ProjectFileName), out.Source)
	assert.Equal(t, ServiceDef{Name: "api", Command: "go run ./cmd/api", CWD: filepath.Join(dir, "backend"), EnvKeys: []string{"PORT"}}, out.Service,
		"the result is read back from disk, with cwd resolved")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	require.Contains(t, proj.Services, "api")
	assert.Equal(t, "backend", proj.Services["api"].CWD, "stored relative to the project file")
	_, err = os.Stat(config.RegistryPath())
	assert.True(t, os.IsNotExist(err), "the global registry is untouched")
}

// An agent must never silently replace a service someone defined.
func TestAddService_RefusesAnExistingName(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "original command"}, nil)

	msg := e.call("add_service", map[string]any{"name": "web", "command": "something else"}, nil)
	assert.Contains(t, msg, `service "web" already registered`)
	reg, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	assert.Equal(t, "original command", reg.Services["web"].Command, "the file is left as it was")

	dir := e.project("services:\n  app:\n    command: x\n")
	assert.Contains(t, e.call("add_service", map[string]any{"project_dir": dir, "name": "app", "command": "y"}, nil), "already defined in devrun.yaml")
}

func TestAddService_Validates(t *testing.T) {
	e := newEnv(t)
	assert.Contains(t, e.call("add_service", map[string]any{"name": "../x", "command": "y"}, nil), "invalid service name")
	assert.Contains(t, e.call("add_service", map[string]any{"name": "ok", "command": "   "}, nil), "command cannot be empty")
	assert.Contains(t, e.call("add_service", map[string]any{"name": "ok", "command": "y", "env": map[string]any{"BAD-KEY": "1"}}, nil), "invalid environment variable name")
	// Missing required fields are rejected by the schema before the handler runs.
	assert.NotEmpty(t, e.call("add_service", map[string]any{"name": "ok"}, nil))
	_, err := os.Stat(config.RegistryPath())
	assert.True(t, os.IsNotExist(err), "nothing was written by any failed call")
}

func TestAddToTarget_CreatesAndMerges(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "x", "api": "y"}, nil)

	var out AddToTargetOutput
	require.Empty(t, e.call("add_to_target", map[string]any{"target": "stack", "services": []string{"web"}}, &out))
	assert.Equal(t, []string{"web"}, out.Members)
	require.Empty(t, e.call("add_to_target", map[string]any{"target": "stack", "services": []string{"api", "web"}}, &out))
	assert.Equal(t, []string{"web", "api"}, out.Members, "existing members kept, duplicates merged")

	msg := e.call("add_to_target", map[string]any{"target": "stack", "services": []string{"ghost"}}, nil)
	assert.Contains(t, msg, `service "ghost" is not defined in this config`)
	assert.Contains(t, msg, "defined services: [api web]")
	assert.Contains(t, e.call("add_to_target", map[string]any{"target": "stack", "services": []string{}}, nil), "at least one service")
}

func TestStartStop_ValidateBeforeDoingAnything(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "sleep 30"}, map[string][]string{"stack": {"web"}})

	for _, tool := range []string{"start", "stop"} {
		assert.Contains(t, e.call(tool, map[string]any{}, nil), "exactly one of service or target", tool)
		assert.Contains(t, e.call(tool, map[string]any{"service": "web", "target": "stack"}, nil), "exactly one of service or target", tool)
		assert.Contains(t, e.call(tool, map[string]any{"service": "nope"}, nil), `"nope" not found`, tool)
		assert.Contains(t, e.call(tool, map[string]any{"target": "nope"}, nil), `target "nope" not found`, tool)
	}
	assert.Contains(t, e.call("start", map[string]any{"service": "web", "timeout_s": 500}, nil), "timeout_s must be between 1 and 120")
}

// Stopping what is not running is the state the caller wanted: success.
func TestStop_NothingRunningIsANoOp(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "sleep 30"}, map[string][]string{"stack": {"web"}})

	var out StopOutput
	require.Empty(t, e.call("stop", map[string]any{"service": "web"}, &out))
	assert.Equal(t, []StopState{{Name: "web", State: "stopped"}}, out.Services)

	require.Empty(t, e.call("stop", map[string]any{"target": "stack"}, &out))
	assert.Equal(t, []StopState{{Name: "web", State: "stopped"}}, out.Services)
	assert.Contains(t, out.Note, "was not started as a target")
}
