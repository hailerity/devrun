package ops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/gateway"
	"github.com/hailerity/devrun/internal/ipc"
)

// GatewayFetcher returns the function the gateway process uses to learn what is
// running. It is an ordinary daemon client — the same `list` the TUI polls —
// which is why the gateway needs no privileged channel of its own.
//
// The allowlist is closed over rather than fetched: it is configuration, not
// state, and the daemon restarts the gateway when it changes.
func GatewayFetcher(socketPath string, exposed []string) gateway.Fetcher {
	return func(ctx context.Context) (gateway.Snapshot, error) {
		type result struct {
			snap gateway.Snapshot
			err  error
		}
		done := make(chan result, 1)

		go func() {
			snap, err := fetchRoutes(socketPath, exposed)
			done <- result{snap, err}
		}()

		select {
		case <-ctx.Done():
			return gateway.Snapshot{}, ctx.Err()
		case r := <-done:
			return r.snap, r.err
		}
	}
}

func fetchRoutes(socketPath string, exposed []string) (gateway.Snapshot, error) {
	c, err := client.Connect(socketPath)
	if err != nil {
		return gateway.Snapshot{}, fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	resp, err := c.Send("list", struct{}{})
	if err != nil {
		return gateway.Snapshot{}, fmt.Errorf("list: %w", err)
	}
	if !resp.OK {
		return gateway.Snapshot{}, fmt.Errorf("list: %s", resp.Error)
	}

	var payload ipc.ListResponsePayload
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return gateway.Snapshot{}, fmt.Errorf("decode list: %w", err)
	}
	return gateway.Snapshot{Routes: routesFrom(payload.Services), Exposed: exposed}, nil
}

// routesFrom narrows what the daemon reports to what the gateway needs. A
// declared port wins over the detected one; detection is correct but lags a
// start by up to one poll and picks the lowest when there are several.
func routesFrom(services []ipc.ServiceInfo) []gateway.Route {
	out := make([]gateway.Route, 0, len(services))
	for _, svc := range services {
		r := gateway.Route{Name: svc.Name, State: svc.State}
		if svc.Port != nil {
			r.Port = *svc.Port
		}
		out = append(out, r)
	}
	return out
}

// GatewayServerConfig turns a validated gateway block into the handler's
// configuration. Keeping the translation here means internal/gateway never has
// to import the config package.
func GatewayServerConfig(g config.GatewayConfig, version string) gateway.Config {
	cfg := gateway.Config{
		Bind:       g.Addr(),
		Mode:       gateway.Mode(g.Mode),
		Posture:    g.Posture,
		Auth:       g.Auth,
		HostHeader: g.HostHeader,
		Version:    version,
	}
	if len(g.Routes) > 0 {
		cfg.Rules = make(map[string]gateway.Rule, len(g.Routes))
		for path, route := range g.Routes {
			cfg.Rules[path] = gateway.Rule{Service: route.Service, Strip: route.Strip}
		}
	}
	return cfg
}
