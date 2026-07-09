package cmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/wdphoto/cardBot/app"
	"github.com/wdphoto/cardBot/cblog"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/instance"
	"github.com/wdphoto/cardBot/term"
	"github.com/wdphoto/cardBot/update"
)

// BuildInfo holds values stamped at build time and assets embedded by main.
type BuildInfo struct {
	Version   string
	Commit    string
	Date      string
	Changelog string
}

type interactiveOptions struct {
	Verbose       bool
	Dest          string
	DryRun        bool
	Reset         bool
	Setup         bool
	Daemon        bool
	TargetPathB64 string
	Args          []string
}

type commandError struct {
	code   int
	err    error
	silent bool
}

func (e commandError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e commandError) Unwrap() error {
	return e.err
}

func usageError(err error) error {
	return commandError{code: 2, err: err}
}

func silentExit(code int) error {
	return commandError{code: code, silent: true}
}

// Execute builds and runs the cardbot command tree, returning a process exit code.
func Execute(info BuildInfo) int {
	root := NewRootCommand(info)
	if err := root.Execute(); err != nil {
		var cmdErr commandError
		if errors.As(err, &cmdErr) {
			if !cmdErr.silent && cmdErr.err != nil {
				fmt.Fprintln(root.ErrOrStderr(), cmdErr.err)
			}
			return cmdErr.code
		}
		fmt.Fprintln(root.ErrOrStderr(), err)
		return 1
	}
	return 0
}

// NewRootCommand creates a fresh Cobra command tree.
// Tests should use this instead of mutating package-level command state.
func NewRootCommand(info BuildInfo) *cobra.Command {
	var opts interactiveOptions
	v := newCommandViper()

	root := &cobra.Command{
		Use:           "cardbot [card path]",
		Short:         "Camera memory-card ingest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageError(fmt.Errorf("unexpected arguments: %s", strings.Join(args[1:], " ")))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && looksLikeCommandToken(args[0]) {
				return usageError(fmt.Errorf("unknown command %q\nKnown commands: self-update, install-daemon, uninstall-daemon, daemon-status, daemon-debug", args[0]))
			}
			if cmd.Flags().Changed("version") {
				printVersion(info)
				return nil
			}
			opts.Args = args
			code := runInteractive(cmd.Context(), info, v, cmd.Flags(), opts)
			if code != 0 {
				return silentExit(code)
			}
			return nil
		},
	}

	flags := root.Flags()
	flags.Bool("version", false, "print version and exit")
	flags.BoolVar(&opts.Verbose, "verbose", false, "verbose startup output")
	flags.StringVar(&opts.Dest, "dest", "", "destination path for copied cards")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "scan cards but do not copy files")
	flags.BoolVar(&opts.Reset, "reset", false, "clear saved config and exit")
	flags.BoolVar(&opts.Setup, "setup", false, "re-run first-time setup (destination, naming)")
	flags.BoolVar(&opts.Daemon, "daemon", false, "run as background daemon watching for cards")
	flags.StringVar(&opts.TargetPathB64, "target-path-b64", "", "internal: base64-encoded target card path")
	_ = flags.MarkHidden("target-path-b64")
	bindRootFlags(v, flags)

	root.AddCommand(newSelfUpdateCommand(info))
	root.AddCommand(newInstallDaemonCommand())
	root.AddCommand(newUninstallDaemonCommand())
	root.AddCommand(newDaemonStatusCommand(info))
	root.AddCommand(newDaemonDebugCommand())

	return root
}

func newCommandViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("cardbot")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	_ = v.BindEnv("destination", "CARDBOT_DESTINATION")
	_ = v.BindEnv("naming", "CARDBOT_NAMING")
	_ = v.BindEnv("log-file", "CARDBOT_LOG_FILE")
	_ = v.BindEnv("verify-mode", "CARDBOT_VERIFY_MODE")
	return v
}

func bindRootFlags(v *viper.Viper, flags *pflag.FlagSet) {
	_ = v.BindPFlag("destination", flags.Lookup("dest"))
	_ = v.BindPFlag("dry-run", flags.Lookup("dry-run"))
	_ = v.BindPFlag("setup", flags.Lookup("setup"))
	_ = v.BindPFlag("daemon", flags.Lookup("daemon"))
}

func newSelfUpdateCommand(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:           "self-update",
		Short:         "Update cardbot to the latest release",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          noArgs("self-update"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := app.RunSelfUpdate(info.Version); code != 0 {
				return silentExit(code)
			}
			return nil
		},
	}
}

func newInstallDaemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install-daemon",
		Short: "Install the LaunchAgent daemon",
		Args:  noArgs("install-daemon"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := runInstallDaemonCommand(); code != 0 {
				return silentExit(code)
			}
			return nil
		},
	}
}

func newUninstallDaemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall-daemon",
		Short: "Uninstall the LaunchAgent daemon",
		Args:  noArgs("uninstall-daemon"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := runUninstallDaemonCommand(); code != 0 {
				return silentExit(code)
			}
			return nil
		},
	}
}

func newDaemonStatusCommand(info BuildInfo) *cobra.Command {
	var opts daemonStatusOptions
	c := &cobra.Command{
		Use:   "daemon-status",
		Short: "Show daemon and LaunchAgent status",
		Args:  noArgs("daemon-status"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.RecentLaunches < 0 {
				return usageError(fmt.Errorf("--recent-launches must be >= 0"))
			}
			if code := runDaemonStatus(opts, info.Version); code != 0 {
				return silentExit(code)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&opts.JSON, "json", false, "output daemon status as JSON")
	c.Flags().IntVar(&opts.RecentLaunches, "recent-launches", 0, "include last N launcher exec log lines")
	return c
}

func newDaemonDebugCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon-debug [status|on|off]",
		Short: "View or change daemon debug logging",
		Args: func(cmd *cobra.Command, args []string) error {
			if _, err := parseDaemonDebugMode(args); err != nil {
				return usageError(fmt.Errorf("%v\nUsage: cardbot daemon-debug [status|on|off]", err))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := runDaemonDebugCommand(args); code != 0 {
				return silentExit(code)
			}
			return nil
		},
	}
}

func noArgs(name string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return usageError(fmt.Errorf("%s does not accept arguments: %s", name, strings.Join(args, " ")))
	}
}

func runInteractive(ctx context.Context, info BuildInfo, v *viper.Viper, flags *pflag.FlagSet, opts interactiveOptions) int {
	if opts.Reset {
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
	cfgStatus := config.LoadMissing

	if cfgPath != "" {
		cfg, cfgWarnings, cfgStatus, err = config.LoadWithStatus(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %s — using defaults\n", term.FriendlyErr(err))
			cfg = config.Defaults()
		}
	} else {
		cfg = config.Defaults()
	}

	applyConfigOverrides(cfg, v, flags)

	// --- First-run or --setup: prompt for destination, then continue into the app ---
	needsSetup := opts.Setup
	if cfgPath != "" {
		if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
			needsSetup = true
		}
	}
	if needsSetup {
		if cfgStatus == config.LoadMalformed || cfgStatus == config.LoadUnsupported {
			fmt.Fprintf(os.Stderr, "Error: refusing to overwrite existing %s config at %s; move or repair it first\n", cfgStatus, cfgPath)
			return 1
		}
		setupReader := bufio.NewReader(os.Stdin)
		setupPrompter := app.NewSetupPrompter(setupReader, os.Stdout)
		promptDestinationFn := func(defaultPath string) string {
			return promptDestinationWithIO(defaultPath, setupReader, os.Stdout)
		}
		if saveErr := app.RunSetup(cfg, cfgPath, promptDestinationFn, setupPrompter.PromptNamingMode); saveErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save config: %s\n", term.FriendlyErr(saveErr))
		} else if cfgPath != "" {
			cfgStatus = config.LoadValid
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
				defer func() {
					if closeErr := logger.Close(); closeErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: log write failed: %v\n", closeErr)
					}
				}()
			}
		}
	}

	// --- Daemon mode (manages its own signal handling) ---
	if opts.Daemon {
		return runDaemonCommand(cfg, logger)
	}

	// --- Build app ---
	targetPath := ""
	if encoded := strings.TrimSpace(opts.TargetPathB64); encoded != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --target-path-b64 value: %v\n", decodeErr)
			return 1
		}
		targetPath = string(decoded)
	}
	if len(opts.Args) > 0 && strings.TrimSpace(targetPath) == "" {
		targetPath = opts.Args[0]
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

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	runCtx, stop := signal.NotifyContext(runCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a := app.New(app.Config{
		Cfg:        cfg,
		Logger:     logger,
		DryRun:     opts.DryRun,
		Version:    info.Version,
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
	s.Prefix = fmt.Sprintf("%s Starting cardBot v%s ", term.DimTS(ts1), info.Version)
	s.Start()
	time.Sleep(300 * time.Millisecond)
	s.Stop()
	fmt.Printf("\r%s Starting cardBot v%s ✓%s\n", term.DimTS(ts1), info.Version, clearEOL)

	// What's new: show changelog on first run of a new version.
	if cfg.Meta.LastSeenVersion == "" {
		// First install — record version silently.
		cfg.Meta.LastSeenVersion = info.Version
		if cfgPath != "" && cfgStatus == config.LoadValid {
			if saveErr := config.Save(cfg, cfgPath); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not record current version: %v\n", saveErr)
			}
		}
	} else if cfg.Meta.LastSeenVersion != info.Version {
		bullets := parseChangelogSection(info.Changelog, info.Version)
		if len(bullets) > 0 {
			fprintChangelog(os.Stdout, bullets)
		}
		cfg.Meta.LastSeenVersion = info.Version
		if cfgPath != "" && cfgStatus == config.LoadValid {
			if saveErr := config.Save(cfg, cfgPath); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not record current version: %v\n", saveErr)
			}
		}
	}

	// Verbose mode: show settings before the update check.
	if opts.Verbose {
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
	latest, updateErr := app.MaybeCheckForUpdate(logger, info.Version, update.CheckLatest)
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

	if opts.DryRun {
		a.Printf("%s Dry-run mode — no files will be copied\n", term.DimTS(term.Ts()))
	}

	if targetPath == "" {
		a.StartScanning()
	}

	if err := a.Run(runCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func applyConfigOverrides(cfg *config.Config, v *viper.Viper, flags *pflag.FlagSet) {
	if cfg == nil {
		return
	}

	if v != nil {
		if v.IsSet("destination") {
			cfg.Destination.Path = v.GetString("destination")
		}
		if v.IsSet("naming") {
			cfg.Naming.Mode = config.NormalizeNamingMode(v.GetString("naming"))
		}
		if v.IsSet("log-file") {
			cfg.Advanced.LogFile = v.GetString("log-file")
		}
		if v.IsSet("verify-mode") {
			cfg.Advanced.VerifyMode = config.NormalizeVerifyMode(v.GetString("verify-mode"))
		}
		return
	}

	config.ApplyEnvOverrides(cfg)

	if flags != nil && flags.Changed("dest") {
		dest, _ := flags.GetString("dest")
		cfg.Destination.Path = dest
	}
}

func printVersion(info BuildInfo) {
	if info.Commit == "none" && info.Date == "unknown" {
		fmt.Printf("cardbot %s\n", info.Version)
		return
	}
	fmt.Printf("cardbot %s (commit: %s, built: %s)\n", info.Version, info.Commit, info.Date)
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
