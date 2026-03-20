package daemon

import (
	"encoding/json"
	"net"
)

// handleAttach stub — replaced by full implementation in attach.go (Task 13)
func (s *supervisor) handleAttach(conn net.Conn, raw json.RawMessage) {
	_ = raw
	conn.Close()
}
