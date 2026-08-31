package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortedTargetNames(t *testing.T) {
	got := config.SortedTargetNames(map[string][]string{
		"zeta":  {"a"},
		"alpha": {"b"},
		"mid":   {"c"},
	})
	assert.Equal(t, []string{"alpha", "mid", "zeta"}, got)
	assert.Empty(t, config.SortedTargetNames(nil))
}

func TestRegistry_TargetMemberConfigs(t *testing.T) {
	reg := &config.Registry{
		Services: map[string]*config.ServiceConfig{
			"web": {Name: "web", Command: "yarn dev"},
			"api": {Name: "api", Command: "go run ."},
			"db":  {Name: "db", Command: "postgres"},
		},
		Targets: map[string][]string{
			"stack": {"web", "api", "ghost", "web"}, // dangling + duplicate
		},
	}

	got := reg.TargetMemberConfigs("stack")
	require.Len(t, got, 2, "dangling member dropped, duplicate collapsed")
	assert.Equal(t, "web", got[0].Name)
	assert.Equal(t, "api", got[1].Name)

	assert.Nil(t, reg.TargetMemberConfigs("nope"), "unknown target yields nil")
}

func TestRegistry_TargetsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.yaml")
	reg := &config.Registry{
		Version: "1",
		Services: map[string]*config.ServiceConfig{
			"web": {Name: "web", Command: "yarn dev"},
			"api": {Name: "api", Command: "go run ."},
		},
		Targets: map[string][]string{"frontend": {"web"}, "all": {"web", "api"}},
	}
	require.NoError(t, config.SaveRegistry(path, reg))

	loaded, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"web"}, loaded.Targets["frontend"])
	assert.Equal(t, []string{"web", "api"}, loaded.Targets["all"])
}

func TestRegistry_TargetsDefaultEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: \"1\"\nservices:\n  web:\n    command: yarn\n"), 0644))
	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.NotNil(t, reg.Targets)
	assert.Empty(t, reg.Targets)
}

func TestLoadProject_Targets(t *testing.T) {
	dir := t.TempDir()
	content := `
name: myapp
services:
  web:
    command: yarn dev
  api:
    command: go run ./cmd/api
targets:
  frontend:
    - web
  full:
    - web
    - api
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte(content), 0644))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"web"}, proj.Targets["frontend"])
	assert.Equal(t, []string{"web", "api"}, proj.Targets["full"])
}

func TestLoadProject_TargetsDefaultEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("services:\n  web:\n    command: yarn\n"), 0644))
	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.NotNil(t, proj.Targets)
	assert.Empty(t, proj.Targets)
}

func TestProject_SaveRoundTripsTargets(t *testing.T) {
	dir := t.TempDir()
	proj := &config.ProjectConfig{
		Name:     "myapp",
		Services: map[string]*config.ProjectServiceConfig{"web": {Command: "yarn"}},
		Targets:  map[string][]string{"frontend": {"web"}},
	}
	require.NoError(t, config.SaveProject(dir, proj))

	loaded, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"web"}, loaded.Targets["frontend"])
}

func TestResolve_LocalCarriesTargets(t *testing.T) {
	dir := t.TempDir()
	content := `
name: myapp
services:
  web:
    command: yarn dev
targets:
  frontend:
    - web
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte(content), 0644))

	reg, src, err := config.Resolve(dir, false)
	require.NoError(t, err)
	assert.True(t, src.IsLocal())
	assert.Equal(t, []string{"web"}, reg.Targets["frontend"])
	assert.Equal(t, "web", reg.TargetMemberConfigs("frontend")[0].Name)
}
