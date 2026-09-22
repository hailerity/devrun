//go:build integration

package integration_test

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCP_StdioRoundTrip drives the real binary the way Claude Code or Codex
// does: spawn `devrun mcp`, speak newline-delimited JSON-RPC on its stdin, and
// read its stdout. Anything on stdout that is not a JSON-RPC message — a stray
// log line, a progress note — would corrupt the stream for a real client, so
// every line is checked.
func TestMCP_StdioRoundTrip(t *testing.T) {
	testEnv(t) // XDG sandbox + in-process daemon; the subprocess inherits the env
	registerService(t, "web", "sleep 30", t.TempDir())

	work := t.TempDir()
	cmd := exec.Command(os.Getenv("DEVRUN_DAEMON_BIN"), "mcp")
	cmd.Dir = work
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	send := func(msg string) {
		_, err := io.WriteString(stdin, msg+"\n")
		require.NoError(t, err)
	}
	type rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int            `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	// next returns the response with the given id, checking every line it
	// passes on the way is a well-formed JSON-RPC 2.0 message.
	next := func(id int) rpc {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				require.True(t, ok, "stdout closed early; stderr:\n%s", stderr.String())
				var m rpc
				require.NoError(t, json.Unmarshal([]byte(line), &m), "stdout carried a non-JSON line: %q", line)
				require.Equal(t, "2.0", m.JSONRPC, "not a JSON-RPC message: %q", line)
				if m.ID != nil && *m.ID == id {
					return m
				}
			case <-deadline:
				t.Fatalf("no response to request %d; stderr:\n%s", id, stderr.String())
			}
		}
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"it","version":"0"}}}`)
	init := next(1)
	require.Empty(t, string(init.Error))
	assert.Contains(t, string(init.Result), `"name":"devrun"`)
	assert.Contains(t, string(init.Result), "devrun manages long-running development processes", "instructions reach the client")
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := next(2)
	for _, name := range []string{"list_services", "service_status", "logs", "add_service", "add_to_target", "start", "stop"} {
		assert.Contains(t, string(tools.Result), `"name":"`+name+`"`)
	}

	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_services","arguments":{}}}`)
	list := next(3)
	require.Empty(t, string(list.Error))
	var res struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Scope    string `json:"scope"`
			Services []struct {
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"services"`
		} `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(list.Result, &res))
	assert.False(t, res.IsError)
	assert.Equal(t, "global", res.StructuredContent.Scope)
	require.Len(t, res.StructuredContent.Services, 1)
	assert.Equal(t, "web", res.StructuredContent.Services[0].Name)

	// Closing stdin is how a client ends the session: the server must exit.
	require.NoError(t, stdin.Close())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("devrun mcp did not exit after stdin closed")
	}
	for line := range lines {
		var m rpc
		assert.NoError(t, json.Unmarshal([]byte(line), &m), "trailing non-JSON on stdout: %q", line)
	}
}

// Run by hand in a terminal, `devrun mcp` explains itself instead of waiting
// for JSON-RPC that will never come. (A pty is what makes stdin a terminal.)
//
// This takes about 5 s, as does any devrun command under script(1): startup
// waits out a terminal query that script's pty never answers. That predates
// the MCP server and does not affect it in use — agents connect over pipes.
func TestMCP_RefusesAnInteractiveTerminal(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script(1) not available to provide a pty")
	}
	testEnv(t)
	bin := os.Getenv("DEVRUN_DAEMON_BIN")
	var cmd *exec.Cmd
	if out, _ := exec.Command("uname").Output(); strings.TrimSpace(string(out)) == "Darwin" {
		cmd = exec.Command("script", "-q", "/dev/null", bin, "mcp")
	} else {
		cmd = exec.Command("script", "-qec", bin+" mcp", "/dev/null")
	}
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "it must exit non-zero")
	assert.Contains(t, string(out), "started by an AI agent, not by hand")
	assert.Contains(t, string(out), "claude mcp add devrun -- devrun mcp")
}
