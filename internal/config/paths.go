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

func RegistryPath() string  { return filepath.Join(ConfigDir(), "services.yaml") }
func SocketPath() string    { return filepath.Join(DataDir(), "devrun.sock") }
func StatePath() string     { return filepath.Join(DataDir(), "state.json") }
func DaemonPIDPath() string { return filepath.Join(DataDir(), "daemon.pid") }

// LogPath is where a *service's* output is kept. The name comes from the
// user, so it shares a directory with every other service's log and nothing
// else may claim a name in here — see InternalLogPath.
func LogPath(name string) string {
	return filepath.Join(DataDir(), "logs", name+".log")
}

// InternalLogPath is where a log devrun itself owns is kept — the gateway's
// stderr, cloudflared's output — in a subdirectory of the same logs directory,
// where anyone looking for logs will still find it.
//
// A subdirectory rather than a name convention, because the convention cannot
// be enforced where it matters. Spelling these as LogPath("gateway") put them
// in the user's namespace, where a service called `gateway` — an ordinary
// thing to run — shared the file: its output was quoted back as the gateway's
// reason for failing to start, and `devrun logs gateway` interleaved the two.
// A "_gateway" prefix did not fix it either: the load-bearing check on a name
// that becomes a path is SafeFileName, not ValidateName, and SafeFileName
// accepts a leading underscore, so a hand-edited devrun.yaml reached it again.
//
// SafeFileName does reject '/' and '\', and it is applied before any service
// name becomes a path. So no service log can descend into here, by
// construction rather than by agreement.
func InternalLogPath(name string) string {
	return filepath.Join(DataDir(), "logs", "devrun", name+".log")
}
