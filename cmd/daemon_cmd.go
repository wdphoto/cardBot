package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wdphoto/cardBot/cblog"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/daemon"
	"github.com/wdphoto/cardBot/instance"
	"github.com/wdphoto/cardBot/launch"
	"github.com/wdphoto/cardBot/term"
)

// runDaemonCommand starts the background daemon that watches for card
// insertions and launches a terminal window with cardbot targeting the
// detected card.
func runDaemonCommand(cfg *config.Config, logger *cblog.Logger) int {
	if cfg == nil {
		cfg = config.Defaults()
	}

	cardbotBinary, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not determine executable path: %v\n", err)
		return 1
	}

	logf := func(format string, args ...any) {
		if logger != nil {
			logger.Printf(format, args...)
		}
	}
	debugEnabled := cfg.Daemon.Debug
	debugf := func(format string, args ...any) {
		if !debugEnabled {
			return
		}
		msg := fmt.Sprintf(format, args...)
		fmt.Printf("%s Debug: %s\n", term.DimTS(term.Ts()), msg)
		logf("Debug: %s", msg)
	}

	appName := normalizeDaemonTerminalAppForLaunch(cfg.Daemon.TerminalApp)
	workingDir := resolveDaemonWorkingDirectory(cfg.Destination.Path)
	fmt.Printf("%s Daemon terminal app: %s\n", term.DimTS(term.Ts()), daemonTerminalAppLabel(appName))
	fmt.Printf("%s Daemon working directory: %s\n", term.DimTS(term.Ts()), daemonWorkingDirectoryLabel(cfg.Destination.Path, workingDir))
	if len(cfg.Daemon.LaunchArgs) > 0 {
		fmt.Printf("%s Daemon custom launch args enabled\n", term.DimTS(term.Ts()))
	}
	if debugEnabled {
		fmt.Printf("%s Daemon debug logging: enabled\n", term.DimTS(term.Ts()))
	}
	processName := filepath.Base(cardbotBinary)
	debugf("daemon startup: binary=%q process=%q terminal=%q working_dir=%q custom_launch_args=%d", cardbotBinary, processName, appName, workingDir, len(cfg.Daemon.LaunchArgs))

	d := daemon.New(daemon.Config{
		OnCardInserted: func(path string) {
			debugf("card insert callback: mount=%q", path)

			// Strict single-instance guard: if any other cardbot process is running,
			// do not auto-launch a second interactive instance.
			hasOther, checkErr := instance.HasOtherProcess(processName, os.Getpid())
			if checkErr != nil {
				fmt.Fprintf(os.Stderr, "%s Warning: single-instance check failed (%v)\n", term.DimTS(term.Ts()), checkErr)
				logf("Single-instance check failed: %v", checkErr)
			} else if hasOther {
				debugf("single-instance guard blocked launch for %q", path)
				fmt.Printf("%s cardBot already running in another process — skipping auto-launch\n", term.DimTS(term.Ts()))
				logf("Auto-launch skipped for %s: another cardbot process is running", path)
				return
			}

			debugf("launch attempt: terminal=%q mount=%q", appName, path)
			err := launch.Open(launch.Options{
				TerminalApp:      appName,
				WorkingDirectory: workingDir,
				LaunchArgs:       cfg.Daemon.LaunchArgs,
				CardBotBinary:    cardbotBinary,
				MountPath:        path,
				Debugf:           debugf,
				Logf:             logf,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s Launch failed: %v\n", term.DimTS(term.Ts()), err)
				if hint := daemonLaunchHint(err); hint != "" {
					fmt.Fprintf(os.Stderr, "%s Hint: %s\n", term.DimTS(term.Ts()), hint)
					logf("Launch hint for %s: %s", path, hint)
				}
				logf("Launch failed for %s: %v", path, err)
				return
			}
			fmt.Printf("%s Launched %s for %s\n", term.DimTS(term.Ts()), appName, path)
			logf("Launched terminal app %q for %s", appName, path)
		},
	})

	if err := d.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func runInstallDaemonCommand() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not determine executable path: %v\n", err)
		return 1
	}

	plist, err := launch.Install(exe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	updateSavedDaemonPrefs(func(cfg *config.Config) {
		cfg.Daemon.Enabled = true
		cfg.Daemon.StartAtLogin = true
	})

	fmt.Printf("Installed LaunchAgent: %s\n", plist)
	fmt.Println("cardBot daemon will start at login.")
	return 0
}

func runUninstallDaemonCommand() int {
	plist, err := launch.Uninstall()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	updateSavedDaemonPrefs(func(cfg *config.Config) {
		cfg.Daemon.StartAtLogin = false
	})

	fmt.Printf("Uninstalled LaunchAgent: %s\n", plist)
	fmt.Println("cardBot daemon will no longer start at login.")
	return 0
}
