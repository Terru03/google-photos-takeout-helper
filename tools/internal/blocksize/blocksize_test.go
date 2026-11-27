package blocksize

import "testing"

func TestParse(t *testing.T) {
	valid := map[string]int64{
		"65536":  64 * 1024,
		"64K":    64 * 1024,
		"64k":    64 * 1024,
		"64KiB":  64 * 1024,
		"64 KB":  64 * 1024,
		"4M":     4 * 1024 * 1024,
		"64MB":   64 * 1024 * 1024,
		" 64m ":  64 * 1024 * 1024,
		"1G":     1024 * 1024 * 1024,
		"1536K":  1536 * 1024,
		"131072": 128 * 1024,
	}
	for input, want := range valid {
		got, err := Parse(input)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "0", "-64K", "abc", "64X", "4K", "2G", "65537", "9999999999G"} {
		if got, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) = %d; want error", input, got)
		}
	}
}
