package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

// TunnelUp publishes the gateway, starting it if it is not up. Idempotent on
// the same configuration and origin, so repeating it does not hand a quick
// tunnel a new URL.
//
// cfg must already be resolved: flags, config and any prompt are the CLI's
// work, because a daemon must never block on a terminal.
func TunnelUp(socketPath string, cfg *config.TunnelConfig, gw *config.GatewayConfig, expose []string) (ipc.GatewayStatusPayload, error) {
	return gatewayCall(socketPath, "tunnel-up",
		ipc.TunnelUpPayload{Config: cfg, Gateway: gw, Expose: expose})
}

// TunnelDown stops publishing and leaves the gateway serving locally.
func TunnelDown(socketPath string) error {
	_, err := gatewayCall(socketPath, "tunnel-down", struct{}{})
	return err
}

// TunnelNames lists the account's cloudflared tunnels, for checking a name
// before cloudflared fails on it.
//
// known is false when the question could not be asked — cloudflared missing,
// no `cloudflared tunnel login`, no network. Callers must not read that as
// "no such tunnel": an unverifiable name goes through unchecked rather than
// blocking a publish.
func TunnelNames(socketPath string) (names []string, known bool, err error) {
	c, err := client.Connect(socketPath)
	if err != nil {
		return nil, false, ErrNoDaemon
	}
	defer func() { _ = c.Close() }()

	resp, err := c.Send("tunnel-list", struct{}{})
	if err != nil {
		return nil, false, err
	}
	if !resp.OK {
		if strings.Contains(resp.Error, "unknown request type") {
			return nil, false, errors.New("this daemon predates the tunnel — run `devrun daemon restart`")
		}
		return nil, false, errors.New(resp.Error)
	}
	var out ipc.TunnelListPayload
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return nil, false, fmt.Errorf("decode tunnel-list: %w", err)
		}
	}
	return out.Names, out.Known, nil
}
