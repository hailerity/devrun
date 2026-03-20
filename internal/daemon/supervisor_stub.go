package daemon

import (
	"log/slog"
	"net"
)

// supervisor stub — replaced by full implementation in supervisor.go (Task 11)
type supervisor struct{}

func newSupervisor(socketPath string, logger *slog.Logger) *supervisor {
	return &supervisor{}
}
func (s *supervisor) loadState() error         { return nil }
func (s *supervisor) handleConn(conn net.Conn) { conn.Close() }
func (s *supervisor) startPortPoller()         {}
func (s *supervisor) shutdown()                {}
