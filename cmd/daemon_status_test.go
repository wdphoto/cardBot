package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseDaemonStatusOptions_Default(t *testing.T) {
	t.Parallel()

	opts, err := parseDaemonStatusOptions(nil)
	if err != nil {
		t.Fatalf("parseDaemonStatusOptions error: %v", err)
	}
	if opts.JSON {
		t.Fatal("opts.JSON = true, want false")
	}
	if opts.RecentLaunches != 0 {
		t.Fatalf("opts.RecentLaunches = %d, want 0", opts.RecentLaunches)
	}
}

func TestParseDaemonStatusOptions_JSON(t *testing.T) {
	t.Parallel()

	opts, err := parseDaemonStatusOptions([]string{"--json"})
	if err != nil {
		t.Fatalf("parseDaemonStatusOptions error: %v", err)
	}
	if !opts.JSON {
		t.Fatal("opts.JSON = false, want true")
	}
	if opts.RecentLaunches != 0 {
		t.Fatalf("opts.RecentLaunches = %d, want 0", opts.RecentLaunches)
	}
}

func TestParseDaemonStatusOptions_RecentLaunches(t *testing.T) {
	t.Parallel()

	opts, err := parseDaemonStatusOptions([]string{"--recent-launches", "7"})
	if err != nil {
		t.Fatalf("parseDaemonStatusOptions error: %v", err)
	}
	if opts.RecentLaunches != 7 {
		t.Fatalf("opts.RecentLaunches = %d, want 7", opts.RecentLaunches)
	}
}

func TestParseDaemonStatusOptions_RecentLaunchesNegative(t *testing.T) {
	t.Parallel()

	_, err := parseDaemonStatusOptions([]string{"--recent-launches", "-1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseDaemonStatusOptions_UnexpectedArg(t *testing.T) {
	t.Parallel()

	_, err := parseDaemonStatusOptions([]string{"wat"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCollectSingleInstanceGuardStatus_OtherProcess(t *testing.T) {
	t.Parallel()

	st := collectSingleInstanceGuardStatus("cardbot", 1234, func(processName string, selfPID int) (bool, error) {
		if processName != "cardbot" {
			t.Fatalf("processName = %q, want %q", processName, "cardbot")
		}
		if selfPID != 1234 {
			t.Fatalf("selfPID = %d, want 1234", selfPID)
		}
		return true, nil
	})

	if !st.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if !st.HasOtherProcess {
		t.Fatal("HasOtherProcess = false, want true")
	}
	if st.CheckError != "" {
		t.Fatalf("CheckError = %q, want empty", st.CheckError)
	}
}

func TestCollectSingleInstanceGuardStatus_CheckError(t *testing.T) {
	t.Parallel()

	st := collectSingleInstanceGuardStatus("cardbot", 1234, func(processName string, selfPID int) (bool, error) {
		return false, errors.New("boom")
	})

	if !st.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if st.HasOtherProcess {
		t.Fatal("HasOtherProcess = true, want false")
	}
	if st.CheckError == "" {
		t.Fatal("CheckError empty, want value")
	}
}

func TestReadRecentLauncherExecLines_CurrentLogOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cardbot.log")
	content := "\n" +
		"[2026-03-19T01:00:00] Launcher exec: open '-a' 'Ghostty'\n" +
		"[2026-03-19T01:01:00] other line\n" +
		"[2026-03-19T01:02:00] Launcher exec: open '-a' 'Ghostty' '--args'\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	lines, err := readRecentLauncherExecLines(path, 2)
	if err != nil {
		t.Fatalf("readRecentLauncherExecLines error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "[2026-03-19T01:00:00] Launcher exec: open '-a' 'Ghostty'" {
		t.Fatalf("lines[0] = %q", lines[0])
	}
	if lines[1] != "[2026-03-19T01:02:00] Launcher exec: open '-a' 'Ghostty' '--args'" {
		t.Fatalf("lines[1] = %q", lines[1])
	}
}

func TestReadRecentLauncherExecLines_FallsBackToOldLog(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cardbot.log")
	if err := os.WriteFile(path, []byte("[now] Launcher exec: current\n"), 0600); err != nil {
		t.Fatalf("write current log: %v", err)
	}
	if err := os.WriteFile(path+".old", []byte("[old1] Launcher exec: older one\n[old2] Launcher exec: older two\n"), 0600); err != nil {
		t.Fatalf("write old log: %v", err)
	}

	lines, err := readRecentLauncherExecLines(path, 3)
	if err != nil {
		t.Fatalf("readRecentLauncherExecLines error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	if lines[0] != "[old1] Launcher exec: older one" {
		t.Fatalf("lines[0] = %q", lines[0])
	}
	if lines[2] != "[now] Launcher exec: current" {
		t.Fatalf("lines[2] = %q", lines[2])
	}
}

func TestCollectDaemonStatusReport_AppliesEnvOverrides(t *testing.T) {
	t.Setenv("CARDBOT_DESTINATION", "/tmp/cardbot-env")

	report := collectDaemonStatusReport(daemonStatusOptions{}, "0.0.10")
	if report.Daemon.WorkingDirectory != "/tmp/cardbot-env" {
		t.Fatalf("WorkingDirectory = %q, want %q", report.Daemon.WorkingDirectory, "/tmp/cardbot-env")
	}
}
