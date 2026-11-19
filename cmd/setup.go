package cmd

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
	// Strip only the line terminator so significant leading/trailing spaces in
	// a manually entered path are preserved; blank/whitespace-only still defaults.
	// A CR is removed only when it immediately precedes a stripped LF (CRLF), so
	// a path ending in a literal CR at EOF is preserved.
	if strings.HasSuffix(line, "\n") {
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
	}
	if strings.TrimSpace(line) == "" {
		return defaultPath
	}
	return line
}

func persistSetup(cfg *config.Config, path string, status config.LoadStatus, destination, naming func(string) string, syncLogin func(*config.Config), out io.Writer) error {
	if path == "" || (status != config.LoadValid && status != config.LoadMissing) {
		return fmt.Errorf("refusing setup: config at %q is not known to be valid or missing; repair it first", path)
	}
	if err := app.RunSetup(cfg, path, destination, naming); err != nil {
		return fmt.Errorf("could not save config: %w", err)
	}
	syncLogin(cfg)
	fprintSetupSummary(out, cfg)
	fmt.Fprintln(out)
	return nil
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
