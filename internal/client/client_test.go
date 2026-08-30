package client_test

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

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

// A daemon that accepts the connection and the request but never replies must
// not hang the client forever.
func TestClient_SendTimesOut(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req ipc.Request
		_ = ipc.ReadMessage(conn, &req)
		<-time.After(2 * time.Second) // never send a response
	}()

	c, err := client.Connect(sockPath)
	require.NoError(t, err)
	defer c.Close()
	c.SetTimeout(150 * time.Millisecond)

	start := time.Now()
	_, err = c.Send("list", struct{}{})
	require.Error(t, err)
	assert.Less(t, time.Since(start), time.Second, "Send should give up near the deadline")
}
