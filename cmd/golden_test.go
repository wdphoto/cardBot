package cmd

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/wdphoto/cardBot/config"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestFprintDaemonStatusReport_default(t *testing.T) {
	t.Parallel()

	report := daemonStatusReport{
		Version:    "0.6.1",
		PID:        12345,
		ConfigPath: "~/Library/Application Support/cardbot/config.json",
		Daemon: daemonStatusDaemonReport{
			Enabled:          true,
			StartAtLogin:     true,
			TerminalApp:      "Terminal",
			WorkingDirectory: "~/Pictures/cardBot",
			LaunchArgs:       []string{},
			Debug:            false,
		},
		SingleInstanceGuard: daemonStatusSIGuardReport{
			Enabled:     true,
			ProcessName: "cardbot",
		},
		DaemonInstance: daemonStatusDIReport{
			Running:    true,
			RunningPID: 54321,
		},
		LaunchAgent: daemonStatusLAReport{
			Supported: true,
			PlistPath: "~/Library/LaunchAgents/com.cardbot.daemon.plist",
			Installed: true,
			Loaded:    true,
		},
	}

	var buf bytes.Buffer
	fprintDaemonStatusReport(&buf, report)

	golden := filepath.Join("testdata", t.Name()+".golden")
	if *updateGolden {
		os.MkdirAll("testdata", 0755)
		os.WriteFile(golden, buf.Bytes(), 0644)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file: %v (run with -update to create)", err)
	}

	if diff := cmp.Diff(string(want), buf.String()); diff != "" {
		t.Errorf("output mismatch (-want +got):\n%s", diff)
	}
}

func TestFprintVerboseSettings_defaults(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfgPath := "~/Library/Application Support/cardbot/config.json"

	var buf bytes.Buffer
	fprintVerboseSettings(&buf, cfg, cfgPath)

	golden := filepath.Join("testdata", t.Name()+".golden")
	if *updateGolden {
		os.MkdirAll("testdata", 0755)
		os.WriteFile(golden, buf.Bytes(), 0644)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file: %v (run with -update to create)", err)
	}

	if diff := cmp.Diff(string(want), buf.String()); diff != "" {
		t.Errorf("output mismatch (-want +got):\n%s", diff)
	}
}
