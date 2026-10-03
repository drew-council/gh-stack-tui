//go:build !unix

package tui

import "os/exec"

func detach(*exec.Cmd) {}

func interrupt(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
