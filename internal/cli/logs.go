package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hailerity/devrun/internal/ops"
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

	// Raw lines — colour codes and all — exactly as the service wrote them.
	res, err := ops.Logs(name, ops.LogQuery{Lines: logsFlags.lines})
	if err != nil {
		return err
	}
	for _, line := range res.Lines {
		fmt.Println(line)
	}

	if !logsFlags.follow {
		return nil
	}

	// Follow from exactly where the snapshot ended, so no line written in
	// between is lost or printed twice.
	f, err := os.Open(res.Path)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()
	if _, err := f.Seek(res.Offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err == io.EOF {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			return fmt.Errorf("read log: %w", err)
		}
	}
}
