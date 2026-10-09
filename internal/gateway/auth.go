package gateway

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// tokenCookie holds the key once it has been handed over, so it appears in the
// URL exactly once rather than on every link the user then clicks.
const tokenCookie = "devrun_key"

// authPath is where the key form posts. It serves no service: a name created
// through devrun cannot start with "_" (config.ValidateName), so this cannot
// collide with one. A hand-edited devrun.yaml could still name a service
// "_devrun", in which case the form shadows it.
const authPath = "/_devrun/auth"

// maxKeyForm bounds the body the key form will read. The form carries one short
// field, so anything larger is not it.
const maxKeyForm = 4 << 10

// authorize gates a request when the posture calls for it. It reports whether
// the caller may continue; when it returns false it has already answered.
//
// Four ways in: the form at authPath, the cookie set by a previous handover,
// the ?k= in a shared link, and a bearer header for anything that is not a
// browser.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if !s.NeedsToken(r) {
		return true
	}

	// Before the cookie, so the form is reachable even while holding one: a key
	// that has been rotated leaves a cookie that no longer matches, and the page
	// to paste the new one must not depend on the old one being wrong.
	if r.URL.Path == authPath {
		return s.keyForm(w, r)
	}

	if c, err := r.Cookie(tokenCookie); err == nil && tokenMatches(c.Value, s.cfg.Token) {
		return true
	}

	if key := r.URL.Query().Get("k"); key != "" {
		if !tokenMatches(key, s.cfg.Token) {
			s.denied(w, r)
			return false
		}
		s.bankKey(w, r, key)
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

// keyForm handles authPath, where the denial page's form posts the key. It never
// lets a request through to routing, because no service is served there: it
// either banks the key and redirects, or answers as any other denial.
//
// A form is not a weaker gate than the ?k= link. The token is 128 bits of
// randomness, so neither can be guessed, and a script can post to one as easily
// as it can fetch the other. It is the stronger of the two for the person using
// it: a posted key never enters the address bar, the browser's history, a
// Referer header or the tunnel's request log, whereas ?k= is in a URL for one
// round trip before it is traded away.
func (s *Server) keyForm(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		// A GET here is someone who came to paste a key — the operator with the
		// token in their terminal, most likely — so it is the form, not an error.
		s.denied(w, r)
		return false
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxKeyForm)
	if err := r.ParseForm(); err != nil {
		s.denied(w, r)
		return false
	}

	key := r.PostFormValue("k")
	if !tokenMatches(key, s.cfg.Token) {
		s.denied(w, r)
		return false
	}

	s.bankKey(w, r, key)
	// The index, not the page they were denied: carrying a destination through
	// the form would make the denial page differ by the path that was asked for,
	// and that page is byte-identical for every path on purpose.
	http.Redirect(w, r, "/", http.StatusFound)
	return false
}

// bankKey stores a key that has just been checked, so it stops riding along in
// the address bar, in Referer headers, and in whatever the user copies out of
// it.
func (s *Server) bankKey(w http.ResponseWriter, r *http.Request, key string) {
	http.SetCookie(w, &http.Cookie{
		Name:     tokenCookie,
		Value:    key,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

// denied answers a request with no usable key. It says where the key is read
// from, offers the form to paste it into, and answers a wrong key exactly as it
// answers a missing one so it never confirms a near miss — which is also why
// the page carries no destination and says nothing about which of the two was
// wrong.
//
// It does not say why a key is being asked for. "It is published" is only one
// of the reasons: auth: always demands one on a gateway that is not published
// at all, and naming a tunnel there sends anyone running that setup looking for
// a tunnel they never started.
func (s *Server) denied(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="devrun"`)
	s.statusForm(w, r, http.StatusUnauthorized,
		"This gateway needs a key.",
		"devrun gateway status prints it, on the machine serving this. Paste it below, or open the full link you were sent — that carries the key.")
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
