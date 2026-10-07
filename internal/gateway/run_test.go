package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// start runs a gateway on a kernel-chosen port and returns its base URL.
func start(t *testing.T, cfg Config, fetch Fetcher) string {
	t.Helper()
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1:0"
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	addrc := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, fetch, func(a string) { addrc <- a }) }()

	select {
	case addr := <-addrc:
		t.Cleanup(func() {
			cancel()
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Error("gateway did not shut down")
			}
		})
		return "http://" + addr
	case err := <-done:
		t.Fatalf("gateway exited before listening: %v", err)
		return ""
	case <-time.After(10 * time.Second):
		t.Fatal("gateway never announced an address")
		return ""
	}
}

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // short-lived test request
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

// The first fetch happens before the address is announced, so the gateway never
// serves a confidently empty index while the daemon has services.
func TestRun_ServesTheFirstSnapshotImmediately(t *testing.T) {
	base := start(t, Config{}, func(context.Context) (Snapshot, error) {
		return Snapshot{Routes: []Route{running("web", 4200)}}, nil
	})

	code, body := getBody(t, base+"/")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "web", "the route table was read before the listener was announced")
}

// Port 0 means the kernel picks, and every URL the index prints depends on
// knowing which one it actually got.
func TestRun_ReportsTheRealAddress(t *testing.T) {
	base := start(t, Config{Bind: "127.0.0.1:0", Mode: Path}, func(context.Context) (Snapshot, error) {
		return Snapshot{Routes: []Route{running("web", 4200)}}, nil
	})
	assert.NotContains(t, base, ":0", "the announced address is the one in use")

	code, _ := getBody(t, base+"/")
	assert.Equal(t, http.StatusOK, code)
}

// A daemon restart must not empty the index.
func TestRun_KeepsTheLastGoodSnapshotWhenAFetchFails(t *testing.T) {
	var calls atomic.Int32
	base := start(t, Config{}, func(context.Context) (Snapshot, error) {
		if calls.Add(1) == 1 {
			return Snapshot{Routes: []Route{running("web", 4200)}}, nil
		}
		return Snapshot{}, errors.New("daemon is restarting")
	})

	require.Eventually(t, func() bool { return calls.Load() > 1 }, 10*time.Second, 100*time.Millisecond,
		"the refresh loop should have run at least once more")

	code, body := getBody(t, base+"/")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "web", "a failed refresh leaves the last good view in place")
}

// A name newer than the last poll should not 404 for a whole refresh interval.
func TestMissRefresh_PicksUpAServiceStartedSinceTheLastPoll(t *testing.T) {
	var started atomic.Bool
	fetch := func(context.Context) (Snapshot, error) {
		if started.Load() {
			return Snapshot{Routes: []Route{running("web", 4200)}}, nil
		}
		return Snapshot{}, nil
	}

	s := server(t, Config{}, Snapshot{})
	s.SetFetcher(fetch)

	_, outcome := s.Resolve(get("", "/web/"))
	require.Equal(t, NoSuchRoute, outcome, "not in the snapshot yet")

	started.Store(true)
	require.True(t, s.missRefresh(context.Background()), "a refresh should have been allowed")
	_, outcome = s.Resolve(get("", "/web/"))
	assert.Equal(t, OK, outcome, "and the new service is reachable at once")
}

// A 404 flood must not become a request flood at the daemon, which answers list
// under the supervisor lock.
func TestMissRefresh_IsRateLimited(t *testing.T) {
	var calls atomic.Int32
	s := server(t, Config{}, Snapshot{})
	s.SetFetcher(func(context.Context) (Snapshot, error) {
		calls.Add(1)
		return Snapshot{}, nil
	})

	assert.True(t, s.missRefresh(context.Background()))
	for i := 0; i < 20; i++ {
		assert.False(t, s.missRefresh(context.Background()))
	}
	assert.Equal(t, int32(1), calls.Load(), "one fetch, however many misses")
}

func TestMissRefresh_WithoutAFetcherDoesNothing(t *testing.T) {
	s := server(t, Config{}, Snapshot{})
	assert.False(t, s.missRefresh(context.Background()), "a test server has no daemon to ask")
}

// The point of the miss-refresh limit is to bound how often the daemon is
// asked. A periodic poll asks it too, so it has to count.
func TestMissRefresh_PeriodicPollCountsTowardTheBudget(t *testing.T) {
	var calls atomic.Int32
	s := server(t, Config{}, Snapshot{})
	s.SetFetcher(func(context.Context) (Snapshot, error) {
		calls.Add(1)
		return Snapshot{}, nil
	})

	s.markFetched() // stand in for the refresh loop having just run
	assert.False(t, s.missRefresh(context.Background()),
		"a miss straight after a poll must not ask again")
	assert.Equal(t, int32(0), calls.Load())
}

func TestNewToken(t *testing.T) {
	a, b := NewToken(), NewToken()
	assert.NotEqual(t, a, b, "a predictable key is worse than no gateway")
	assert.True(t, strings.HasPrefix(a, "k_"))
	assert.Len(t, a, 2+32, "128 bits as hex")
}
