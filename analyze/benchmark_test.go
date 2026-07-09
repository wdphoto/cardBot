package analyze

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkAnalyze1000Files(b *testing.B) {
	card := b.TempDir()
	dcim := filepath.Join(card, "DCIM", "100MEDIA")
	if err := os.MkdirAll(dcim, 0o755); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		path := filepath.Join(dcim, fmt.Sprintf("IMG_%04d.JPG", i))
		if err := os.WriteFile(path, []byte("benchmark metadata fixture"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	analyzer := New(card)
	analyzer.SetWorkers(4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := analyzer.Analyze(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
