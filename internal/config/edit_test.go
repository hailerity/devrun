package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// globalSrc points SaveServiceEdit at a temp global registry and returns its path.
func globalSrc(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return config.RegistryPath()
}

func TestSaveServiceEdit_Registry_CommandOnly(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version: "1",
		Services: map[string]*config.ServiceConfig{
			"web": {Name: "web", Command: "old", CWD: "/app", Group: "g", Env: map[string]string{"K": "V"}},
		},
	}))

	require.NoError(t, config.SaveServiceEdit(config.Source{}, "web", "web", "new", "/app"))

	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	require.Contains(t, reg.Services, "web")
	assert.Equal(t, "new", reg.Services["web"].Command)
	assert.Equal(t, "web", reg.Services["web"].Name)
	assert.Equal(t, "g", reg.Services["web"].Group, "group preserved")
	assert.Equal(t, "V", reg.Services["web"].Env["K"], "env preserved")
}

func TestSaveServiceEdit_Registry_Rename(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version:  "1",
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "run", CWD: "/a"}},
	}))

	require.NoError(t, config.SaveServiceEdit(config.Source{}, "web", "frontend", "run", "/b"))

	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.NotContains(t, reg.Services, "web", "old key removed")
	require.Contains(t, reg.Services, "frontend")
	assert.Equal(t, "frontend", reg.Services["frontend"].Name)
	assert.Equal(t, "/b", reg.Services["frontend"].CWD)
}

func TestSaveServiceEdit_Registry_RenameCollision(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version: "1",
		Services: map[string]*config.ServiceConfig{
			"web": {Name: "web", Command: "a"},
			"api": {Name: "api", Command: "b"},
		},
	}))

	err := config.SaveServiceEdit(config.Source{}, "web", "api", "a", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	reg, _ := config.LoadRegistry(path)
	assert.Equal(t, "a", reg.Services["web"].Command, "file left unchanged")
}

func TestSaveServiceEdit_Registry_Rejects(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version:  "1",
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "run"}},
	}))

	assert.Error(t, config.SaveServiceEdit(config.Source{}, "web", "  ", "run", ""), "empty name")
	assert.Error(t, config.SaveServiceEdit(config.Source{}, "web", "web", "", ""), "empty command")
	assert.Error(t, config.SaveServiceEdit(config.Source{}, "ghost", "ghost2", "run", ""), "missing service")
}

func TestSaveServiceEdit_Project_RelCWDAndRename(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: old\n    cwd: sub\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	require.NoError(t, config.SaveServiceEdit(src, "web", "ui", "new", filepath.Join(dir, "client")))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.NotContains(t, proj.Services, "web")
	require.Contains(t, proj.Services, "ui")
	assert.Equal(t, "new", proj.Services["ui"].Command)
	assert.Equal(t, "client", proj.Services["ui"].CWD, "cwd stored relative to the project dir")
}

func TestSaveServiceEdit_Project_CWDAtRootDropped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: run\n    cwd: sub\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	require.NoError(t, config.SaveServiceEdit(src, "web", "web", "run", dir))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Empty(t, proj.Services["web"].CWD, "cwd at the project root is stored empty")
}

func TestSaveServiceEdit_Project_RenameCollision(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\n  api:\n    command: b\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	err := config.SaveServiceEdit(src, "web", "api", "a", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}
