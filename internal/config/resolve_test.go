package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeProject(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ProjectFileName), []byte(body), 0644))
}

func TestResolve_LocalProjectTakesPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	require.NoError(t, SaveRegistry(RegistryPath(), &Registry{
		Version:  "1",
		Services: map[string]*ServiceConfig{"global-svc": {Name: "global-svc", Command: "true"}},
	}))

	dir := t.TempDir()
	writeProject(t, dir, "name: proj\nservices:\n  web:\n    command: yarn dev\n")

	reg, src, err := Resolve(dir, false)
	require.NoError(t, err)
	assert.True(t, src.IsLocal())
	assert.Equal(t, filepath.Join(dir, ProjectFileName), src.Local)
	assert.Equal(t, dir, src.Dir)
	assert.Contains(t, reg.Services, "web")
	assert.NotContains(t, reg.Services, "global-svc")
	assert.Equal(t, "proj", reg.Services["web"].Group)
	assert.Equal(t, dir, reg.Services["web"].CWD)
}

func TestResolve_GlobalFlagBypassesLocalProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	require.NoError(t, SaveRegistry(RegistryPath(), &Registry{
		Version:  "1",
		Services: map[string]*ServiceConfig{"global-svc": {Name: "global-svc", Command: "true"}},
	}))

	dir := t.TempDir()
	writeProject(t, dir, "services:\n  web:\n    command: yarn dev\n")

	reg, src, err := Resolve(dir, true)
	require.NoError(t, err)
	assert.False(t, src.IsLocal())
	assert.Contains(t, reg.Services, "global-svc")
	assert.NotContains(t, reg.Services, "web")
}

func TestResolve_NoProjectFallsBackToGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	require.NoError(t, SaveRegistry(RegistryPath(), &Registry{
		Version:  "1",
		Services: map[string]*ServiceConfig{"global-svc": {Name: "global-svc", Command: "true"}},
	}))

	reg, src, err := Resolve(t.TempDir(), false)
	require.NoError(t, err)
	assert.False(t, src.IsLocal())
	assert.Contains(t, reg.Services, "global-svc")
}

func TestSaveProject_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, "name: proj\nservices:\n  web:\n    command: yarn dev\n")

	proj, err := LoadProject(dir)
	require.NoError(t, err)
	proj.Services["api"] = &ProjectServiceConfig{Command: "go run ./cmd/api", CWD: "backend"}
	delete(proj.Services, "web")
	require.NoError(t, SaveProject(dir, proj))

	reloaded, err := LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "proj", reloaded.Name)
	assert.NotContains(t, reloaded.Services, "web")
	require.Contains(t, reloaded.Services, "api")
	assert.Equal(t, "go run ./cmd/api", reloaded.Services["api"].Command)
	assert.Equal(t, "backend", reloaded.Services["api"].CWD)
}

func TestResolve_MalformedProjectReturnsSource(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, ":::not yaml")

	_, src, err := Resolve(dir, false)
	require.Error(t, err)
	assert.True(t, src.IsLocal(), "the source still points at the unparseable devrun.yaml")
	assert.Equal(t, dir, src.Dir)
}
