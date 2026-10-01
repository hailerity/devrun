package process_test

import (
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Real `lsof -a -p <pid> -i -n -P -FpnT` output: one process, an IPv4 listener,
// an IPv6 listener and an established connection that must not be counted.
const lsofFields = `p4242
f3
n127.0.0.1:7411
TST=LISTEN
TQR=0
TQS=0
f4
n[::1]:7412
TST=LISTEN
TQR=0
TQS=0
f5
n127.0.0.1:52000->127.0.0.1:443
TST=ESTABLISHED
TQR=0
TQS=0
`

func TestParseLsofListenPorts(t *testing.T) {
	got := process.ParseLsofListenPorts(lsofFields)
	assert.Equal(t, map[int][]int{4242: {7411, 7412}}, got,
		"both listeners, IPv6 included; the ESTABLISHED socket is not a port")
}

func TestParseLsofListenPorts_AttributesEachSocketToItsPid(t *testing.T) {
	// One lsof call covers a whole process tree, so rows must not be pooled.
	out := "p100\nf3\nn*:3000\nTST=LISTEN\np200\nf3\nn*:9229\nTST=LISTEN\n"
	assert.Equal(t, map[int][]int{100: {3000}, 200: {9229}}, process.ParseLsofListenPorts(out))
}

func TestParseLsofListenPorts_Empty(t *testing.T) {
	assert.Empty(t, process.ParseLsofListenPorts(""))
	assert.Empty(t, process.ParseLsofListenPorts("p1\nf3\nn*:3000\nTST=ESTABLISHED\n"))
}

// /proc/net/tcp is the whole network namespace, so the inode column is the only
// thing tying a row to the process that owns it. 0BB8 = 3000, 240D = 9229.
const procNetTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0000000000000000 100 0 0 10 0
   1: 00000000:240D 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 222 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1F90 0100007F:CB20 01 00000000:00000000 00:00000000 00000000  1000        0 333 1 0000000000000000 100 0 0 10 0
`

func TestParseProcNetTCPListen(t *testing.T) {
	got := process.ParseProcNetTCPListen(procNetTCP)
	assert.Equal(t, map[string]int{"111": 3000, "222": 9229}, got,
		"keyed by inode; the ESTABLISHED row (state 01) is skipped")
}

func TestParseProcNetTCPListen_Empty(t *testing.T) {
	assert.Empty(t, process.ParseProcNetTCPListen(""))
	assert.Empty(t, process.ParseProcNetTCPListen("header only\n"))
}

// The regression this whole file exists for. Both platforms had a way to answer
// confidently about the wrong process — lsof ORs its selectors without -a, and
// /proc/<pid>/net/tcp lists the entire namespace — so a service's reported port
// could belong to something else running on the machine.
func TestDetectPort_ReportsTheServicesOwnPort(t *testing.T) {
	p, want := startListeningService(t)

	require.Eventually(t, func() bool {
		got, _ := process.DetectPort(p.Pid)
		return got == want
	}, 15*time.Second, 250*time.Millisecond,
		"DetectPort never reported the child's own port :%d", want)
}

// startListeningService launches the test binary as a real devrun service —
// sh -c, on a PTY — bound to a free port, and returns it with that port.
func startListeningService(t *testing.T) (*process.Process, int) {
	t.Helper()

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	require.NoError(t, probe.Close()) // hand the port to the child

	self, err := os.Executable()
	require.NoError(t, err)

	p, err := process.Start(
		fmt.Sprintf("'%s' -test.run='^TestPortListenHelper$'", self),
		t.TempDir(),
		map[string]string{"DEVRUN_TEST_LISTEN": fmt.Sprintf("127.0.0.1:%d", port)},
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop() })
	go func() { _, _ = io.Copy(io.Discard, p.PTY) }() // an unread PTY blocks the child
	return p, port
}

// lsof exits non-zero when any requested pid cannot be examined, while still
// reporting the ones it could. A batch covers whole process trees, so a pid
// going away mid-call is routine — and must not cost every other service its
// port. This failed when DetectPorts judged lsof by its exit status.
func TestDetectPorts_SurvivesADeadPidInTheBatch(t *testing.T) {
	p, want := startListeningService(t)

	require.Eventually(t, func() bool {
		// 0x7FFFFFFF cannot be a live pid; lsof will complain and exit non-zero.
		return process.DetectPorts([]int{p.Pid, 0x7FFFFFFF})[p.Pid] == want
	}, 15*time.Second, 250*time.Millisecond,
		"a dead pid in the batch lost the live service's port :%d", want)
}

func TestDetectPorts_NoListenerIsAbsent(t *testing.T) {
	assert.Empty(t, process.DetectPorts(nil))
}

// pid 0 is the parent of init/launchd, so walking down from it reaches every
// process on the machine — it once came back with :53 from the DNS resolver.
func TestDetectPorts_NonPidsAreNotWalked(t *testing.T) {
	assert.Empty(t, process.DetectPorts([]int{0}))
	assert.Empty(t, process.DetectPorts([]int{-1}))
}

// TestPortListenHelper is not a test. It is the child process spawned by
// TestDetectPort_ReportsTheServicesOwnPort, re-execing this binary the way a
// real service is launched — under sh -c, on a PTY.
func TestPortListenHelper(t *testing.T) {
	addr := os.Getenv("DEVRUN_TEST_LISTEN")
	if addr == "" {
		t.Skip("helper process; only runs when the parent test asks for it")
	}
	ln, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	time.Sleep(30 * time.Second)
}
