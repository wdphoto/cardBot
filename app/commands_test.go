package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/cardcopy"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/dotfile"
)

func TestHandleCopySuccess_DryRun_NoSideEffects(t *testing.T) {
	t.Parallel()

	cardPath := t.TempDir()
	a := &App{copiedModes: make(map[string]bool)}
	card := &detect.Card{Path: cardPath}

	a.handleCopySuccess(card, "all", t.TempDir(), &cardcopy.Result{
		FilesCopied: 42,
		BytesCopied: 1024,
		Elapsed:     2 * time.Second,
	}, true, 10)

	if a.copiedModes["all"] {
		t.Fatal("dry-run must not mark mode as copied")
	}

	dotPath := filepath.Join(cardPath, ".cardbot")
	if _, err := os.Stat(dotPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write dotfile, got err=%v", err)
	}
}

func TestHandleCopySuccess_RealCopy_WritesDotfileAndMarksMode(t *testing.T) {
	t.Parallel()

	cardPath := t.TempDir()
	a := &App{copiedModes: make(map[string]bool), writeDotfile: defaultDotfileWriter}
	card := &detect.Card{Path: cardPath}
	dest := t.TempDir()

	a.handleCopySuccess(card, "all", dest, &cardcopy.Result{
		FilesCopied: 7,
		BytesCopied: 2048,
		Elapsed:     3 * time.Second,
	}, false, 0)

	if !a.copiedModes["all"] {
		t.Fatal("real copy should mark mode as copied")
	}

	status := dotfile.Read(cardPath)
	if !status.Copied {
		t.Fatal("expected dotfile copied status")
	}
	if len(status.Entries) == 0 {
		t.Fatal("expected at least one dotfile entry")
	}
	if status.Entries[0].Mode != "all" {
		t.Fatalf("dotfile mode = %q, want %q", status.Entries[0].Mode, "all")
	}
	if status.Entries[0].Destination != dest {
		t.Fatalf("dotfile destination = %q, want %q", status.Entries[0].Destination, dest)
	}
}

func TestHandleCopySuccess_UsesInjectedDotfileWriter(t *testing.T) {
	t.Parallel()

	cardPath := t.TempDir()
	a := &App{copiedModes: make(map[string]bool)}
	card := &detect.Card{Path: cardPath}

	called := 0
	var got dotfile.WriteOptions
	a.writeDotfile = func(opts dotfile.WriteOptions) error {
		called++
		got = opts
		return nil
	}

	a.handleCopySuccess(card, "photos", "/dest/path", &cardcopy.Result{
		FilesCopied: 5,
		BytesCopied: 4096,
		Elapsed:     time.Second,
	}, false, 0)

	if called != 1 {
		t.Fatalf("dotfile writer called %d times, want 1", called)
	}
	if got.Mode != "photos" {
		t.Fatalf("mode = %q, want %q", got.Mode, "photos")
	}
	if got.Destination != "/dest/path" {
		t.Fatalf("destination = %q, want %q", got.Destination, "/dest/path")
	}
}

func TestCopyFiltered_ReportsActualStatusWriteFailureWithoutProbe(t *testing.T) {
	for _, writeErr := range []error{os.ErrPermission, syscall.EROFS, errors.New("metadata write failed")} {
		t.Run(writeErr.Error(), func(t *testing.T) {
			a, _, card := newLifecycleApp(t, func(context.Context, cardcopy.Options, cardcopy.ProgressFunc) (*cardcopy.Result, error) {
				return &cardcopy.Result{FilesCopied: 11653}, nil
			})
			a.dryRun = false
			// The injected runner does not need a real source. A speculative
			// permission probe here would produce a false read-only warning.
			card.Path = filepath.Join(t.TempDir(), "unprobed-card")
			writes := 0
			a.writeDotfile = func(dotfile.WriteOptions) error {
				writes++
				return writeErr
			}
			out := captureStdout(t, func() {
				a.handleCopyCmd(card, "all")
				outcome := waitOutcome(t, a)
				a.copyWG.Wait()
				a.handleCopyDone(outcome)
			})
			if strings.Contains(out, "appears to be write-protected") {
				t.Fatalf("unexpected speculative permission warning:\n%s", out)
			}
			if !strings.Contains(out, "ingest completed, but could not save .cardbot on card: "+writeErr.Error()) {
				t.Fatalf("missing actual status-write failure:\n%s", out)
			}
			if writes != 1 || !a.copiedModes["all"] {
				t.Fatalf("metadata failure lost completed copy: writes=%d modes=%v", writes, a.copiedModes)
			}
			if !strings.Contains(out, "11,653 files") {
				t.Fatalf("missing grouped copy count:\n%s", out)
			}
		})
	}
}

func TestIsPermissionErr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"os.ErrPermission", os.ErrPermission, true},
		{"wrapped ErrPermission", fmt.Errorf("op: %w", os.ErrPermission), true},
		{"permission denied string", errors.New("permission denied"), true},
		{"other error", errors.New("something else"), false},
	}

	for _, tt := range tests {
		if got := isPermissionErr(tt.err); got != tt.want {
			t.Fatalf("%s: isPermissionErr(%v) = %v, want %v", tt.name, tt.err, got, tt.want)
		}
	}
}
