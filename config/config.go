package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const schemaVersion = "cardbot-config-v1"

// LoadStatus describes whether a configuration file can be safely updated.
type LoadStatus string

const (
	LoadMissing     LoadStatus = "missing"
	LoadValid       LoadStatus = "valid"
	LoadMalformed   LoadStatus = "malformed"
	LoadUnsupported LoadStatus = "unsupported-schema"
)

const (
	NamingOriginal  = "original"
	NamingTimestamp = "timestamp"

	VerifySize = "size"
	VerifyFull = "full"
)

// Config holds all cardBot configuration.
type Config struct {
	Schema      string      `json:"$schema"`
	Destination Destination `json:"destination"`
	Naming      Naming      `json:"naming"`
	Daemon      Daemon      `json:"daemon"`
	Output      Output      `json:"output"`
	Advanced    Advanced    `json:"advanced"`
	Meta        Meta        `json:"meta,omitempty"`
}

// Meta holds internal tracking state (not user-editable settings).
type Meta struct {
	LastSeenVersion string `json:"last_seen_version,omitempty"`
}

// Destination settings.
type Destination struct {
	Path string `json:"path"`
}

// Naming settings.
type Naming struct {
	Mode string `json:"mode"`
}

// Daemon settings.
type Daemon struct {
	Enabled      bool     `json:"enabled"`
	StartAtLogin bool     `json:"start_at_login"`
	TerminalApp  string   `json:"terminal_app"`
	LaunchArgs   []string `json:"launch_args"`
	Debug        bool     `json:"debug"`
}

// Output settings.
type Output struct {
	Color bool `json:"color"`
}

// Advanced settings.
type Advanced struct {
	BufferSizeKB int    `json:"buffer_size_kb"`
	ExifWorkers  int    `json:"exif_workers"`
	LogFile      string `json:"log_file"`
	VerifyMode   string `json:"verify_mode"`
}

// Defaults returns a Config populated with built-in defaults.
func Defaults() *Config {
	return &Config{
		Schema: schemaVersion,
		Destination: Destination{
			Path: "~/Pictures/cardBot",
		},
		Naming: Naming{
			Mode: NamingOriginal,
		},
		Daemon: Daemon{
			Enabled:      false,
			StartAtLogin: false,
			TerminalApp:  "Terminal",
			LaunchArgs:   nil,
			Debug:        false,
		},
		Output: Output{
			Color: true,
		},
		Advanced: Advanced{
			BufferSizeKB: 256,
			ExifWorkers:  4,
			LogFile:      "~/.cardbot/cardbot.log",
			VerifyMode:   VerifySize,
		},
	}
}

// Path returns the default config file path.
// Uses os.UserConfigDir() for platform-appropriate location:
//   - macOS: ~/Library/Application Support/cardbot/config.json
//   - Linux: ~/.config/cardbot/config.json
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cardbot", "config.json"), nil
}

// Load reads the config file and merges it over defaults.
// Returns (config, warnings, error).
func Load(path string) (*Config, []string, error) {
	cfg, warnings, _, err := LoadWithStatus(path)
	return cfg, warnings, err
}

// LoadWithStatus is Load with an explicit file status. Callers that may write
// the configuration must refuse automatic writes for malformed or unsupported
// files so future-schema or recoverable user data is not replaced by defaults.
func LoadWithStatus(path string) (*Config, []string, LoadStatus, error) {
	cfg := Defaults()
	var warnings []string

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil, LoadMissing, nil
		}
		return cfg, nil, "", fmt.Errorf("reading config: %w", err)
	}

	// Parse schema field first to verify compatibility, then unmarshal into config.
	// Use a single json.Unmarshal into a struct that embeds both to avoid parsing twice.
	var probe struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return cfg, []string{"config file is malformed JSON, using defaults without overwriting it"}, LoadMalformed, nil
	}
	if probe.Schema != "" && probe.Schema != schemaVersion {
		warnings = append(warnings, fmt.Sprintf("unknown config schema %q, using defaults", probe.Schema))
		return cfg, warnings, LoadUnsupported, nil
	}

	// Unmarshal into config (merges over defaults since cfg already has them).
	if err := json.Unmarshal(data, cfg); err != nil {
		warnings = append(warnings, "config file could not be parsed, using defaults")
		return Defaults(), warnings, LoadMalformed, nil
	}

	// Validate and clamp.
	if cfg.Advanced.BufferSizeKB < 64 {
		warnings = append(warnings, fmt.Sprintf("buffer_size_kb %d is below minimum 64, using 64", cfg.Advanced.BufferSizeKB))
		cfg.Advanced.BufferSizeKB = 64
	} else if cfg.Advanced.BufferSizeKB > 4096 {
		warnings = append(warnings, fmt.Sprintf("buffer_size_kb %d exceeds maximum 4096, using 4096", cfg.Advanced.BufferSizeKB))
		cfg.Advanced.BufferSizeKB = 4096
	}
	if cfg.Advanced.ExifWorkers < 1 {
		warnings = append(warnings, fmt.Sprintf("exif_workers %d is below minimum 1, using 1", cfg.Advanced.ExifWorkers))
		cfg.Advanced.ExifWorkers = 1
	} else if cfg.Advanced.ExifWorkers > 16 {
		warnings = append(warnings, fmt.Sprintf("exif_workers %d exceeds maximum 16, using 16", cfg.Advanced.ExifWorkers))
		cfg.Advanced.ExifWorkers = 16
	}

	normalizedNaming := NormalizeNamingMode(cfg.Naming.Mode)
	if cfg.Naming.Mode != "" && normalizedNaming != cfg.Naming.Mode {
		warnings = append(warnings, fmt.Sprintf("naming.mode %q is invalid, using %q", cfg.Naming.Mode, NamingOriginal))
	}
	cfg.Naming.Mode = normalizedNaming

	normalizedVerify := NormalizeVerifyMode(cfg.Advanced.VerifyMode)
	if cfg.Advanced.VerifyMode != "" && normalizedVerify != cfg.Advanced.VerifyMode {
		warnings = append(warnings, fmt.Sprintf("advanced.verify_mode %q is invalid, using %q", cfg.Advanced.VerifyMode, VerifySize))
	}
	cfg.Advanced.VerifyMode = normalizedVerify

	if !cfg.Daemon.Enabled && cfg.Daemon.StartAtLogin {
		warnings = append(warnings, "daemon.start_at_login requires daemon.enabled=true, disabling start_at_login")
		cfg.Daemon.StartAtLogin = false
	}
	if strings.TrimSpace(cfg.Daemon.TerminalApp) == "" {
		warnings = append(warnings, "daemon.terminal_app is empty, using Terminal")
		cfg.Daemon.TerminalApp = "Terminal"
	}

	return cfg, warnings, LoadValid, nil
}

// Save writes cfg to path, creating parent directories as needed.
// File is created with 0600 permissions.
func Save(cfg *Config, path string) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	cfg.Schema = schemaVersion
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.json.tmp-*")
	if err != nil {
		return fmt.Errorf("creating temporary config: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("setting config permissions: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("syncing temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	committed = true
	return nil
}

// ExpandPath expands a leading ~/ to the user's home directory.
// Only expands "~" or "~/..." — does not handle "~user/..." syntax.
func ExpandPath(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path, err
	}
	return filepath.Join(home, path[2:]), nil
}

// ContractPath replaces the user's home directory prefix with ~.
// Reverses ExpandPath for display and storage.
func ContractPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	// Clean trailing slashes for consistent comparison.
	path = filepath.Clean(path)
	home = filepath.Clean(home)
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// NormalizeNamingMode returns a supported naming mode, defaulting to original.
func NormalizeNamingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case NamingTimestamp:
		return NamingTimestamp
	case NamingOriginal, "":
		return NamingOriginal
	default:
		return NamingOriginal
	}
}

// NormalizeVerifyMode returns a supported verify mode, defaulting to size.
func NormalizeVerifyMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case VerifyFull, "sha256", "checksum":
		return VerifyFull
	case VerifySize, "":
		return VerifySize
	default:
		return VerifySize
	}
}

// ApplyEnvOverrides reads CARDBOT_ environment variables and applies them
// to cfg, overriding any file-based values. This is useful for daemon/launchd
// contexts where environment variables are the primary configuration mechanism.
func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := os.Getenv("CARDBOT_DESTINATION"); v != "" {
		cfg.Destination.Path = v
	}
	if v := os.Getenv("CARDBOT_NAMING"); v != "" {
		cfg.Naming.Mode = NormalizeNamingMode(v)
	}
	if v := os.Getenv("CARDBOT_LOG_FILE"); v != "" {
		cfg.Advanced.LogFile = v
	}
	if v := os.Getenv("CARDBOT_VERIFY_MODE"); v != "" {
		cfg.Advanced.VerifyMode = NormalizeVerifyMode(v)
	}
}
