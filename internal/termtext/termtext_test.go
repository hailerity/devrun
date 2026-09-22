package termtext

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripUnsafe_KeepsColourDropsTheRest(t *testing.T) {
	in := "\x1b[31mred\x1b[0m\r\x1b[2K\x1b]0;title\x07\x1bMdone\tend"
	assert.Equal(t, "\x1b[31mred\x1b[0mdone    end", StripUnsafe(in))
}

func TestPlain_RemovesEveryControl(t *testing.T) {
	cases := map[string]string{
		"\x1b[1;32mINFO\x1b[0m ready":            "INFO ready",
		"progress 10%\rprogress 100%":            "progress 10%progress 100%",
		"bell\x07 and backspace\x08 and del\x7f": "bell and backspace and del",
		"cut off \x1b":                           "cut off ",
		"tab\there":                              "tab    here",
		"日本語 ✓":                                  "日本語 ✓",
		"":                                       "",
	}
	for in, want := range cases {
		assert.Equal(t, want, Plain(in), "%q", in)
	}
}
