package cmd

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/wdphoto/cardBot/app"
	"github.com/wdphoto/cardBot/config"
)

func TestPromptDestinationReadlineIO_UsesProvidedReader(t *testing.T) {
	t.Parallel()

	in := bufio.NewReader(strings.NewReader("~/Pictures/Ingest\n"))
	var out bytes.Buffer

	got := promptDestinationReadlineIO("~/Pictures/cardBot", in, &out)
	if got != "~/Pictures/Ingest" {
		t.Fatalf("promptDestinationReadlineIO() = %q, want %q", got, "~/Pictures/Ingest")
	}
	if !strings.Contains(out.String(), "Destination [~/Pictures/cardBot]:") {
		t.Fatalf("missing destination prompt, got:\n%s", out.String())
	}
}

func TestSetupInput_SequentialAcrossDestinationAndPrompts(t *testing.T) {
	t.Parallel()

	in := bufio.NewReader(strings.NewReader("~/Pictures/Ingest\n2\n"))
	var out bytes.Buffer

	dest := promptDestinationReadlineIO("~/Pictures/cardBot", in, &out)
	if dest != "~/Pictures/Ingest" {
		t.Fatalf("destination = %q, want %q", dest, "~/Pictures/Ingest")
	}

	prompter := app.NewSetupPrompter(in, &out)
	if mode := prompter.PromptNamingMode(config.NamingOriginal); mode != config.NamingTimestamp {
		t.Fatalf("PromptNamingMode = %q, want %q", mode, config.NamingTimestamp)
	}
	// Note: daemon prompts (auto-launch, start-at-login) have been removed from setup.
	// Daemon options remain disabled by default.
}
