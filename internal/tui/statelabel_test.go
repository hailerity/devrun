package tui

import (
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
)

func running(uptime int64, port *int) ipc.ServiceInfo {
	return ipc.ServiceInfo{Name: "svc", State: "running", UptimeSec: uptime, Port: port}
}

// A listening service shows its port, whatever its uptime — that is the whole
// point of detection, and it must not be reachable only inside some window.
func TestStateLabel_AListeningServiceShowsItsPort(t *testing.T) {
	assert.Equal(t, ":5173", stateLabel(running(1, intp(5173))))
	assert.Equal(t, ":5173", stateLabel(running(portDetectSettle*100, intp(5173))))
}

// Early on, "detecting" is honest: the daemon polls every 5s and the port may
// genuinely be about to appear.
func TestStateLabel_SaysDetectingWhileThereIsStillReasonTo(t *testing.T) {
	assert.Equal(t, "detecting", stateLabel(running(0, nil)))
	assert.Equal(t, "detecting", stateLabel(running(portDetectSettle-1, nil)))
}

// But it has to end. A worker, a watcher or a log emitter never binds a port,
// and before this it sat at "detecting" for as long as it ran — describing a
// service that was up and working as though it were still starting.
func TestStateLabel_SettlesOnRunningWhenNoPortArrives(t *testing.T) {
	assert.Equal(t, "running", stateLabel(running(portDetectSettle, nil)))
	assert.Equal(t, "running", stateLabel(running(3600, nil)),
		"an hour in, it is not still detecting anything")
}

// A port found after the label settled still wins: detection does not stop, so
// settling is a description of what is known, not a decision that ends it.
func TestStateLabel_ALatePortStillReplacesTheSettledLabel(t *testing.T) {
	late := running(portDetectSettle*4, nil)
	assert.Equal(t, "running", stateLabel(late))
	late.Port = intp(8080)
	assert.Equal(t, ":8080", stateLabel(late))
}

// Every other state is its own word, settled or not.
func TestStateLabel_OtherStatesAreUnaffected(t *testing.T) {
	for _, state := range []string{"crashed", "stopped", "starting", "stopping", "exited"} {
		assert.Equal(t, state, stateLabel(ipc.ServiceInfo{State: state, UptimeSec: 9999}))
	}
}

// The state column is sized for the longest token it can hold.
func TestStateLabel_FitsTheStateColumn(t *testing.T) {
	for _, label := range []string{"detecting", "running", "stopping"} {
		assert.LessOrEqual(t, len(label), rowStateW, label)
	}
}
