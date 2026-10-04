package cli

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/ops"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Publish the gateway over a Cloudflare tunnel",
}

var tunnelUpCmd = &cobra.Command{
	Use:   "up [service...]",
	Short: "Publish the named services (or whatever is already exposed)",
	RunE:  runTunnelUp,
}

var tunnelDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop publishing; the gateway keeps serving locally",
	Args:  cobra.NoArgs,
	RunE:  runTunnelDown,
}

var tunnelStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what is published, and where",
	Args:  cobra.NoArgs,
	RunE:  runTunnelStatus,
}

var tunnelUpFlags struct {
	hostname string
	name     string
	quick    bool
}

func init() {
	tunnelUpCmd.Flags().StringVar(&tunnelUpFlags.hostname, "hostname", "", "Public hostname DNS points at the tunnel")
	tunnelUpCmd.Flags().StringVar(&tunnelUpFlags.name, "name", "", "cloudflared tunnel to run")
	tunnelUpCmd.Flags().BoolVar(&tunnelUpFlags.quick, "quick", false, "Throwaway trycloudflare.com URL; needs no account or DNS")

	tunnelCmd.AddCommand(tunnelUpCmd, tunnelDownCmd, tunnelStatusCmd)
}

// resolveTunnel decides which tunnel to run: flags, then config, then — on a
// terminal — a prompt, and failing all of that a quick tunnel.
//
// The order matters more than it looks. Guessing between a stable hostname
// that needs DNS set up and a throwaway URL that needs nothing is wrong either
// way, so on a terminal it asks. Off one it must never block: an agent or a
// script gets the quick tunnel and a line saying so.
func resolveTunnel(cmd *cobra.Command, src config.Source, reg *config.Registry) (config.TunnelConfig, error) {
	if tunnelUpFlags.quick {
		if tunnelUpFlags.hostname != "" || tunnelUpFlags.name != "" {
			return config.TunnelConfig{}, errors.New("--quick takes no --hostname or --name: a quick tunnel's URL is assigned per run")
		}
		return config.TunnelConfig{}, nil
	}
	if tunnelUpFlags.hostname != "" || tunnelUpFlags.name != "" {
		cfg := config.TunnelConfig{
			Provider: config.ProviderCloudflare,
			Hostname: tunnelUpFlags.hostname,
			Name:     tunnelUpFlags.name,
		}
		if cfg.Name == "" {
			cfg.Name = firstLabel(cfg.Hostname)
		}
		return cfg, cfg.Validate()
	}
	if reg != nil && reg.Tunnel != nil && (reg.Tunnel.Name != "" || reg.Tunnel.Hostname != "") {
		return *reg.Tunnel, reg.Tunnel.Validate()
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Println("no tunnel configured and not a terminal — starting a Cloudflare quick tunnel")
		fmt.Println("  this URL changes every run; set tunnel.hostname for a stable one")
		return config.TunnelConfig{}, nil
	}
	return promptTunnel(cmd, src)
}

// promptTunnel asks once and remembers, so the question costs ten seconds the
// first time and nothing after.
func promptTunnel(cmd *cobra.Command, src config.Source) (config.TunnelConfig, error) {
	in := bufio.NewReader(cmd.InOrStdin())
	fmt.Println("No tunnel configured.")
	fmt.Println()

	hostname, err := ask(in, "  hostname (blank for a quick tunnel): ", "")
	if err != nil {
		return config.TunnelConfig{}, err
	}
	if hostname == "" {
		fmt.Println()
		fmt.Println("Starting a quick tunnel. Its URL changes every run.")
		return config.TunnelConfig{}, nil
	}

	// Pre-filled from the first label, since people name the tunnel after the
	// subdomain, and skipped outright when that name already exists — which is
	// the common case and not worth a question.
	suggested := firstLabel(hostname)
	name := suggested
	if names, known, err := ops.TunnelNames(config.SocketPath()); err == nil && known {
		if !contains(names, suggested) {
			if len(names) > 0 {
				fmt.Printf("  (tunnels on this account: %s)\n", strings.Join(names, ", "))
			}
			name, err = ask(in, fmt.Sprintf("  cloudflared tunnel [%s]: ", suggested), suggested)
			if err != nil {
				return config.TunnelConfig{}, err
			}
		}
	} else {
		// Unverifiable, so the name goes through unchecked rather than
		// blocking; cloudflared will say if it is wrong.
		name, err = ask(in, fmt.Sprintf("  cloudflared tunnel [%s]: ", suggested), suggested)
		if err != nil {
			return config.TunnelConfig{}, err
		}
	}

	cfg := config.TunnelConfig{Provider: config.ProviderCloudflare, Name: name, Hostname: hostname}
	if err := cfg.Validate(); err != nil {
		return config.TunnelConfig{}, err
	}
	path, err := config.SaveTunnel(src, cfg)
	if err != nil {
		return config.TunnelConfig{}, fmt.Errorf("save tunnel config: %w", err)
	}
	fmt.Println()
	fmt.Printf("Saved tunnel.name and tunnel.hostname to %s\n", path)
	return cfg, nil
}

func ask(in *bufio.Reader, prompt, fallback string) (string, error) {
	fmt.Print(prompt)
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read answer: %w", err)
	}
	if answer := strings.TrimSpace(line); answer != "" {
		return answer, nil
	}
	return fallback, nil
}

// firstLabel is the leftmost part of a hostname — "devrun" of
// "devrun.example.com" — which is what people tend to name the tunnel.
func firstLabel(hostname string) string {
	label, _, _ := strings.Cut(hostname, ".")
	return label
}

func runTunnelUp(cmd *cobra.Command, args []string) error {
	for _, n := range args {
		if err := config.ValidateName("service", n); err != nil {
			return err
		}
	}
	reg, src, err := activeRegistry()
	if err != nil {
		return err
	}
	tcfg, err := resolveTunnel(cmd, src, reg)
	if err != nil {
		return err
	}
	gcfg, err := gatewayConfig(cmd)
	if err != nil {
		return err
	}
	// Persisted first, for the same reason `gateway expose` does it: the
	// allowlist is what may leave this machine, so it has to outlive the
	// running gateway rather than vanish at the next up.
	if len(args) > 0 {
		if err := config.SaveGatewayExpose(src, args, true); err != nil {
			return err
		}
	}
	if gcfg.Posture == config.PosturePublished {
		fmt.Println("note: gateway.posture is published, so something already fronts this gateway.")
		fmt.Println("      Starting a tunnel as well means two things publishing it.")
	}

	if err := ops.EnsureDaemon(); err != nil {
		return err
	}
	status, err := ops.TunnelUp(config.SocketPath(), &tcfg, gcfg, args)
	if err != nil {
		return err
	}
	printPublished(status, tcfg)
	return nil
}

func runTunnelDown(cmd *cobra.Command, args []string) error {
	err := ops.TunnelDown(config.SocketPath())
	if errors.Is(err, ops.ErrNoDaemon) {
		fmt.Println("no daemon running")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Println("tunnel stopped; the gateway is still serving locally")
	return nil
}

func runTunnelStatus(cmd *cobra.Command, args []string) error {
	status, err := ops.GatewayStatus(config.SocketPath())
	if errors.Is(err, ops.ErrNoDaemon) {
		fmt.Println("no daemon running")
		return nil
	}
	if err != nil {
		return err
	}
	if status.Tunnel == nil || !status.Tunnel.Running {
		fmt.Println(styleLabel.Render("tunnel  not running"))
		return nil
	}
	printPublished(status, config.TunnelConfig{})
	return nil
}

// printPublished names every service that is now reachable from outside, by
// name and URL, because the dangerous moment is this one and not the config
// edit that set the allowlist days ago.
func printPublished(s ipc.GatewayStatusPayload, cfg config.TunnelConfig) {
	t := s.Tunnel
	if t == nil || !t.Running {
		fmt.Println(styleLabel.Render("tunnel  not running"))
		return
	}

	where := t.PublicURL
	if where == "" {
		where = "URL unknown — cloudflared is running but its banner could not be read"
	}
	fmt.Printf("%s  %s\n", styleBold.Render("tunnel"), styleAccent.Render(where))
	kind := t.Kind
	if kind == KindQuick {
		kind += styleLabel.Render("  — this URL changes every run")
	}
	fmt.Printf("  %s  %s\n", styleLabel.Render("kind    "), kind)

	if len(s.Exposed) == 0 {
		fmt.Println("  " + styleYellow.Render("nothing is exposed") +
			styleLabel.Render("  — published, but no service may leave this machine"))
		return
	}
	fmt.Println("  " + styleLabel.Render("published"))
	for _, name := range s.Exposed {
		fmt.Printf("    %-16s %s\n", name, styleAccent.Render(serviceURLFor(s, name)))
	}
	if s.Token != "" {
		fmt.Printf("  %s  %s\n", styleLabel.Render("key     "), styleValue.Render(s.Token))
	}
	warnMissingDNS(s, cfg)
}

// KindQuick mirrors the daemon's constant without importing it, the CLI and
// the daemon being separate processes that only share the IPC payloads.
const KindQuick = "quick"

// serviceURLFor is where a published service is reached, which depends on
// whether the gateway addresses services by hostname or by path.
func serviceURLFor(s ipc.GatewayStatusPayload, name string) string {
	base := ""
	if s.Tunnel != nil {
		base = s.Tunnel.PublicURL
	}
	if base == "" {
		return "(URL unknown)"
	}
	if host := publicHostFor(s, name); host != "" {
		return "https://" + host + "/"
	}
	return strings.TrimSuffix(base, "/") + "/" + name + "/"
}

// publicHostFor applies the gateway's hostname template, when it has one.
func publicHostFor(s ipc.GatewayStatusPayload, name string) string {
	if s.PublicHostname == "" {
		return ""
	}
	return strings.ReplaceAll(s.PublicHostname, config.ServicePlaceholder, name)
}

// warnMissingDNS prints the command devrun will not run for itself.
//
// Creating DNS records is out of scope — devrun does not take write access to
// your zone — but a missing record fails at Cloudflare with error 1033 and
// nothing here explains why: the gateway is serving, the tunnel is up, and
// only the name is absent. A resolver lookup needs no credentials, so the gap
// is at least named.
func warnMissingDNS(s ipc.GatewayStatusPayload, cfg config.TunnelConfig) {
	if s.Tunnel == nil || s.Tunnel.Kind != "named" || s.PublicHostname == "" {
		return // a quick tunnel owns its hostname; path mode needs no per-service record
	}
	tunnelName := s.Tunnel.Name
	if tunnelName == "" {
		tunnelName = cfg.Name
	}
	var missing []string
	for _, name := range s.Exposed {
		host := publicHostFor(s, name)
		if host == "" {
			continue
		}
		if _, err := net.LookupHost(host); err != nil {
			missing = append(missing, host)
		}
	}
	if len(missing) == 0 {
		return
	}
	fmt.Println()
	fmt.Println(styleYellow.Render(plural(len(missing), "hostname does", "hostnames do") + " not resolve yet:"))
	if tunnelName == "" {
		// Without the name there is no command to give. Printing one with a
		// hole in it would be worse than printing none: the point of this is
		// that it can be pasted.
		for _, host := range missing {
			fmt.Printf("    %s  — needs a CNAME to this tunnel\n", host)
		}
		return
	}
	for _, host := range missing {
		fmt.Printf("    cloudflared tunnel route dns %s %s\n", tunnelName, host)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
