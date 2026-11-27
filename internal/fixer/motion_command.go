package fixer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

const motionCommandStopGrace = 15 * time.Second

func runHiddenCommandWithTimeout(
	ctx context.Context,
	timeout time.Duration,
	name string,
	args ...string,
) ([]byte, error, bool) {
	outputFile, err := os.CreateTemp("", "gtf-motionphoto2-*.log")
	if err != nil {
		return nil, err, false
	}
	outputPath := outputFile.Name()
	defer func() {
		_ = os.Remove(outputPath)
	}()

	command := newHiddenCommand(name, args...)
	command.Stdout = outputFile
	command.Stderr = outputFile
	if err := command.Start(); err != nil {
		_ = outputFile.Close()
		return nil, err, false
	}

	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	timedOut := false
	var runErr error
	select {
	case runErr = <-wait:
	case <-ctx.Done():
		runErr = stopMotionCommandTree(command, wait)
		runErr = errors.Join(ctx.Err(), runErr)
	case <-timer.C:
		timedOut = true
		runErr = stopMotionCommandTree(command, wait)
		runErr = errors.Join(context.DeadlineExceeded, runErr)
	}

	_ = outputFile.Close()
	output, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		runErr = errors.Join(runErr, readErr)
	}
	return output, runErr, timedOut
}

func stopMotionCommandTree(command *exec.Cmd, wait <-chan error) error {
	killErr := killCommandTree(command)
	timer := time.NewTimer(motionCommandStopGrace)
	defer timer.Stop()
	select {
	case waitErr := <-wait:
		return errors.Join(killErr, waitErr)
	case <-timer.C:
		fallbackErr := command.Process.Kill()
		return errors.Join(killErr, fallbackErr, errors.New("motionphoto2 process tree did not exit"))
	}
}
