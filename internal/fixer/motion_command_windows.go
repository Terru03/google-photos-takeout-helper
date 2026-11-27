//go:build windows

package fixer

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func killCommandTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	killer := exec.Command(
		"taskkill.exe",
		"/PID", strconv.Itoa(command.Process.Pid),
		"/T",
		"/F",
	)
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := killer.CombinedOutput()
	if err == nil {
		return nil
	}
	fallbackErr := command.Process.Kill()
	if fallbackErr == nil {
		return fmt.Errorf("taskkill failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return fmt.Errorf(
		"taskkill failed: %w: %s; direct kill failed: %v",
		err,
		strings.TrimSpace(string(output)),
		fallbackErr,
	)
}
