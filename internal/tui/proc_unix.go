//go:build unix

package tui

import (
	"os/exec"
	"syscall"
)

// detach runs cmd in a new session: without a controlling terminal, prompts
// that would open /dev/tty fail instead of drawing over the TUI, and the
// session's process group lets interrupt reach the git processes cmd spawns.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// interrupt sends SIGINT to cmd's process group, like ctrl+c in a terminal.
func interrupt(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}
