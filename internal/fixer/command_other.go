//go:build !windows

package fixer

import (
	"context"
	"os/exec"
)

func newHiddenCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func newHiddenCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
