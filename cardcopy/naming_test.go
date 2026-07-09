package cardcopy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSequenceDigits(t *testing.T) {
	t.Parallel()
	// Fixed at 4 digits for event/wedding work.
	if SequenceDigits != 4 {
		t.Errorf("SequenceDigits = %d, want 4", SequenceDigits)
	}
}

func TestFormatSequence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n      int
		digits int
		want   string
	}{
		{1, 3, "001"},
		{999, 3, "999"},
		{1, 4, "0001"},
		{42, 5, "00042"},
		// Edge: 0 clamps to 001 (sequence is 1-based)
		{0, 3, "001"},
		// Edge: negative clamps to 001
		{-5, 3, "001"},
		// Edge: digits clamped to 3..5.
		{1, 1, "001"},
		{1, 99, "00001"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n%d_d%d", tt.n, tt.digits), func(t *testing.T) {
			if got := formatSequence(tt.n, tt.digits); got != tt.want {
				t.Errorf("formatSequence(%d, %d) = %q, want %q", tt.n, tt.digits, got, tt.want)
			}
		})
	}
}

func TestSequenceMax(t *testing.T) {
	t.Parallel()
	if sequenceMax != 9999 {
		t.Fatalf("sequenceMax = %d, want 9999", sequenceMax)
	}
	// Verify formatSequence at max produces correct width.
	s := formatSequence(sequenceMax, SequenceDigits)
	if len(s) != SequenceDigits {
		t.Errorf("formatSequence(%d, %d) = %q, length %d, want %d", sequenceMax, SequenceDigits, s, len(s), SequenceDigits)
	}
}

func TestRenamedRelativePath(t *testing.T) {
	t.Parallel()
	capture := time.Date(2026, 3, 14, 14, 30, 52, 0, time.UTC)

	t.Run("with_subdirectory", func(t *testing.T) {
		got := renamedRelativePath("100NIKON/DSC_0001.nef", capture, 12, 4)
		want := filepath.Join("100NIKON", "260314T143052_0012.NEF")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("flat_path", func(t *testing.T) {
		got := renamedRelativePath("DSC_0001.MOV", capture, 1, 3)
		if got != "260314T143052_001.MOV" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("extension_uppercased", func(t *testing.T) {
		got := renamedRelativePath("100NIKON/img.mov", capture, 1, 3)
		want := filepath.Join("100NIKON", "260314T143052_001.MOV")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestIsTimestampMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want bool
	}{
		{"timestamp", true},
		{"TIMESTAMP", true},
		{"original", false},
		{"", false},
		{"banana", false},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			if got := isTimestampMode(tt.mode); got != tt.want {
				t.Errorf("isTimestampMode(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestCopy_DryRun_ReportsRenameMappings(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"100NIKON/DSC_0001.NEF": {data: []byte("a"), mtime: date(2026, 3, 8)},
		"100NIKON/DSC_0002.MOV": {data: []byte("b"), mtime: date(2026, 3, 8)},
	})
	dest := t.TempDir()
	ts := time.Date(2026, 3, 14, 14, 30, 52, 0, time.UTC)

	var got [][2]string
	res, err := Run(context.Background(), Options{
		CardPath:   card,
		DestBase:   dest,
		DryRun:     true,
		NamingMode: "timestamp",
		FileDates: map[string]string{
			"100NIKON/DSC_0001.NEF": "2026-03-14",
			"100NIKON/DSC_0002.MOV": "2026-03-14",
		},
		FileDateTimes: map[string]time.Time{
			"100NIKON/DSC_0001.NEF": ts,
			"100NIKON/DSC_0002.MOV": ts,
		},
	}, func(p Progress) {
		if p.SourceFile != "" {
			got = append(got, [2]string{p.SourceFile, p.CurrentFile})
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesCopied != 2 {
		t.Fatalf("FilesCopied = %d, want 2", res.FilesCopied)
	}
	if len(got) != 2 {
		t.Fatalf("progress mapping count = %d, want 2", len(got))
	}
	if got[0][0] != "100NIKON/DSC_0001.NEF" || got[0][1] != filepath.Join("100NIKON", "260314T143052_0001.NEF") {
		t.Fatalf("first mapping = %q -> %q", got[0][0], got[0][1])
	}
	if got[1][0] != "100NIKON/DSC_0002.MOV" || got[1][1] != filepath.Join("100NIKON", "260314T143052_0002.MOV") {
		t.Fatalf("second mapping = %q -> %q", got[1][0], got[1][1])
	}
	if _, err := os.Stat(filepath.Join(dest, "2026-03-14")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create destination files/dirs, stat err=%v", err)
	}
}

func TestCopy_TimestampNaming(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"100NIKON/DSC_0001.NEF": {data: []byte("a"), mtime: date(2026, 3, 8)},
		"100NIKON/DSC_0002.MOV": {data: []byte("b"), mtime: date(2026, 3, 8)},
	})
	dest := t.TempDir()

	ts := time.Date(2026, 3, 14, 14, 30, 52, 0, time.UTC)
	res, err := Run(context.Background(), Options{
		CardPath:   card,
		DestBase:   dest,
		NamingMode: "timestamp",
		FileDates: map[string]string{
			"100NIKON/DSC_0001.NEF": "2026-03-14",
			"100NIKON/DSC_0002.MOV": "2026-03-14",
		},
		FileDateTimes: map[string]time.Time{
			"100NIKON/DSC_0001.NEF": ts,
			"100NIKON/DSC_0002.MOV": ts,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesCopied != 2 {
		t.Fatalf("FilesCopied = %d, want 2", res.FilesCopied)
	}

	assertFileSize(t, filepath.Join(dest, "2026-03-14", "100NIKON", "260314T143052_0001.NEF"), 1)
	assertFileSize(t, filepath.Join(dest, "2026-03-14", "100NIKON", "260314T143052_0002.MOV"), 1)

	// Original camera names should not be present in timestamp mode.
	if _, err := os.Stat(filepath.Join(dest, "2026-03-14", "100NIKON", "DSC_0001.NEF")); !os.IsNotExist(err) {
		t.Fatal("original filename should not exist in timestamp mode")
	}
}

func TestCopy_TimestampNaming_AlwaysUses4Digits(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"100NIKON/DSC_0001.NEF": {data: []byte("a"), mtime: date(2026, 3, 8)},
	})
	dest := t.TempDir()
	ts := time.Date(2026, 3, 14, 14, 30, 52, 0, time.UTC)

	_, err := Run(context.Background(), Options{
		CardPath:   card,
		DestBase:   dest,
		NamingMode: "timestamp",
		FileDates:  map[string]string{"100NIKON/DSC_0001.NEF": "2026-03-14"},
		FileDateTimes: map[string]time.Time{
			"100NIKON/DSC_0001.NEF": ts,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Fixed 4-digit
	assertFileSize(t, filepath.Join(dest, "2026-03-14", "100NIKON", "260314T143052_0001.NEF"), 1)
}

func TestPlanCopy_TimestampNamingRejectsSequenceOverflow(t *testing.T) {
	files := make(map[string]testFileSpec, sequenceMax+1)
	for i := 0; i <= sequenceMax; i++ {
		files[fmt.Sprintf("100NIKON/DSC_%05d.NEF", i)] = testFileSpec{
			data:  []byte("x"),
			mtime: date(2026, 3, 8),
		}
	}
	card := createTestCard(t, files)
	dest := t.TempDir()

	_, err := PlanCopy(context.Background(), Options{
		CardPath:   card,
		DestBase:   dest,
		NamingMode: "timestamp",
	})
	if err == nil {
		t.Fatal("PlanCopy() error = nil, want sequence overflow error")
	}
	if !strings.Contains(err.Error(), "timestamp naming supports at most") {
		t.Fatalf("PlanCopy() error = %q, want timestamp overflow error", err)
	}
}

func TestSortFilesByCaptureTime(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	files := []fileEntry{
		{relPath: "101NIKON/DSC_0002.NEF", captureTime: base.Add(2 * time.Second)},
		{relPath: "100NIKON/DSC_0001.NEF", captureTime: base.Add(1 * time.Second)},
		{relPath: "102NIKON/DSC_0003.NEF", captureTime: base.Add(3 * time.Second)},
	}

	sortFilesByCaptureTime(files)

	want := []string{
		"100NIKON/DSC_0001.NEF",
		"101NIKON/DSC_0002.NEF",
		"102NIKON/DSC_0003.NEF",
	}

	for i, f := range files {
		if f.relPath != want[i] {
			t.Fatalf("position %d: got %q, want %q", i, f.relPath, want[i])
		}
	}
}

func TestSortFilesByCaptureTime_TieBreakByPath(t *testing.T) {
	t.Parallel()

	// Same timestamp — should fall back to path order.
	ts := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	files := []fileEntry{
		{relPath: "b.nef", captureTime: ts},
		{relPath: "a.nef", captureTime: ts},
		{relPath: "c.nef", captureTime: ts},
	}

	sortFilesByCaptureTime(files)

	want := []string{"a.nef", "b.nef", "c.nef"}
	for i, f := range files {
		if f.relPath != want[i] {
			t.Fatalf("position %d: got %q, want %q", i, f.relPath, want[i])
		}
	}
}

func TestCopy_OriginalNaming_Unchanged(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"100NIKON/DSC_0001.NEF": {data: []byte("a"), mtime: date(2026, 3, 8)},
	})
	dest := t.TempDir()

	_, err := Run(context.Background(), Options{
		CardPath:   card,
		DestBase:   dest,
		NamingMode: "original",
		FileDates:  map[string]string{"100NIKON/DSC_0001.NEF": "2026-03-08"},
		FileDateTimes: map[string]time.Time{
			"100NIKON/DSC_0001.NEF": date(2026, 3, 8),
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertFileSize(t, filepath.Join(dest, "2026-03-08", "100NIKON", "DSC_0001.NEF"), 1)
}

func TestPlanCopy_TimestampNameStableAcrossFilters(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"100NIKON/DSC_0001.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
		"100NIKON/DSC_0002.MOV": {data: []byte("video"), mtime: date(2026, 3, 9)},
	})
	ts1 := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(time.Second)
	base := Options{
		CardPath:   card,
		DestBase:   t.TempDir(),
		DryRun:     true,
		NamingMode: "timestamp",
		FileDateTimes: map[string]time.Time{
			"100NIKON/DSC_0001.JPG": ts1,
			"100NIKON/DSC_0002.MOV": ts2,
		},
	}
	all, err := PlanCopy(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	base.Filter = func(_ string, ext string) bool { return ext == "MOV" }
	videos, err := PlanCopy(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos.Files) != 1 || len(all.Files) != 2 {
		t.Fatalf("all files=%d video files=%d", len(all.Files), len(videos.Files))
	}
	if videos.Files[0].DestRelPath != all.Files[1].DestRelPath {
		t.Fatalf("video name changed across filters: all=%q videos=%q", all.Files[1].DestRelPath, videos.Files[0].DestRelPath)
	}
}

func TestCopy_TimestampNamingPreservesSidecarBasename(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"100NIKON/DSC_0001.NEF": {data: []byte("raw"), mtime: date(2026, 3, 8)},
		"100NIKON/DSC_0001.XMP": {data: []byte("xmp"), mtime: date(2026, 3, 9)},
	})
	dest := t.TempDir()
	ts := time.Date(2026, 3, 8, 12, 34, 56, 0, time.UTC)
	_, err := Run(context.Background(), Options{
		CardPath:   card,
		DestBase:   dest,
		NamingMode: "timestamp",
		FileDates: map[string]string{
			"100NIKON/DSC_0001.NEF": "2026-03-08",
		},
		FileDateTimes: map[string]time.Time{
			"100NIKON/DSC_0001.NEF": ts,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dest, "2026-03-08", "100NIKON", "260308T123456_0001")
	assertFileSize(t, base+".NEF", 3)
	assertFileSize(t, base+".XMP", 3)
}
