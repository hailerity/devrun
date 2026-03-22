package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type logsPanel struct {
	sb         scrollBuffer
	filePath   string
	fileOffset int64
	noLogMsg   string
}

func newLogsPanel() logsPanel {
	return logsPanel{
		sb: scrollBuffer{followMode: true},
	}
}

// setFile switches the panel to tail a new log file.
func (lp *logsPanel) setFile(path string) {
	lp.filePath = path
	lp.fileOffset = 0
	lp.sb.lines = nil
	lp.sb.cursor = 0
	lp.sb.yOffset = 0
	lp.sb.visualMode = false
	lp.sb.followMode = true
	lp.noLogMsg = ""
}

// poll reads any new lines appended to the log file since the last poll.
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
		lp.fileOffset = 0
		lp.sb.lines = nil
		f.Seek(0, io.SeekStart) //nolint:errcheck
	}
	scanner := bufio.NewScanner(f)
	var added bool
	for scanner.Scan() {
		lp.sb.lines = append(lp.sb.lines, scanner.Text()) // raw, ANSI preserved
		added = true
	}
	lp.fileOffset, _ = f.Seek(0, io.SeekCurrent)
	lp.noLogMsg = ""

	if added && lp.sb.followMode {
		lp.sb.gotoBottom()
	}
	return added
}

// view renders the panel: shows noLogMsg if no file, otherwise delegates to scrollBuffer.
func (lp *logsPanel) view() string {
	if lp.noLogMsg != "" {
		return styleMuted.Render(lp.noLogMsg)
	}
	return lp.sb.View()
}

// colorizeLog wraps HTTP status codes (2xx/4xx/5xx) with palette colors.
var httpStatusRe = regexp.MustCompile(`\b([2-5]\d{2})\b`)

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

// logName extracts "myservice" from "/path/to/logs/myservice.log".
func logName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".log")
}
