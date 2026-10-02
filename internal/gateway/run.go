package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Fetcher returns the current view of what the daemon is running. The gateway
// does not know, and must not know, how that is obtained — keeping it a
// function is what lets the whole handler be tested without a daemon.
type Fetcher func(ctx context.Context) (Snapshot, error)

// RefreshEvery is how often the route table is re-read. It matches the TUI's
// poll, which is the established way to follow the daemon's state.
const RefreshEvery = 2 * time.Second

// refreshTimeout bounds one fetch, so a wedged daemon stalls the refresh loop
// rather than the requests being served from the last good snapshot.
const refreshTimeout = 5 * time.Second

// Run binds the gateway and serves until ctx ends. It returns the address it
// actually listened on through addr, which matters when Config.Port is 0 and
// the kernel picks one.
//
// The first fetch happens before the listener is announced, so the gateway
// never serves a confidently empty index while the daemon has services.
func Run(ctx context.Context, cfg Config, fetch Fetcher, announce func(addr string)) error {
	srv := New(cfg)
	srv.SetFetcher(fetch)

	ln, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Bind, err)
	}
	// Re-read the bind from the listener: with port 0 the configured address is
	// not the one in use, and every URL the index prints depends on it.
	srv.cfg.Bind = ln.Addr().String()

	if snap, err := fetchOnce(ctx, fetch); err == nil {
		srv.SetSnapshot(snap)
	}
	if announce != nil {
		announce(srv.cfg.Bind)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.refresh(ctx, fetch)
	}()

	httpSrv := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()

	select {
	case err := <-errc:
		wg.Wait()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// Give in-flight requests a moment; a proxied download should not be
		// cut off because the gateway was asked to stop.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		wg.Wait()
		return nil
	}
}

// refresh keeps the route table current until ctx ends. A failed fetch leaves
// the last good snapshot in place: a daemon restart should not empty the index.
func (s *Server) refresh(ctx context.Context, fetch Fetcher) {
	t := time.NewTicker(RefreshEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if snap, err := fetchOnce(ctx, fetch); err == nil {
				s.SetSnapshot(snap)
			}
		}
	}
}

func fetchOnce(ctx context.Context, fetch Fetcher) (Snapshot, error) {
	if fetch == nil {
		return Snapshot{}, errors.New("no fetcher")
	}
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	return fetch(ctx)
}

// missRefresh re-reads the route table when a request named something the
// current snapshot does not have, so a service started since the last tick is
// reachable at once instead of 404ing for up to RefreshEvery.
//
// Rate-limited: a 404 flood must not turn into a request flood at the daemon,
// which answers list under the supervisor lock.
func (s *Server) missRefresh(ctx context.Context) bool {
	s.mu.Lock()
	fetch := s.fetch
	if fetch == nil || time.Since(s.lastFetch) < missRefreshEvery {
		s.mu.Unlock()
		return false
	}
	s.lastFetch = time.Now()
	s.mu.Unlock()

	snap, err := fetchOnce(ctx, fetch)
	if err != nil {
		return false
	}
	s.SetSnapshot(snap)
	return true
}

// missRefreshEvery bounds how often an unknown name may trigger a fetch.
const missRefreshEvery = time.Second
