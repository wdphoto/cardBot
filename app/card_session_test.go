package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/dotfile"
)

func TestRelativeTargetMatchesAbsoluteRemoval(t *testing.T) {
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "DCIM"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "DCIM", "A.JPG"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	started := make(chan struct{})
	a := New(Config{Cfg: cfg, TargetPath: rel, runCopy: blockingCopy(started)})
	a.detector = newFakeDetector()
	t.Cleanup(a.stopScanning)
	a.launchTargetPath(a.targetPath)
	waitForPhase(t, a, phaseReady, 2*time.Second)
	a.scanWG.Wait()
	a.handleCopyCmd(a.currentCard, "all")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("copy did not start")
	}
	a.handleRemoval(path)
	out := waitOutcome(t, a)
	a.copyWG.Wait()
	if !a.copyRemoved || a.currentCard != nil || !errors.Is(out.err, context.Canceled) {
		t.Fatalf("relative target not removed: removed=%v current=%v err=%v", a.copyRemoved, a.currentCard, out.err)
	}
	a.handleCopyDone(out)
}

func TestCopyDone_ReplacementBeforeReceipt(t *testing.T) {
	a, _, card := newLifecycleApp(t, nil)
	a.dryRun = false
	if err := os.WriteFile(filepath.Join(card.Path, "DCIM", "A.JPG"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	a.handleCopyCmd(card, "all")
	out := waitOutcome(t, a)
	a.copyWG.Wait()
	if out.err != nil || out.result.FilesCopied != 1 {
		t.Fatalf("copy: %+v", out)
	}
	if err := os.Rename(card.Path, filepath.Join(t.TempDir(), "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(card.Path, 0700); err != nil {
		t.Fatal(err)
	}
	a.handleCopyDone(out)
	if a.copiedModes["all"] || dotfile.Read(card.Path).Copied {
		t.Fatal("credited old ingest to replacement")
	}
}

func TestLateScanCannotAffectCurrentGeneration(t *testing.T) {
	a, _, card := newLifecycleApp(t, nil)
	newResult := a.lastResult
	a.scanID = 2
	next := &detect.Card{Path: t.TempDir()}
	a.cardQueue = []*detect.Card{next}
	// Deliver late outcomes after a newer scan took ownership, including a
	// reused Card object and path. The old context need not be cancelled yet.
	for _, err := range []error{nil, os.ErrNotExist, errors.New("scan failed")} {
		a.applyScan(context.Background(), card, 1, &analyze.Result{}, err, 0)
		if a.currentCard != card || a.lastResult != newResult || a.cardInvalid ||
			a.phase != phaseReady || len(a.cardQueue) != 1 || a.cardQueue[0] != next {
			t.Fatalf("late outcome (%v) changed the new session", err)
		}
	}
}
