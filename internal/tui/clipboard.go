package tui

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
)

type clipboardBackend struct {
	cmd  string
	args []string
}

type clipboard struct {
	backend *clipboardBackend
}

// detectClipboard checks for clipboard backends at startup.
func detectClipboard() clipboard {
	return detectClipboardWith(func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
}

// detectClipboardWith is the testable form; lookup returns true if the named
// command exists on PATH.
func detectClipboardWith(lookup func(string) bool) clipboard {
	if runtime.GOOS == "darwin" {
		if lookup("pbcopy") {
			return clipboard{backend: &clipboardBackend{cmd: "pbcopy"}}
		}
		return clipboard{}
	}
	if lookup("xclip") {
		return clipboard{backend: &clipboardBackend{
			cmd:  "xclip",
			args: []string{"-selection", "clipboard"},
		}}
	}
	if lookup("xsel") {
		return clipboard{backend: &clipboardBackend{
			cmd:  "xsel",
			args: []string{"--clipboard", "--input"},
		}}
	}
	return clipboard{}
}

// Available reports whether a clipboard backend was found.
func (c clipboard) Available() bool { return c.backend != nil }

// Copy writes text to the system clipboard.
func (c clipboard) Copy(text string) error {
	if !c.Available() {
		return fmt.Errorf("no clipboard backend")
	}
	cmd := exec.Command(c.backend.cmd, c.backend.args...)
	cmd.Stdin = bytes.NewBufferString(text)
	return cmd.Run()
}
