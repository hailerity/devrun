package cli

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/ops"
	"github.com/spf13/cobra"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Serve your running services over HTTP, locally",
}

var gatewayUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the local gateway",
	Args:  cobra.NoArgs,
	RunE:  runGatewayUp,
}

var gatewayDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop the gateway",
	Args:  cobra.NoArgs,
	RunE:  runGatewayDown,
}

var gatewayStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what the gateway is serving, and to whom",
	Args:  cobra.NoArgs,
	RunE:  runGatewayStatus,
}

var gatewayExposeCmd = &cobra.Command{
	Use:   "expose <service>...",
	Short: "Allow services to leave this machine",
	Args:  cobra.MinimumNArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setExposed(args, true) },
}

var gatewayHideCmd = &cobra.Command{
	Use:   "hide <service>...",
	Short: "Stop services leaving this machine",
	Args:  cobra.MinimumNArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setExposed(args, false) },
}

var gatewayUpFlags struct {
	port int
	bind string
	mode string
}

func init() {
	gatewayUpCmd.Flags().IntVar(&gatewayUpFlags.port, "port", 0, "Port to listen on (0 = from config, or 7788)")
	gatewayUpCmd.Flags().StringVar(&gatewayUpFlags.bind, "bind", "", "Address to bind (default 127.0.0.1)")
	gatewayUpCmd.Flags().StringVar(&gatewayUpFlags.mode, "mode", "", "URL shape to advertise: subdomain | path")

	gatewayCmd.AddCommand(gatewayUpCmd, gatewayDownCmd, gatewayStatusCmd, gatewayExposeCmd, gatewayHideCmd)
}

// gatewayConfig resolves the gateway block from whichever config is active,
// then applies any flags. It travels to the daemon inline, because the daemon
// cannot read a project devrun.yaml.
func gatewayConfig() (*config.GatewayConfig, error) {
	reg, _, err := activeRegistry()
	if err != nil {
		return nil, err
	}
	cfg := &config.GatewayConfig{}
	if reg != nil && reg.Gateway != nil {
		copied := *reg.Gateway
		cfg = &copied
	}
	if gatewayUpFlags.port != 0 {
		cfg.Port = gatewayUpFlags.port
	}
	if gatewayUpFlags.bind != "" {
		cfg.Bind = gatewayUpFlags.bind
	}
	if gatewayUpFlags.mode != "" {
		cfg.Mode = gatewayUpFlags.mode
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func runGatewayUp(cmd *cobra.Command, args []string) error {
	cfg, err := gatewayConfig()
	if err != nil {
		return err
	}
	if err := ops.EnsureDaemon(); err != nil {
		return err
	}
	socketPath := config.SocketPath()

	status, err := ops.GatewayUp(socketPath, cfg)
	if err != nil {
		return err
	}
	printGateway(status)
	return nil
}

func runGatewayDown(cmd *cobra.Command, args []string) error {
	err := ops.GatewayDown(config.SocketPath())
	if errors.Is(err, ops.ErrNoDaemon) {
		fmt.Println("no daemon running")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Println("gateway stopped")
	return nil
}

func runGatewayStatus(cmd *cobra.Command, args []string) error {
	status, err := ops.GatewayStatus(config.SocketPath())
	if errors.Is(err, ops.ErrNoDaemon) {
		fmt.Println("no daemon running")
		return nil
	}
	if err != nil {
		return err
	}
	printGateway(status)
	return nil
}

func setExposed(names []string, exposed bool) error {
	for _, n := range names {
		if err := config.ValidateName("service", n); err != nil {
			return err
		}
	}
	status, err := ops.GatewayExpose(config.SocketPath(), names, exposed)
	if err != nil {
		return err
	}
	printGateway(status)
	return nil
}

// displayHost turns a listen address into one a person can open. A wildcard
// bind is announced as 0.0.0.0:<port>, which is where it listens but not
// somewhere you can go. The real address stays untouched everywhere else —
// Posture reads it, and rewriting it to loopback would wrongly report a
// wildcard bind as local.
func displayHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && ip.IsUnspecified() {
		return net.JoinHostPort("localhost", port)
	}
	return addr
}

// printGateway reports what the gateway is doing, and — the part that matters —
// whether anything can reach it from off this machine.
func printGateway(s ipc.GatewayStatusPayload) {
	if !s.Running {
		fmt.Println(styleLabel.Render("gateway  not running"))
		return
	}

	url := "http://" + displayHost(s.Addr) + "/"
	fmt.Printf("%s  %s\n", styleBold.Render("gateway"), styleAccent.Render(url))

	// The posture is the whole security model, so it is never left implicit.
	if s.Posture == config.PosturePublished {
		fmt.Println("  " + styleYellow.Render("published") +
			styleLabel.Render("  — reachable from off this machine; the allowlist and token apply"))
	} else {
		fmt.Println("  " + styleGreen.Render("local") +
			styleLabel.Render("  — loopback only; every running service is served, no token needed"))
	}

	if len(s.Exposed) == 0 {
		fmt.Println("  " + styleLabel.Render("exposed    nothing may leave this machine"))
	} else {
		fmt.Printf("  %s  %v\n", styleLabel.Render("exposed  "), s.Exposed)
	}
	if s.Token != "" {
		fmt.Printf("  %s  %s\n", styleLabel.Render("key      "), styleValue.Render(url+"?k="+s.Token))
	}
}
