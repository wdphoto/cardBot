package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/cardcopy"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/detect"
)

type fakeDetector struct {
	startErr error
	started  atomic.Bool
	stopped  atomic.Bool
	events   chan *detect.Card
	removals chan string
}

func newFakeDetector() *fakeDetector {
	return &fakeDetector{
		events:   make(chan *detect.Card, 10),
		removals: make(chan string, 10),
	}
}

func (f *fakeDetector) Start() error {
	f.started.Store(true)
	return f.startErr
}

func (f *fakeDetector) Stop() { f.stopped.Store(true) }
func (f *fakeDetector) Events() <-chan *detect.Card {
	return f.events
}
func (f *fakeDetector) Removals() <-chan string {
	return f.removals
}
func (f *fakeDetector) Eject(path string) error { return nil }
func (f *fakeDetector) Remove(path string)      {}

func TestPhaseString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		phase appPhase
		want  string
	}{
		{phaseScanning, "scanning"},
		{phaseAnalyzing, "analyzing"},
		{phaseReady, "ready"},
		{phaseCopying, "copying"},
		{phaseShuttingDown, "shutting_down"},
		{appPhase(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Fatalf("phase %v String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestCanTransitionPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from appPhase
		to   appPhase
		want bool
	}{
		{phaseScanning, phaseAnalyzing, true},
		{phaseReady, phaseCopying, true},
		{phaseAnalyzing, phaseReady, true},
		{phaseCopying, phaseReady, true},
		{phaseCopying, phaseScanning, true},
		{phaseReady, phaseReady, true},
		{phaseScanning, phaseCopying, false},
		{phaseShuttingDown, phaseReady, false},
		{phaseReady, phaseShuttingDown, true},
	}

	for _, tt := range tests {
		if got := canTransitionPhase(tt.from, tt.to); got != tt.want {
			t.Fatalf("canTransitionPhase(%v,%v) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestPhaseAfterFinish(t *testing.T) {
	t.Parallel()

	if got := phaseAfterFinish(0); got != phaseScanning {
		t.Fatalf("phaseAfterFinish(0) = %v, want %v", got, phaseScanning)
	}
	if got := phaseAfterFinish(2); got != phaseAnalyzing {
		t.Fatalf("phaseAfterFinish(2) = %v, want %v", got, phaseAnalyzing)
	}
}

func TestSetPhase_UsesTransitionTable(t *testing.T) {
	t.Parallel()

	a := &App{phase: phaseScanning}
	a.setPhase(phaseCopying)
	if got := a.currentPhase(); got != phaseScanning {
		t.Fatalf("invalid transition should be ignored, got %v", got)
	}

	a.setPhase(phaseAnalyzing)
	if got := a.currentPhase(); got != phaseAnalyzing {
		t.Fatalf("valid transition should apply, got %v", got)
	}
}

func TestFinishCopyPhase(t *testing.T) {
	t.Parallel()

	a := &App{
		currentCard: &detect.Card{Path: "/card"},
		phase:       phaseCopying,
	}

	a.finishCopyPhase("/card")
	if got := a.currentPhase(); got != phaseReady {
		t.Fatalf("phase = %v, want %v", got, phaseReady)
	}

	a.phase = phaseCopying
	a.currentCard = &detect.Card{Path: "/other"}
	a.finishCopyPhase("/card")
	if got := a.currentPhase(); got != phaseCopying {
		t.Fatalf("phase should remain copying for different card, got %v", got)
	}
}

func TestRun_UsesInjectedDetector(t *testing.T) {
	fd := newFakeDetector()
	a := New(Config{
		Cfg:         config.Defaults(),
		newDetector: func() cardDetector { return fd },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.After(2 * time.Second)
	for !fd.started.Load() {
		select {
		case <-deadline:
			t.Fatal("detector Start() was not called")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit on signal")
	}

	if !fd.stopped.Load() {
		t.Fatal("expected detector Stop() to be called")
	}
	if got := a.currentPhase(); got != phaseShuttingDown {
		t.Fatalf("phase = %v, want %v", got, phaseShuttingDown)
	}
}

func TestRun_DetectorStartError(t *testing.T) {
	t.Parallel()

	fd := newFakeDetector()
	fd.startErr = errors.New("boom")

	a := New(Config{
		Cfg:         config.Defaults(),
		newDetector: func() cardDetector { return fd },
	})

	err := a.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !fd.started.Load() {
		t.Fatal("expected Start() call")
	}
	if fd.stopped.Load() {
		t.Fatal("Stop() should not run when Start fails")
	}
}

func TestCopyFiltered_UsesInjectedCopyRunnerAndRestoresPhase(t *testing.T) {
	t.Parallel()

	cardPath := t.TempDir()
	dcim := filepath.Join(cardPath, "DCIM")
	if err := os.MkdirAll(dcim, 0o755); err != nil {
		t.Fatalf("mkdir dcim: %v", err)
	}

	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	called := 0
	var gotOpts cardcopy.Options

	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, opts cardcopy.Options, onProgress cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			called++
			gotOpts = opts
			return &cardcopy.Result{}, nil
		},
	})

	a.detector = fd
	a.currentCard = &detect.Card{Path: cardPath, Name: "CARD"}
	a.phase = phaseReady

	a.copyFiltered(a.currentCard, "all")

	if called != 1 {
		t.Fatalf("copy runner called %d times, want 1", called)
	}
	if gotOpts.CardPath != cardPath {
		t.Fatalf("CardPath = %q, want %q", gotOpts.CardPath, cardPath)
	}
	if got := a.currentPhase(); got != phaseReady {
		t.Fatalf("phase = %v, want %v", got, phaseReady)
	}
}

func TestCopyFiltered_BackslashCancels(t *testing.T) {
	t.Parallel()

	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			<-ctx.Done()
			return &cardcopy.Result{FilesCopied: 3}, context.Canceled
		},
	})
	a.detector = fd
	card := &detect.Card{Path: cardPath, Name: "CARD"}
	a.currentCard = card
	a.phase = phaseReady

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.copyFiltered(card, "all")
	}()

	// Give the copy goroutine time to block on ctx.Done.
	time.Sleep(50 * time.Millisecond)
	a.handleInput("\\")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("copyFiltered did not return after backslash cancel")
	}

	if a.copiedModes["all"] {
		t.Fatal("cancelled copy must not mark mode as completed")
	}
}

func TestCopyFiltered_CardRemovedDuringCopy_CancelsAndFinishesCard(t *testing.T) {
	t.Parallel()

	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			<-ctx.Done()
			return &cardcopy.Result{FilesCopied: 7}, context.Canceled
		},
	})
	a.detector = fd
	card := &detect.Card{Path: cardPath, Name: "CARD"}
	a.currentCard = card
	a.phase = phaseReady
	t.Cleanup(a.stopScanning)

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.copyFiltered(card, "all")
	}()

	time.Sleep(50 * time.Millisecond)
	a.handleRemoval(cardPath)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("copyFiltered did not return after card removal")
	}

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard should be nil after card removed during copy, got %v", current)
	}
}
