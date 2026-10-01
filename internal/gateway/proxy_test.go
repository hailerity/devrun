package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upstream stands in for a dev server. It echoes back what it was asked, so a
// test can assert on the path and Host the gateway actually sent.
func upstream(t *testing.T, h http.HandlerFunc) (port int, hits *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(r.Context()))
		if h != nil {
			h(w, r)
			return
		}
		_, _ = io.WriteString(w, "upstream ok")
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	p, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return p, &seen
}

func serve(s *Server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestProxy_StripsThePrefixAndRewritesHost(t *testing.T) {
	port, hits := upstream(t, nil)
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", port)}})

	w := serve(s, get("", "/web/assets/app.js?v=2"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "upstream ok", w.Body.String())

	require.Len(t, *hits, 1)
	got := (*hits)[0]
	assert.Equal(t, "/assets/app.js", got.URL.Path, "the service never sees its own mount point")
	assert.Equal(t, "v=2", got.URL.RawQuery, "the query survives")
	assert.Equal(t, "127.0.0.1:"+strconv.Itoa(port), got.Host,
		"dev servers reject a Host they do not know, so it is rewritten")
	assert.Equal(t, "localhost:7788", got.Header.Get("X-Forwarded-Host"),
		"the name the eyeball used is still recoverable")
}

func TestProxy_SubdomainStripsNothing(t *testing.T) {
	port, hits := upstream(t, nil)
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", port)}})

	require.Equal(t, http.StatusOK, serve(s, get("web.localhost:7788", "/assets/app.js")).Code)
	require.Len(t, *hits, 1)
	assert.Equal(t, "/assets/app.js", (*hits)[0].URL.Path)
}

func TestProxy_PreserveHostWhenAsked(t *testing.T) {
	port, hits := upstream(t, nil)
	s := server(t, Config{HostHeader: HostPreserve}, Snapshot{Routes: []Route{running("web", port)}})

	require.Equal(t, http.StatusOK, serve(s, get("web.localhost:7788", "/")).Code)
	require.Len(t, *hits, 1)
	assert.Equal(t, "web.localhost:7788", (*hits)[0].Host)
}

// A document served at /web resolves its relative links against /, so every
// asset would be requested one directory too high.
func TestProxy_RedirectsToTrailingSlash(t *testing.T) {
	port, _ := upstream(t, nil)
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", port)}})

	w := serve(s, get("", "/web"))
	assert.Equal(t, http.StatusPermanentRedirect, w.Code)
	assert.Equal(t, "/web/", w.Header().Get("Location"))

	w = serve(s, get("", "/web?a=1"))
	assert.Equal(t, "/web/?a=1", w.Header().Get("Location"), "the query comes along")
}

func TestProxy_PutsThePrefixBackOnRedirects(t *testing.T) {
	port, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Location", "/dashboard")
		case "/self":
			w.Header().Set("Location", "http://127.0.0.1:"+r.Host[strings.LastIndex(r.Host, ":")+1:]+"/dashboard")
		case "/away":
			w.Header().Set("Location", "https://accounts.google.com/signin")
		case "/sibling":
			w.Header().Set("Location", "next")
		}
		w.WriteHeader(http.StatusFound)
	})
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", port)}})

	assert.Equal(t, "/web/dashboard", serve(s, get("", "/web/login")).Header().Get("Location"),
		"a path-only redirect must stay inside the mount point")
	assert.Equal(t, "/web/dashboard", serve(s, get("", "/web/self")).Header().Get("Location"),
		"an absolute URL back to the upstream is the service talking about itself")
	assert.Equal(t, "https://accounts.google.com/signin", serve(s, get("", "/web/away")).Header().Get("Location"),
		"a real elsewhere is left alone")
	assert.Equal(t, "next", serve(s, get("", "/web/sibling")).Header().Get("Location"),
		"a directory-relative redirect is already correct")
}

func TestProxy_NarrowsCookiePaths(t *testing.T) {
	port, _ := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "sid=abc; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "pref=dark; Path=/settings")
		w.Header().Add("Set-Cookie", "bare=1; HttpOnly")
		w.WriteHeader(http.StatusOK)
	})
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", port)}})

	got := serve(s, get("", "/web/")).Result().Header.Values("Set-Cookie")
	require.Len(t, got, 3)
	assert.Equal(t, "sid=abc; Path=/web; HttpOnly", got[0],
		"a whole-origin cookie would otherwise be overwritten by a sibling service")
	assert.Equal(t, "pref=dark; Path=/web/settings", got[1])
	assert.Equal(t, "bare=1; HttpOnly", got[2],
		"no Path attribute: the browser already scopes it to this directory")
}

func TestProxy_StatusesForWhatCannotBeProxied(t *testing.T) {
	s := server(t, Config{}, Snapshot{Routes: []Route{
		{Name: "db", State: "stopped", Port: 5432},
		running("admin", 9000),
	}, Exposed: []string{}})

	assert.Equal(t, http.StatusServiceUnavailable, serve(s, get("", "/db/")).Code)
	assert.Equal(t, http.StatusNotFound, serve(s, get("", "/nope/")).Code)

	// Withheld reads exactly as absent: that a service exists but is not shared
	// is itself something a stranger should not learn.
	published := server(t, Config{Posture: PostureForced}, Snapshot{Routes: []Route{running("admin", 9000)}})
	assert.Equal(t, http.StatusNotFound, serve(published, get("", "/admin/")).Code)
}

// The service is listed as running but nothing answers — it is still coming up,
// or it died between the snapshot and the request.
func TestProxy_DeadUpstreamIsABadGateway(t *testing.T) {
	port, _ := upstream(t, nil)
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", port+1)}})

	w := serve(s, get("", "/web/"))
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "web did not answer")
}

func TestStripPrefix(t *testing.T) {
	for _, tc := range []struct {
		path, prefix, want string
	}{
		{"/web/assets/app.js", "/web", "/assets/app.js"},
		{"/web/", "/web", "/"},
		{"/web", "/web", "/"},
		{"/assets/app.js", "", "/assets/app.js"},
		// /webhook is not /web's to strip, however much it looks like it.
		{"/webhook/x", "/web", "/webhook/x"},
	} {
		assert.Equalf(t, tc.want, stripPrefix(tc.path, tc.prefix), "%q under %q", tc.path, tc.prefix)
	}
}
