package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkDiscoverMediaPlan(b *testing.B) {
	root := b.TempDir()
	yearDir := filepath.Join(root, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		b.Fatal(err)
	}

	for i := 0; i < 200; i++ {
		name := fmt.Sprintf("IMG_%04d.jpg", i)
		if err := os.WriteFile(filepath.Join(yearDir, name), []byte("image"), 0o644); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(yearDir, name+".json"), []byte(fmt.Sprintf(`{"title":%q,"photoTakenTime":{"timestamp":"1700000000"}}`, name)), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plans, err := DiscoverMediaPlan(root, ProcessOptions{})
		if err != nil {
			b.Fatal(err)
		}
		if len(plans) != 200 {
			b.Fatalf("expected 200 plans, got %d", len(plans))
		}
	}
}
