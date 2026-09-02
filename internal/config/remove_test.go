package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveService_Registry(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version: "1",
		Services: map[string]*config.ServiceConfig{
			"web": {Name: "web", Command: "run"},
			"api": {Name: "api", Command: "serve"},
		},
	}))

	require.NoError(t, config.RemoveService(config.Source{}, "web"))

	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.NotContains(t, reg.Services, "web", "removed service is gone")
	assert.Contains(t, reg.Services, "api", "other services are untouched")
}

func TestRemoveService_Registry_Missing(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version:  "1",
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "run"}},
	}))

	err := config.RemoveService(config.Source{}, "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRemoveService_Project(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\n  api:\n    command: b\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	require.NoError(t, config.RemoveService(src, "web"))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.NotContains(t, proj.Services, "web")
	assert.Contains(t, proj.Services, "api")
}

func TestRemoveService_Project_Missing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	err := config.RemoveService(src, "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
