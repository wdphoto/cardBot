package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wdphoto/cardBot/config"
)

func TestDaemonConfig_IsolatedStartupValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      string
		wantValid bool
	}{
		{"missing", "", false},
		{"malformed", `{"daemon":`, false},
		{"unsupported", `{"$schema":"future-schema"}`, false},
		{"wrong type", `{"daemon":true}`, false},
		{"valid", `{"daemon":{"enabled":true,"start_at_login":true}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.json")
			if tc.data != "" {
				if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, _, status, err := config.LoadWithStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateDaemonConfig(path, status); (err == nil) != tc.wantValid {
				t.Fatalf("startup validation = %v, want valid=%v", err, tc.wantValid)
			}
			data, err := os.ReadFile(path)
			if tc.data == "" {
				if !os.IsNotExist(err) {
					t.Fatalf("validation created config: %v", err)
				}
			} else if err != nil || string(data) != tc.data {
				t.Fatalf("validation changed config: %q, %v", data, err)
			}
		})
	}
	if err := validateDaemonConfig("", config.LoadValid); err == nil {
		t.Fatal("missing config path accepted")
	}
	path := t.TempDir() // A directory is a deterministic read error, even as root.
	_, _, status, err := config.LoadWithStatus(path)
	if err == nil || validateDaemonConfig(path, status) == nil {
		t.Fatal("unreadable config accepted")
	}
}

func TestDaemonCommand_RejectsInteractiveFlags(t *testing.T) {
	for _, flag := range []string{"--setup", "--reset"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			root := NewRootCommand(BuildInfo{Version: "dev"})
			// Even a validation regression must not invoke real setup/detection.
			root.RunE = func(*cobra.Command, []string) error { t.Fatal("daemon accepted interactive flag"); return nil }
			root.SetArgs([]string{"--daemon", flag})
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "none of the others") {
				t.Fatalf("flag validation = %v", err)
			}
		})
	}
}

func isolatedDaemonConfigPath(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDaemonPreferences_IsolatedSaveAndDebug(t *testing.T) {
	path := isolatedDaemonConfigPath(t)
	cfg := config.Defaults()
	cfg.Destination.Path = filepath.Join(filepath.Dir(path), "destination  ")
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	// Exercise preference changes separately from the fake launchctl lifecycle
	// in launch's tests. Never install a live agent from a command test.
	updateSavedDaemonPrefs(func(cfg *config.Config) {
		cfg.Daemon.Enabled = true
		cfg.Daemon.StartAtLogin = true
	})
	if err := executeTestRoot("daemon-debug", "on"); err != nil {
		t.Fatal(err)
	}
	got, _, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Daemon.Enabled || !got.Daemon.StartAtLogin || !got.Daemon.Debug || got.Destination.Path != cfg.Destination.Path {
		t.Fatalf("saved preferences = %+v, destination=%q", got.Daemon, got.Destination.Path)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeTestRoot("daemon-debug", "status"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("debug status changed config: %v", err)
	}
	updateSavedDaemonPrefs(func(cfg *config.Config) { cfg.Daemon.StartAtLogin = false })
	if err := executeTestRoot("daemon-debug", "off"); err != nil {
		t.Fatal(err)
	}
	got, _, err = config.Load(path)
	if err != nil || got.Daemon.StartAtLogin || got.Daemon.Debug {
		t.Fatalf("disabled preferences = %+v, %v", got, err)
	}
}

func TestDaemonPreferences_PreserveUnreadableConfig(t *testing.T) {
	path := isolatedDaemonConfigPath(t)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	updateSavedDaemonPrefs(func(*config.Config) { t.Fatal("must not replace unreadable config with defaults") })
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		t.Fatalf("unreadable config changed: %v", err)
	}
}

func TestDaemonPreferences_PreserveBrokenConfig(t *testing.T) {
	for _, data := range []string{`{"daemon":`, `{"$schema":"future-schema"}`} {
		t.Run(data, func(t *testing.T) {
			path := isolatedDaemonConfigPath(t)
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			updateSavedDaemonPrefs(func(*config.Config) { t.Fatal("must not mutate broken configuration") })
			for _, mode := range []string{"on", "off"} {
				if err := executeTestRoot("daemon-debug", mode); err == nil {
					t.Fatalf("debug %s accepted broken config", mode)
				}
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != data {
				t.Fatalf("broken config changed: %q, %v", got, err)
			}
		})
	}
}
