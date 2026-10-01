package gateway

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func body(t *testing.T, s *Server, r *http.Request) string {
	t.Helper()
	w := serve(s, r)
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func TestIndex_ListsEverythingRunningWhenLocal(t *testing.T) {
	s := server(t, Config{Version: "v1.4.0"}, Snapshot{Routes: []Route{
		running("web", 4200),
		running("api", 3000),
		{Name: "db", State: "stopped"},
		{Name: "worker", State: "crashed"},
	}})

	out := body(t, s, get("", "/"))
	for _, name := range []string{"web", "api", "db", "worker"} {
		assert.Containsf(t, out, name, "%s should be listed", name)
	}
	assert.Contains(t, out, `href="http://web.localhost:7788/"`, "a running service is a link")
	assert.Contains(t, out, ":4200")
	assert.Contains(t, out, "crashed", "a service that cannot be reached says why")
	assert.Contains(t, out, "4 services · 2 running")
	assert.Contains(t, out, "devrun v1.4.0")
}

func TestIndex_PublishedShowsOnlyTheAllowlist(t *testing.T) {
	s := server(t,
		Config{Posture: PostureForced, Mode: Subdomain},
		Snapshot{Routes: []Route{running("web", 4200), running("admin", 9000)}, Exposed: []string{"web"}},
	)

	out := body(t, s, get("devrun.example.com", "/"))
	assert.Contains(t, out, "web.devrun.example.com", "subdomain mode advertises the hostname")
	assert.Contains(t, out, "https://", "a published gateway is reached over TLS")
	assert.NotContains(t, out, "admin", "a withheld service is not even named")
	assert.Contains(t, out, "1 of 2 published")
	assert.Contains(t, out, "public")
}

// Two different emptinesses, and conflating them would send someone hunting a
// bug that is not there.
func TestIndex_EmptyStates(t *testing.T) {
	nothing := server(t, Config{}, Snapshot{})
	out := body(t, nothing, get("", "/"))
	assert.Contains(t, out, "Nothing is running")
	assert.Contains(t, out, "devrun start")

	withheld := server(t, Config{Posture: PostureForced},
		Snapshot{Routes: []Route{running("web", 4200), running("api", 3000)}})
	out = body(t, withheld, get("", "/"))
	assert.Contains(t, out, "Nothing is published")
	assert.Contains(t, out, "2 service(s) are running")
	assert.Contains(t, out, "devrun gateway expose")
}

func TestIndex_RowsThatCannotBeLinked(t *testing.T) {
	s := server(t, Config{}, Snapshot{Routes: []Route{
		{Name: "tauri", State: "running", Port: 0},
	}})

	out := body(t, s, get("", "/"))
	assert.Contains(t, out, "port unknown")
	assert.NotContains(t, out, `href="/tauri/"`, "a link that 503s is worse than a row explaining itself")
}

func TestIndex_PathModeCarriesTheCaveatOnce(t *testing.T) {
	snap := Snapshot{Routes: []Route{running("web", 4200), running("api", 3000)}}

	path := body(t, server(t, Config{Mode: Path}, snap), get("", "/"))
	assert.Equal(t, 1, strings.Count(path, "Served under a path"),
		"the caveat sits above the list once, not on every row")
	assert.Contains(t, path, `href="/web/"`)

	sub := body(t, server(t, Config{Mode: Subdomain}, snap), get("", "/"))
	assert.NotContains(t, sub, "Served under a path")
	assert.Contains(t, sub, "web.localhost:7788", "subdomain links keep the gateway's port")
}

// An explicit routes table is already a set of paths, so the index must
// advertise paths whatever Mode says.
func TestIndex_RoutesTableForcesPathLinks(t *testing.T) {
	s := server(t,
		Config{Mode: Subdomain, Rules: map[string]Rule{"/api": {Service: "api"}}},
		Snapshot{Routes: []Route{running("api", 3000)}},
	)
	out := body(t, s, get("", "/"))
	assert.Contains(t, out, `href="/api/"`)
	assert.NotContains(t, out, "api.localhost")
}

// No webfont, no CDN, no JavaScript. A tool serving a page from your own machine
// should not phone home, and once published a font CDN would be handed the IP of
// everyone the link is shared with.
//
// Only resource loads count — an <a href> to a service is a navigation the user
// chooses, not something the page fetches on their behalf.
func TestIndex_MakesNoExternalRequests(t *testing.T) {
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", 4200)}})
	out := body(t, s, get("", "/"))

	for _, forbidden := range []string{"<script", "<link", "<img", "<iframe", "src=", "@import", "url("} {
		assert.NotContainsf(t, out, forbidden,
			"the page must fetch nothing: found %q", forbidden)
	}
}

func TestStatusPages(t *testing.T) {
	s := server(t, Config{}, Snapshot{Routes: []Route{{Name: "db", State: "stopped"}}})

	down := serve(s, get("", "/db/"))
	assert.Equal(t, http.StatusServiceUnavailable, down.Code)
	assert.Contains(t, down.Body.String(), "db is not running")
	assert.Contains(t, down.Body.String(), "devrun start db")

	missing := serve(s, get("", "/nope/"))
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.Contains(t, missing.Body.String(), "No service here")
}

// Withheld and absent must be indistinguishable from outside.
func TestStatusPages_WithheldLooksExactlyLikeAbsent(t *testing.T) {
	s := server(t, Config{Posture: PostureForced},
		Snapshot{Routes: []Route{running("admin", 9000)}})

	withheld := serve(s, get("devrun.example.com", "/admin/"))
	absent := serve(s, get("devrun.example.com", "/nope/"))

	assert.Equal(t, absent.Code, withheld.Code)
	assert.Equal(t, absent.Body.String(), withheld.Body.String())
	assert.NotContains(t, withheld.Body.String(), "admin")
}

func TestIndex_IsNotCached(t *testing.T) {
	s := server(t, Config{}, Snapshot{Routes: []Route{running("web", 4200)}})
	assert.Equal(t, "no-store", serve(s, get("", "/")).Header().Get("Cache-Control"),
		"which services are up changes under the reader")
}

func TestItoa(t *testing.T) {
	for n, want := range map[int]string{0: "0", 1: "1", 9: "9", 10: "10", 4200: "4200"} {
		assert.Equal(t, want, itoa(n))
	}
}

// Round 1 of review flagged that `/admin/` 404s while admin.host/ renders the
// index, and proposed 404ing both. That would be backwards: a stranger probing
// subdomains would then see 404 for a withheld service and 200 for a name that
// does not exist, which is the enumeration vector. Compared like with like,
// each URL shape is already indistinguishable — pinned here, since the original
// test only covered paths.
func TestWithheldIsIndistinguishable_InBothUrlShapes(t *testing.T) {
	s := server(t, Config{Posture: PostureForced, Auth: AuthNone},
		Snapshot{Routes: []Route{running("admin", 9000), running("web", 4200)}, Exposed: []string{"web"}})

	withheldHost := serve(s, get("admin.devrun.example.com", "/"))
	unknownHost := serve(s, get("zzz.devrun.example.com", "/"))
	assert.Equal(t, unknownHost.Code, withheldHost.Code, "host form: status must not differ")
	assert.NotContains(t, withheldHost.Body.String(), "9000",
		"the index must not describe a service it is withholding")

	withheldPath := serve(s, get("devrun.example.com", "/admin/"))
	unknownPath := serve(s, get("devrun.example.com", "/zzz/"))
	assert.Equal(t, unknownPath.Code, withheldPath.Code, "path form: status must not differ")
	assert.Equal(t, unknownPath.Body.String(), withheldPath.Body.String())
}
