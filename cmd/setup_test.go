package cmd

import (
	"bufio"
	"bytes"
	"io"
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

func TestPromptDestinationReadlineIO_WhitespaceSemantics(t *testing.T) {
	t.Parallel()

	const def = "~Default"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty EOF", "", def},
		{"whitespace-only LF", "   \n", def},
		{"whitespace-only CRLF", "   \r\n", def},
		{"ordinary path EOF", "/pics/Client", "/pics/Client"},
		{"leading-space EOF", "/pics/ Client", "/pics/ Client"},
		{"trailing-space EOF", "/pics/Client ", "/pics/Client "},
		{"trailing-space LF", "/pics/Client \n", "/pics/Client "},
		{"trailing-space CRLF", "/pics/Client \r\n", "/pics/Client "},
		{"literal CR at EOF", "/pics/Client\r", "/pics/Client\r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := bufio.NewReader(strings.NewReader(tt.input))
			got := promptDestinationReadlineIO(def, in, io.Discard)
			if got != tt.want {
				t.Fatalf("promptDestinationReadlineIO(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
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
