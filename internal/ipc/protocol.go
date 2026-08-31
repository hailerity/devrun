package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/hailerity/devrun/internal/config"
)

// Request is the envelope for all CLI→daemon requests.
type Request struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Response is the envelope for all daemon→CLI responses.
type Response struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
	EOF     bool            `json:"eof,omitempty"`
}

// --- Specific payload types ---

// StartPayload names the service to start. Config, when set, carries the full
// service definition inline (used for project devrun.yaml services that are not
// in the global registry); when nil the daemon resolves Name from the registry.
type StartPayload struct {
	Name   string                `json:"name"`
	Config *config.ServiceConfig `json:"config,omitempty"`
}
type StartResponsePayload struct{ PID int `json:"pid"` }
type StopPayload struct{ Name string `json:"name"` }
type RemovePayload struct{ Name string `json:"name"` }
type AttachPayload struct{ Name string `json:"name"` }
type DetachPayload struct{ Name string `json:"name"` }

// TargetStartPayload starts every service in a target. Services carries the full
// definitions inline — the daemon has no target vocabulary of its own — the same
// way project devrun.yaml services are shipped in StartPayload.
type TargetStartPayload struct {
	Name     string                  `json:"name"`
	Services []*config.ServiceConfig `json:"services"`
}

// TargetStopPayload stops a target. The daemon resolves which members to stop
// from the snapshot it recorded at target-start time, skipping any still held by
// another active target.
type TargetStopPayload struct{ Name string `json:"name"` }

type ServiceInfo struct {
	Name      string  `json:"name"`
	Group     string  `json:"group"`
	State     string  `json:"state"`
	PID       *int    `json:"pid"`
	Port      *int    `json:"port"`
	UptimeSec int64   `json:"uptime_s"`
	CPUPct    float64 `json:"cpu_pct"`
	MemBytes  int64   `json:"mem_bytes"`
}

type ListResponsePayload struct {
	Services []ServiceInfo `json:"services"`
	// ActiveTargets names the targets the daemon currently considers started,
	// sorted. Empty when no target has been started.
	ActiveTargets []string `json:"active_targets,omitempty"`
}

// --- Framing: 4-byte big-endian uint32 length prefix + JSON body ---

const maxMessageSize = 10 * 1024 * 1024 // 10 MB sanity limit

// WriteMessage encodes msg as JSON and writes it with a 4-byte big-endian length prefix.
func WriteMessage(w io.Writer, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("write length prefix: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write message body: %w", err)
	}
	return nil
}

// ReadMessage reads a length-prefixed JSON message and decodes it into dst.
func ReadMessage(r io.Reader, dst interface{}) error {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return fmt.Errorf("read length prefix: %w", err)
	}
	if length > maxMessageSize {
		return fmt.Errorf("message too large: %d bytes", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("read message body: %w", err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("unmarshal message: %w", err)
	}
	return nil
}
