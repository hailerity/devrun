package config

import (
	"os"
	"path/filepath"
)

// ConfigDir returns ~/.config/devrun (or $XDG_CONFIG_HOME/devrun).
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "devrun")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic("devrun: cannot determine home directory: " + err.Error())
	}
	return filepath.Join(home, ".config", "devrun")
}

// DataDir returns ~/.local/share/devrun (or $XDG_DATA_HOME/devrun).
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "devrun")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic("devrun: cannot determine home directory: " + err.Error())
	}
	return filepath.Join(home, ".local", "share", "devrun")
}

func RegistryPath() string { return filepath.Join(ConfigDir(), "services.yaml") }
func SocketPath() string   { return filepath.Join(DataDir(), "devrun.sock") }
func StatePath() string    { return filepath.Join(DataDir(), "state.json") }
func LogPath(name string) string {
	return filepath.Join(DataDir(), "logs", name+".log")
}
