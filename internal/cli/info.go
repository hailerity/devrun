package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/config"
)

var (
	infoStyleLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681"))
	infoStyleValue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9d1d9"))
	infoStyleVersion = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9d1d9")).Bold(true)
	infoStyleSection = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9d1d9")).Bold(true)
	infoStyleGreen   = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	infoStyleRed     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
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
		infoStyleVersion.Render("devrun"),
		infoStyleValue.Render(Version),
		infoStyleLabel.Render("(commit: "+Commit+", built: "+Date+")"),
	)
	fmt.Printf("  %s  %s\n\n",
		infoStyleLabel.Render("binary"),
		infoStyleValue.Render(bin),
	)

	socketPath := config.SocketPath()
	running := isDaemonRunning(socketPath)
	status := infoStyleGreen.Render("running")
	if !running {
		status = infoStyleRed.Render("stopped")
	}
	fmt.Printf("%s  %s\n", infoStyleSection.Render("daemon"), status)
	fmt.Printf("  %s  %s\n\n",
		infoStyleLabel.Render("socket"),
		infoStyleValue.Render(socketPath),
	)

	fmt.Println(infoStyleSection.Render("paths"))
	fmt.Printf("  %s  %s\n",
		infoStyleLabel.Render("config"),
		infoStyleValue.Render(config.RegistryPath()),
	)
	fmt.Printf("  %s   %s\n",
		infoStyleLabel.Render("state"),
		infoStyleValue.Render(config.StatePath()),
	)
	fmt.Printf("  %s    %s\n",
		infoStyleLabel.Render("logs"),
		infoStyleValue.Render(filepath.Join(config.DataDir(), "logs")),
	)

	return nil
}

func runServiceInfo(name string) error {
	reg, err := config.LoadRegistry(config.RegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
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

	// Header: name + status
	statusStr := infoStyleLabel.Render("stopped")
	if ss != nil {
		switch ss.Status {
		case config.StatusRunning:
			statusStr = infoStyleGreen.Render("running")
		case config.StatusCrashed:
			statusStr = infoStyleRed.Render("crashed")
		case config.StatusStarting:
			statusStr = infoStyleLabel.Render("starting")
		case config.StatusStopping:
			statusStr = infoStyleLabel.Render("stopping")
		}
	}
	fmt.Printf("%s  %s\n", infoStyleSection.Render(name), statusStr)

	if ss != nil {
		if ss.PID != nil {
			fmt.Printf("  %s  %s\n",
				infoStyleLabel.Render("pid"),
				infoStyleValue.Render(fmt.Sprintf("%d", *ss.PID)),
			)
		}
		if ss.Port != nil {
			fmt.Printf("  %s  %s\n",
				infoStyleLabel.Render("port"),
				infoStyleValue.Render(fmt.Sprintf(":%d", *ss.Port)),
			)
		}
		if ss.StartedAt != nil {
			age := time.Since(*ss.StartedAt)
			fmt.Printf("  %s  %s\n",
				infoStyleLabel.Render("started"),
				infoStyleValue.Render(formatUptime(int64(age.Seconds()))+" ago"),
			)
		}
		if ss.LastExitCode != nil && ss.Status == config.StatusCrashed {
			fmt.Printf("  %s  %s\n",
				infoStyleLabel.Render("exit code"),
				infoStyleRed.Render(fmt.Sprintf("%d", *ss.LastExitCode)),
			)
		}
	}

	fmt.Println()
	fmt.Println(infoStyleSection.Render("config"))
	fmt.Printf("  %s  %s\n",
		infoStyleLabel.Render("command"),
		infoStyleValue.Render(svc.Command),
	)
	dir := svc.CWD
	if dir == "" {
		dir = "-"
	}
	fmt.Printf("  %s  %s\n",
		infoStyleLabel.Render("directory"),
		infoStyleValue.Render(dir),
	)
	if svc.Group != "" {
		fmt.Printf("  %s  %s\n",
			infoStyleLabel.Render("group"),
			infoStyleValue.Render(svc.Group),
		)
	}
	if svc.Desc != "" {
		fmt.Printf("  %s  %s\n",
			infoStyleLabel.Render("desc"),
			infoStyleValue.Render(svc.Desc),
		)
	}

	if len(svc.Env) > 0 {
		fmt.Println()
		fmt.Println(infoStyleSection.Render("env"))
		keys := make([]string, 0, len(svc.Env))
		for k := range svc.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s  %s\n",
				infoStyleLabel.Render(k),
				infoStyleValue.Render(svc.Env[k]),
			)
		}
	}

	fmt.Println()
	fmt.Printf("%s  %s\n",
		infoStyleLabel.Render("log"),
		infoStyleValue.Render(config.LogPath(name)),
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
