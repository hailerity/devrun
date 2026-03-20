package config

import (
	"os"
	"path/filepath"
)

// ConfigDir returns ~/.config/procet (or $XDG_CONFIG_HOME/procet).
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "procet")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "procet")
}

// DataDir returns ~/.local/share/procet (or $XDG_DATA_HOME/procet).
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "procet")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "procet")
}

func RegistryPath() string { return filepath.Join(ConfigDir(), "services.yaml") }
func SocketPath() string   { return filepath.Join(DataDir(), "procet.sock") }
func StatePath() string    { return filepath.Join(DataDir(), "state.json") }
func LogPath(name string) string {
	return filepath.Join(DataDir(), "logs", name+".log")
}
