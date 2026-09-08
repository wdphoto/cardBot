package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wdphoto/cardBot/launch"
)

func daemonStatusCommand(t *testing.T) *cobra.Command {
	t.Helper()
	root := NewRootCommand(BuildInfo{Version: "test"})
	for _, c := range root.Commands() {
		if c.Use == "daemon-status" {
			return c
		}
	}
	t.Fatal("daemon-status subcommand not found")
	return nil
}

func TestDaemonStatusCommand_Default(t *testing.T) {
	t.Parallel()

	c := daemonStatusCommand(t)
	if err := c.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags error: %v", err)
	}
	if v, _ := c.Flags().GetBool("json"); v {
		t.Fatal("json = true, want false")
	}
	if v, _ := c.Flags().GetInt("recent-launches"); v != 0 {
		t.Fatalf("recent-launches = %d, want 0", v)
	}
}

func TestDaemonStatusCommand_JSON(t *testing.T) {
	t.Parallel()

	c := daemonStatusCommand(t)
	if err := c.ParseFlags([]string{"--json"}); err != nil {
		t.Fatalf("ParseFlags error: %v", err)
	}
	if v, _ := c.Flags().GetBool("json"); !v {
		t.Fatal("json = false, want true")
	}
}

func TestDaemonStatusCommand_RecentLaunches(t *testing.T) {
	t.Parallel()

	c := daemonStatusCommand(t)
	if err := c.ParseFlags([]string{"--recent-launches", "7"}); err != nil {
		t.Fatalf("ParseFlags error: %v", err)
	}
	if v, _ := c.Flags().GetInt("recent-launches"); v != 7 {
		t.Fatalf("recent-launches = %d, want 7", v)
	}
}

func TestDaemonStatusCommand_NegativeRecentLaunches(t *testing.T) {
	t.Parallel()

	c := daemonStatusCommand(t)
	if err := c.ParseFlags([]string{"--recent-launches", "-1"}); err != nil {
		t.Fatalf("ParseFlags error: %v", err)
	}
	// RunE returns the early negative error before runDaemonStatus (no live host).
	err := c.RunE(c, nil)
	if err == nil || !strings.Contains(err.Error(), "--recent-launches must be >= 0") {
		t.Fatalf("RunE error = %v, want early negative usage error", err)
	}
}

func TestDaemonStatusCommand_UnexpectedArg(t *testing.T) {
	t.Parallel()

	c := daemonStatusCommand(t)
	if err := c.Args(c, []string{"wat"}); err == nil {
		t.Fatal("expected error for unexpected argument")
	}
}

func TestDaemonStatusCommand_UnknownOption(t *testing.T) {
	t.Parallel()

	c := daemonStatusCommand(t)
	if err := c.ParseFlags([]string{"--bogus"}); err == nil {
		t.Fatal("expected error for unknown option")
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
	isolatedDaemonConfigPath(t)
	t.Setenv("CARDBOT_DESTINATION", "/tmp/cardbot-env")

	report := collectDaemonStatusReportWith(daemonStatusOptions{}, "dev",
		func(string, int) (bool, error) { return false, nil },
		func() daemonStatusDIReport { return daemonStatusDIReport{} },
		func() (launch.Status, error) { return launch.Status{}, nil },
	)
	if report.Daemon.WorkingDirectory != "/tmp/cardbot-env" {
		t.Fatalf("WorkingDirectory = %q, want %q", report.Daemon.WorkingDirectory, "/tmp/cardbot-env")
	}
}

func TestCollectDaemonStatusReport_PreservesTrailingSpaceWorkingDir(t *testing.T) {
	isolatedDaemonConfigPath(t)
	t.Setenv("CARDBOT_DESTINATION", "/tmp/cardbot-env  ")

	report := collectDaemonStatusReportWith(daemonStatusOptions{}, "dev",
		func(string, int) (bool, error) { return false, nil },
		func() daemonStatusDIReport { return daemonStatusDIReport{} },
		func() (launch.Status, error) { return launch.Status{}, nil },
	)
	if report.Daemon.WorkingDirectory != "/tmp/cardbot-env  " {
		t.Fatalf("WorkingDirectory = %q, want preserved %q", report.Daemon.WorkingDirectory, "/tmp/cardbot-env  ")
	}
}
