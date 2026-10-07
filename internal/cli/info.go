package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/daemon"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/hailerity/devrun/internal/ops"
)

var infoCmd = &cobra.Command{
	Use:   "info [service]",
	Short: "Show system info, or details of a named service",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return runServiceInfo(args[0])
		}
		return runSysInfo()
	},
}

func runSysInfo() error {
	bin, _ := os.Executable()
	bin, _ = filepath.EvalSymlinks(bin)

	fmt.Printf("%s %s %s\n",
		styleBold.Render("devrun"),
		styleValue.Render(Version),
		styleLabel.Render("(commit: "+Commit+", built: "+Date+")"),
	)
	fmt.Printf("  %s  %s\n\n",
		styleLabel.Render("binary"),
		styleValue.Render(bin),
	)

	socketPath := config.SocketPath()
	running := isDaemonRunning(socketPath)
	status := styleGreen.Render("running")
	if !running {
		status = styleRed.Render("stopped")
	}
	fmt.Printf("%s  %s\n", styleBold.Render("daemon"), status)
	fmt.Printf("  %s  %s\n\n",
		styleLabel.Render("socket"),
		styleValue.Render(socketPath),
	)

	printChildInfo(socketPath)

	fmt.Println(styleBold.Render("paths"))
	if _, src, err := activeRegistry(); err == nil && src.IsLocal() {
		fmt.Printf("  %s  %s  %s\n",
			styleLabel.Render("config"),
			styleValue.Render(src.Local),
			styleLabel.Render("(local)"),
		)
		fmt.Printf("  %s  %s\n",
			styleLabel.Render("global"),
			styleValue.Render(config.RegistryPath()),
		)
	} else {
		fmt.Printf("  %s  %s\n",
			styleLabel.Render("config"),
			styleValue.Render(config.RegistryPath()),
		)
	}
	fmt.Printf("  %s   %s\n",
		styleLabel.Render("state"),
		styleValue.Render(config.StatePath()),
	)
	fmt.Printf("  %s    %s\n",
		styleLabel.Render("logs"),
		styleValue.Render(filepath.Join(config.DataDir(), "logs")),
	)
	fmt.Printf("  %s  %s\n",
		styleLabel.Render("tunnel"),
		styleValue.Render(daemon.TunnelLogPath()),
	)
	fmt.Printf("  %s %s\n",
		styleLabel.Render("gateway"),
		styleValue.Render(daemon.GatewayLogPath()),
	)

	return nil
}

// printChildInfo reports the two long-lived children devrun may run besides
// the daemon: whether each is up, where, and under which pid.
//
// Deliberately not what they serve — that is `devrun gateway status` and
// `devrun tunnel status`, which list services and their URLs. This answers
// the question `info` already answers for the daemon, for the two processes
// that can fail to start and leave nothing behind to look at.
//
// The token is omitted although the status payload carries it. `devrun info`
// is the output people paste into a bug report, and a gateway token is a
// credential; `gateway status` is where it belongs.
func printChildInfo(socketPath string) {
	st, err := ops.GatewayStatus(socketPath)
	if err != nil {
		// No daemon, or it could not answer. Either way nothing is running
		// that this can describe, which the zero payload already says.
		st = ipc.GatewayStatusPayload{}
	}
	for _, line := range childInfoLines(st) {
		fmt.Println(line)
	}
}

// childInfoLines renders the gateway and tunnel blocks. Separate from the
// printing so a test can read them without a daemon — including the one
// assertion that matters, that the token never appears.
func childInfoLines(st ipc.GatewayStatusPayload) []string {
	if !st.Running {
		return []string{
			fmt.Sprintf("%s  %s", styleBold.Render("gateway"), styleLabel.Render("not running")),
			"",
		}
	}

	out := []string{
		fmt.Sprintf("%s  %s", styleBold.Render("gateway"), styleGreen.Render("running")),
		field("url", styleAccent.Render("http://"+config.DisplayHost(st.Addr)+"/")),
	}
	if st.PID != nil {
		out = append(out, field("pid", styleValue.Render(fmt.Sprintf("%d", *st.PID))))
	}
	out = append(out, field("mode", styleValue.Render(st.Mode)+styleLabel.Render("  posture: "+st.Posture)))
	if st.PublicHostname != "" {
		out = append(out, field("hosts", styleValue.Render(st.PublicHostname)))
	}
	out = append(out, "")

	t := st.Tunnel
	if t == nil || !t.Running {
		return append(out,
			fmt.Sprintf("%s   %s", styleBold.Render("tunnel"), styleLabel.Render("not running")),
			"")
	}
	out = append(out, fmt.Sprintf("%s   %s", styleBold.Render("tunnel"), styleGreen.Render("running")))

	where := styleAccent.Render(t.PublicURL)
	if t.PublicURL == "" {
		where = styleLabel.Render("unknown — cloudflared is up but its banner could not be read")
	}
	out = append(out, field("url", where))

	kind := styleValue.Render(t.Kind)
	if t.Name != "" {
		kind += styleLabel.Render("  " + t.Name)
	}
	out = append(out, field("kind", kind))
	if t.PID != nil {
		out = append(out, field("pid", styleValue.Render(fmt.Sprintf("%d", *t.PID))))
	}
	return append(out, "")
}

// field is one indented label/value row, the label padded to the longest in
// use here so values line up down both blocks.
func field(label, value string) string {
	return fmt.Sprintf("  %s  %s", styleLabel.Render(fmt.Sprintf("%-5s", label)), value)
}

func runServiceInfo(name string) error {
	reg, _, err := activeRegistry()
	if err != nil {
		return err
	}

	svc, err := (&ops.Resolved{Registry: reg}).Service(name)
	if err != nil {
		return err
	}

	ss, err := ops.ServiceState(name)
	if err != nil {
		return err
	}

	statusStr := styleLabel.Render("stopped")
	if ss != nil {
		switch ss.Status {
		case config.StatusRunning:
			statusStr = styleGreen.Render("running")
		case config.StatusCrashed:
			statusStr = styleRed.Render("crashed")
		case config.StatusExited:
			statusStr = styleLabel.Render("exited")
		case config.StatusStarting:
			statusStr = styleLabel.Render("starting")
		case config.StatusStopping:
			statusStr = styleLabel.Render("stopping")
		}
	}
	fmt.Printf("%s  %s\n", styleBold.Render(name), statusStr)

	if ss != nil {
		if ss.PID != nil {
			fmt.Printf("  %s  %s\n",
				styleLabel.Render("pid"),
				styleValue.Render(fmt.Sprintf("%d", *ss.PID)),
			)
		}
		if ss.Port != nil {
			fmt.Printf("  %s  %s\n",
				styleLabel.Render("port"),
				styleValue.Render(fmt.Sprintf(":%d", *ss.Port)),
			)
		}
		if ss.StartedAt != nil {
			age := time.Since(*ss.StartedAt)
			fmt.Printf("  %s  %s\n",
				styleLabel.Render("started"),
				styleValue.Render(formatUptime(int64(age.Seconds()))+" ago"),
			)
		}
		if ss.LastExitCode != nil && (ss.Status == config.StatusCrashed || ss.Status == config.StatusExited) {
			codeStyle := styleRed
			if ss.Status == config.StatusExited {
				codeStyle = styleValue
			}
			fmt.Printf("  %s  %s\n",
				styleLabel.Render("exit code"),
				codeStyle.Render(fmt.Sprintf("%d", *ss.LastExitCode)),
			)
		}
	}

	fmt.Println()
	fmt.Println(styleBold.Render("config"))
	fmt.Printf("  %s  %s\n",
		styleLabel.Render("command"),
		styleValue.Render(svc.Command),
	)
	dir := svc.CWD
	if dir == "" {
		dir = "-"
	}
	fmt.Printf("  %s  %s\n",
		styleLabel.Render("directory"),
		styleValue.Render(dir),
	)
	if svc.Group != "" {
		fmt.Printf("  %s  %s\n",
			styleLabel.Render("group"),
			styleValue.Render(svc.Group),
		)
	}
	local, public := serviceURLs(name)
	if local != "" {
		fmt.Printf("  %s  %s\n",
			styleLabel.Render("url"),
			styleAccent.Render(local),
		)
	}
	if public != "" {
		// Beside the local address, not instead of it: both work, and only
		// one of them can be reached by anyone.
		fmt.Printf("  %s  %s\n",
			styleLabel.Render("public"),
			styleYellow.Render(public),
		)
	}
	if svc.Desc != "" {
		fmt.Printf("  %s  %s\n",
			styleLabel.Render("desc"),
			styleValue.Render(svc.Desc),
		)
	}

	if len(svc.Env) > 0 {
		fmt.Println()
		fmt.Println(styleBold.Render("env"))
		keys := make([]string, 0, len(svc.Env))
		for k := range svc.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s  %s\n",
				styleLabel.Render(k),
				styleValue.Render(svc.Env[k]),
			)
		}
	}

	fmt.Println()
	fmt.Printf("%s  %s\n",
		styleLabel.Render("log"),
		styleValue.Render(config.LogPath(name)),
	)

	return nil
}

func isDaemonRunning(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// serviceURLs is where this service can be opened: from this machine, and
// from anywhere when a tunnel publishes it. Either may be empty, and both are
// on any failure: `devrun info` must still describe a service when the daemon
// is down.
func serviceURLs(name string) (local, public string) {
	reg, _, err := activeRegistry()
	if err != nil {
		return "", ""
	}
	res, err := ops.List(&ops.Resolved{Registry: reg})
	if err != nil || res.Offline {
		return "", ""
	}
	for _, svc := range res.Services {
		if svc.Name == name {
			return svc.URL, svc.PublicURL
		}
	}
	return "", ""
}
