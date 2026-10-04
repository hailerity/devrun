package cli

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
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
	Long: `Publish the named services (or whatever is already exposed).

A quick tunnel needs nothing — no account, no login, no DNS — and its URL
changes every run:

    devrun tunnel up web api --quick

A named tunnel needs three things on your Cloudflare account. devrun creates
none of them: writing to your zone needs access it has no business holding.

    cloudflared tunnel login
    cloudflared tunnel create devrun
    cloudflared tunnel route dns devrun devrun.example.com

That last hostname is the tunnel's own, where the service index is served. In
path mode it is the only record there is, and services hang off it as paths:
https://devrun.example.com/web/.

Giving each service a hostname of its own adds one record per published
service, on top of the tunnel's:

    gateway:
      public_hostname: "{service}-devrun.example.com"

    cloudflared tunnel route dns devrun web-devrun.example.com

Keep those one label under the apex. Cloudflare's Universal SSL covers the
apex and one wildcard level, so web.devrun.example.com is refused at the TLS
handshake.

Once the tunnel is up, devrun resolves every hostname it expects and prints
the exact command for any that is missing.`,
	RunE: runTunnelUp,
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
	hostname       string
	name           string
	quick          bool
	restartExposed bool
}

func init() {
	tunnelUpCmd.Flags().StringVar(&tunnelUpFlags.hostname, "hostname", "", "Public hostname DNS points at the tunnel")
	tunnelUpCmd.Flags().StringVar(&tunnelUpFlags.name, "name", "", "cloudflared tunnel to run")
	tunnelUpCmd.Flags().BoolVar(&tunnelUpFlags.quick, "quick", false, "Throwaway trycloudflare.com URL; needs no account or DNS")
	tunnelUpCmd.Flags().BoolVar(&tunnelUpFlags.restartExposed, "restart-exposed", false,
		"Restart published services so they pick up the new DEVRUN_URL_* values")

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
	// The commands go above the question, not below it. What a hostname costs
	// is the whole decision being made here, and naming that cost after the
	// answer is taken is too late to inform it.
	fmt.Println("  A hostname gives a stable URL. It needs a Cloudflare account and three")
	fmt.Println("  commands, which devrun will not run for you:")
	fmt.Println()
	for _, step := range setupSteps(exampleTunnel) {
		fmt.Println("      " + step)
	}
	fmt.Println()
	fmt.Println("  Blank gives a quick tunnel instead: no account, nothing to set up, and")
	fmt.Println("  a new URL every run.")
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

	// Flattened out of an if/else whose initialiser shadowed err: both
	// branches assigned to the shadowed copy, which was correct only because
	// each checked it on the next line. One moved check would have dropped
	// the error silently, and the linter does not catch it.
	names, known, listErr := ops.TunnelNames(config.SocketPath())
	// Unverifiable — no daemon, no login, no network — means the name goes
	// through unchecked rather than blocking; cloudflared will say if it is
	// wrong. Known and present means there is nothing to ask about.
	mustAsk := listErr != nil || !known || !contains(names, suggested)
	if mustAsk {
		if known && len(names) > 0 {
			fmt.Printf("  (tunnels on this account: %s)\n", strings.Join(names, ", "))
		}
		answer, err := ask(in, fmt.Sprintf("  cloudflared tunnel [%s]: ", suggested), suggested)
		if err != nil {
			return config.TunnelConfig{}, err
		}
		name = answer
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
	printSetupSteps(cfg)
	return cfg, nil
}

// exampleTunnel fills the setup commands shown above the question, where
// devrun has no name or hostname to put in them yet.
var exampleTunnel = config.TunnelConfig{Name: "devrun", Hostname: "devrun.example.com"}

// setupSteps is the Cloudflare-side work devrun will not do, in the order it
// has to be run.
//
// All three, always, including a login the account may well already have.
// The sequence is the thing being taught: that a tunnel is a named object
// which must exist before it can run, and that the hostname is a separate
// record pointing at it. Printing only the steps devrun can tell are
// outstanding hides that shape — and above the question it cannot tell
// anything, because the name and hostname are what is being asked for.
func setupSteps(cfg config.TunnelConfig) []string {
	return []string{
		"cloudflared tunnel login",
		"cloudflared tunnel create " + cfg.Name,
		"cloudflared tunnel route dns " + cfg.Name + " " + cfg.Hostname,
	}
}

// printSetupSteps repeats the sequence with the answers filled in, so it can
// be pasted rather than transcribed.
func printSetupSteps(cfg config.TunnelConfig) {
	fmt.Println()
	fmt.Println("Your setup, with the values just saved — skip whatever is already done:")
	fmt.Println()
	for _, step := range setupSteps(cfg) {
		fmt.Println("    " + step)
	}
	fmt.Println()
	fmt.Println("That hostname is the tunnel's own, where the service index is served, and")
	fmt.Println("in path mode the only record there is. Giving each service a hostname adds")
	fmt.Println("one record per published service; devrun resolves them all once the tunnel")
	fmt.Println("is up and names any that is missing.")
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
	if tunnelUpFlags.restartExposed {
		restartExposed(status.Exposed)
	}
	return nil
}

// restartExposed brings published services back so they see the addresses
// they are now reachable at.
//
// Dev servers read their environment once, at startup, so a service running
// from before the tunnel started still holds the local DEVRUN_URL_* values —
// and a frontend that baked one into its bundle is calling localhost from
// someone else's browser. Restarting is the only way to refresh them, and it
// is opt-in because it drops in-flight work and loses HMR state.
func restartExposed(names []string) {
	if len(names) == 0 {
		return
	}
	// Resolved once. Inside the loop this is a getwd and a full config read
	// per service, and a failure would print the same message once per name.
	reg, src, err := activeRegistry()
	if err != nil {
		fmt.Println(styleYellow.Render("cannot restart published services: " + err.Error()))
		return
	}

	fmt.Println()
	fmt.Println(styleLabel.Render("restarting published services so they see the new addresses"))
	for _, name := range names {
		cfg, err := inlineConfigFor(reg, src, name)
		if err != nil {
			fmt.Printf("    %-16s %s\n", name, styleYellow.Render(err.Error()))
			continue
		}
		if _, err := ops.Stop(name); err != nil {
			if errors.Is(err, ops.ErrNoDaemon) {
				// Nothing is running, so there is nothing to restart and
				// starting would fail for every remaining name in turn.
				fmt.Println(styleYellow.Render("    no daemon running; nothing to restart"))
				return
			}
			fmt.Printf("    %-16s %s\n", name, styleYellow.Render("stop: "+err.Error()))
			continue
		}
		if _, err := ops.Start(name, cfg); err != nil {
			fmt.Printf("    %-16s %s\n", name, styleRed.Render("start: "+err.Error()))
			continue
		}
		fmt.Printf("    %-16s %s\n", name, styleGreen.Render("restarted"))
	}
}

// inlineConfigFor is the definition to ship with a start, which a project
// service needs because the daemon cannot read a devrun.yaml. A service the
// active config does not define is one this command has no business
// restarting.
func inlineConfigFor(reg *config.Registry, src config.Source, name string) (*config.ServiceConfig, error) {
	if reg == nil || reg.Services[name] == nil {
		return nil, errors.New("not defined in the active config")
	}
	if !src.IsLocal() {
		// A global service: the daemon resolves it from services.yaml itself.
		return nil, nil
	}
	return reg.Services[name], nil
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

// KindQuick and KindNamed mirror the daemon's constants without importing
// them, the CLI and the daemon being separate processes that only share the
// IPC payloads.
const (
	KindQuick = "quick"
	KindNamed = "named"
)

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
	if s.Tunnel == nil || s.Tunnel.Kind != KindNamed {
		return // a quick tunnel is handed its hostname; there is no record to create
	}
	tunnelName := s.Tunnel.Name
	if tunnelName == "" {
		tunnelName = cfg.Name
	}
	var missing []string
	for _, host := range expectedHosts(s) {
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

// expectedHosts is every name that must resolve for a named tunnel, the
// tunnel's own first.
//
// That one was missing until it was asked about. It is where the index page
// is served, and in path mode it is the only record there is — so the one
// mandatory name went unchecked while the optional per-service ones were
// verified, which is exactly backwards.
func expectedHosts(s ipc.GatewayStatusPayload) []string {
	var hosts []string
	seen := map[string]bool{}
	add := func(host string) {
		if host == "" || seen[host] {
			return
		}
		seen[host] = true
		hosts = append(hosts, host)
	}

	add(tunnelHost(s))
	// Without a template services are addressed by path, so they share the
	// tunnel's record and have none of their own.
	if s.PublicHostname != "" {
		for _, name := range s.Exposed {
			add(publicHostFor(s, name))
		}
	}
	return hosts
}

// tunnelHost is the hostname DNS must point at the tunnel. A named tunnel's
// public URL is built from it, so this reads it back out rather than carrying
// the same string twice through the IPC payload.
func tunnelHost(s ipc.GatewayStatusPayload) string {
	if s.Tunnel == nil {
		return ""
	}
	u, err := url.Parse(s.Tunnel.PublicURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
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
