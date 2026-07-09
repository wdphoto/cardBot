package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	if cfg.Schema != schemaVersion {
		t.Errorf("Schema = %q, want %q", cfg.Schema, schemaVersion)
	}
	if cfg.Destination.Path != "~/Pictures/cardBot" {
		t.Errorf("Destination.Path = %q, want ~/Pictures/cardBot", cfg.Destination.Path)
	}
	if cfg.Naming.Mode != NamingOriginal {
		t.Errorf("Naming.Mode = %q, want %q", cfg.Naming.Mode, NamingOriginal)
	}
	if cfg.Daemon.Enabled {
		t.Error("Daemon.Enabled should default to false")
	}
	if cfg.Daemon.StartAtLogin {
		t.Error("Daemon.StartAtLogin should default to false")
	}
	if cfg.Daemon.TerminalApp != "Terminal" {
		t.Errorf("Daemon.TerminalApp = %q, want %q", cfg.Daemon.TerminalApp, "Terminal")
	}
	if cfg.Daemon.LaunchArgs != nil {
		t.Errorf("Daemon.LaunchArgs = %v, want nil", cfg.Daemon.LaunchArgs)
	}
	if cfg.Daemon.Debug {
		t.Error("Daemon.Debug should default to false")
	}
	if cfg.Advanced.BufferSizeKB != 256 {
		t.Errorf("BufferSizeKB = %d, want 256", cfg.Advanced.BufferSizeKB)
	}
	if cfg.Advanced.ExifWorkers != 4 {
		t.Errorf("ExifWorkers = %d, want 4", cfg.Advanced.ExifWorkers)
	}
	if cfg.Advanced.VerifyMode != VerifySize {
		t.Errorf("VerifyMode = %q, want %q", cfg.Advanced.VerifyMode, VerifySize)
	}
	if !cfg.Output.Color {
		t.Error("Color should default to true")
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Defaults()
	cfg.Destination.Path = "~/Photos/Test"
	cfg.Naming.Mode = NamingTimestamp
	cfg.Daemon.Enabled = true
	cfg.Daemon.StartAtLogin = true
	cfg.Daemon.TerminalApp = "Ghostty"
	cfg.Daemon.LaunchArgs = []string{"-e", "cardbot {{mount_path}}"}
	cfg.Daemon.Debug = true

	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}

	loaded, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if loaded.Destination.Path != "~/Photos/Test" {
		t.Errorf("Destination.Path = %q, want ~/Photos/Test", loaded.Destination.Path)
	}
	if loaded.Naming.Mode != NamingTimestamp {
		t.Errorf("Naming.Mode = %q, want %q", loaded.Naming.Mode, NamingTimestamp)
	}
	if !loaded.Daemon.Enabled {
		t.Error("Daemon.Enabled = false, want true")
	}
	if !loaded.Daemon.StartAtLogin {
		t.Error("Daemon.StartAtLogin = false, want true")
	}
	if loaded.Daemon.TerminalApp != "Ghostty" {
		t.Errorf("Daemon.TerminalApp = %q, want %q", loaded.Daemon.TerminalApp, "Ghostty")
	}
	if len(loaded.Daemon.LaunchArgs) != 2 {
		t.Errorf("Daemon.LaunchArgs len = %d, want 2", len(loaded.Daemon.LaunchArgs))
	}
	if !loaded.Daemon.Debug {
		t.Error("Daemon.Debug = false, want true")
	}
	if loaded.Schema != schemaVersion {
		t.Errorf("Schema = %q, want %q", loaded.Schema, schemaVersion)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	// Should return defaults.
	if cfg.Destination.Path != "~/Pictures/cardBot" {
		t.Errorf("expected defaults, got path %q", cfg.Destination.Path)
	}
}

func TestLoadWithStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content *string
		want    LoadStatus
	}{
		{name: "missing", want: LoadMissing},
		{name: "valid", content: ptrString(`{"$schema":"cardbot-config-v1"}`), want: LoadValid},
		{name: "malformed", content: ptrString(`{broken`), want: LoadMalformed},
		{name: "unsupported", content: ptrString(`{"$schema":"cardbot-config-v99"}`), want: LoadUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if tt.content != nil {
				if err := os.WriteFile(path, []byte(*tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, _, got, err := LoadWithStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func ptrString(s string) *string { return &s }

func TestSave_IsAtomicAndRestrictsPermissions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(Defaults(), path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary configs left behind: %v", matches)
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if cfg.Destination.Path != "~/Pictures/cardBot" {
		t.Error("should return defaults for malformed JSON")
	}
}

func TestLoad_WrongSchema(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema": "cardbot-config-v99"}`), 0600); err != nil {
		t.Fatal(err)
	}

	_, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
}

func TestLoad_ClampBufferSizeKB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		json     string
		want     int
		wantWarn bool
	}{
		{"too small", `{"$schema":"cardbot-config-v1","advanced":{"buffer_size_kb":10}}`, 64, true},
		{"too large", `{"$schema":"cardbot-config-v1","advanced":{"buffer_size_kb":9999}}`, 4096, true},
		{"valid", `{"$schema":"cardbot-config-v1","advanced":{"buffer_size_kb":512}}`, 512, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.json), 0600); err != nil {
				t.Fatal(err)
			}

			cfg, warnings, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Advanced.BufferSizeKB != tt.want {
				t.Errorf("BufferSizeKB = %d, want %d", cfg.Advanced.BufferSizeKB, tt.want)
			}
			if tt.wantWarn && len(warnings) == 0 {
				t.Error("expected a warning")
			}
			if !tt.wantWarn && len(warnings) != 0 {
				t.Errorf("unexpected warnings: %v", warnings)
			}
		})
	}
}

func TestLoad_ClampExifWorkers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		json     string
		want     int
		wantWarn bool
	}{
		{"too small", `{"$schema":"cardbot-config-v1","advanced":{"exif_workers":0}}`, 1, true},
		{"too large", `{"$schema":"cardbot-config-v1","advanced":{"exif_workers":99}}`, 16, true},
		{"valid", `{"$schema":"cardbot-config-v1","advanced":{"exif_workers":8}}`, 8, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.json), 0600); err != nil {
				t.Fatal(err)
			}

			cfg, warnings, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Advanced.ExifWorkers != tt.want {
				t.Errorf("ExifWorkers = %d, want %d", cfg.Advanced.ExifWorkers, tt.want)
			}
			if tt.wantWarn && len(warnings) == 0 {
				t.Error("expected a warning")
			}
		})
	}
}

func TestLoad_PartialConfig(t *testing.T) {
	t.Parallel()
	// Only override destination; everything else should use defaults.
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema":"cardbot-config-v1","destination":{"path":"~/Custom"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if cfg.Destination.Path != "~/Custom" {
		t.Errorf("Destination.Path = %q, want ~/Custom", cfg.Destination.Path)
	}
	// All unset fields should preserve defaults.
	defaults := Defaults()
	if cfg.Advanced.BufferSizeKB != defaults.Advanced.BufferSizeKB {
		t.Errorf("BufferSizeKB = %d, want %d (default)", cfg.Advanced.BufferSizeKB, defaults.Advanced.BufferSizeKB)
	}
	if cfg.Advanced.ExifWorkers != defaults.Advanced.ExifWorkers {
		t.Errorf("ExifWorkers = %d, want %d (default)", cfg.Advanced.ExifWorkers, defaults.Advanced.ExifWorkers)
	}
	if cfg.Advanced.LogFile != defaults.Advanced.LogFile {
		t.Errorf("LogFile = %q, want %q (default)", cfg.Advanced.LogFile, defaults.Advanced.LogFile)
	}
	if cfg.Output.Color != defaults.Output.Color {
		t.Errorf("Color = %v, want %v (default)", cfg.Output.Color, defaults.Output.Color)
	}
	if cfg.Naming.Mode != defaults.Naming.Mode {
		t.Errorf("Naming.Mode = %q, want %q (default)", cfg.Naming.Mode, defaults.Naming.Mode)
	}
	if cfg.Daemon.Enabled != defaults.Daemon.Enabled {
		t.Errorf("Daemon.Enabled = %v, want %v (default)", cfg.Daemon.Enabled, defaults.Daemon.Enabled)
	}
	if cfg.Daemon.StartAtLogin != defaults.Daemon.StartAtLogin {
		t.Errorf("Daemon.StartAtLogin = %v, want %v (default)", cfg.Daemon.StartAtLogin, defaults.Daemon.StartAtLogin)
	}
	if cfg.Daemon.TerminalApp != defaults.Daemon.TerminalApp {
		t.Errorf("Daemon.TerminalApp = %q, want %q (default)", cfg.Daemon.TerminalApp, defaults.Daemon.TerminalApp)
	}
	if len(cfg.Daemon.LaunchArgs) != len(defaults.Daemon.LaunchArgs) {
		t.Errorf("Daemon.LaunchArgs len = %d, want %d (default)", len(cfg.Daemon.LaunchArgs), len(defaults.Daemon.LaunchArgs))
	}
	if cfg.Daemon.Debug != defaults.Daemon.Debug {
		t.Errorf("Daemon.Debug = %v, want %v (default)", cfg.Daemon.Debug, defaults.Daemon.Debug)
	}
}

func TestLoad_InvalidNamingMode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema":"cardbot-config-v1","naming":{"mode":"banana"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Naming.Mode != NamingOriginal {
		t.Errorf("Naming.Mode = %q, want %q", cfg.Naming.Mode, NamingOriginal)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for invalid naming mode")
	}
}

func TestLoad_DaemonStartAtLoginRequiresEnabled(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema":"cardbot-config-v1","daemon":{"enabled":false,"start_at_login":true}}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.StartAtLogin {
		t.Fatal("Daemon.StartAtLogin should be forced false when daemon.enabled is false")
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for invalid daemon settings")
	}
}

func TestLoad_DaemonTerminalAppDefaultedWhenEmpty(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema":"cardbot-config-v1","daemon":{"terminal_app":""}}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.TerminalApp != "Terminal" {
		t.Fatalf("Daemon.TerminalApp = %q, want %q", cfg.Daemon.TerminalApp, "Terminal")
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for empty terminal_app")
	}
}

func TestNormalizeNamingMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"", NamingOriginal},
		{"original", NamingOriginal},
		{"ORIGINAL", NamingOriginal},
		{"timestamp", NamingTimestamp},
		{" TIMESTAMP ", NamingTimestamp},
		{"nope", NamingOriginal},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input_%q", tt.in), func(t *testing.T) {
			if got := NormalizeNamingMode(tt.in); got != tt.want {
				t.Errorf("NormalizeNamingMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/Pictures", filepath.Join(home, "Pictures")},
		{"~", home},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~bob/path", "~bob/path"}, // ~user syntax not expanded
	}

	for _, tt := range tests {
		got, err := ExpandPath(tt.input)
		if err != nil {
			t.Errorf("ExpandPath(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestContractPath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{filepath.Join(home, "Pictures"), "~/Pictures"},
		{filepath.Join(home, ""), "~"},
		{home + "/", "~"}, // trailing slash normalized
		{"/other/path", "/other/path"},
	}

	for _, tt := range tests {
		got := ContractPath(tt.input)
		if got != tt.want {
			t.Errorf("ContractPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExpandContractRoundTrip(t *testing.T) {
	t.Parallel()
	original := "~/Pictures/cardBot"
	expanded, err := ExpandPath(original)
	if err != nil {
		t.Fatal(err)
	}
	contracted := ContractPath(expanded)
	if contracted != original {
		t.Errorf("round-trip: %q → %q → %q", original, expanded, contracted)
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a", "b", "c", "config.json")
	cfg := Defaults()

	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Error("config file should exist after Save")
	}
}

func TestNormalizeVerifyMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"", VerifySize},
		{"size", VerifySize},
		{"SIZE", VerifySize},
		{"full", VerifyFull},
		{" FULL ", VerifyFull},
		{"sha256", VerifyFull},
		{"checksum", VerifyFull},
		{"nope", VerifySize},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input_%q", tt.in), func(t *testing.T) {
			if got := NormalizeVerifyMode(tt.in); got != tt.want {
				t.Errorf("NormalizeVerifyMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoad_InvalidVerifyMode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema":"cardbot-config-v1","advanced":{"verify_mode":"banana"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Advanced.VerifyMode != VerifySize {
		t.Errorf("VerifyMode = %q, want %q", cfg.Advanced.VerifyMode, VerifySize)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for invalid verify_mode")
	}
}

func TestLoad_VerifyModeFull(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema":"cardbot-config-v1","advanced":{"verify_mode":"full"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Advanced.VerifyMode != VerifyFull {
		t.Errorf("VerifyMode = %q, want %q", cfg.Advanced.VerifyMode, VerifyFull)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := Defaults()

	t.Setenv("CARDBOT_DESTINATION", "~/Photos/Override")
	t.Setenv("CARDBOT_NAMING", "timestamp")
	t.Setenv("CARDBOT_LOG_FILE", "/tmp/cb.log")
	t.Setenv("CARDBOT_VERIFY_MODE", "full")

	ApplyEnvOverrides(cfg)

	if cfg.Destination.Path != "~/Photos/Override" {
		t.Errorf("Destination = %q, want %q", cfg.Destination.Path, "~/Photos/Override")
	}
	if cfg.Naming.Mode != NamingTimestamp {
		t.Errorf("Naming.Mode = %q, want %q", cfg.Naming.Mode, NamingTimestamp)
	}
	if cfg.Advanced.LogFile != "/tmp/cb.log" {
		t.Errorf("LogFile = %q, want %q", cfg.Advanced.LogFile, "/tmp/cb.log")
	}
	if cfg.Advanced.VerifyMode != VerifyFull {
		t.Errorf("VerifyMode = %q, want %q", cfg.Advanced.VerifyMode, VerifyFull)
	}
}

func TestApplyEnvOverrides_EmptyLeavesDefaults(t *testing.T) {
	cfg := Defaults()
	wantDest := cfg.Destination.Path
	wantNaming := cfg.Naming.Mode

	ApplyEnvOverrides(cfg)

	if cfg.Destination.Path != wantDest {
		t.Errorf("Destination changed: got %q, want %q", cfg.Destination.Path, wantDest)
	}
	if cfg.Naming.Mode != wantNaming {
		t.Errorf("Naming changed: got %q, want %q", cfg.Naming.Mode, wantNaming)
	}
}

func TestApplyEnvOverrides_NilSafe(t *testing.T) {
	t.Parallel()
	ApplyEnvOverrides(nil) // must not panic
}
