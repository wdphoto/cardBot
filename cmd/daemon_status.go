package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/daemon"
	"github.com/wdphoto/cardBot/instance"
	"github.com/wdphoto/cardBot/launch"
)

type daemonStatusOptions struct {
	JSON           bool
	RecentLaunches int
}

type daemonStatusReport struct {
	Version                     string                    `json:"version"`
	PID                         int                       `json:"pid"`
	ConfigPath                  string                    `json:"config_path,omitempty"`
	ConfigPathError             string                    `json:"config_path_error,omitempty"`
	ConfigLoadError             string                    `json:"config_load_error,omitempty"`
	ConfigWarnings              []string                  `json:"config_warnings,omitempty"`
	Daemon                      daemonStatusDaemonReport  `json:"daemon"`
	SingleInstanceGuard         daemonStatusSIGuardReport `json:"single_instance_guard"`
	DaemonInstance              daemonStatusDIReport      `json:"daemon_instance"`
	LaunchAgent                 daemonStatusLAReport      `json:"launch_agent"`
	RecentLauncherExecRequested int                       `json:"recent_launcher_exec_requested,omitempty"`
	RecentLauncherExec          []string                  `json:"recent_launcher_exec,omitempty"`
	RecentLauncherExecError     string                    `json:"recent_launcher_exec_error,omitempty"`
}

type daemonStatusDaemonReport struct {
	Enabled          bool     `json:"enabled"`
	StartAtLogin     bool     `json:"start_at_login"`
	TerminalApp      string   `json:"terminal_app"`
	WorkingDirectory string   `json:"working_directory"`
	LaunchArgs       []string `json:"launch_args"`
	Debug            bool     `json:"debug"`
}

type daemonStatusSIGuardReport struct {
	Enabled         bool   `json:"enabled"`
	ProcessName     string `json:"process_name"`
	HasOtherProcess bool   `json:"has_other_process"`
	CheckError      string `json:"check_error,omitempty"`
}

type daemonStatusDIReport struct {
	PIDPath    string `json:"pid_path,omitempty"`
	Running    bool   `json:"running"`
	RunningPID int    `json:"running_pid,omitempty"`
	CheckError string `json:"check_error,omitempty"`
}

type daemonStatusLAReport struct {
	Supported bool   `json:"supported"`
	PlistPath string `json:"plist_path,omitempty"`
	Installed bool   `json:"installed"`
	Loaded    bool   `json:"loaded"`
	Error     string `json:"error,omitempty"`
}

func runDaemonStatusCommand(args []string) int {
	opts, err := parseDaemonStatusOptions(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	return runDaemonStatus(opts, "")
}

func runDaemonStatus(opts daemonStatusOptions, version string) int {
	report := collectDaemonStatusReport(opts, version)
	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not encode daemon status JSON: %v\n", err)
			return 1
		}
		return 0
	}

	fprintDaemonStatusReport(os.Stdout, report)
	return 0
}

func parseDaemonStatusOptions(args []string) (daemonStatusOptions, error) {
	fs := flag.NewFlagSet("daemon-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "output daemon status as JSON")
	recentLaunches := fs.Int("recent-launches", 0, "include last N launcher exec log lines")
	if err := fs.Parse(args); err != nil {
		return daemonStatusOptions{}, err
	}
	if fs.NArg() > 0 {
		return daemonStatusOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *recentLaunches < 0 {
		return daemonStatusOptions{}, fmt.Errorf("--recent-launches must be >= 0")
	}
	return daemonStatusOptions{JSON: *jsonOut, RecentLaunches: *recentLaunches}, nil
}

func collectDaemonStatusReport(opts daemonStatusOptions, version string) daemonStatusReport {
	processName := "cardbot"
	if exe, err := os.Executable(); err == nil {
		processName = filepath.Base(exe)
	}
	pid := os.Getpid()
	report := daemonStatusReport{
		Version:             version,
		PID:                 pid,
		SingleInstanceGuard: collectSingleInstanceGuardStatus(processName, pid, instance.HasOtherProcess),
		LaunchAgent:         daemonStatusLAReport{Supported: runtime.GOOS == "darwin"},
	}

	cfg := config.Defaults()
	cfgPath, cfgPathErr := config.Path()
	if cfgPathErr != nil {
		report.ConfigPathError = cfgPathErr.Error()
	} else {
		report.ConfigPath = cfgPath
		loaded, warnings, loadErr := config.Load(cfgPath)
		if loadErr != nil {
			report.ConfigLoadError = loadErr.Error()
		} else {
			cfg = loaded
			report.ConfigWarnings = warnings
		}
	}

	// Runtime behavior honors CARDBOT_* env overrides, so status should too.
	config.ApplyEnvOverrides(cfg)

	launchArgs := cfg.Daemon.LaunchArgs
	if launchArgs == nil {
		launchArgs = []string{}
	}
	terminalApp := normalizeDaemonTerminalAppForLaunch(cfg.Daemon.TerminalApp)
	report.Daemon = daemonStatusDaemonReport{
		Enabled:          cfg.Daemon.Enabled,
		StartAtLogin:     cfg.Daemon.StartAtLogin,
		TerminalApp:      terminalApp,
		WorkingDirectory: config.ContractPath(resolveDaemonWorkingDirectory(cfg.Destination.Path)),
		LaunchArgs:       launchArgs,
		Debug:            cfg.Daemon.Debug,
	}

	// Check if a daemon is currently running via PID file.
	report.DaemonInstance = collectDaemonInstanceStatus()

	if opts.RecentLaunches > 0 {
		report.RecentLauncherExecRequested = opts.RecentLaunches
		logPath, err := config.ExpandPath(cfg.Advanced.LogFile)
		if err != nil {
			report.RecentLauncherExecError = fmt.Sprintf("resolving log path: %v", err)
		} else {
			lines, readErr := readRecentLauncherExecLines(logPath, opts.RecentLaunches)
			if readErr != nil {
				report.RecentLauncherExecError = readErr.Error()
			} else {
				report.RecentLauncherExec = lines
			}
		}
	}

	if !report.LaunchAgent.Supported {
		return report
	}

	st, err := launch.CurrentStatus()
	if err != nil {
		report.LaunchAgent.Error = err.Error()
		return report
	}
	report.LaunchAgent.PlistPath = st.PlistPath
	report.LaunchAgent.Installed = st.Installed
	report.LaunchAgent.Loaded = st.Loaded
	return report
}

func collectSingleInstanceGuardStatus(processName string, selfPID int, checker func(processName string, selfPID int) (bool, error)) daemonStatusSIGuardReport {
	report := daemonStatusSIGuardReport{
		Enabled:     true,
		ProcessName: processName,
	}
	if checker == nil {
		report.CheckError = "no checker configured"
		return report
	}
	hasOther, err := checker(processName, selfPID)
	if err != nil {
		report.CheckError = err.Error()
		return report
	}
	report.HasOtherProcess = hasOther
	return report
}

func collectDaemonInstanceStatus() daemonStatusDIReport {
	report := daemonStatusDIReport{}

	pidPath, err := daemon.PidPath()
	if err != nil {
		report.CheckError = fmt.Sprintf("resolving PID path: %v", err)
		return report
	}
	report.PIDPath = pidPath

	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			report.Running = false
			return report
		}
		report.CheckError = fmt.Sprintf("reading PID file: %v", err)
		return report
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		report.CheckError = "PID file contains invalid data"
		return report
	}

	report.RunningPID = pid
	process, err := os.FindProcess(pid)
	if err != nil {
		report.Running = false
		return report
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		report.Running = false
		return report
	}
	report.Running = true
	return report
}

func fprintDaemonStatusReport(w io.Writer, report daemonStatusReport) {
	fmt.Fprintln(w, "cardBot Daemon Status")
	fmt.Fprintln(w, "────────────────────────────────────────")
	fmt.Fprintf(w, "Version: %s\n", report.Version)
	fmt.Fprintf(w, "PID: %d\n", report.PID)

	if report.ConfigPathError != "" {
		fmt.Fprintf(w, "Config path: unavailable (%s)\n", report.ConfigPathError)
	} else {
		fmt.Fprintf(w, "Config path: %s\n", report.ConfigPath)
	}
	if report.ConfigLoadError != "" {
		fmt.Fprintf(w, "Config load: error (%s), using defaults\n", report.ConfigLoadError)
	}
	if len(report.ConfigWarnings) > 0 {
		fmt.Fprintf(w, "Config warnings: %d\n", len(report.ConfigWarnings))
	}

	fmt.Fprintf(w, "Daemon enabled: %s\n", boolEnabled(report.Daemon.Enabled))
	fmt.Fprintf(w, "Start at login: %s\n", boolEnabled(report.Daemon.StartAtLogin))
	fmt.Fprintf(w, "Terminal app: %s\n", daemonTerminalAppLabel(report.Daemon.TerminalApp))
	fmt.Fprintf(w, "Working directory: %s\n", report.Daemon.WorkingDirectory)
	fmt.Fprintf(w, "Debug logging: %s\n", boolEnabled(report.Daemon.Debug))
	if len(report.Daemon.LaunchArgs) == 0 {
		fmt.Fprintln(w, "Launch args: (default)")
	} else {
		fmt.Fprintf(w, "Launch args: %v\n", report.Daemon.LaunchArgs)
	}

	fmt.Fprintf(w, "Single-instance guard: %s\n", boolEnabled(report.SingleInstanceGuard.Enabled))
	fmt.Fprintf(w, "Guard process name: %s\n", report.SingleInstanceGuard.ProcessName)
	if report.SingleInstanceGuard.CheckError != "" {
		fmt.Fprintf(w, "Guard check: error (%s)\n", report.SingleInstanceGuard.CheckError)
	} else {
		fmt.Fprintf(w, "Other cardBot process running: %s\n", boolYesNo(report.SingleInstanceGuard.HasOtherProcess))
	}

	// Daemon instance status via PID file.
	if report.DaemonInstance.CheckError != "" {
		fmt.Fprintf(w, "Daemon instance: error (%s)\n", report.DaemonInstance.CheckError)
	} else if report.DaemonInstance.Running {
		fmt.Fprintf(w, "Daemon running: yes (PID %d)\n", report.DaemonInstance.RunningPID)
	} else {
		fmt.Fprintln(w, "Daemon running: no")
	}

	if report.RecentLauncherExecRequested > 0 {
		if report.RecentLauncherExecError != "" {
			fmt.Fprintf(w, "Recent launcher exec: unavailable (%s)\n", report.RecentLauncherExecError)
		} else if len(report.RecentLauncherExec) == 0 {
			fmt.Fprintf(w, "Recent launcher exec (%d): none found\n", report.RecentLauncherExecRequested)
		} else {
			fmt.Fprintf(w, "Recent launcher exec (%d):\n", len(report.RecentLauncherExec))
			for _, line := range report.RecentLauncherExec {
				fmt.Fprintf(w, "  %s\n", line)
			}
		}
	}

	if !report.LaunchAgent.Supported {
		fmt.Fprintln(w, "LaunchAgent: unsupported on this platform")
		return
	}
	if report.LaunchAgent.Error != "" {
		fmt.Fprintf(w, "LaunchAgent status: error (%s)\n", report.LaunchAgent.Error)
		return
	}

	fmt.Fprintf(w, "LaunchAgent plist: %s\n", report.LaunchAgent.PlistPath)
	fmt.Fprintf(w, "LaunchAgent installed: %s\n", boolEnabled(report.LaunchAgent.Installed))
	fmt.Fprintf(w, "LaunchAgent loaded: %s\n", boolEnabled(report.LaunchAgent.Loaded))
}

func readRecentLauncherExecLines(logPath string, limit int) ([]string, error) {
	if strings.TrimSpace(logPath) == "" {
		return nil, fmt.Errorf("log path is empty")
	}
	if limit <= 0 {
		return []string{}, nil
	}

	current, err := readRecentMatchingLogLines(logPath, "Launcher exec:", limit)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		current = []string{}
	}

	if len(current) >= limit {
		return current, nil
	}

	remaining := limit - len(current)
	older, oldErr := readRecentMatchingLogLines(logPath+".old", "Launcher exec:", remaining)
	if oldErr != nil {
		if os.IsNotExist(oldErr) {
			return current, nil
		}
		return nil, oldErr
	}

	// Keep chronological order: older log lines first, then current log lines.
	return append(older, current...), nil
}

func readRecentMatchingLogLines(path, needle string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	buf := make([]string, limit)
	count := 0
	next := 0

	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || !strings.Contains(line, needle) {
			continue
		}
		buf[next] = line
		next = (next + 1) % limit
		if count < limit {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if count == 0 {
		return []string{}, nil
	}

	start := 0
	if count == limit {
		start = next
	}

	matches := make([]string, 0, count)
	for i := 0; i < count; i++ {
		idx := (start + i) % limit
		matches = append(matches, buf[idx])
	}
	return matches, nil
}
