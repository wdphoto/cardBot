package app

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wdphoto/cardBot/config"
)

func TestRunSetup_WritesNamingModeToConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	err := RunSetup(
		cfg,
		cfgPath,
		func(string) string { return "~/Pictures/Jobs" },
		func(string) string { return config.NamingTimestamp },
	)
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}

	loaded, warnings, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if loaded.Destination.Path != "~/Pictures/Jobs" {
		t.Fatalf("Destination.Path = %q, want %q", loaded.Destination.Path, "~/Pictures/Jobs")
	}
	if loaded.Naming.Mode != config.NamingTimestamp {
		t.Fatalf("Naming.Mode = %q, want %q", loaded.Naming.Mode, config.NamingTimestamp)
	}
	if loaded.Daemon.TerminalApp == "" {
		t.Fatal("Daemon.TerminalApp empty, want existing value preserved")
	}
}

func TestRunSetup_NormalizesInvalidNamingMode(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	err := RunSetup(
		cfg,
		cfgPath,
		func(string) string { return "~/Pictures/Jobs" },
		func(string) string { return "banana" },
	)
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}

	loaded, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Naming.Mode != config.NamingOriginal {
		t.Fatalf("Naming.Mode = %q, want %q", loaded.Naming.Mode, config.NamingOriginal)
	}
}

func TestRunSetup_PreservesDaemonPreferences(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.Daemon.Enabled = true
	cfg.Daemon.StartAtLogin = true
	cfg.Daemon.TerminalApp = "Ghostty"
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	err := RunSetup(
		cfg,
		cfgPath,
		func(string) string { return "~/Pictures/Jobs" },
		func(string) string { return config.NamingTimestamp },
	)
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}

	loaded, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !loaded.Daemon.Enabled {
		t.Fatal("Daemon.Enabled = false, want true")
	}
	if !loaded.Daemon.StartAtLogin {
		t.Fatal("Daemon.StartAtLogin = false, want true")
	}
	if loaded.Daemon.TerminalApp != "Ghostty" {
		t.Fatalf("Daemon.TerminalApp = %q, want %q", loaded.Daemon.TerminalApp, "Ghostty")
	}
}

func TestParseNamingChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"1", config.NamingOriginal, true},
		{"original", config.NamingOriginal, true},
		{"o", config.NamingOriginal, true},
		{"2", config.NamingTimestamp, true},
		{"timestamp", config.NamingTimestamp, true},
		{"t", config.NamingTimestamp, true},
		{"nope", "", false},
		{"", "", false},
		{"  2  ", config.NamingTimestamp, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input_%q", tt.in), func(t *testing.T) {
			got, ok := parseNamingChoice(tt.in)
			if ok != tt.wantOK {
				t.Errorf("parseNamingChoice(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
				return
			}
			if got != tt.want {
				t.Errorf("parseNamingChoice(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPromptNamingMode_DefaultOriginal(t *testing.T) {
	t.Parallel()
	in := bufio.NewReader(strings.NewReader("\n"))
	var out bytes.Buffer

	mode := promptNamingModeReader(in, &out, config.NamingOriginal)
	if mode != config.NamingOriginal {
		t.Fatalf("mode = %q, want %q", mode, config.NamingOriginal)
	}
	if !strings.Contains(out.String(), "Choice [1]:") {
		t.Fatalf("expected default [1] prompt, got:\n%s", out.String())
	}
}

func TestPromptNamingMode_DefaultTimestamp(t *testing.T) {
	t.Parallel()
	in := bufio.NewReader(strings.NewReader("\n"))
	var out bytes.Buffer

	mode := promptNamingModeReader(in, &out, config.NamingTimestamp)
	if mode != config.NamingTimestamp {
		t.Fatalf("mode = %q, want %q", mode, config.NamingTimestamp)
	}
	if !strings.Contains(out.String(), "Choice [2]:") {
		t.Fatalf("expected default [2] prompt, got:\n%s", out.String())
	}
}

func TestPromptNamingMode_InvalidThenValid(t *testing.T) {
	t.Parallel()
	in := bufio.NewReader(strings.NewReader("x\n2\n"))
	var out bytes.Buffer

	mode := promptNamingModeReader(in, &out, config.NamingOriginal)
	if mode != config.NamingTimestamp {
		t.Fatalf("mode = %q, want %q", mode, config.NamingTimestamp)
	}
	if !strings.Contains(out.String(), "Please enter 1 or 2.") {
		t.Fatalf("expected invalid-input message, got:\n%s", out.String())
	}
}

func TestPromptNamingMode_EOF(t *testing.T) {
	t.Parallel()
	in := bufio.NewReader(strings.NewReader(""))
	var out bytes.Buffer

	mode := promptNamingModeReader(in, &out, config.NamingTimestamp)
	if mode != config.NamingTimestamp {
		t.Fatalf("mode = %q, want %q (should return default on EOF)", mode, config.NamingTimestamp)
	}
}

func TestSetupPrompter_SequentialPrompts(t *testing.T) {
	t.Parallel()

	in := strings.NewReader("2\n")
	var out bytes.Buffer
	p := NewSetupPrompter(in, &out)

	if mode := p.PromptNamingMode(config.NamingOriginal); mode != config.NamingTimestamp {
		t.Fatalf("PromptNamingMode = %q, want %q", mode, config.NamingTimestamp)
	}
}

func TestNamingModeLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode string
		want string
	}{
		{config.NamingOriginal, "Camera original"},
		{config.NamingTimestamp, "Timestamp + sequence"},
		{"", "Camera original"},
		{"bogus", "Camera original"},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			if got := NamingModeLabel(tt.mode); got != tt.want {
				t.Fatalf("NamingModeLabel(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}
