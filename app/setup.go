package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wdphoto/cardBot/config"
)

// SetupPrompter reads setup answers from a shared buffered input stream.
// Reusing one reader prevents buffered read-ahead from consuming subsequent answers.
type SetupPrompter struct {
	reader *bufio.Reader
	out    io.Writer
}

// NewSetupPrompter creates a setup prompter with shared input/output.
func NewSetupPrompter(in io.Reader, out io.Writer) *SetupPrompter {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &SetupPrompter{reader: bufio.NewReader(in), out: out}
}

func (p *SetupPrompter) PromptNamingMode(defaultMode string) string {
	return promptNamingModeReader(p.reader, p.out, defaultMode)
}

// RunSetup executes first-time/--setup prompts and persists config.
// Setup currently prompts only destination and naming. Existing daemon settings
// are preserved as-is.
func RunSetup(
	cfg *config.Config,
	cfgPath string,
	promptDestinationFn func(string) string,
	promptNamingFn func(string) string,
) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}

	cfg.Destination.Path = config.ContractPath(promptDestinationFn(cfg.Destination.Path))
	cfg.Naming.Mode = config.NormalizeNamingMode(promptNamingFn(cfg.Naming.Mode))

	if cfgPath == "" {
		return nil
	}
	return config.Save(cfg, cfgPath)
}

func promptNamingModeReader(reader *bufio.Reader, out io.Writer, defaultMode string) string {
	mode := config.NormalizeNamingMode(defaultMode)

	for {
		fmt.Fprintln(out, "────────────────────────────────────────")
		fmt.Fprintln(out, "File Naming")
		fmt.Fprintln(out, "────────────────────────────────────────")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Camera filenames reset every 10,000 shots.")
		fmt.Fprintln(out, "Timestamp names stay stable across selective copies of the same card.")
		fmt.Fprintln(out, "If another ingest would reuse a name, cardBot stops without overwriting it.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "[1] Keep camera filenames")
		fmt.Fprintln(out, "    DSC_0001.NEF, DSC_0002.NEF ...")
		fmt.Fprintln(out, "    Use if you rely on camera numbering.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "[2] Timestamp + sequence")
		fmt.Fprintln(out, "    260314T143052_0001.NEF, _0002.NEF ...")
		fmt.Fprintln(out, "    Use for chronological order with stable per-card sequences.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "You can change this later with cardbot --setup.")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Choice [%s]: ", namingChoiceDefault(mode))

		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintln(out)
			return mode
		}
		line = strings.TrimSpace(line)
		if line == "" {
			fmt.Fprintf(out, "Naming set to: %s\n", NamingModeLabel(mode))
			return mode
		}

		if chosen, ok := parseNamingChoice(line); ok {
			fmt.Fprintf(out, "Naming set to: %s\n", NamingModeLabel(chosen))
			return chosen
		}

		fmt.Fprintln(out, "Please enter 1 or 2.")
		fmt.Fprintln(out)
	}
}

func parseNamingChoice(input string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "1", "o", "original":
		return config.NamingOriginal, true
	case "2", "t", "timestamp":
		return config.NamingTimestamp, true
	default:
		return "", false
	}
}

func namingChoiceDefault(mode string) string {
	if config.NormalizeNamingMode(mode) == config.NamingTimestamp {
		return "2"
	}
	return "1"
}

// NamingModeLabel returns a human-readable label for a naming mode.
func NamingModeLabel(mode string) string {
	if config.NormalizeNamingMode(mode) == config.NamingTimestamp {
		return "Timestamp + sequence"
	}
	return "Camera original"
}
