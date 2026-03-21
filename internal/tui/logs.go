package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
)

var httpStatusRe = regexp.MustCompile(`\b([2-5]\d{2})\b`)

type logsPanel struct {
	vp         viewport.Model
	lines      []string
	cursor     int  // line index under cursor (for single-line copy)
	selStart   int  // visual selection start index
	selEnd     int  // visual selection end index
	visualMode bool
	followMode bool

	// file state
	filePath   string
	fileOffset int64
	noLogMsg   string // set when file is missing
}

func newLogsPanel() logsPanel {
	return logsPanel{
		followMode: true,
	}
}

// setFile switches the panel to tail a new log file.
func (lp *logsPanel) setFile(path string) {
	lp.filePath = path
	lp.fileOffset = 0
	lp.lines = nil
	lp.cursor = 0
	lp.visualMode = false
	lp.noLogMsg = ""
}

// poll reads any new lines appended to the log file since the last poll.
// Returns true if new lines were added.
func (lp *logsPanel) poll() bool {
	if lp.filePath == "" {
		return false
	}
	f, err := os.Open(lp.filePath)
	if err != nil {
		lp.noLogMsg = fmt.Sprintf("No logs yet for %s", logName(lp.filePath))
		return false
	}
	defer f.Close()

	if _, err := f.Seek(lp.fileOffset, io.SeekStart); err != nil {
		// File was likely rotated or truncated — re-read from scratch
		lp.fileOffset = 0
		lp.lines = nil
		f.Seek(0, io.SeekStart) //nolint:errcheck
	}
	scanner := bufio.NewScanner(f)
	var added bool
	for scanner.Scan() {
		lp.lines = append(lp.lines, scanner.Text())
		added = true
	}
	lp.fileOffset, _ = f.Seek(0, io.SeekCurrent)
	lp.noLogMsg = ""

	if added && lp.followMode {
		lp.cursor = len(lp.lines) - 1
	}
	return added
}

// rebuildViewport refreshes the viewport content from lp.lines.
func (lp *logsPanel) rebuildViewport() {
	if lp.noLogMsg != "" {
		lp.vp.SetContent(styleMuted.Render(lp.noLogMsg))
		return
	}
	var sb strings.Builder
	for i, line := range lp.lines {
		sb.WriteString(lp.renderLine(i, line))
		sb.WriteByte('\n')
	}
	lp.vp.SetContent(sb.String())
	if lp.followMode {
		lp.vp.GotoBottom()
	}
}

func (lp *logsPanel) renderLine(idx int, line string) string {
	colored := colorizeLog(line)
	if lp.visualMode && idx >= lp.selStart && idx <= lp.selEnd {
		return styleVisualLine.Render(colored)
	}
	if idx == lp.cursor {
		return styleSelectedLine.Render(colored)
	}
	return colored // no wrapper — preserve embedded color codes
}

// colorizeLog wraps HTTP status codes (2xx/4xx/5xx) with palette colors.
func colorizeLog(line string) string {
	return httpStatusRe.ReplaceAllStringFunc(line, func(code string) string {
		switch code[0] {
		case '2':
			return styleGreen.Render(code)
		case '4':
			return styleYellow.Render(code)
		case '5':
			return styleRed.Render(code)
		}
		return code
	})
}

func (lp *logsPanel) copyLine() string {
	if len(lp.lines) == 0 || lp.cursor >= len(lp.lines) {
		return ""
	}
	return lp.lines[lp.cursor]
}

func (lp *logsPanel) copySelection() string {
	if !lp.visualMode || len(lp.lines) == 0 {
		return ""
	}
	start, end := lp.selStart, lp.selEnd
	if start > end {
		start, end = end, start
	}
	if end >= len(lp.lines) {
		end = len(lp.lines) - 1
	}
	return strings.Join(lp.lines[start:end+1], "\n")
}

func (lp *logsPanel) enterVisual() {
	lp.visualMode = true
	lp.selStart = lp.cursor
	lp.selEnd = lp.cursor
}

func (lp *logsPanel) exitVisual() {
	lp.visualMode = false
}

func (lp *logsPanel) moveUp() {
	if lp.cursor > 0 {
		lp.cursor--
		lp.followMode = false
		if lp.visualMode {
			lp.selEnd = lp.cursor
		}
	}
}

func (lp *logsPanel) moveDown() {
	if lp.cursor < len(lp.lines)-1 {
		lp.cursor++
		lp.followMode = false
		if lp.visualMode {
			lp.selEnd = lp.cursor
		}
	}
}

func (lp *logsPanel) resize(w, h int) {
	lp.vp.Width = w
	lp.vp.Height = h
}

// logName extracts "myservice" from "/path/to/logs/myservice.log".
func logName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".log")
}
