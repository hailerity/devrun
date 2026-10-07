package config_test

import (
	"os"
	"path/filepath"
	"strings"
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

// A log devrun owns must never land on a log a service owns. The gateway's
// and the tunnel's were spelled LogPath("gateway") and LogPath("tunnel"),
// which are in the user's namespace: a service called `gateway` shares the
// file, so its dev server's output gets quoted back as the gateway's reason
// for failing to start, and `devrun logs gateway` interleaves the two.
func TestInternalLogPath_CannotCollideWithAService(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	for _, name := range []string{"gateway", "tunnel"} {
		assert.NotEqual(t, config.LogPath(name), config.InternalLogPath(name),
			"%q is a legal service name, so it must not claim the internal log", name)

		// And the reverse: no service can reach the internal name, because
		// ValidateName requires a leading letter or digit.
		internal := strings.TrimSuffix(filepath.Base(config.InternalLogPath(name)), ".log")
		assert.Error(t, config.ValidateName("service", internal),
			"%q must be unreachable as a service name", internal)
	}
}
