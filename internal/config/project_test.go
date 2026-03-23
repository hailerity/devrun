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
	assert.Equal(t, dir, cfgs["web"].CWD)                              // empty → dir
	assert.Equal(t, "/projects/myapp/backend", cfgs["api"].CWD)        // relative → resolved
	assert.Equal(t, "yarn dev", cfgs["web"].Command)
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
