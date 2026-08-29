package client

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/hailerity/devrun/internal/ipc"
)

// DefaultTimeout bounds a single Send (write request + read response). It is
// generous enough for the slowest handler — `stop` waits out the process
// termination grace period before replying — while still turning a wedged
// daemon into a prompt error instead of a command that hangs forever.
const DefaultTimeout = 30 * time.Second

// Client is a connection to the devrun daemon.
type Client struct {
	conn    net.Conn
	timeout time.Duration
}

// Connect opens a Unix socket connection to the daemon.
func Connect(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	return &Client{conn: conn, timeout: DefaultTimeout}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Conn returns the underlying connection (used during attach for raw byte streaming).
func (c *Client) Conn() net.Conn { return c.conn }

// SetTimeout overrides the per-Send deadline. A non-positive value disables it
// (use this before switching to raw streaming via Conn).
func (c *Client) SetTimeout(d time.Duration) { c.timeout = d }

// Send sends a typed request and reads a single JSON response.
func (c *Client) Send(reqType string, payload interface{}) (*ipc.Response, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	if c.timeout > 0 {
		_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
		// Clear it so a later Conn()-based stream isn't left with a stale deadline.
		defer func() { _ = c.conn.SetDeadline(time.Time{}) }()
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
