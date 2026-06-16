package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/term"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureStdoutFD(t, fn)
}

func TestCardIsReadOnly(t *testing.T) {
	if cardIsReadOnly(t.TempDir()) {
		t.Fatal("expected writable temp dir")
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if !cardIsReadOnly(missing) {
		t.Fatal("expected missing dir to be treated as read-only")
	}
}

func TestPrintCardInfo(t *testing.T) {
	cfg := config.Defaults()
	cfg.Output.Color = false
	a := &App{cfg: cfg, copiedModes: make(map[string]bool)}
	card := &detect.Card{
		Path:       t.TempDir(),
		Brand:      "Nikon",
		TotalBytes: 1024 * 1024,
		UsedBytes:  512 * 1024,
	}

	result := &analyze.Result{
		Bodies:     []string{"NIKON Z 9"},
		Lenses:     []string{"NIKKOR Z 24-70mm f/2.8 S"},
		Starred:    1,
		PhotoCount: 2,
		VideoCount: 1,
		TotalSize:  123456,
		FileCount:  3,
		Groups: []analyze.DateGroup{
			{Date: "2026-03-10", Size: 100000, FileCount: 2, Extensions: []string{"JPG", "NEF"}},
			{Date: "2026-03-09", Size: 23456, FileCount: 1, Extensions: []string{"MOV"}},
		},
	}

	out := captureStdout(t, func() {
		a.printCardInfo(card, result)
	})

	for _, want := range []string{
		"Status:", "Path:", "Storage:", "Gear:", "Starred:", "Content:",
		"Total:", "Copy to:", "Naming:", "[a] Copy All",
		"NIKON Z 9", "NIKKOR Z 24-70mm f/2.8 S",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q\n%s", want, out)
		}
	}
}

func TestPrintInvalidCardInfo(t *testing.T) {
	cfg := config.Defaults()
	cfg.Output.Color = false
	a := &App{cfg: cfg, copiedModes: make(map[string]bool)}
	card := &detect.Card{Path: t.TempDir(), Brand: "Unknown"}

	out := captureStdout(t, func() {
		a.printInvalidCardInfo(card)
	})

	if !strings.Contains(out, "(no DCIM — not a camera card)") {
		t.Fatalf("missing invalid-card message\n%s", out)
	}
	if !strings.Contains(out, "[e] Eject") {
		t.Fatalf("missing prompt\n%s", out)
	}
	// Invalid card with no EXIF falls back to card.Brand.
	if !strings.Contains(out, "Gear:") {
		t.Fatalf("missing Gear label\n%s", out)
	}
	if !strings.Contains(out, "Unknown") {
		t.Fatalf("missing brand fallback\n%s", out)
	}
}

func TestGearDisplay(t *testing.T) {
	cfg := config.Defaults()
	cfg.Output.Color = false

	t.Run("single body single lens", func(t *testing.T) {
		a := &App{cfg: cfg, copiedModes: make(map[string]bool)}
		card := &detect.Card{Path: t.TempDir(), Brand: "Nikon", TotalBytes: 1024, UsedBytes: 512}
		result := &analyze.Result{
			Bodies:     []string{"NIKON Z 9"},
			Lenses:     []string{"NIKKOR Z 24-70mm f/2.8 S"},
			FileCount:  1,
			PhotoCount: 1,
			Groups:     []analyze.DateGroup{{Date: "2026-03-25", Size: 100, FileCount: 1, Extensions: []string{"NEF"}}},
		}
		out := captureStdout(t, func() { a.printCardInfo(card, result) })
		if !strings.Contains(out, "NIKON Z 9") {
			t.Fatalf("missing body\n%s", out)
		}
		if !strings.Contains(out, "NIKKOR Z 24-70mm f/2.8 S") {
			t.Fatalf("missing lens\n%s", out)
		}
	})

	t.Run("single body multiple lenses", func(t *testing.T) {
		a := &App{cfg: cfg, copiedModes: make(map[string]bool)}
		card := &detect.Card{Path: t.TempDir(), Brand: "Nikon", TotalBytes: 1024, UsedBytes: 512}
		result := &analyze.Result{
			Bodies:     []string{"NIKON Z 9"},
			Lenses:     []string{"NIKKOR Z 24-70mm f/2.8 S", "NIKKOR Z 50mm f/1.2 S", "NIKKOR Z 70-200mm f/2.8 VR S"},
			FileCount:  3,
			PhotoCount: 3,
			Groups:     []analyze.DateGroup{{Date: "2026-03-25", Size: 300, FileCount: 3, Extensions: []string{"NEF"}}},
		}
		out := captureStdout(t, func() { a.printCardInfo(card, result) })
		if !strings.Contains(out, "NIKON Z 9") {
			t.Fatalf("missing body\n%s", out)
		}
		for _, lens := range result.Lenses {
			if !strings.Contains(out, lens) {
				t.Fatalf("missing lens %q\n%s", lens, out)
			}
		}
	})

	t.Run("multiple bodies", func(t *testing.T) {
		a := &App{cfg: cfg, copiedModes: make(map[string]bool)}
		card := &detect.Card{Path: t.TempDir(), Brand: "Nikon", TotalBytes: 1024, UsedBytes: 512}
		result := &analyze.Result{
			Bodies:     []string{"Canon EOS R5", "NIKON Z 9"},
			Lenses:     []string{"NIKKOR Z 24-70mm f/2.8 S", "RF24-70mm F2.8 L IS USM"},
			FileCount:  2,
			PhotoCount: 2,
			Groups:     []analyze.DateGroup{{Date: "2026-03-25", Size: 200, FileCount: 2, Extensions: []string{"CR3", "NEF"}}},
		}
		out := captureStdout(t, func() { a.printCardInfo(card, result) })
		// Bodies should be comma-separated on one line.
		if !strings.Contains(out, "Canon EOS R5, NIKON Z 9") {
			t.Fatalf("missing multi-body line\n%s", out)
		}
	})

	t.Run("no EXIF falls back to brand", func(t *testing.T) {
		a := &App{cfg: cfg, copiedModes: make(map[string]bool)}
		card := &detect.Card{Path: t.TempDir(), Brand: "Nikon", TotalBytes: 1024, UsedBytes: 512}
		result := &analyze.Result{
			FileCount:  1,
			PhotoCount: 1,
			Groups:     []analyze.DateGroup{{Date: "2026-03-25", Size: 100, FileCount: 1, Extensions: []string{"NEF"}}},
		}
		out := captureStdout(t, func() { a.printCardInfo(card, result) })
		if !strings.Contains(out, "Gear:") {
			t.Fatalf("missing Gear label\n%s", out)
		}
		// Falls back to card.Brand when no bodies found.
		if !strings.Contains(out, "Nikon") {
			t.Fatalf("missing brand fallback\n%s", out)
		}
	})

	t.Run("body only no lens", func(t *testing.T) {
		a := &App{cfg: cfg, copiedModes: make(map[string]bool)}
		card := &detect.Card{Path: t.TempDir(), Brand: "Nikon", TotalBytes: 1024, UsedBytes: 512}
		result := &analyze.Result{
			Bodies:     []string{"NIKON Z 9"},
			FileCount:  1,
			PhotoCount: 1,
			Groups:     []analyze.DateGroup{{Date: "2026-03-25", Size: 100, FileCount: 1, Extensions: []string{"NEF"}}},
		}
		out := captureStdout(t, func() { a.printCardInfo(card, result) })
		if !strings.Contains(out, "NIKON Z 9") {
			t.Fatalf("missing body\n%s", out)
		}
		// No lens lines should appear.
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "NIKKOR") {
				t.Fatalf("unexpected lens line: %s", trimmed)
			}
		}
	})
}

func TestShowHelp(t *testing.T) {
	a := &App{}
	out := captureStdout(t, func() { a.showHelp() })
	if !strings.Contains(out, "[a]  Copy All") || !strings.Contains(out, "[?]  Help") {
		t.Fatalf("unexpected help output:\n%s", out)
	}
}

func TestShowHardwareInfo(t *testing.T) {
	cfg := config.Defaults()
	cfg.Output.Color = false
	a := &App{cfg: cfg, copiedModes: make(map[string]bool)}

	t.Run("nil hardware", func(t *testing.T) {
		card := &detect.Card{}
		out := captureStdout(t, func() { a.showHardwareInfo(card) })
		if !strings.Contains(out, "Hardware info unavailable") {
			t.Fatalf("unexpected output:\n%s", out)
		}
	})

	t.Run("with hardware", func(t *testing.T) {
		card := &detect.Card{}
		card.SetHW(&detect.HardwareInfo{})
		out := captureStdout(t, func() { a.showHardwareInfo(card) })
		if strings.Contains(out, "Hardware info unavailable") {
			t.Fatalf("expected hardware path, got:\n%s", out)
		}
		if !strings.Contains(out, "[a] Copy All") {
			t.Fatalf("missing prompt after hardware info:\n%s", out)
		}
	})
}

func TestPrintf(t *testing.T) {
	a := &App{}
	out := captureStdout(t, func() {
		a.Printf("hello %s", "world")
	})
	if out != "hello world" {
		t.Fatalf("Printf output = %q, want %q", out, "hello world")
	}
}

func TestStartAndStopScanning(t *testing.T) {
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})

	a.StartScanning()
	if a.spinner == nil {
		t.Fatal("expected spinner to be created")
	}
	if got := a.currentPhase(); got != phaseScanning {
		t.Fatalf("phase = %v, want %v", got, phaseScanning)
	}

	a.stopScanning()
	if a.spinner != nil {
		t.Fatal("expected spinner to be cleared")
	}

	// No-op second stop must be safe.
	a.stopScanning()
}

func TestTsPrefix_FirstCall_ReturnsTimestamp(t *testing.T) {
	a := New(Config{Cfg: config.Defaults()})
	prefix := a.TsPrefix()
	// Should contain ANSI dim codes and a bracketed timestamp.
	if !strings.Contains(prefix, "\033[2m") {
		t.Fatalf("expected dim ANSI code, got %q", prefix)
	}
	if !strings.Contains(prefix, "\033[0m") {
		t.Fatalf("expected reset ANSI code, got %q", prefix)
	}
	if !strings.Contains(prefix, "[") || !strings.Contains(prefix, "]") {
		t.Fatalf("expected bracketed timestamp, got %q", prefix)
	}
}

func TestTsPrefix_SameSecond_ReturnsIndent(t *testing.T) {
	a := New(Config{Cfg: config.Defaults()})
	first := a.TsPrefix()
	second := a.TsPrefix()
	if first == tsIndent {
		t.Fatal("first call should not return indent")
	}
	if second != tsIndent {
		t.Fatalf("second call in same second should return indent, got %q", second)
	}
}

func TestSetLastTS_SyncsWithTsPrefix(t *testing.T) {
	a := New(Config{Cfg: config.Defaults()})
	now := term.Ts()
	a.SetLastTS(now)
	// TsPrefix should return indent since we set the same second.
	got := a.TsPrefix()
	if got != tsIndent {
		t.Fatalf("TsPrefix after SetLastTS with current second should indent, got %q", got)
	}
}

func TestSetLastTS_DifferentSecond_ShowsTimestamp(t *testing.T) {
	a := New(Config{Cfg: config.Defaults()})
	a.SetLastTS("2000-01-01T00:00:00")
	got := a.TsPrefix()
	// Should return a new timestamp, not indent.
	if got == tsIndent {
		t.Fatal("TsPrefix after SetLastTS with old timestamp should not indent")
	}
}

func TestTsIndent_Width(t *testing.T) {
	// tsIndent must match the visible width of "[2006-01-02T15:04:05]" (21 chars).
	if len(tsIndent) != 21 {
		t.Fatalf("tsIndent length = %d, want 21", len(tsIndent))
	}
}
