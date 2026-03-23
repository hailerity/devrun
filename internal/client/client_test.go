package client_test

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_SendReceive(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	// Start a minimal echo server
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		var req ipc.Request
		_ = ipc.ReadMessage(conn, &req)
		resp := ipc.Response{OK: true, Payload: json.RawMessage(`{"pid":42}`)}
		_ = ipc.WriteMessage(conn, resp)
	}()

	c, err := client.Connect(sockPath)
	require.NoError(t, err)
	defer c.Close()

	resp, err := c.Send("start", ipc.StartPayload{Name: "web"})
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestClient_ConnectionRefused(t *testing.T) {
	_, err := client.Connect(filepath.Join(t.TempDir(), "missing.sock"))
	assert.Error(t, err)
}
