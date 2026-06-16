package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/wdphoto/cardBot/config"
)

type daemonDebugMode string

const (
	daemonDebugStatus daemonDebugMode = "status"
	daemonDebugOn     daemonDebugMode = "on"
	daemonDebugOff    daemonDebugMode = "off"
)

func runDaemonDebugCommand(args []string) int {
	mode, err := parseDaemonDebugMode(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Usage: cardbot daemon-debug [status|on|off]")
		return 2
	}

	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not determine config path: %v\n", err)
		return 1
	}

	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not load config: %v\n", err)
		return 1
	}
	if cfg == nil {
		cfg = config.Defaults()
	}

	switch mode {
	case daemonDebugStatus:
		fmt.Printf("Daemon debug logging: %s\n", boolEnabled(cfg.Daemon.Debug))
		return 0

	case daemonDebugOn, daemonDebugOff:
		enabled := mode == daemonDebugOn
		if cfg.Daemon.Debug == enabled {
			fmt.Printf("Daemon debug logging already %s\n", boolEnabled(enabled))
			return 0
		}

		cfg.Daemon.Debug = enabled
		if strings.TrimSpace(cfg.Daemon.TerminalApp) == "" {
			cfg.Daemon.TerminalApp = "Terminal"
		}
		if !cfg.Daemon.Enabled {
			cfg.Daemon.StartAtLogin = false
		}

		if err := config.Save(cfg, cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not save config: %v\n", err)
			return 1
		}

		fmt.Printf("Daemon debug logging: %s\n", boolEnabled(enabled))
		fmt.Println("Restart daemon mode to apply this setting to a running daemon process.")
		return 0
	}

	fmt.Fprintf(os.Stderr, "Error: unsupported mode %q\n", mode)
	return 2
}

func parseDaemonDebugMode(args []string) (daemonDebugMode, error) {
	if len(args) == 0 {
		return daemonDebugStatus, nil
	}
	if len(args) > 1 {
		return "", fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "", "status":
		return daemonDebugStatus, nil
	case "on", "enable", "enabled", "true", "1":
		return daemonDebugOn, nil
	case "off", "disable", "disabled", "false", "0":
		return daemonDebugOff, nil
	default:
		return "", fmt.Errorf("unknown mode %q", args[0])
	}
}
