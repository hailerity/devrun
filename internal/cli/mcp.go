package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/hailerity/devrun/internal/mcpserver"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP server over stdio so AI agents (Claude Code, Codex, …) can drive devrun",
	Long: `Run a Model Context Protocol server on stdin/stdout.

This is not meant to be run by hand: register it with an agent, which then
starts it and talks to it. For example:

  claude mcp add devrun -- devrun mcp
  codex mcp add devrun -- devrun mcp

The agent can then list, add, start and stop services and targets and read
their logs. Nothing is printed to stdout except protocol messages.`,
	Args: cobra.NoArgs,
	RunE: runMCP,
}

func runMCP(_ *cobra.Command, _ []string) error {
	// Run by hand, the server would sit waiting for JSON-RPC on the terminal.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "devrun mcp speaks the Model Context Protocol on stdin/stdout and is started by an AI agent, not by hand.")
		fmt.Fprintln(os.Stderr, "Register it with your agent, e.g.:")
		fmt.Fprintln(os.Stderr, "  claude mcp add devrun -- devrun mcp")
		fmt.Fprintln(os.Stderr, "  codex mcp add devrun -- devrun mcp")
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := mcpserver.New(Version, mcpserver.Options{Dir: cwd})
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
