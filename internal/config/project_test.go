package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProject_FileNotExist(t *testing.T) {
	proj, err := LoadProject(t.TempDir())
	assert.NoError(t, err)
	assert.Nil(t, proj)
}

func TestLoadProject_NameFromFile(t *testing.T) {
	dir := t.TempDir()
	content := `
name: myapp
services:
  web:
    command: yarn dev
  api:
    command: go run ./cmd/api
    cwd: ./backend
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ProjectFileName), []byte(content), 0644))

	proj, err := LoadProject(dir)
	require.NoError(t, err)
	require.NotNil(t, proj)
	assert.Equal(t, "myapp", proj.Name)
	assert.Len(t, proj.Services, 2)
	assert.Equal(t, "yarn dev", proj.Services["web"].Command)
	assert.Equal(t, "./backend", proj.Services["api"].CWD)
}

func TestLoadProject_RejectsEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	content := "services:\n  web:\n    command: \"\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ProjectFileName), []byte(content), 0644))

	_, err := LoadProject(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "web")
}

func TestLoadProject_RejectsEntryWithNoDefinition(t *testing.T) {
	dir := t.TempDir()
	content := "services:\n  web:\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ProjectFileName), []byte(content), 0644))

	_, err := LoadProject(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "web")
}

func TestLoadProject_NameDefaultsToDirName(t *testing.T) {
	// Create a subdirectory with a known name so we can assert the default.
	parent := t.TempDir()
	dir := filepath.Join(parent, "my-project")
	require.NoError(t, os.Mkdir(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ProjectFileName), []byte("services:\n  web:\n    command: yarn\n"), 0644))

	proj, err := LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "my-project", proj.Name)
}

func TestProjectConfig_ToServiceConfigs(t *testing.T) {
	dir := "/projects/myapp"
	proj := &ProjectConfig{
		Name: "myapp",
		Services: map[string]*ProjectServiceConfig{
			"web": {Command: "yarn dev", CWD: ""},
			"api": {Command: "go run .", CWD: "./backend"},
		},
	}

	cfgs := proj.ToServiceConfigs(dir)

	assert.Equal(t, "myapp", cfgs["web"].Group)
	assert.Equal(t, dir, cfgs["web"].CWD)                       // empty → dir
	assert.Equal(t, "/projects/myapp/backend", cfgs["api"].CWD) // relative → resolved
	assert.Equal(t, "yarn dev", cfgs["web"].Command)
}

// A service's own group wins over the project-name default, and the two can sit
// side by side in one file — which is the point: before this, every service in
// a devrun.yaml arrived as a single undifferentiated group.
func TestProjectConfig_ToServiceConfigs_PerServiceGroup(t *testing.T) {
	proj := &ProjectConfig{
		Name: "myapp",
		Services: map[string]*ProjectServiceConfig{
			"web":    {Command: "yarn dev", Group: "frontend"},
			"api":    {Command: "go run .", Group: "backend"},
			"legacy": {Command: "./run.sh"}, // no group: inherits the project
		},
	}

	cfgs := proj.ToServiceConfigs("/projects/myapp")
	assert.Equal(t, "frontend", cfgs["web"].Group)
	assert.Equal(t, "backend", cfgs["api"].Group)
	assert.Equal(t, "myapp", cfgs["legacy"].Group, "unset inherits the project name")
}

// A file that says nothing about groups has to behave exactly as it did: every
// service under the project's name.
func TestProjectConfig_ToServiceConfigs_NoGroupsIsUnchanged(t *testing.T) {
	proj := &ProjectConfig{
		Name: "myapp",
		Services: map[string]*ProjectServiceConfig{
			"web": {Command: "yarn dev"},
			"api": {Command: "go run ."},
		},
	}
	cfgs := proj.ToServiceConfigs("/projects/myapp")
	assert.Equal(t, "myapp", cfgs["web"].Group)
	assert.Equal(t, "myapp", cfgs["api"].Group)
}

// It round-trips through the file, not just the struct.
func TestLoadProject_ReadsPerServiceGroup(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: shop\nservices:\n" +
		"  web:\n    command: yarn\n    group: frontend\n" +
		"  api:\n    command: go run .\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ProjectFileName), []byte(yaml), 0644))

	proj, err := LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "frontend", proj.Services["web"].Group)
	assert.Empty(t, proj.Services["api"].Group)

	cfgs := proj.ToServiceConfigs(dir)
	assert.Equal(t, "frontend", cfgs["web"].Group)
	assert.Equal(t, "shop", cfgs["api"].Group)
}

// And survives a save: SaveProject re-marshals the whole file, so a group it
// dropped would be silently lost on any `devrun add` or TUI edit.
func TestSaveProject_KeepsPerServiceGroup(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, SaveProject(dir, &ProjectConfig{
		Name: "shop",
		Services: map[string]*ProjectServiceConfig{
			"web": {Command: "yarn", Group: "frontend"},
		},
	}))

	proj, err := LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "frontend", proj.Services["web"].Group)
}

func TestProjectConfig_ToServiceConfigs_AbsoluteCWD(t *testing.T) {
	proj := &ProjectConfig{
		Name: "myapp",
		Services: map[string]*ProjectServiceConfig{
			"db": {Command: "postgres", CWD: "/var/data"},
		},
	}
	cfgs := proj.ToServiceConfigs("/projects/myapp")
	assert.Equal(t, "/var/data", cfgs["db"].CWD) // absolute unchanged
}

func TestSanitizeName(t *testing.T) {
	assert.Equal(t, "my-project", sanitizeName("my-project"))
	assert.Equal(t, "my-project", sanitizeName("my project"))
	assert.Equal(t, "my-project", sanitizeName("my.project"))
	assert.Equal(t, "MyApp", sanitizeName("MyApp"))
}
