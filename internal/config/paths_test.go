package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestPaths_XDGOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	assert.Equal(t, filepath.Join(tmp, "devrun"), config.ConfigDir())
	assert.Equal(t, filepath.Join(tmp, "devrun"), config.DataDir())
	assert.Equal(t, filepath.Join(tmp, "devrun", "services.yaml"), config.RegistryPath())
	assert.Equal(t, filepath.Join(tmp, "devrun", "devrun.sock"), config.SocketPath())
	assert.Equal(t, filepath.Join(tmp, "devrun", "state.json"), config.StatePath())
	assert.Equal(t, filepath.Join(tmp, "devrun", "logs", "web.log"), config.LogPath("web"))
}

func TestPaths_HomeDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".config", "devrun"), config.ConfigDir())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun"), config.DataDir())
	assert.Equal(t, filepath.Join(home, ".config", "devrun", "services.yaml"), config.RegistryPath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun", "devrun.sock"), config.SocketPath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun", "state.json"), config.StatePath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun", "logs", "web.log"), config.LogPath("web"))
}
