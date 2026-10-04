package cli

import (
	"testing"

	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
)

// The URL column costs width, so it appears only when there is something to
// put in it — most users are local with no gateway up.
func TestListHeadersFor(t *testing.T) {
	assert.Equal(t, listHeaders, listHeadersFor([]ipc.ServiceInfo{{Name: "web"}}),
		"no URLs, no column")

	with := listHeadersFor([]ipc.ServiceInfo{{Name: "web"}, {Name: "api", URL: "http://x/"}})
	assert.Equal(t, append(append([]string{}, listHeaders...), "URL"), with,
		"one service with a URL is enough to earn the column")
}

func TestServiceRowCellsFor(t *testing.T) {
	headers := listHeadersFor([]ipc.ServiceInfo{{URL: "http://x/"}})

	withURL := serviceRowCellsFor(ipc.ServiceInfo{Name: "api", URL: "http://x/"}, headers)
	assert.Equal(t, "http://x/", withURL[len(withURL)-1])

	// A service the gateway cannot reach still gets a row, with the reason
	// visible as an absent URL rather than a link that would 503.
	without := serviceRowCellsFor(ipc.ServiceInfo{Name: "web"}, headers)
	assert.Equal(t, "-", without[len(without)-1])

	// Without the column, the row is unchanged.
	narrow := serviceRowCellsFor(ipc.ServiceInfo{Name: "web", URL: "http://x/"}, listHeaders)
	assert.Len(t, narrow, len(listHeaders))
}
