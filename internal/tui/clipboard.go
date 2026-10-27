package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// errNoClipboard means the system has no clipboard command to use.
var errNoClipboard = errors.New("no clipboard command")

// clipboards are the commands that set the system clipboard from their
// input, in the order to try them, and what each needs to work.
var clipboards = []struct {
	goos string   // the system it is for, or "" for any
	env  string   // a variable that must be set, if any
	args []string // the command
}{
	{"darwin", "", []string{"pbcopy"}},
	{"windows", "", []string{"clip.exe"}},
	{"", "WAYLAND_DISPLAY", []string{"wl-copy"}},
	{"", "DISPLAY", []string{"xclip", "-selection", "clipboard"}},
	{"", "DISPLAY", []string{"xsel", "--clipboard", "--input"}},
}

// clipboardCommand puts s on the system clipboard with the first clipboard
// command there is, and returns its name.
func clipboardCommand(s string) (string, error) {
	for _, c := range clipboards {
		if c.goos != "" && c.goos != runtime.GOOS || c.env != "" && os.Getenv(c.env) == "" {
			continue
		}
		path, err := exec.LookPath(c.args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c.args[1:]...)
		cmd.Stdin = strings.NewReader(s)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				err = fmt.Errorf("%s: %s", c.args[0], msg)
			}
			return c.args[0], err
		}
		return c.args[0], nil
	}
	return "", errNoClipboard
}
