package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/briandowns/spinner"
	"github.com/wdphoto/cardBot/app"
	"github.com/wdphoto/cardBot/cblog"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/instance"
	"github.com/wdphoto/cardBot/term"
	"github.com/wdphoto/cardBot/update"
)

// Set at build time via -ldflags.
var (
	version = "0.9.0"
	commit  = "none"
	date    = "unknown"
)

//go:embed CHANGELOG.md
var changelogRaw string

func main() {
	if handled, code := tryRunSubcommand(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(runInteractive())
}

func tryRunSubcommand(args []string) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}

	commands := map[string]func([]string) int{
		"self-update": func(a []string) int {
			return runNoArgSubcommand("self-update", a, func() int { return app.RunSelfUpdate(version) })
		},
		"install-daemon":   func(a []string) int { return runNoArgSubcommand("install-daemon", a, runInstallDaemonCommand) },
		"uninstall-daemon": func(a []string) int { return runNoArgSubcommand("uninstall-daemon", a, runUninstallDaemonCommand) },
		"daemon-status":    runDaemonStatusCommand,
		"daemon-debug":     runDaemonDebugCommand,
	}

	cmdName := strings.TrimSpace(args[0])
	if cmd, ok := commands[cmdName]; ok {
		return true, cmd(args[1:])
	}
	if looksLikeCommandToken(cmdName) {
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n", cmdName)
		fmt.Fprintln(os.Stderr, "Known commands: self-update, install-daemon, uninstall-daemon, daemon-status, daemon-debug")
		return true, 2
	}
	return false, 0
}

func runNoArgSubcommand(name string, args []string, run func() int) int {
	if len(args) == 0 {
		return run()
	}
	fmt.Fprintf(os.Stderr, "Error: %s does not accept arguments: %s\n", name, strings.Join(args, " "))
	return 2
}

func looksLikeCommandToken(arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.HasPrefix(arg, "-") {
		return false
	}
	if isPathLikeArg(arg) {
		return false
	}
	if _, err := os.Stat(arg); err == nil {
		return false
	}
	return true
}

func isPathLikeArg(arg string) bool {
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") || strings.HasPrefix(arg, "~/") || arg == "~" {
		return true
	}
	if len(arg) >= 2 && arg[1] == ':' {
		// Windows drive-paths like C:\foo
		return true
	}
	return strings.ContainsRune(arg, os.PathSeparator)
}

func runInteractive() int {
	var (
		flagVersion       = flag.Bool("version", false, "print version and exit")
		flagVerbose       = flag.Bool("verbose", false, "verbose startup output")
		flagDest          = flag.String("dest", "", "destination path for copied cards")
		flagDryRun        = flag.Bool("dry-run", false, "scan cards but do not copy files")
		flagReset         = flag.Bool("reset", false, "clear saved config and exit")
		flagSetup         = flag.Bool("setup", false, "re-run first-time setup (destination, naming)")
		flagDaemon        = flag.Bool("daemon", false, "run as background daemon watching for cards")
		flagTargetPathB64 = flag.String("target-path-b64", "", "internal: base64-encoded target card path")
	)
	flag.Parse()

	if *flagVersion {
		printVersion()
		return 0
	}

	if *flagReset {
		return runReset()
	}

	// --- Load config ---
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not determine config path: %v\n", err)
		cfgPath = ""
	}

	var cfg *config.Config
	var cfgWarnings []string

	if cfgPath != "" {
		cfg, cfgWarnings, err = config.Load(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %s — using defaults\n", term.FriendlyErr(err))
			cfg = config.Defaults()
		}
	} else {
		cfg = config.Defaults()
	}

	// Environment variables override config file values.
	config.ApplyEnvOverrides(cfg)

	// CLI flags override everything.
	if *flagDest != "" {
		cfg.Destination.Path = *flagDest
	}

	// --- First-run or --setup: prompt for destination, then continue into the app ---
	needsSetup := *flagSetup
	if cfgPath != "" {
		if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
			needsSetup = true
		}
	}
	if needsSetup {
		setupReader := bufio.NewReader(os.Stdin)
		setupPrompter := app.NewSetupPrompter(setupReader, os.Stdout)
		promptDestinationFn := func(defaultPath string) string {
			return promptDestinationWithIO(defaultPath, setupReader, os.Stdout)
		}
		if saveErr := app.RunSetup(cfg, cfgPath, promptDestinationFn, setupPrompter.PromptNamingMode); saveErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save config: %s\n", term.FriendlyErr(saveErr))
		}
		syncDaemonAutoStartFromConfig(cfg)
		fprintSetupSummary(os.Stdout, cfg)
		fmt.Println()
	}

	// --- Set up logger ---
	var logger *cblog.Logger
	if cfg.Advanced.LogFile != "" {
		logPath, expandErr := config.ExpandPath(cfg.Advanced.LogFile)
		if expandErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not expand log path: %s\n", term.FriendlyErr(expandErr))
		} else {
			logger, err = cblog.Open(logPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not open log file: %s\n", term.FriendlyErr(err))
			} else {
				defer logger.Close()
			}
		}
	}

	// --- Daemon mode (manages its own signal handling) ---
	if *flagDaemon {
		return runDaemonCommand(cfg, logger)
	}

	// --- Build app ---
	targetPath := ""
	if encoded := strings.TrimSpace(*flagTargetPathB64); encoded != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --target-path-b64 value: %v\n", decodeErr)
			return 1
		}
		targetPath = string(decoded)
	}
	if args := flag.Args(); len(args) > 0 && strings.TrimSpace(targetPath) == "" {
		targetPath = args[0]
	}

	if strings.TrimSpace(targetPath) == "" {
		exePath, exeErr := os.Executable()
		processName := "cardbot"
		if exeErr == nil {
			processName = filepath.Base(exePath)
		}
		hasOther, checkErr := instance.HasOtherInteractiveProcess(processName, os.Getpid())
		if checkErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not verify running instances: %v\n", checkErr)
		} else if hasOther {
			fmt.Printf("%s cardBot is already running — skipping duplicate instance\n", term.DimTS(term.Ts()))
			if logger != nil {
				logger.Printf("Duplicate interactive launch skipped: another %s process is already running", processName)
			}
			return 0
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a := app.New(app.Config{
		Cfg:        cfg,
		Logger:     logger,
		DryRun:     *flagDryRun,
		Version:    version,
		TargetPath: targetPath,
	})

	// Print any config warnings now that logging is ready.
	for _, w := range cfgWarnings {
		a.Printf("%s Warning: %s\n", term.DimTS(term.Ts()), w)
	}

	// Checklist bootup — shared by normal and verbose modes.
	clearEOL := "\033[K"
	const tsWidth = 21 // "[2006-01-02T15:04:05]" = 21 chars
	indent := strings.Repeat(" ", tsWidth)

	// Print logo header.
	printLogo()

	// Step 1: Starting cardBot.
	ts1 := term.Ts()
	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	s.Prefix = fmt.Sprintf("%s Starting cardBot v%s ", term.DimTS(ts1), version)
	s.Start()
	time.Sleep(300 * time.Millisecond)
	s.Stop()
	fmt.Printf("\r%s Starting cardBot v%s ✓%s\n", term.DimTS(ts1), version, clearEOL)

	// What's new: show changelog on first run of a new version.
	if cfg.Meta.LastSeenVersion == "" {
		// First install — record version silently.
		cfg.Meta.LastSeenVersion = version
		if cfgPath != "" {
			_ = config.Save(cfg, cfgPath)
		}
	} else if cfg.Meta.LastSeenVersion != version {
		bullets := parseChangelogSection(changelogRaw, version)
		if len(bullets) > 0 {
			fprintChangelog(os.Stdout, bullets)
		}
		cfg.Meta.LastSeenVersion = version
		if cfgPath != "" {
			_ = config.Save(cfg, cfgPath)
		}
	}

	// Verbose mode: show settings before the update check.
	if *flagVerbose {
		fprintVerboseSettings(os.Stdout, cfg, cfgPath)
	}

	// Step 2: Checking for updates (network call runs during spinner).
	ts2 := term.Ts()
	s = spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	if ts2 == ts1 {
		s.Prefix = indent + " Checking for updates "
	} else {
		s.Prefix = fmt.Sprintf("%s Checking for updates ", term.DimTS(ts2))
	}
	s.Start()
	latest, updateErr := app.MaybeCheckForUpdate(logger, version, update.CheckLatest)
	s.Stop()
	updateMark := "✓"
	if updateErr != nil {
		updateMark = "✗ NO SIGNAL"
	}
	if ts2 == ts1 {
		fmt.Printf("\r%s Checking for updates %s%s\n", indent, updateMark, clearEOL)
	} else {
		fmt.Printf("\r%s Checking for updates %s%s\n", term.DimTS(ts2), updateMark, clearEOL)
	}

	// Update notification.
	if latest != "" && updateErr == nil {
		fmt.Printf("%s UPDATE AVAILABLE (v%s)\n", indent, latest)
		fmt.Printf("%s Run 'cardbot self-update'\n", indent)
	}

	// Sync last printed timestamp with app for dedup in scanning output.
	if ts2 != ts1 {
		a.SetLastTS(ts2)
	} else {
		a.SetLastTS(ts1)
	}

	if *flagDryRun {
		a.Printf("%s Dry-run mode — no files will be copied\n", term.DimTS(term.Ts()))
	}

	if targetPath == "" {
		a.StartScanning()
	}

	if err := a.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func printVersion() {
	if commit == "none" && date == "unknown" {
		fmt.Printf("cardbot %s\n", version)
		return
	}
	fmt.Printf("cardbot %s (commit: %s, built: %s)\n", version, commit, date)
}

func runReset() int {
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not determine config path: %v\n", err)
		return 1
	}
	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: could not remove config: %v\n", err)
		return 1
	}
	fmt.Println("Config cleared. Please restart cardBot.")
	return 0
}
