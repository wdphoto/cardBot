package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wdphoto/cardBot/config"
)

func TestSetupRequiresKnownLoadAndSuccessfulSave(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               config.LoadStatus
		wantPrompt, wantSave bool
	}{
		{"failed read", "", false, false},
		{"malformed", config.LoadMalformed, false, false},
		{"future", config.LoadUnsupported, false, false},
		{"failed save", config.LoadValid, true, false},
		{"first run", config.LoadMissing, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if tc.name == "failed save" || tc.name == "failed read" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			status := tc.status
			if tc.name == "failed read" {
				_, _, got, err := config.LoadWithStatus(path)
				if err == nil {
					t.Fatal("expected read error for directory")
				}
				status = got
			}
			var out bytes.Buffer
			prompted, synced := false, false
			prompt := func(s string) string { prompted = true; return s }
			err := persistSetup(config.Defaults(), path, status, prompt, prompt, func(*config.Config) { synced = true }, &out)
			if (err == nil) != tc.wantSave || prompted != tc.wantPrompt || synced != tc.wantSave ||
				strings.Contains(out.String(), "Setup saved.") != tc.wantSave {
				t.Fatalf("prompted=%v synced=%v out=%q err=%v", prompted, synced, out.String(), err)
			}
			if tc.wantSave {
				if _, _, st, err := config.LoadWithStatus(path); err != nil || st != config.LoadValid {
					t.Fatalf("saved: %v %v", st, err)
				}
			}
		})
	}
}

func TestVersionStampPreservesSavedSettings(t *testing.T) {
	t.Setenv("CARDBOT_VERIFY_MODE", "full")
	for _, last := range []string{"", "older"} {
		path := filepath.Join(t.TempDir(), "config.json")
		const prefix = `{"destination":{"path":"saved"},"advanced":{"verify_mode":"size"},"other":42,"meta":{"keep":true,"last_seen_version":"`
		const suffix = `"}}`
		if err := os.WriteFile(path, []byte(prefix+last+suffix), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, _, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		v, flags := testOverrideViperAndFlags()
		if err := flags.Set("dest", "once"); err != nil {
			t.Fatal(err)
		}
		applyConfigOverrides(cfg, v, flags)
		if cfg.Destination.Path != "once" || cfg.Advanced.VerifyMode != "full" {
			t.Fatalf("runtime: %+v", cfg)
		}
		if err := config.RecordVersion(path, "new"); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var actual, want map[string]any
		if err := json.Unmarshal(got, &actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(prefix+"new"+suffix), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("stamp changed settings: %s", got)
		}
	}
}
