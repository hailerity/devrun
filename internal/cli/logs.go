package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hailerity/procet/internal/config"
)

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Print service log output",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

var logsFlags struct {
	follow bool
	lines  int
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFlags.follow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsFlags.lines, "lines", "n", 100, "Number of lines to show")
}

func runLogs(_ *cobra.Command, args []string) error {
	name := args[0]
	logPath := config.LogPath(name)

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return fmt.Errorf("no logs found for %q. Has it been started before?", name)
	}

	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	lines, err := tailLines(f, logsFlags.lines)
	if err != nil {
		return fmt.Errorf("tail log: %w", err)
	}
	for _, line := range lines {
		fmt.Println(line)
	}

	if !logsFlags.follow {
		return nil
	}

	// Follow: seek to end, poll for new content
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
	}
}

// tailLines returns the last n lines of a file.
func tailLines(f *os.File, n int) ([]string, error) {
	// Read entire file for simplicity; for large files this would need a reverse-scan
	scanner := bufio.NewScanner(f)
	var all []string
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}
