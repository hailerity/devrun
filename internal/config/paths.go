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
// stderr, cloudflared's output — in the same directory, where anyone looking
// for logs will find it.
//
// The underscore is what keeps it out of the way: ValidateName requires a
// name to start with a letter or digit, so no service can ever be called
// "_gateway" and no service log can land on one of these. Spelling these as
// LogPath("gateway") put them in the user's namespace, where a service called
// `gateway` — an ordinary thing to have — would have shared the file: its dev
// server's output would have been quoted back as the gateway's reason for
// failing to start, and `devrun logs gateway` would have interleaved the two.
func InternalLogPath(name string) string {
	return LogPath("_" + name)
}
