package config

import "strings"

// ExpandEnv resolves ${NAME} references in a service's env values against
// base, returning a new map. The input is not modified.
//
// Values are literal today, so nothing can refer to anything — which is why
// DEVRUN_URL_<SERVICE> cannot reach a framework that filters env by prefix.
// Vite exposes only VITE_-prefixed variables to client code, Create React App
// only REACT_APP_, and devrun cannot know which. The bridge belongs in config:
//
//	env:
//	  VITE_API_URL: "${DEVRUN_URL_API}"
//
// Expansion is deliberately narrow:
//
//   - ${NAME} only. Bare $NAME is left alone, because env values are full of
//     literal dollars — passwords, awk scripts, jq filters — and silently
//     eating them would be worse than not expanding at all.
//   - $$ is a literal $, the escape for a value that really wants ${...}.
//   - An unset name expands to empty rather than erroring. A service whose URL
//     does not exist yet should start with the variable blank, not fail.
//   - base is the inherited environment plus what devrun injects. Entries in
//     this map cannot see each other: it is a map, so Go's iteration order is
//     random, and sibling references would resolve differently run to run.
func ExpandEnv(env map[string]string, base func(string) (string, bool)) map[string]string {
	if len(env) == 0 {
		return env
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = expand(v, base)
	}
	return out
}

func expand(s string, base func(string) (string, bool)) string {
	if !strings.Contains(s, "$") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			b.WriteByte(s[i])
			continue
		}
		switch {
		case i+1 < len(s) && s[i+1] == '$':
			b.WriteByte('$')
			i++ // the pair is one literal dollar
		case i+1 < len(s) && s[i+1] == '{':
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				// Unterminated: not a reference, so it stays as written
				// rather than swallowing the rest of the value.
				b.WriteByte('$')
				continue
			}
			name := s[i+2 : i+2+end]
			if v, ok := base(name); ok {
				b.WriteString(v)
			}
			i += 2 + end
		default:
			// A bare $, left exactly as it was.
			b.WriteByte('$')
		}
	}
	return b.String()
}
