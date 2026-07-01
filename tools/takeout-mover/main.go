package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func main() {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}

	helperDir := filepath.Dir(exePath)
	ps1Path := filepath.Join(helperDir, "TakeoutZipMover.ps1")

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps1Path)
	cmd.Dir = helperDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		panic(err)
	}
}
