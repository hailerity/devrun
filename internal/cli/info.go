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

	return nil
}

func runServiceInfo(name string) error {
	reg, _, err := activeRegistry()
	if err != nil {
		return err
	}

	svc := reg.Services[name]
	if svc == nil {
		return fmt.Errorf("service %q not found", name)
	}

	state, err := config.LoadState(config.StatePath())
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	ss := state.Services[name]

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
