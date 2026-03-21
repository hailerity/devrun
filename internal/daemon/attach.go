package daemon

import (
	"encoding/json"
	"fmt"
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
// It exits when the connection closes (client detached or stopped the service).
func proxyClientToPTY(conn net.Conn, svc *managedService) {
	buf := make([]byte, 256)
	for {
		n, err := conn.Read(buf)
		if n > 0 && svc.proc != nil {
			_, _ = svc.proc.PTY.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
}
