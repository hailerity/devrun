package process_test

import (
	"testing"

	"github.com/hailerity/devrun/internal/process"
	"github.com/stretchr/testify/assert"
)

func TestParseLsofPort(t *testing.T) {
	// Typical lsof output line: "procname PID user ... TCP *:3000 (LISTEN)"
	output := `COMMAND   PID  USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
node     1234 alice   22u  IPv4 0x1234abc      0t0  TCP *:3000 (LISTEN)
node     1234 alice   23u  IPv4 0x1234def      0t0  TCP 127.0.0.1:52000->127.0.0.1:3000 (ESTABLISHED)`

	port, err := process.ParseLsofPort(output)
	assert.NoError(t, err)
	assert.Equal(t, 3000, port)
}

func TestParseLsofPort_NoListen(t *testing.T) {
	output := "no relevant lines"
	_, err := process.ParseLsofPort(output)
	assert.Error(t, err)
}

func TestParseProcNetTCP(t *testing.T) {
	// /proc/net/tcp format: sl local_address rem_address st ...
	// local_address is hex: 00000000:0BB8 = 0.0.0.0:3000
	// 0BB8 hex = 3000 decimal; state 0A = LISTEN
	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000   1000        0 12345 1 0000000000000000 100 0 0 10 0`

	port, err := process.ParseProcNetTCP(content)
	assert.NoError(t, err)
	assert.Equal(t, 3000, port)
}
