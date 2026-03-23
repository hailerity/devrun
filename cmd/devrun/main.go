package main

import (
	"os"

	"github.com/hailerity/devrun/internal/cli"
	"github.com/hailerity/devrun/internal/daemon"
)

func main() {
	// --_daemon is an internal flag set when the binary re-execs itself as a daemon.
	// It is never documented or shown in help text.
	if len(os.Args) >= 2 && os.Args[1] == "--_daemon" {
		socketPath := ""
		if len(os.Args) >= 3 {
			socketPath = os.Args[2]
		}
		if err := daemon.Run(socketPath); err != nil {
			os.Exit(1)
		}
		return
	}
	cli.Execute()
}
