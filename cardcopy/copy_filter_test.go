package cardcopy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCopy_Filter(t *testing.T) {
	t.Parallel()
	card := t.TempDir()
	dest := t.TempDir()

	dcim := filepath.Join(card, "DCIM", "100MEDIA")
	if err := os.MkdirAll(dcim, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dcim, "TEST1.JPG"), []byte("jpg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcim, "TEST2.MOV"), []byte("mov"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcim, "TEST3.NEF"), []byte("nef"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcim, "TEST3.XMP"), []byte("xmp"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcim, "OTHER.XMP"), []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{
		CardPath: card,
		DestBase: dest,
		Filter: func(rel string, ext string) bool {
			return ext == "JPG" || ext == "NEF"
		},
	}, nil)

	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if res.FilesCopied != 3 {
		t.Errorf("Expected 3 files copied (two photos plus sidecar), got %d", res.FilesCopied)
	}

	// Verify filtered file was NOT copied anywhere under dest.
	found := false
	if err := filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d != nil && d.Name() == "TEST2.MOV" {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("Expected TEST2.MOV to be filtered out, but it was copied")
	}

	// Verify accepted files WERE copied somewhere under dest.
	for _, name := range []string{"TEST1.JPG", "TEST3.NEF", "TEST3.XMP"} {
		exists := false
		if err := filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d != nil && d.Name() == name {
				exists = true
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("Expected %s to be copied, but it was not found under dest", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "2026-03-08", "100MEDIA", "OTHER.XMP")); !os.IsNotExist(err) {
		t.Fatalf("orphan sidecar should not be selected, stat err=%v", err)
	}
}
