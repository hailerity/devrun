package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/hailerity/procet/internal/client"
	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/ipc"
)

var fgCmd = &cobra.Command{
	Use:   "fg <name>",
	Short: "Attach terminal to a running background service",
	Args:  cobra.ExactArgs(1),
	RunE:  runFg,
}

func runFg(_ *cobra.Command, args []string) error {
	return runFgByName(config.SocketPath(), args[0])
}

func runFgByName(socketPath, name string) error {
	c, err := client.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	// Note: we do NOT defer c.Close() here — the connection stays open for the raw stream.

	resp, err := c.Send("attach", ipc.AttachPayload{Name: name})
	if err != nil {
		c.Close()
		return err
	}
	if !resp.OK {
		c.Close()
		fmt.Fprintln(os.Stderr, resp.Error)
		return nil
	}

	// Put terminal in raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		c.Close()
		return fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	fmt.Fprintf(os.Stderr, "\r\n[attached to %s — Ctrl+P, Q to detach]\r\n", name)

	conn := c.Conn()

	// socket → stdout
	go func() {
		rbuf := make([]byte, 4096)
		for {
			n, err := conn.Read(rbuf)
			if n > 0 {
				os.Stdout.Write(rbuf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	// stdin → socket (with Ctrl+P, Q detection)
	// We buffer bytes to suppress the Ctrl+P byte if a detach sequence follows.
	var ctrlPPending bool
	buf := make([]byte, 256)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}
		for _, b := range buf[:n] {
			if ctrlPPending {
				ctrlPPending = false
				if b == 0x71 { // Q — complete Ctrl+P, Q detach sequence
					term.Restore(int(os.Stdin.Fd()), oldState)
					fmt.Fprintf(os.Stderr, "\r\n[detached from %s]\r\n", name)
					conn.Close()
					return nil
				}
				// Not a detach — forward the suppressed Ctrl+P then the current byte
				conn.Write([]byte{0x10, b})
				continue
			}
			if b == 0x10 { // Ctrl+P — hold it, don't forward yet
				ctrlPPending = true
				continue
			}
			if b == 0x03 { // Ctrl+C — stop the service
				term.Restore(int(os.Stdin.Fd()), oldState)
				fmt.Fprintf(os.Stderr, "\r\n[stopping %s]\r\n", name)
				conn.Close()
				if sc, err := client.Connect(socketPath); err == nil {
					_, _ = sc.Send("stop", ipc.StopPayload{Name: name})
					sc.Close()
				}
				return nil
			}
			conn.Write([]byte{b})
		}
	}
	return nil
}
