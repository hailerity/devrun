package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/gateway"
	"github.com/hailerity/devrun/internal/ops"
)

// gatewayConfigEnv carries the gateway's configuration to the child process.
//
// It travels as JSON in the environment rather than on argv: the block has
// nested routes, and argv quoting for that is a bug waiting to happen. The
// socket stays positional, matching --_daemon.
const gatewayConfigEnv = "DEVRUN_GATEWAY_CONFIG"

// gatewayTokenEnv carries the key the daemon minted, so the same one survives a
// gateway restart and whatever link the user already shared keeps working.
const gatewayTokenEnv = "DEVRUN_GATEWAY_TOKEN"

// runGateway is the --_gateway entry point: the same binary re-exec'd as the
// gateway process, the way --_daemon re-execs it as the daemon.
func runGateway(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "gateway: no socket path")
		return 1
	}
	socketPath := args[0]

	var block config.GatewayConfig
	if raw := os.Getenv(gatewayConfigEnv); raw != "" {
		if err := json.Unmarshal([]byte(raw), &block); err != nil {
			fmt.Fprintf(os.Stderr, "gateway: bad %s: %v\n", gatewayConfigEnv, err)
			return 1
		}
	}
	if err := block.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
		return 1
	}
	resolved := block.Defaults()

	cfg := ops.GatewayServerConfig(resolved, version())
	cfg.Token = os.Getenv(gatewayTokenEnv)
	if cfg.Token == "" {
		cfg.Token = newToken()
	}

	// SIGTERM is how the supervisor stops it; SIGINT is a person in a terminal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fetch := ops.GatewayFetcher(socketPath, resolved.ExposedSet())
	announce := func(addr string) {
		// The supervisor reads this line to learn the real address, which it
		// cannot know when the configured port is 0.
		fmt.Printf("gateway listening %s\n", addr)
		_ = os.Stdout.Sync()
	}

	if err := gateway.Run(ctx, cfg, fetch, announce); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
		return 1
	}
	return 0
}

// newToken mints a key for this gateway. 128 bits of randomness, hex so it
// survives a URL, a shell and a copy-paste without escaping.
func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is not survivable: a predictable key is worse
		// than no gateway.
		panic("gateway: cannot mint a token: " + err.Error())
	}
	return "k_" + hex.EncodeToString(b[:])
}
