package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wdphoto/cardBot/app"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/pick"
)

func promptDestinationWithIO(defaultPath string, in *bufio.Reader, out io.Writer) string {
	if in == nil {
		in = bufio.NewReader(os.Stdin)
	}
	if out == nil {
		out = os.Stdout
	}

	fmt.Fprintln(out, "Welcome to cardBot!")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Where should cardBot copy your work?")
	fmt.Fprintln(out)

	expanded, err := config.ExpandPath(defaultPath)
	if err != nil {
		expanded = defaultPath
	}

	picked, err := pick.Folder(expanded)
	if err == nil && picked != "" {
		fmt.Fprintf(out, "Destination: %s\n", picked)
		return picked
	}

	// Fallback: readline with shared buffered input.
	return promptDestinationReadlineIO(expanded, in, out)
}

func promptDestinationReadlineIO(defaultPath string, in *bufio.Reader, out io.Writer) string {
	if in == nil {
		in = bufio.NewReader(os.Stdin)
	}
	if out == nil {
		out = os.Stdout
	}

	fmt.Fprintf(out, "Destination [%s]: ", defaultPath)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultPath
	}
	return line
}

func fprintSetupSummary(w io.Writer, cfg *config.Config) {
	if cfg == nil {
		return
	}

	fmt.Fprintln(w, "Setup saved.")
	fmt.Fprintln(w, "- Destination:", cfg.Destination.Path)
	fmt.Fprintln(w, "- Naming mode:", app.NamingModeLabel(cfg.Naming.Mode))
	// Daemon options (auto-launch, start-at-login) are intentionally not shown here.
	// They remain disabled by default. Revisit in a future release.
	fmt.Fprintln(w, "Tip: run `cardbot --setup` anytime to change these settings.")
}
