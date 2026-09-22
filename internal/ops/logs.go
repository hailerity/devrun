package ops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/termtext"
)

// ErrNoLogs means the service has no log file — it has never been started.
// Logs returns an error matching it (errors.Is).
var ErrNoLogs = errors.New("no logs")

// noLogsError carries the message the CLI has always printed.
type noLogsError struct{ name string }

func (e noLogsError) Error() string {
	return fmt.Sprintf("no logs found for %q. Has it been started before?", e.name)
}
func (e noLogsError) Is(target error) bool { return target == ErrNoLogs }

// LogQuery selects which lines of a service's log to return.
type LogQuery struct {
	// Lines is how many lines to return, counted from the end. Zero or less
	// returns nothing.
	Lines int
	// Grep, when set, keeps only lines containing it, case-insensitively, and
	// Lines then counts matching lines.
	Grep string
	// MaxBytes caps the total size of the returned lines. Zero means no cap.
	// When the cap bites, the oldest lines are dropped first, and a single line
	// longer than the cap is cut.
	MaxBytes int
	// Plain strips every terminal control sequence from each line. Raw output
	// keeps colour codes and control bytes, as the CLI prints them.
	Plain bool
}

// LogResult is a snapshot of the end of a service's log.
type LogResult struct {
	Path  string
	Lines []string // oldest first
	// Offset is the file size the snapshot was read up to. A follower that
	// seeks here picks up exactly the lines written after the snapshot.
	Offset int64
	// Truncated is true when earlier lines exist that were not returned —
	// because of Lines or MaxBytes — or when a line was cut to fit MaxBytes.
	Truncated bool
}

// logBlock is how much of the file is read per step when scanning backwards. A
// variable only so tests can shrink it to put line breaks across block edges.
var logBlock = 64 * 1024

// Logs returns the last lines of a service's log. It reads the file backwards
// from the end, so the cost is proportional to what is returned (or, with Grep,
// to how far back the matches go) rather than to the size of the file.
//
// Lines are split the way bufio.ScanLines splits them: on "\n", with one
// trailing "\r" removed, and a final line without a newline still counted.
func Logs(name string, q LogQuery) (*LogResult, error) {
	path := config.LogPath(name)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, noLogsError{name}
	}
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat log: %w", err)
	}
	res := &LogResult{Path: path, Offset: info.Size()}
	if q.Lines <= 0 {
		return res, nil
	}
	needle := strings.ToLower(q.Grep)

	var newestFirst []string
	size := 0
	complete := true // reached the start of the file without stopping early
	err = scanBackward(f, res.Offset, func(line string) bool {
		if q.Plain {
			line = termtext.Plain(line)
		}
		if needle != "" && !strings.Contains(strings.ToLower(termtext.Plain(line)), needle) {
			return true
		}
		if len(newestFirst) == q.Lines {
			complete = false // one more exists than we will return
			return false
		}
		if q.MaxBytes > 0 && size+len(line) > q.MaxBytes {
			if len(newestFirst) == 0 {
				// The newest line alone is over the cap: keep its tail — the
				// end of a line is usually the part that matters.
				newestFirst = append(newestFirst, keepTail(line, q.MaxBytes))
			}
			complete = false
			return false
		}
		newestFirst = append(newestFirst, line)
		size += len(line)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("tail log: %w", err)
	}

	res.Lines = make([]string, len(newestFirst))
	for i, l := range newestFirst {
		res.Lines[len(newestFirst)-1-i] = l
	}
	res.Truncated = !complete
	return res, nil
}

// keepTail cuts line to at most max bytes by dropping its start, marking the cut
// with "…". The cut moves forward to a rune boundary so the result stays valid
// UTF-8, which is also why it can come out a few bytes under max.
func keepTail(line string, max int) string {
	const mark = "…"
	keep := max - len(mark)
	if keep <= 0 {
		return mark
	}
	start := len(line) - keep
	for start < len(line) && !utf8.RuneStart(line[start]) {
		start++
	}
	return mark + line[start:]
}

// scanBackward calls fn with each line of f[:size] from the last to the first,
// until fn returns false or the start of the file is reached. Reading only up to
// a size fixed by the caller keeps the snapshot consistent while the service is
// still appending.
func scanBackward(f *os.File, size int64, fn func(line string) bool) error {
	pos := size
	var carry []byte // the start of a line whose beginning is in an earlier block
	first := true
	buf := make([]byte, logBlock)
	for pos > 0 {
		n := int64(len(buf))
		if pos < n {
			n = pos
		}
		pos -= n
		if _, err := f.ReadAt(buf[:n], pos); err != nil && err != io.EOF {
			return err
		}
		chunk := append(append([]byte(nil), buf[:n]...), carry...)
		if first {
			// A file ending in "\n" has no empty line after it.
			chunk = bytes.TrimSuffix(chunk, []byte("\n"))
			first = false
		}
		for {
			i := bytes.LastIndexByte(chunk, '\n')
			if i < 0 {
				break
			}
			if !fn(string(bytes.TrimSuffix(chunk[i+1:], []byte("\r")))) {
				return nil
			}
			chunk = chunk[:i]
		}
		carry = chunk
	}
	// What is left is the file's first line — possibly empty, as in a file that
	// starts with "\n". Only an empty file has no first line.
	if size > 0 {
		fn(string(bytes.TrimSuffix(carry, []byte("\r"))))
	}
	return nil
}
