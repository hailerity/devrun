package gateway

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// tokenCookie holds the key once it has been handed over, so it appears in the
// URL exactly once rather than on every link the user then clicks.
const tokenCookie = "devrun_key"

// authorize gates a request when the posture calls for it. It reports whether
// the caller may continue; when it returns false it has already answered.
//
// Three ways in, in the order they are cheapest to check: the cookie set by a
// previous handover, the ?k= in a shared link, and a bearer header for anything
// that is not a browser.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if !s.NeedsToken(r) {
		return true
	}

	if c, err := r.Cookie(tokenCookie); err == nil && tokenMatches(c.Value, s.cfg.Token) {
		return true
	}

	if key := r.URL.Query().Get("k"); key != "" {
		if !tokenMatches(key, s.cfg.Token) {
			s.denied(w, r)
			return false
		}
		// Trade the query parameter for a cookie, so the key stops riding along
		// in the address bar, in Referer headers, and in whatever the user
		// copies out of it.
		http.SetCookie(w, &http.Cookie{
			Name:     tokenCookie,
			Value:    key,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		})
		// Only a navigation gains anything from the clean address, and a
		// redirect would turn a POST into a GET and drop its body — so anything
		// else keeps the request it made and proceeds with the cookie set.
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			http.Redirect(w, r, cleanURL(r), http.StatusFound)
			return false
		}
		return true
	}

	if after, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if tokenMatches(strings.TrimSpace(after), s.cfg.Token) {
			return true
		}
	}

	s.denied(w, r)
	return false
}

// denied answers a request with no usable key. There is no login form: there is
// no account to log into, and a password box would invite guessing. It names
// what is missing and where it came from, and answers a wrong key exactly as it
// answers a missing one so it never confirms a near miss.
func (s *Server) denied(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="devrun"`)
	s.statusPage(w, r, http.StatusUnauthorized,
		"This gateway needs a key.",
		"It is published, so it asks for the token printed by devrun tunnel up. Open the full link you were sent — it carries the key.")
}

// tokenMatches compares in constant time, so a wrong key reveals nothing about
// how much of it was right.
func tokenMatches(got, want string) bool {
	if want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// cleanURL is the request's URL with the key taken out of the query.
func cleanURL(r *http.Request) string {
	u := *r.URL
	q := u.Query()
	q.Del("k")
	u.RawQuery = q.Encode()
	// Keep it relative: the gateway does not know whether it is behind https.
	u.Scheme, u.Host = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}
