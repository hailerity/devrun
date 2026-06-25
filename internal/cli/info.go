package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
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
	Use:   "info",
	Short: "Show system information about devrun",
	RunE: func(cmd *cobra.Command, args []string) error {
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
	},
}

func isDaemonRunning(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
