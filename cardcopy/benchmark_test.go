package cardcopy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkPlanCopy1000Files(b *testing.B) {
	card := b.TempDir()
	dcim := filepath.Join(card, "DCIM", "100MEDIA")
	if err := os.MkdirAll(dcim, 0o755); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		path := filepath.Join(dcim, fmt.Sprintf("IMG_%04d.JPG", i))
		if err := os.WriteFile(path, make([]byte, 1024), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	opts := Options{CardPath: card, DestBase: b.TempDir(), DryRun: true, NamingMode: "timestamp"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PlanCopy(context.Background(), opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyBytes64MiB(b *testing.B) {
	dir := b.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	data := make([]byte, 64<<20)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		b.Fatal(err)
	}
	buf := make([]byte, 256<<10)
	b.SetBytes(int64(len(data) * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := verifyBytes(src, dst, buf); err != nil {
			b.Fatal(err)
		}
	}
}
