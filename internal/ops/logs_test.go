package ops

import (
	"bufio"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLog(t *testing.T, name, content string) {
	t.Helper()
	p := config.LogPath(name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0644))
}

// scanLinesAll is the reference: how `devrun logs` has always split a file.
func scanLinesAll(content string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func lastN(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// The backward reader must split exactly like bufio.ScanLines — the CLI's
// output depends on it — including across block boundaries, which a tiny block
// size forces on almost every line.
func TestLogs_MatchesScanLinesAcrossBlockEdges(t *testing.T) {
	sandbox(t)
	defer func(b int) { logBlock = b }(logBlock)

	fixed := []string{
		"", "\n", "\n\n", "a", "a\n", "a\n\n", "\na", "a\r\nb\r\n", "a\r", "\r\n",
		"one\ntwo\nthree", "one\ntwo\nthree\n", strings.Repeat("x", 50) + "\n" + strings.Repeat("y", 7),
	}
	rng := rand.New(rand.NewSource(1))
	alphabet := []byte("ab\n\r\n日")
	for i := 0; i < 300; i++ {
		b := make([]byte, rng.Intn(60))
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		fixed = append(fixed, string(b))
	}

	for _, block := range []int{1, 2, 3, 7, 64 * 1024} {
		logBlock = block
		for _, content := range fixed {
			writeLog(t, "svc", content)
			all := scanLinesAll(content)
			for _, n := range []int{1, 2, 5, 1000} {
				res, err := Logs("svc", LogQuery{Lines: n})
				require.NoError(t, err)
				want := lastN(all, n)
				if len(want) == 0 {
					want = []string{}
				}
				require.Equal(t, want, res.Lines, "block=%d n=%d content=%q", block, n, content)
				assert.Equal(t, len(all) > n, res.Truncated, "block=%d n=%d content=%q", block, n, content)
			}
		}
	}
}

func TestLogs_MissingFile(t *testing.T) {
	sandbox(t)
	_, err := Logs("never", LogQuery{Lines: 10})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoLogs))
	assert.Equal(t, `no logs found for "never". Has it been started before?`, err.Error(),
		"exactly the text the CLI has always printed")
}

// Offset is where a follower resumes: a line appended after the snapshot is not
// in it, and starts exactly at Offset.
func TestLogs_OffsetMarksTheEndOfTheSnapshot(t *testing.T) {
	sandbox(t)
	writeLog(t, "svc", "a\nb\n")
	res, err := Logs("svc", LogQuery{Lines: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(4), res.Offset)

	f, err := os.OpenFile(config.LogPath("svc"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, _ = f.WriteString("c\n")
	require.NoError(t, f.Close())
	data, err := os.ReadFile(config.LogPath("svc"))
	require.NoError(t, err)
	assert.Equal(t, "c\n", string(data[res.Offset:]))
}

func TestLogs_ZeroOrNegativeLinesReturnsNothing(t *testing.T) {
	sandbox(t)
	writeLog(t, "svc", "a\nb\n")
	for _, n := range []int{0, -3} {
		res, err := Logs("svc", LogQuery{Lines: n})
		require.NoError(t, err)
		assert.Empty(t, res.Lines)
	}
}

func TestLogs_GrepCountsMatchingLines(t *testing.T) {
	sandbox(t)
	writeLog(t, "svc", "INFO boot\nERROR one\nINFO tick\n\x1b[31mError\x1b[0m two\nINFO tick\nerror three\nINFO done\n")

	res, err := Logs("svc", LogQuery{Lines: 2, Grep: "ERROR"})
	require.NoError(t, err)
	assert.Equal(t, []string{"\x1b[31mError\x1b[0m two", "error three"}, res.Lines,
		"case-insensitive, matched through colour codes, the last N matches")
	assert.True(t, res.Truncated, "an earlier match exists")

	res, err = Logs("svc", LogQuery{Lines: 10, Grep: "error"})
	require.NoError(t, err)
	assert.Len(t, res.Lines, 3)
	assert.False(t, res.Truncated)

	res, err = Logs("svc", LogQuery{Lines: 10, Grep: "absent"})
	require.NoError(t, err)
	assert.Empty(t, res.Lines)
}

func TestLogs_PlainStripsControlSequences(t *testing.T) {
	sandbox(t)
	writeLog(t, "svc", "\x1b[32mready\x1b[0m\r\x1b[2K on :8080\x07\n")
	res, err := Logs("svc", LogQuery{Lines: 1, Plain: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"ready on :8080"}, res.Lines)

	raw, err := Logs("svc", LogQuery{Lines: 1})
	require.NoError(t, err)
	assert.Contains(t, raw.Lines[0], "\x1b[32m", "raw mode keeps the bytes, as the CLI prints them")
}

func TestLogs_MaxBytesDropsOldestFirst(t *testing.T) {
	sandbox(t)
	writeLog(t, "svc", "aaaa\nbbbb\ncccc\ndddd\n")
	res, err := Logs("svc", LogQuery{Lines: 100, MaxBytes: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"cccc", "dddd"}, res.Lines)
	assert.True(t, res.Truncated)
}

// A single line bigger than the cap keeps its end — usually where the error is
// — and stays valid UTF-8 even when the cut lands inside a multi-byte rune.
func TestLogs_OversizedLineKeepsItsTail(t *testing.T) {
	sandbox(t)
	long := strings.Repeat("日本", 100) + " the actual error"
	writeLog(t, "svc", "earlier\n"+long+"\n")
	for _, limit := range []int{1, 3, 4, 20, 21, 22, 23} {
		res, err := Logs("svc", LogQuery{Lines: 5, MaxBytes: limit})
		require.NoError(t, err)
		require.Len(t, res.Lines, 1, "limit=%d", limit)
		got := res.Lines[0]
		assert.True(t, utf8.ValidString(got), "limit=%d got %q", limit, got)
		assert.LessOrEqual(t, len(got), max(limit, len("…")), "limit=%d", limit)
		assert.True(t, strings.HasPrefix(got, "…"))
		assert.True(t, res.Truncated)
	}
	res, err := Logs("svc", LogQuery{Lines: 5, MaxBytes: 40})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(res.Lines[0], " the actual error"))
}

// The reader must not load the whole file: with a tiny block, fetching two
// lines from the end of a large log touches only the last blocks.
func TestLogs_ReadsOnlyTheEnd(t *testing.T) {
	sandbox(t)
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString("filler line that is long enough to matter\n")
	}
	b.WriteString("second to last\nlast\n")
	writeLog(t, "svc", b.String())

	calls := 0
	f, err := os.Open(config.LogPath("svc"))
	require.NoError(t, err)
	defer f.Close()
	var got []string
	info, err := f.Stat()
	require.NoError(t, err)
	require.NoError(t, scanBackward(f, info.Size(), func(line string) bool {
		calls++
		got = append(got, line)
		return len(got) < 2
	}))
	assert.Equal(t, []string{"last", "second to last"}, got)
	assert.Equal(t, 2, calls, "stops as soon as the caller has enough")
}
