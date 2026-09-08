package cardcopy

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// No fuzz input is ever opened as a filesystem path.
func FuzzDestinationAndNaming(f *testing.F) {
	for _, path := range []string{"IMG.NEF", "100NIKON/IMG.XMP", "../photo.JPG", "../../outside", " CARD  /IMG.NEF", "", "/absolute", "a\\b.NEF"} {
		f.Add("2026-09-08", path, 10000, 4)
	}
	base := f.TempDir()
	capture := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, date, path string, seq, digits int) {
		if len(date)+len(path) > 4096 {
			t.Skip()
		}
		dst, err := safeDestPath(base, date, path)
		if err == nil {
			rel, err := filepath.Rel(base, dst)
			if err != nil || !filepath.IsLocal(rel) {
				t.Fatalf("accepted escape: %q (%v)", dst, err)
			}
		}
		formatted := formatSequence(seq, digits)
		n, err := strconv.Atoi(formatted)
		if err != nil || n != max(1, seq) || len(formatted) < min(5, max(3, digits)) {
			t.Fatalf("sequence %d/%d became %q", seq, digits, formatted)
		}
		renamed := renamedRelativePath(path, capture, seq, digits)
		if filepath.Dir(renamed) != filepath.Dir(path) {
			t.Fatalf("renaming changed parent: %q -> %q", path, renamed)
		}
		if !strings.HasSuffix(renamed, "_"+formatted+strings.ToUpper(filepath.Ext(path))) {
			t.Fatalf("invalid renamed suffix: %q", renamed)
		}
		if again := renamedRelativePath(path, capture, seq, digits); again != renamed {
			t.Fatal("unstable naming")
		}
	})
}

func FuzzVerifyBytes(f *testing.F) {
	f.Add([]byte("photo"), []byte("photo"), uint16(0))
	f.Add([]byte("photo"), []byte("other"), uint16(8193))
	f.Add([]byte{}, []byte("longer"), uint16(8192))
	f.Add(bytes.Repeat([]byte("x"), 4097), bytes.Repeat([]byte("x"), 4097), uint16(8192))
	f.Fuzz(func(t *testing.T, source, dest []byte, size uint16) {
		if len(source)+len(dest) > 16384 {
			t.Skip()
		}
		// Independent read fragmentation must not affect byte identity.
		err := verifyBytes(context.Background(), iotest.OneByteReader(bytes.NewReader(source)), iotest.HalfReader(bytes.NewReader(dest)), make([]byte, int(size)%16385))
		if (err == nil) != bytes.Equal(source, dest) {
			t.Fatalf("verification disagrees with byte equality: %v", err)
		}
	})
}
