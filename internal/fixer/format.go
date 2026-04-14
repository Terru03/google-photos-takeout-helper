package fixer

import "fmt"

func FormatBytes(size int64) string {
	if size <= 0 {
		return "0 B"
	}

	value := float64(size)
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	unitIndex := 0

	for value >= 1024 && unitIndex < len(units)-1 {
		value /= 1024
		unitIndex++
	}

	if unitIndex == 0 {
		return fmt.Sprintf("%d %s", size, units[unitIndex])
	}

	return fmt.Sprintf("%.1f %s", value, units[unitIndex])
}
