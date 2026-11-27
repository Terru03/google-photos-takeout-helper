// Package blocksize parses rescue read sizes shared by tools/rescue-file and
// tools/recovery-runner, so both validate -coarse-block/-reverse-block alike.
package blocksize

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// Default is the rescue-file first-pass read size.
	Default = "64M"
	// Min matches the first refine block, so a coarse pass never reads
	// smaller blocks than the refine pass that follows it.
	Min = int64(64 * 1024)
	Max = int64(1024 * 1024 * 1024)
)

// Parse accepts a byte count with an optional binary suffix:
// "65536", "64K", "64KiB", "4M", "64MB", "1G". Sizes must be a multiple of
// 512 bytes and between 64 KiB and 1 GiB.
func Parse(value string) (int64, error) {
	text := strings.ToUpper(strings.TrimSpace(value))
	multiplier := int64(1)
	for _, unit := range []struct {
		suffix string
		scale  int64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30},
		{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30},
		{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30},
	} {
		if strings.HasSuffix(text, unit.suffix) {
			text = strings.TrimSpace(strings.TrimSuffix(text, unit.suffix))
			multiplier = unit.scale
			break
		}
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("invalid block size %q: use bytes or a K/M/G suffix, such as 64K or 64M", value)
	}
	if number > Max/multiplier {
		return 0, fmt.Errorf("block size %q is larger than 1G", value)
	}
	size := number * multiplier
	if size < Min || size > Max {
		return 0, fmt.Errorf("block size %q must be between 64K and 1G", value)
	}
	if size%512 != 0 {
		return 0, fmt.Errorf("block size %q must be a multiple of 512 bytes", value)
	}
	return size, nil
}
