package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
		// Bounded by the connection's own deadline rather than by racing a
		// goroutine against ctx: the loser of that race stays blocked on the
		// socket, leaking a goroutine and a half-open connection every tick a
		// wedged daemon causes to time out.
		deadline := gateway.RefreshEvery
		if d, ok := ctx.Deadline(); ok {
			if remaining := time.Until(d); remaining > 0 {
				deadline = remaining
			}
		}
		return fetchRoutes(socketPath, exposed, deadline)
	}
}

func fetchRoutes(socketPath string, exposed []string, deadline time.Duration) (gateway.Snapshot, error) {
	c, err := client.Connect(socketPath)
	if err != nil {
		return gateway.Snapshot{}, fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()
	c.SetTimeout(deadline)

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

// routesFrom narrows what the daemon reports to what the gateway needs.
//
// The port is whatever the daemon detected. A service's declared port: reaches
// the daemon but is not yet carried in ServiceInfo, so it does not reach here —
// surfacing the effective port belongs with the group that adds URL to that
// struct, which touches it anyway.
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
		Bind:           g.Addr(),
		Mode:           gateway.Mode(g.Mode),
		Posture:        g.Posture,
		Auth:           g.Auth,
		HostHeader:     g.HostHeader,
		PublicHostname: g.PublicHostname,
		Version:        version,
	}
	if len(g.Routes) > 0 {
		cfg.Rules = make(map[string]gateway.Rule, len(g.Routes))
		for path, route := range g.Routes {
			cfg.Rules[path] = gateway.Rule{Service: route.Service, Strip: route.Strip}
		}
	}
	return cfg
}

// GatewayUp starts the gateway, or reconfigures a running one. Idempotent: the
// daemon leaves an identically-configured gateway alone.
func GatewayUp(socketPath string, cfg *config.GatewayConfig) (ipc.GatewayStatusPayload, error) {
	return gatewayCall(socketPath, "gateway-up", ipc.GatewayUpPayload{Config: cfg})
}

// GatewayDown stops it. Stopping a gateway that is not running is not an error.
func GatewayDown(socketPath string) error {
	_, err := gatewayCall(socketPath, "gateway-down", struct{}{})
	return err
}

// GatewayStatus reports what the gateway is doing. A zero payload means it is
// not running.
func GatewayStatus(socketPath string) (ipc.GatewayStatusPayload, error) {
	return gatewayCall(socketPath, "gateway-status", struct{}{})
}

// GatewayExpose adds or removes services from the allowlist — what may leave
// this machine.
func GatewayExpose(socketPath string, names []string, exposed bool) (ipc.GatewayStatusPayload, error) {
	return gatewayCall(socketPath, "gateway-expose",
		ipc.GatewayExposePayload{Names: names, Exposed: exposed})
}

func gatewayCall(socketPath, reqType string, payload any) (ipc.GatewayStatusPayload, error) {
	var out ipc.GatewayStatusPayload

	c, err := client.Connect(socketPath)
	if err != nil {
		return out, ErrNoDaemon
	}
	defer func() { _ = c.Close() }()

	resp, err := c.Send(reqType, payload)
	if err != nil {
		return out, err
	}
	if !resp.OK {
		// A daemon from before the gateway existed answers every new request
		// type the same way; say what to do about it rather than echoing it.
		if strings.Contains(resp.Error, "unknown request type") {
			return out, fmt.Errorf("this daemon predates the gateway — run `devrun daemon restart`")
		}
		return out, errors.New(resp.Error)
	}
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return out, fmt.Errorf("decode %s: %w", reqType, err)
		}
	}
	return out, nil
}
