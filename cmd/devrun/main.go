package main

import (
	"fmt"
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
			// Say why. A daemon that dies silently is indistinguishable from
			// one that never started, and the caller only sees "timed out
			// waiting for daemon to start".
			fmt.Fprintf(os.Stderr, "devrun daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}
	// --_gateway is the same trick for the gateway child.
	if len(os.Args) >= 2 && os.Args[1] == "--_gateway" {
		os.Exit(runGateway(os.Args[2:]))
	}
	cli.Execute()
}

// version is what the gateway prints in its footer.
func version() string { return cli.Version }
