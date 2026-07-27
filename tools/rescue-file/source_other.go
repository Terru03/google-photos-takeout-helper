//go:build !windows

package main

import (
	"os"
	"time"
)

func openRescueSource(path string, _ time.Duration) (sourceReader, error) {
	return os.Open(path)
}
