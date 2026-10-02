package gateway

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const key = "k_7f3a9c21"

func published(t *testing.T, cfg Config, snap Snapshot) *Server {
	t.Helper()
	cfg.Posture = PostureForced
	cfg.Token = key
	return server(t, cfg, snap)
}

func oneService(port int) Snapshot {
	return Snapshot{Routes: []Route{running("web", port)}, Exposed: []string{"web"}}
}

// A local gateway grants nothing localhost:<port> did not already grant the same
// caller, so it must not ask for anything.
func TestAuth_LocalAsksForNothing(t *testing.T) {
	s := server(t, Config{Token: key}, oneService(4200))
	assert.Equal(t, http.StatusOK, serve(s, get("", "/")).Code)
}

func TestAuth_PublishedWithoutAKey(t *testing.T) {
	s := published(t, Config{}, oneService(4200))

	w := serve(s, get("devrun.example.com", "/"))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "needs a key")
	assert.Equal(t, `Bearer realm="devrun"`, w.Header().Get("WWW-Authenticate"))
}

// The key appears in the URL exactly once: it is traded for a cookie and the
// browser is sent to the clean address, so it stops riding along in the address
// bar, in Referer, and in whatever the user copies out of it.
func TestAuth_QueryKeyIsTradedForACookie(t *testing.T) {
	s := published(t, Config{}, oneService(4200))

	w := serve(s, get("devrun.example.com", "/?k="+key+"&keep=1"))
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/?keep=1", w.Header().Get("Location"), "only k is dropped")

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, tokenCookie, cookies[0].Name)
	assert.Equal(t, key, cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly, "no script needs to read it")
	assert.Equal(t, "/", cookies[0].Path)
}

func TestAuth_CookieCarriesTheSession(t *testing.T) {
	port, _ := upstream(t, nil)
	s := published(t, Config{}, oneService(port))

	r := get("devrun.example.com", "/web/")
	r.AddCookie(&http.Cookie{Name: tokenCookie, Value: key})
	assert.Equal(t, http.StatusOK, serve(s, r).Code)
}

func TestAuth_BearerForNonBrowsers(t *testing.T) {
	s := published(t, Config{}, oneService(4200))

	r := get("devrun.example.com", "/")
	r.Header.Set("Authorization", "Bearer "+key)
	assert.Equal(t, http.StatusOK, serve(s, r).Code)

	r = get("devrun.example.com", "/")
	r.Header.Set("Authorization", "Bearer wrong")
	assert.Equal(t, http.StatusUnauthorized, serve(s, r).Code)
}

// A wrong key is answered exactly as a missing one, so the page never confirms
// a near miss.
func TestAuth_WrongKeyLooksLikeNoKey(t *testing.T) {
	s := published(t, Config{}, oneService(4200))

	wrong := serve(s, get("devrun.example.com", "/?k=nope"))
	none := serve(s, get("devrun.example.com", "/"))

	assert.Equal(t, none.Code, wrong.Code)
	assert.Equal(t, none.Body.String(), wrong.Body.String())
	assert.Empty(t, wrong.Result().Cookies(), "a wrong key is never stored")
}

// The gate runs before resolution: whether a name exists is itself something a
// caller without a key is not entitled to learn.
func TestAuth_GateComesBeforeResolution(t *testing.T) {
	s := published(t, Config{}, oneService(4200))

	real := serve(s, get("devrun.example.com", "/web/"))
	fake := serve(s, get("devrun.example.com", "/nope/"))

	assert.Equal(t, http.StatusUnauthorized, real.Code)
	assert.Equal(t, fake.Code, real.Code)
	assert.Equal(t, fake.Body.String(), real.Body.String())
}

func TestAuth_Modes(t *testing.T) {
	snap := oneService(4200)

	none := server(t, Config{Posture: PostureForced, Auth: AuthNone, Token: key}, snap)
	assert.Equal(t, http.StatusOK, serve(none, get("devrun.example.com", "/")).Code,
		"auth: none opts out even when published")

	always := server(t, Config{Auth: AuthAlways, Token: key}, snap)
	assert.Equal(t, http.StatusUnauthorized, serve(always, get("", "/")).Code,
		"auth: always keeps the token on locally, for a shared machine")

	// Nothing to check against: a gateway with no token cannot demand one.
	untokened := server(t, Config{Posture: PostureForced, Auth: AuthAlways}, snap)
	assert.Equal(t, http.StatusOK, serve(untokened, get("devrun.example.com", "/")).Code)
}

func TestAuth_SecureCookieBehindTLS(t *testing.T) {
	s := published(t, Config{}, oneService(4200))

	r := get("devrun.example.com", "/?k="+key)
	r.Header.Set("X-Forwarded-Proto", "https")
	cookies := serve(s, r).Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].Secure, "a tunnel terminates TLS, so the cookie must not leak over http")
}

func TestCleanURL(t *testing.T) {
	for in, want := range map[string]string{
		"/?k=abc":            "/",
		"/web/?k=abc":        "/web/",
		"/web/?k=abc&page=2": "/web/?page=2",
		"/web/?page=2&k=abc": "/web/?page=2",
		"/deep/path?k=abc":   "/deep/path",
	} {
		assert.Equal(t, want, cleanURL(get("", in)), in)
	}
}

func TestTokenMatches(t *testing.T) {
	assert.True(t, tokenMatches("abc", "abc"))
	assert.False(t, tokenMatches("abc", "abd"))
	assert.False(t, tokenMatches("ab", "abc"), "a prefix is not a match")
	assert.False(t, tokenMatches("", ""), "an unset token can never be matched")
	assert.False(t, tokenMatches("anything", ""))
}

// A 302 rewrites a POST to a GET and drops its body, so a non-GET request
// carrying the key could never authenticate. Only a navigation gains anything
// from the clean address; everything else keeps the request it made.
func TestAuth_QueryKeyDoesNotRedirectNonNavigations(t *testing.T) {
	port, hits := upstream(t, nil)
	s := published(t, Config{}, oneService(port))

	r := get("devrun.example.com", "/web/items?k="+key)
	r.Method = http.MethodPost
	w := serve(s, r)

	assert.Equal(t, http.StatusOK, w.Code, "the POST is served, not redirected")
	require.Len(t, *hits, 1)
	assert.Equal(t, http.MethodPost, (*hits)[0].Method, "the method survives")
	require.Len(t, w.Result().Cookies(), 1, "and the key is still banked for next time")
}
