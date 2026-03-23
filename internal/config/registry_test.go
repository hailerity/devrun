package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.yaml")

	reg := &config.Registry{
		Version: "1",
		Services: map[string]*config.ServiceConfig{
			"web": {Name: "web", Command: "yarn dev", CWD: "/app", Group: "fullstack"},
		},
	}
	require.NoError(t, config.SaveRegistry(path, reg))

	loaded, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.Equal(t, "yarn dev", loaded.Services["web"].Command)
	assert.Equal(t, "fullstack", loaded.Services["web"].Group)
}

func TestRegistry_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.yaml")
	reg, err := config.LoadRegistry(path)
	require.NoError(t, err)
	assert.NotNil(t, reg.Services)
	assert.Empty(t, reg.Services)
}

func TestRegistry_MalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(":::bad yaml"), 0644))
	_, err := config.LoadRegistry(path)
	assert.Error(t, err)
}
