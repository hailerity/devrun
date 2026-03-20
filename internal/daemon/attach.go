package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/ipc"
)

func (s *supervisor) handleAttach(conn net.Conn, raw json.RawMessage) {
	var p ipc.AttachPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		_ = ipc.WriteMessage(conn, ipc.Response{OK: false, Error: "invalid attach payload"})
		return
	}

	s.mu.RLock()
	svc := s.services[p.Name]
	s.mu.RUnlock()

	if svc == nil || svc.state.Status != config.StatusRunning {
		_ = ipc.WriteMessage(conn, ipc.Response{OK: false,
			Error: fmt.Sprintf("%s is not running. Start it with 'procet start %s'.", p.Name, p.Name)})
		return
	}
	if svc.state.ReAdopted {
		_ = ipc.WriteMessage(conn, ipc.Response{OK: false,
			Error: fmt.Sprintf("%s was re-adopted after a daemon restart. Restart it with 'procet start %s' to enable attach.", p.Name, p.Name)})
		return
	}

	svc.mu.Lock()
	if svc.attached != nil {
		svc.mu.Unlock()
		_ = ipc.WriteMessage(conn, ipc.Response{OK: false,
			Error: fmt.Sprintf("%s is already attached to another terminal", p.Name)})
		return
	}
	svc.attached = conn
	svc.mu.Unlock()

	// Send attach confirmation; raw byte stream begins immediately after
	_ = ipc.WriteMessage(conn, ipc.Response{OK: true})

	// PTY → client: handled by teeOutput goroutine (already writing to svc.attached)
	// Client → PTY: handled here
	proxyClientToPTY(conn, svc)

	// Cleanup
	svc.mu.Lock()
	svc.attached = nil
	svc.mu.Unlock()
}

// proxyClientToPTY reads raw bytes from the client socket and writes them to the PTY.
// It exits when it receives a DetachRequest JSON frame or the connection closes.
//
// Protocol: raw bytes flow freely until the client sends a length-prefixed DetachRequest.
// We use a bufio.Reader so we can reliably read the full 4-byte length prefix and body
// even if the TCP stack fragments the write across multiple Read() calls.
func proxyClientToPTY(conn net.Conn, svc *managedService) {
	r := bufio.NewReader(conn)
	for {
		// Read the 4-byte length prefix using Peek so we don't consume bytes yet.
		header, err := r.Peek(4)
		if err != nil {
			break // connection closed or error
		}
		msgLen := int(header[0])<<24 | int(header[1])<<16 | int(header[2])<<8 | int(header[3])

		// Check if this could be a control frame (DetachRequest is ~40 bytes).
		// We treat frames under 1KB as potential control frames; larger is definitely PTY data.
		if msgLen > 0 && msgLen < 1024 {
			// Try to read the full frame
			frameBuf := make([]byte, 4+msgLen)
			if _, readErr := io.ReadFull(r, frameBuf); readErr == nil {
				var req ipc.Request
				if json.Unmarshal(frameBuf[4:], &req) == nil && req.Type == "detach" {
					_ = ipc.WriteMessage(conn, ipc.Response{OK: true})
					return
				}
				// Not a detach frame — forward raw bytes to PTY
				if svc.proc != nil {
					_, _ = svc.proc.PTY.Write(frameBuf)
				}
				continue
			}
		}

		// Read one byte at a time for raw PTY data (avoids blocking on partial reads)
		b, err := r.ReadByte()
		if err != nil {
			break
		}
		if svc.proc != nil {
			_, _ = svc.proc.PTY.Write([]byte{b})
		}
	}
}
