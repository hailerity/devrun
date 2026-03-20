package client

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/hailerity/procet/internal/ipc"
)

// Client is a connection to the procet daemon.
type Client struct {
	conn net.Conn
}

// Connect opens a Unix socket connection to the daemon.
func Connect(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Conn returns the underlying connection (used during attach for raw byte streaming).
func (c *Client) Conn() net.Conn { return c.conn }

// Send sends a typed request and reads a single JSON response.
func (c *Client) Send(reqType string, payload interface{}) (*ipc.Response, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	req := ipc.Request{Type: reqType, Payload: json.RawMessage(p)}
	if err := ipc.WriteMessage(c.conn, req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	var resp ipc.Response
	if err := ipc.ReadMessage(c.conn, &resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}
