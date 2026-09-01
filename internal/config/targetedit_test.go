package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveTargetEdit_Registry_MembersOnly(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version:  "1",
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}, "api": {Name: "api", Command: "y"}},
		Targets:  map[string][]string{"stack": {"web"}},
	}))

	require.NoError(t, config.SaveTargetEdit(config.Source{}, "stack", "stack", []string{"api", "web", "api"}))

	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "web"}, reg.Targets["stack"], "members sorted and de-duplicated")
}

func TestSaveTargetEdit_Registry_Rename(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version: "1",
		Targets: map[string][]string{"old": {"web"}},
	}))

	require.NoError(t, config.SaveTargetEdit(config.Source{}, "old", "new", []string{"web"}))

	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.NotContains(t, reg.Targets, "old")
	assert.Equal(t, []string{"web"}, reg.Targets["new"])
}

func TestSaveTargetEdit_Registry_RenameCollision(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version: "1",
		Targets: map[string][]string{"a": {"web"}, "b": {"api"}},
	}))

	err := config.SaveTargetEdit(config.Source{}, "a", "b", []string{"web"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	reg, _ := config.LoadRegistry(path)
	assert.Equal(t, []string{"web"}, reg.Targets["a"], "left unchanged")
}

func TestSaveTargetEdit_Registry_Rejects(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version: "1",
		Targets: map[string][]string{"t": {"web"}},
	}))

	assert.Error(t, config.SaveTargetEdit(config.Source{}, "t", "  ", []string{"web"}), "empty name")
	assert.Error(t, config.SaveTargetEdit(config.Source{}, "ghost", "ghost2", nil), "missing target")
}

func TestSaveTargetEdit_Registry_EmptyMembers(t *testing.T) {
	path := globalSrc(t)
	require.NoError(t, config.SaveRegistry(path, &config.Registry{
		Version: "1",
		Targets: map[string][]string{"t": {"web"}},
	}))

	require.NoError(t, config.SaveTargetEdit(config.Source{}, "t", "t", nil))
	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.Empty(t, reg.Targets["t"])
}

func TestSaveTargetEdit_Project_RenameAndMembers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\n  api:\n    command: b\ntargets:\n  fe: [web]\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	require.NoError(t, config.SaveTargetEdit(src, "fe", "frontend", []string{"web", "api"}))

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.NotContains(t, proj.Targets, "fe")
	assert.Equal(t, []string{"api", "web"}, proj.Targets["frontend"])
}

func TestSaveTargetEdit_Project_RenameCollision(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: proj\nservices:\n  web:\n    command: a\ntargets:\n  a: [web]\n  b: [web]\n"), 0644))

	src := config.Source{Local: filepath.Join(dir, config.ProjectFileName), Dir: dir}
	err := config.SaveTargetEdit(src, "a", "b", []string{"web"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}
