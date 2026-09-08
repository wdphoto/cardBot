package dotfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_PreservesPreexistingTemporary(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		name := "regular"
		if symlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			card := t.TempDir()
			oldTemp := filepath.Join(card, fileName+".tmp")
			media := filepath.Join(card, "photo.NEF")
			original := []byte("synthetic media; never truncate")
			if err := os.WriteFile(media, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if symlink {
				if err := os.Symlink(media, oldTemp); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			} else if err := os.WriteFile(oldTemp, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := Write(WriteOptions{CardPath: card, Mode: "all"}); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{media, oldTemp} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, original) {
					t.Fatalf("changed %s: %q, %v", filepath.Base(path), got, err)
				}
			}
			if !Read(card).Copied {
				t.Fatal("new ingest history missing")
			}
			entries, err := os.ReadDir(card)
			if err != nil || len(entries) != 3 {
				t.Fatalf("unexpected leftovers: %v, %v", entries, err)
			}
		})
	}
}

func TestWrite_UnreadableTargetLeavesNoTemporary(t *testing.T) {
	t.Parallel()
	card := t.TempDir()
	// A directory at the history path must fail before creating a temporary.
	target := filepath.Join(card, fileName)
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(WriteOptions{CardPath: card, Mode: "all"}); err == nil {
		t.Fatal("expected error for directory target")
	}
	entries, err := os.ReadDir(card)
	if err != nil || len(entries) != 1 || entries[0].Name() != fileName {
		t.Fatalf("failed write changed card: %v, %v", entries, err)
	}
}
