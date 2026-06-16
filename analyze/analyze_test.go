package analyze

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// createTestCard builds a fake DCIM structure in a temp directory.
// File keys are relative paths from DCIM (e.g. "100NIKON/DSC_0001.NEF").
func createTestCard(t *testing.T, files map[string]testFile) string {
	t.Helper()
	root := t.TempDir()
	for relPath, tf := range files {
		path := filepath.Join(root, "DCIM", relPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, tf.size), 0o644); err != nil {
			t.Fatal(err)
		}
		if !tf.mtime.IsZero() {
			if err := os.Chtimes(path, tf.mtime, tf.mtime); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

type testFile struct {
	size  int
	mtime time.Time
}

func date(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
}

func TestAnalyze_MultiDay(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/DSC_0001.NEF": {size: 50000, mtime: date(2025, 3, 8)},
		"100NIKON/DSC_0002.NEF": {size: 30000, mtime: date(2025, 3, 8)},
		"100NIKON/DSC_0003.JPG": {size: 10000, mtime: date(2025, 3, 8)},
		"100NIKON/DSC_0004.MOV": {size: 90000, mtime: date(2025, 3, 7)},
		"100NIKON/DSC_0005.NEF": {size: 40000, mtime: date(2025, 3, 7)},
	})

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.FileCount != 5 {
		t.Errorf("FileCount = %d, want 5", result.FileCount)
	}
	if result.TotalSize != 220000 {
		t.Errorf("TotalSize = %d, want 220000", result.TotalSize)
	}
	if result.PhotoCount != 4 {
		t.Errorf("PhotoCount = %d, want 4", result.PhotoCount)
	}
	if result.VideoCount != 1 {
		t.Errorf("VideoCount = %d, want 1", result.VideoCount)
	}
	if result.PhotoSize != 130000 {
		t.Errorf("PhotoSize = %d, want 130000", result.PhotoSize)
	}
	if result.VideoSize != 90000 {
		t.Errorf("VideoSize = %d, want 90000", result.VideoSize)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("len(Groups) = %d, want 2", len(result.Groups))
	}

	// Newest first.
	g0 := result.Groups[0]
	if g0.Date != "2025-03-08" {
		t.Errorf("Groups[0].Date = %q, want 2025-03-08", g0.Date)
	}
	if g0.FileCount != 3 {
		t.Errorf("Groups[0].FileCount = %d, want 3", g0.FileCount)
	}
	if g0.Size != 90000 {
		t.Errorf("Groups[0].Size = %d, want 90000", g0.Size)
	}
	wantExts := []string{"JPG", "NEF"}
	if len(g0.Extensions) != len(wantExts) {
		t.Errorf("Groups[0].Extensions = %v, want %v", g0.Extensions, wantExts)
	} else {
		for i, ext := range g0.Extensions {
			if ext != wantExts[i] {
				t.Errorf("Groups[0].Extensions[%d] = %q, want %q", i, ext, wantExts[i])
			}
		}
	}

	g1 := result.Groups[1]
	if g1.Date != "2025-03-07" {
		t.Errorf("Groups[1].Date = %q, want 2025-03-07", g1.Date)
	}
	if g1.FileCount != 2 {
		t.Errorf("Groups[1].FileCount = %d, want 2", g1.FileCount)
	}
	if g1.Size != 130000 {
		t.Errorf("Groups[1].Size = %d, want 130000", g1.Size)
	}
	wantExts1 := []string{"MOV", "NEF"}
	if len(g1.Extensions) != len(wantExts1) {
		t.Errorf("Groups[1].Extensions = %v, want %v", g1.Extensions, wantExts1)
	}
}

func TestAnalyze_SkipsHiddenFiles(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/DSC_0001.NEF":   {size: 1000, mtime: date(2025, 3, 8)},
		"100NIKON/.DS_Store":      {size: 500, mtime: date(2025, 3, 8)},
		"100NIKON/._DSC_0001.NEF": {size: 300, mtime: date(2025, 3, 8)},
	})

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1 (hidden files should be skipped)", result.FileCount)
	}
	if result.TotalSize != 1000 {
		t.Errorf("TotalSize = %d, want 1000", result.TotalSize)
	}
}

func TestAnalyze_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/DSC_0001.NEF": {size: 1000, mtime: date(2025, 3, 8)},
	})

	dcim := filepath.Join(root, "DCIM", "100NIKON")
	if err := os.Symlink(filepath.Join(dcim, "DSC_0001.NEF"), filepath.Join(dcim, "LINK.NEF")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.FileCount != 1 {
		t.Fatalf("FileCount = %d, want 1", result.FileCount)
	}
	if _, ok := result.FileDates[filepath.Join("100NIKON", "LINK.NEF")]; ok {
		t.Fatal("symlink should not be included in FileDates")
	}
}

func TestAnalyze_EmptyCard(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dcim := filepath.Join(root, "DCIM")
	if err := os.MkdirAll(dcim, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.FileCount != 0 {
		t.Errorf("FileCount = %d, want 0", result.FileCount)
	}
	if result.TotalSize != 0 {
		t.Errorf("TotalSize = %d, want 0", result.TotalSize)
	}
	if len(result.Groups) != 0 {
		t.Errorf("len(Groups) = %d, want 0", len(result.Groups))
	}
	if result.FileDates == nil {
		t.Error("FileDates should be non-nil (empty map, not nil)")
	}
	if result.FileDateTimes == nil {
		t.Error("FileDateTimes should be non-nil (empty map, not nil)")
	}
}

func TestAnalyze_NoDCIM(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	_, err := New(root).Analyze(context.Background())
	if err == nil {
		t.Error("expected error for missing DCIM, got nil")
	}
}

func TestAnalyze_HiddenDirectory(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/DSC_0001.NEF": {size: 1000, mtime: date(2025, 3, 8)},
		".Trashes/junk.dat":     {size: 500, mtime: date(2025, 3, 8)},
	})

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1 (files in hidden dirs should be skipped)", result.FileCount)
	}
}

func TestAnalyze_ExtensionNormalization(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/photo.nef":  {size: 100, mtime: date(2025, 3, 8)},
		"100NIKON/photo.Nef":  {size: 100, mtime: date(2025, 3, 8)},
		"100NIKON/photo2.NEF": {size: 100, mtime: date(2025, 3, 8)},
	})

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Groups) != 1 {
		t.Fatalf("len(Groups) = %d, want 1", len(result.Groups))
	}
	g := result.Groups[0]
	if len(g.Extensions) != 1 || g.Extensions[0] != "NEF" {
		t.Errorf("Extensions = %v, want [NEF] (case should be normalized)", g.Extensions)
	}
}

func TestAnalyze_FileDates(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/DSC_0001.NEF": {size: 100, mtime: date(2025, 3, 8)},
		"100NIKON/DSC_0002.MOV": {size: 200, mtime: date(2025, 3, 9)},
	})

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result.FileDates) != 2 {
		t.Fatalf("len(FileDates) = %d, want 2", len(result.FileDates))
	}
	if len(result.FileDateTimes) != 2 {
		t.Fatalf("len(FileDateTimes) = %d, want 2", len(result.FileDateTimes))
	}

	// Without real EXIF data, dates should fall back to mtime.
	want := map[string]string{
		"100NIKON/DSC_0001.NEF": "2025-03-08",
		"100NIKON/DSC_0002.MOV": "2025-03-09",
	}
	for path, wantDate := range want {
		got, ok := result.FileDates[path]
		if !ok {
			t.Errorf("FileDates missing key %q", path)
			continue
		}
		if got != wantDate {
			t.Errorf("FileDates[%q] = %q, want %q", path, got, wantDate)
		}
		if ts, ok := result.FileDateTimes[path]; !ok || ts.IsZero() {
			t.Errorf("FileDateTimes missing or zero for %q", path)
		}
	}
}

func TestAnalyze_MultipleSubfolders(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/DSC_0001.NEF": {size: 100, mtime: date(2025, 3, 8)},
		"101NIKON/DSC_0010.NEF": {size: 200, mtime: date(2025, 3, 8)},
		"102NIKON/DSC_0020.JPG": {size: 300, mtime: date(2025, 3, 9)},
	})

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.FileCount != 3 {
		t.Errorf("FileCount = %d, want 3", result.FileCount)
	}
	if len(result.Groups) != 2 {
		t.Errorf("len(Groups) = %d, want 2", len(result.Groups))
	}
}

func TestAnalyze_UnsupportedExtensionsSkipped(t *testing.T) {
	t.Parallel()
	root := createTestCard(t, map[string]testFile{
		"100NIKON/DSC_0001.NEF": {size: 100, mtime: date(2025, 3, 8)},
		"100NIKON/readme.txt":   {size: 50, mtime: date(2025, 3, 8)},
		"100NIKON/data.xml":     {size: 75, mtime: date(2025, 3, 8)},
	})

	result, err := New(root).Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1 (only NEF should be counted)", result.FileCount)
	}
	if result.TotalSize != 100 {
		t.Errorf("TotalSize = %d, want 100", result.TotalSize)
	}
}

func TestNormalizeExt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{".nef", "NEF"},
		{".NEF", "NEF"},
		{".Jpg", "JPG"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeExt(tt.input)
		if got != tt.want {
			t.Errorf("normalizeExt(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSortedKeys(t *testing.T) {
	t.Parallel()

	t.Run("nil map", func(t *testing.T) {
		got := sortedKeys(nil)
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		got := sortedKeys(map[string]bool{})
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("sorted output", func(t *testing.T) {
		got := sortedKeys(map[string]bool{
			"NIKKOR Z 70-200mm f/2.8 VR S": true,
			"NIKKOR Z 24-70mm f/2.8 S":     true,
			"NIKKOR Z 50mm f/1.2 S":        true,
		})
		want := []string{
			"NIKKOR Z 24-70mm f/2.8 S",
			"NIKKOR Z 50mm f/1.2 S",
			"NIKKOR Z 70-200mm f/2.8 VR S",
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("position %d: got %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("single entry", func(t *testing.T) {
		got := sortedKeys(map[string]bool{
			"NIKON Z 9": true,
		})
		if len(got) != 1 || got[0] != "NIKON Z 9" {
			t.Fatalf("expected [NIKON Z 9], got %v", got)
		}
	})
}

func TestScanXMPRating(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{"rating 3", []byte(`<xmp:Rating>3</xmp:Rating>`), 3},
		{"rating 5", []byte(`<xmp:Rating>5</xmp:Rating>`), 5},
		{"rating 1", []byte(`<xmp:Rating>1</xmp:Rating>`), 1},
		{"rating 0", []byte(`<xmp:Rating>0</xmp:Rating>`), 0},
		{"no rating", []byte(`<xmp:Title>test</xmp:Title>`), 0},
		{"empty", []byte{}, 0},
		{"truncated at prefix", []byte(`<xmp:Rating>`), 0},
		{"rating 6 out of range", []byte(`<xmp:Rating>6</xmp:Rating>`), 0},
		{"embedded in larger buffer", append(make([]byte, 1000), []byte(`<xmp:Rating>4</xmp:Rating>`)...), 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scanXMPRating(tt.input); got != tt.want {
				t.Errorf("scanXMPRating() = %d, want %d", got, tt.want)
			}
		})
	}
}
