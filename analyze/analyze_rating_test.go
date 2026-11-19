package analyze

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyze_MissingRatingSidecarIsNotAnUnratedPhoto(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFile{"IMG.JPG": {size: 3}, "IMG.XMP": {size: 4}})
	a := New(card)
	// The final progress sample separates EXIF decoding from the rating merge.
	a.OnProgress(func(int) {
		if err := os.Remove(filepath.Join(card, "DCIM", "IMG.XMP")); err != nil {
			t.Fatal(err)
		}
	})
	result, err := a.Analyze(context.Background())
	if result != nil || err == nil || !strings.Contains(err.Error(), "IMG.XMP") {
		t.Fatalf("result=%+v err=%v, want incomplete analysis", result, err)
	}
}

func TestIsPhoto(t *testing.T) {
	if !IsPhoto("NEF") {
		t.Error("Expected NEF to be a photo")
	}
	if IsPhoto("MOV") {
		t.Error("Expected MOV not to be a photo")
	}
}

func TestIsVideo(t *testing.T) {
	if !IsVideo("MOV") {
		t.Error("Expected MOV to be a video")
	}
	if IsVideo("NEF") {
		t.Error("Expected NEF not to be a video")
	}
}

func TestAnalyze_ExternalXMPRating(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcim := filepath.Join(root, "DCIM", "100NIKON")
	if err := os.MkdirAll(dcim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcim, "DSC_0001.JPG"), []byte("not real jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcim, "DSC_0001.XMP"), []byte(`<rdf:Description xmp:Rating="4"/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("100NIKON", "DSC_0001.JPG")
	if got := result.FileRatings[rel]; got != 4 {
		t.Fatalf("rating = %d, want 4", got)
	}
	if result.Starred != 1 {
		t.Fatalf("Starred = %d, want 1", result.Starred)
	}
}
