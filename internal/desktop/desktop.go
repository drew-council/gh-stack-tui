// Package desktop reaches the operator's machine outside the terminal: the
// clipboard, the browser, and the tabs of a terminal multiplexer.
package desktop

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoClipboard is returned by [Copy] when no clipboard tool is installed,
// for example over SSH.
var ErrNoClipboard = errors.New("no clipboard tool found")

// Copy puts text on the system clipboard.
func Copy(text string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		candidates = [][]string{{"clip"}}
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		candidates = append(
			candidates,
			[]string{"xclip", "-selection", "clipboard"},
			[]string{"xsel", "--clipboard", "--input"},
		)
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return ErrNoClipboard
}

// OpenURL opens url in the default browser, without waiting for it.
func OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
