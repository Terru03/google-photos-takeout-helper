//go:build !windows

package batch

import "os"

func preserveFileTimestamps(sourcePath string, targetPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	return os.Chtimes(targetPath, info.ModTime(), info.ModTime())
}
