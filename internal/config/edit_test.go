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

	require.NoError(t, config.SaveServiceEdit(config.Source{}, "web", "web", "new", "/app", "g"))

	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	require.Contains(t, reg.Services, "web")
	assert.Equal(t, "new", reg.Services["web"].Command)
	assert.Equal(t, "web", reg.Services["web"].Name)
	assert.Equal(t, "g", reg.Services["web"].Group, "group round-trips through the edit")
	assert.Equal(t, "V", reg.Services["web"].Env["K"], "env preserved")
}

func TestSaveServiceEdit_Registry_Rename(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version:  "1",
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "run", CWD: "/a"}},
	}))

	require.NoError(t, config.SaveServiceEdit(config.Source{}, "web", "frontend", "run", "/b", ""))

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

	err := config.SaveServiceEdit(config.Source{}, "web", "api", "a", "", "")
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

	assert.Error(t, config.SaveServiceEdit(config.Source{}, "web", "  ", "run", "", ""), "empty name")
	assert.Error(t, config.SaveServiceEdit(config.Source{}, "web", "web", "", "", ""), "empty command")
	assert.Error(t, config.SaveServiceEdit(config.Source{}, "ghost", "ghost2", "run", "", ""), "missing service")
}

func TestSaveServiceEdit_Project_RelCWDAndRename(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: old\n    cwd: sub\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	require.NoError(t, config.SaveServiceEdit(src, "web", "ui", "new", filepath.Join(dir, "client"), ""))

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
	require.NoError(t, config.SaveServiceEdit(src, "web", "web", "run", dir, ""))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Empty(t, proj.Services["web"].CWD, "cwd at the project root is stored empty")
}

func TestSaveServiceEdit_Project_RelativeCWDKeptAsGiven(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: run\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	// A relative cwd is interpreted relative to the project dir — not the
	// process working directory — and stored verbatim.
	require.NoError(t, config.SaveServiceEdit(src, "web", "web", "run", "frontend", ""))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "frontend", proj.Services["web"].CWD)
}

// The group is an edited field in both config shapes: settable, changeable, and
// clearable. Clearing matters on its own — a project service with no group goes
// back to inheriting the project's name.
func TestSaveServiceEdit_Project_SetsChangesAndClearsTheGroup(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: run\n"), 0644))
	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}

	// Set.
	require.NoError(t, config.SaveServiceEdit(src, "web", "web", "run", "", "frontend"))
	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "frontend", proj.Services["web"].Group)
	assert.Equal(t, "frontend", proj.ToServiceConfigs(dir)["web"].Group)

	// Change.
	require.NoError(t, config.SaveServiceEdit(src, "web", "web", "run", "", "edge"))
	proj, err = config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "edge", proj.Services["web"].Group)

	// Clear: the field empties, and the derived group falls back to the project.
	require.NoError(t, config.SaveServiceEdit(src, "web", "web", "run", "", "  "))
	proj, err = config.LoadProject(dir)
	require.NoError(t, err)
	assert.Empty(t, proj.Services["web"].Group, "whitespace is trimmed to empty")
	assert.Equal(t, "proj", proj.ToServiceConfigs(dir)["web"].Group, "back to the project's name")
}

func TestSaveServiceEdit_Registry_SetsAndClearsTheGroup(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version:  "1",
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "run", Group: "old"}},
	}))

	require.NoError(t, config.SaveServiceEdit(config.Source{}, "web", "web", "run", "", "new"))
	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.Equal(t, "new", reg.Services["web"].Group)

	// A global service has no project to fall back on: empty means ungrouped.
	require.NoError(t, config.SaveServiceEdit(config.Source{}, "web", "web", "run", "", ""))
	reg, err = config.LoadRegistry(path)
	require.NoError(t, err)
	assert.Empty(t, reg.Services["web"].Group)
}

// Editing a group must not take the fields the form does not show with it.
func TestSaveServiceEdit_GroupEditKeepsEnvDescAndPort(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: run\n    desc: the ui\n    port: 5173\n    env:\n      K: V\n"), 0644))
	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}

	require.NoError(t, config.SaveServiceEdit(src, "web", "web", "run", "", "frontend"))
	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "frontend", proj.Services["web"].Group)
	assert.Equal(t, "the ui", proj.Services["web"].Desc)
	assert.Equal(t, 5173, proj.Services["web"].Port)
	assert.Equal(t, "V", proj.Services["web"].Env["K"])
}

func TestSaveServiceEdit_Project_RenameCollision(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\n  api:\n    command: b\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	err := config.SaveServiceEdit(src, "web", "api", "a", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}
