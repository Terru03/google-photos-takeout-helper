//go:build !windows

package fixer

import "os/exec"

func killCommandTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
