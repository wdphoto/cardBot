package main

import (
	"fmt"
	"io"

	"github.com/wdphoto/cardBot/app"
	"github.com/wdphoto/cardBot/config"
)

// fprintVerboseSettings prints the settings table after the shared bootup checklist.
func fprintVerboseSettings(w io.Writer, cfg *config.Config, cfgPath string) {
	fmt.Fprintln(w)
	if cfgPath != "" {
		fmt.Fprintf(w, "  Config       %s\n", config.ContractPath(cfgPath))
	}
	fmt.Fprintf(w, "  Destination  %s\n", cfg.Destination.Path)
	fmt.Fprintf(w, "  Naming       %s\n", app.NamingModeLabel(cfg.Naming.Mode))
	fmt.Fprintf(w, "  Verify       %s\n", cfg.Advanced.VerifyMode)
	fmt.Fprintf(w, "  Buffer       %d KB\n", cfg.Advanced.BufferSizeKB)
	fmt.Fprintf(w, "  Workers      %d\n", cfg.Advanced.ExifWorkers)
	fmt.Fprintf(w, "  Colors       %s\n", boolEnabled(cfg.Output.Color))
	fmt.Fprintf(w, "  Daemon       %s\n", boolEnabled(cfg.Daemon.Enabled))
	if cfg.Daemon.Enabled {
		fmt.Fprintf(w, "  Login        %s\n", boolEnabled(cfg.Daemon.StartAtLogin))
		if cfg.Daemon.Debug {
			fmt.Fprintln(w, "  Debug        enabled")
		}
	}
	fmt.Fprintln(w)
}
