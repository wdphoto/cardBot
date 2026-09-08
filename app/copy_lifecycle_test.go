package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/cardcopy"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/detect"
)

// These tests exercise the copy lifecycle invariants:
//
//   - handleCopyCmd registers the copy cancel synchronously, so removal or
//     backslash immediately after the command still cancels the worker.
//   - exit/eject are blocked while a copy is active.
//   - a stale worker outcome (identified by a per-copy monotonic id) cannot
//     affect a replacement card or a newer copy, even for the same path.
//   - new card events and new copy commands are gated while a worker is active.
//   - the event loop's removal marker is honoured even if the removal is
//     processed after the worker enqueued its completion.
//   - the copy context is released on normal completion too.
//   - shutdown cancels the active copy and waits for the worker.
//
// All synchronization is channel-based; no sleeps are used to order events.

// blockingCopy returns a runCopy that signals start and blocks until the
// context is cancelled, then reports a cancelled outcome.
func blockingCopy(started chan struct{}) copyRunner {
	return func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
		close(started)
		<-ctx.Done()
		return &cardcopy.Result{FilesCopied: 3}, context.Canceled
	}
}

func newLifecycleApp(t *testing.T, runCopy copyRunner) (*App, *fakeDetector, *detect.Card) {
	t.Helper()
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
		runCopy:     runCopy,
	})
	a.detector = fd
	card := &detect.Card{Path: cardPath, Name: "CARD"}
	a.currentCard = card
	a.phase = phaseReady
	a.lastResult = &analyze.Result{FileCount: 1}
	t.Cleanup(a.stopScanning)
	return a, fd, card
}

// waitOutcome reads the next copy outcome, failing the test if it does not
// arrive in time.
func waitOutcome(t *testing.T, a *App) copyOutcome {
	t.Helper()
	select {
	case out := <-a.copyDone:
		return out
	case <-time.After(2 * time.Second):
		t.Fatal("copy worker did not report completion")
		return copyOutcome{}
	}
}

func TestCopyLifecycle_ImmediateRemovalCancelsCopy(t *testing.T) {
	a, _, card := newLifecycleApp(t, blockingCopy(make(chan struct{})))

	// Start the copy and remove the card immediately — before the worker
	// goroutine has necessarily run. The cancel must already be registered
	// synchronously by handleCopyCmd.
	a.handleCopyCmd(card, "all")
	a.handleRemoval(card.Path)

	a.mu.Lock()
	removed := a.copyRemoved
	a.mu.Unlock()
	if !removed {
		t.Fatal("removal marker should be set by the removal handler")
	}

	out := waitOutcome(t, a)
	if !errors.Is(out.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", out.err)
	}

	a.handleCopyDone(out)

	a.mu.Lock()
	current := a.currentCard
	phase := a.phase
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard = %v, want nil", current)
	}
	if phase != phaseScanning {
		t.Fatalf("phase = %v, want phaseScanning", phase)
	}
}

func TestCopyLifecycle_ImmediateBackslashCancels(t *testing.T) {
	a, _, card := newLifecycleApp(t, blockingCopy(make(chan struct{})))

	a.handleCopyCmd(card, "all")
	// Cancel immediately — copyCancel must already be registered.
	a.handleInput("\\")

	out := waitOutcome(t, a)
	if !errors.Is(out.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", out.err)
	}

	a.handleCopyDone(out)

	if a.copiedModes["all"] {
		t.Fatal("cancelled copy must not mark mode as completed")
	}
	if got := a.currentPhase(); got != phaseReady {
		t.Fatalf("phase = %v, want phaseReady", got)
	}
}

func TestCopyLifecycle_ExitBlockedDuringCopy(t *testing.T) {
	started := make(chan struct{})
	a, _, card := newLifecycleApp(t, blockingCopy(started))

	a.handleCopyCmd(card, "all")
	<-started

	a.handleInput("x")

	a.mu.Lock()
	current := a.currentCard
	phase := a.phase
	a.mu.Unlock()
	if current == nil || current.Path != card.Path {
		t.Fatalf("exit during copy must not clear currentCard, got %v", current)
	}
	if phase != phaseCopying {
		t.Fatalf("phase = %v, want phaseCopying", phase)
	}

	// Clean up: cancel the copy and process the outcome.
	a.handleInput("\\")
	a.handleCopyDone(waitOutcome(t, a))
}

func TestCopyLifecycle_EjectBlockedDuringCopy(t *testing.T) {
	started := make(chan struct{})
	a, _, card := newLifecycleApp(t, blockingCopy(started))

	a.handleCopyCmd(card, "all")
	<-started

	a.handleInput("e")

	a.mu.Lock()
	current := a.currentCard
	phase := a.phase
	a.mu.Unlock()
	if current == nil || current.Path != card.Path {
		t.Fatalf("eject during copy must not clear currentCard, got %v", current)
	}
	if phase != phaseCopying {
		t.Fatalf("phase = %v, want phaseCopying", phase)
	}

	a.handleInput("\\")
	a.handleCopyDone(waitOutcome(t, a))
}

func TestCopyLifecycle_StaleOutcomeDoesNotAffectNewCopy(t *testing.T) {
	card1 := "/card/one"
	card2 := "/card/two"

	a := New(Config{Cfg: config.Defaults()})
	a.currentCard = &detect.Card{Path: card2}
	a.phase = phaseCopying
	a.copyID = 2
	a.copyCancel = func() {}
	a.copiedModes = make(map[string]bool)

	// A stale outcome from card1's worker (id 1) arrives while card2's copy
	// (id 2) is active. It must be ignored entirely.
	a.handleCopyDone(copyOutcome{
		id:       1,
		cardPath: card1,
		mode:     "all",
		err:      context.Canceled,
		result:   &cardcopy.Result{FilesCopied: 5},
	})

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.copyID != 2 {
		t.Fatalf("copyID = %d, want 2", a.copyID)
	}
	if a.copyCancel == nil {
		t.Fatal("stale outcome must not clear the active copy's cancel")
	}
	if a.phase != phaseCopying {
		t.Fatalf("phase = %v, want phaseCopying", a.phase)
	}
	if a.copiedModes["all"] {
		t.Fatal("stale outcome must not mark mode as copied")
	}
}

func TestCopyLifecycle_StaleSuccessSamePathDoesNotAffectNewCopy(t *testing.T) {
	cardPath := "/card/same"

	a := New(Config{Cfg: config.Defaults()})
	a.currentCard = &detect.Card{Path: cardPath}
	a.phase = phaseCopying
	a.copyID = 2
	a.copyCancel = func() {}
	a.copiedModes = make(map[string]bool)

	// A stale SUCCESS outcome from an earlier copy of the same path (id 1)
	// arrives while a newer copy (id 2) is active. It must be ignored: no
	// success handling, no phase restore, no copiedModes mutation.
	a.handleCopyDone(copyOutcome{
		id:       1,
		cardPath: cardPath,
		mode:     "all",
		err:      nil,
		result:   &cardcopy.Result{FilesCopied: 10},
	})

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.copyID != 2 {
		t.Fatalf("copyID = %d, want 2", a.copyID)
	}
	if a.copyCancel == nil {
		t.Fatal("stale success must not clear the active copy's cancel")
	}
	if a.phase != phaseCopying {
		t.Fatalf("phase = %v, want phaseCopying", a.phase)
	}
	if a.copiedModes["all"] {
		t.Fatal("stale success must not mark mode as copied")
	}
}

func TestCopyLifecycle_RemovalDuringCopy_DefersNextCardUntilWorkerDone(t *testing.T) {
	for _, samePath := range []bool{false, true} {
		name := "different path"
		if samePath {
			name = "same-path remount"
		}
		t.Run(name, func(t *testing.T) { testCopyRemovalQueue(t, samePath) })
	}
}

func testCopyRemovalQueue(t *testing.T, samePath bool) {
	t.Helper()
	card1Path := t.TempDir()
	if err := os.MkdirAll(filepath.Join(card1Path, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	card2Path := t.TempDir()
	if samePath {
		card2Path = card1Path
	}
	if err := os.MkdirAll(filepath.Join(card2Path, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	started := make(chan struct{})
	release := make(chan struct{})
	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(_ string) cardAnalyzer {
			return &fakeAnalyzer{result: &analyze.Result{FileCount: 1}}
		},
		runCopy: func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			close(started)
			select {
			case <-release:
				return &cardcopy.Result{}, nil
			case <-ctx.Done():
				return &cardcopy.Result{}, context.Canceled
			}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stop the next card's analysis before returning from the test
	a.ctx = ctx
	a.detector = fd
	card1 := &detect.Card{Path: card1Path, Name: "ONE"}
	card2 := &detect.Card{Path: card2Path, Name: "TWO"}
	a.currentCard = card1
	a.phase = phaseReady
	a.lastResult = &analyze.Result{FileCount: 1}
	t.Cleanup(a.stopScanning)

	a.handleCopyCmd(card1, "all")
	<-started

	a.handleRemoval(card1Path)

	// The next card must not start while the old worker is still active.
	a.mu.Lock()
	current := a.currentCard
	phase := a.phase
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard = %v, want nil while old worker is active", current)
	}
	if phase != phaseCopying {
		t.Fatalf("phase = %v, want phaseCopying while old worker is active", phase)
	}

	// A NEW card event while the old worker is still blocked must be queued,
	// not started: the active-worker gate applies to card events too.
	_ = captureStdout(t, func() {
		a.handleCardEvent(card2)
	})

	a.mu.Lock()
	current = a.currentCard
	qLen := len(a.cardQueue)
	phase = a.phase
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("card event during active copy must not become current, got %v", current)
	}
	if qLen != 1 {
		t.Fatalf("queue length = %d, want 1", qLen)
	}
	if phase != phaseCopying {
		t.Fatalf("phase = %v, want phaseCopying", phase)
	}

	// Release the old worker; its outcome lets the event loop advance to card2.
	close(release)
	a.handleCopyDone(waitOutcome(t, a))

	a.mu.Lock()
	current = a.currentCard
	phase = a.phase
	a.mu.Unlock()
	if current == nil || current.Path != card2Path {
		t.Fatalf("currentCard = %v, want card2 after worker done", current)
	}
	if phase != phaseAnalyzing {
		t.Fatalf("phase = %v, want phaseAnalyzing after worker done", phase)
	}
}

func TestCopyLifecycle_RemovalMarkerPreservedAfterWorkerCompletion(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	started := make(chan struct{})
	release := make(chan struct{})
	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			close(started)
			select {
			case <-release:
				return &cardcopy.Result{FilesCopied: 5}, nil
			case <-ctx.Done():
				return &cardcopy.Result{}, context.Canceled
			}
		},
	})
	a.detector = fd
	card := &detect.Card{Path: cardPath, Name: "CARD"}
	a.currentCard = card
	a.phase = phaseReady
	a.lastResult = &analyze.Result{FileCount: 1}
	t.Cleanup(a.stopScanning)

	a.handleCopyCmd(card, "all")
	<-started

	// Let the worker complete and enqueue its success outcome.
	close(release)
	out := waitOutcome(t, a)

	// The removal is processed AFTER the worker enqueued its completion. The
	// event loop's removal marker must still be honoured: no success handling,
	// and the next-card flow advances.
	a.handleRemoval(card.Path)
	a.handleCopyDone(out)

	a.mu.Lock()
	current := a.currentCard
	phase := a.phase
	copiedAll := a.copiedModes["all"]
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard = %v, want nil", current)
	}
	if phase != phaseScanning {
		t.Fatalf("phase = %v, want phaseScanning", phase)
	}
	if copiedAll {
		t.Fatal("removed copy must not mark mode as completed")
	}
}

func TestCopyLifecycle_CopyCommandRefusedWhileWorkerActive(t *testing.T) {
	a, _, card := newLifecycleApp(t, blockingCopy(make(chan struct{})))

	// Simulate an active worker whose phase was (incorrectly) restored: the
	// explicit worker gate must still refuse a new copy.
	a.copyID = 1
	a.phase = phaseReady

	a.handleCopyCmd(card, "all")

	a.mu.Lock()
	phase := a.phase
	id := a.copyID
	a.mu.Unlock()
	if phase != phaseReady {
		t.Fatalf("phase = %v, want phaseReady (copy must be refused)", phase)
	}
	if id != 1 {
		t.Fatalf("copyID = %d, want 1 (no new copy started)", id)
	}
}

func TestCopyLifecycle_NoOverlappingCopy(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	a, _, card := newLifecycleApp(t, func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return &cardcopy.Result{}, nil
		case <-ctx.Done():
			return &cardcopy.Result{}, context.Canceled
		}
	})

	a.handleCopyCmd(card, "all")
	<-started

	// A second copy command while the first is active must be refused.
	a.handleCopyCmd(card, "all")
	if got := calls.Load(); got != 1 {
		t.Fatalf("copy runner called %d times, want 1", got)
	}

	close(release)
	a.handleCopyDone(waitOutcome(t, a))

	if got := calls.Load(); got != 1 {
		t.Fatalf("copy runner called %d times after release, want 1", got)
	}
}

func TestCopyLifecycle_CancelCalledOnNormalCompletion(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	ctxCancelled := make(chan struct{})
	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			go func() {
				<-ctx.Done()
				close(ctxCancelled)
			}()
			return &cardcopy.Result{}, nil
		},
	})
	a.detector = fd
	card := &detect.Card{Path: cardPath, Name: "CARD"}
	a.currentCard = card
	a.phase = phaseReady
	a.lastResult = &analyze.Result{FileCount: 1}

	a.handleCopyCmd(card, "all")
	a.handleCopyDone(waitOutcome(t, a))

	// The worker must release the copy context on normal completion.
	select {
	case <-ctxCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("copy context was not cancelled after normal completion")
	}
}

func TestCopyLifecycle_ActiveWorkerGatesCardEndingActions(t *testing.T) {
	for _, action := range []string{"e", "x", "removal"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan struct{})
			a, _, card := newLifecycleApp(t, blockingCopy(started))
			a.handleCopyCmd(card, "all")
			<-started
			// Even a stale phase update must not hide the active worker.
			a.setPhase(phaseReady)
			if action == "removal" {
				a.handleRemoval(card.Path)
			} else {
				a.handleInput(action)
				if a.currentCard != card {
					t.Fatal("card-ending command ignored the active worker")
				}
				a.handleInput("\\")
			}
			out := waitOutcome(t, a)
			if !errors.Is(out.err, context.Canceled) {
				t.Fatalf("error = %v, want cancellation", out.err)
			}
			a.handleCopyDone(out)
			if a.copyID != 0 || a.copyCancel != nil {
				t.Fatal("completion left active copy state")
			}
		})
	}
}

func TestCopyLifecycle_PreCancelledWorkerDoesNotAccessCard(t *testing.T) {
	a, _, card := newLifecycleApp(t, func(context.Context, cardcopy.Options, cardcopy.ProgressFunc) (*cardcopy.Result, error) {
		t.Fatal("cancelled worker invoked copy runner")
		return nil, nil
	})
	a.dryRun = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output := captureStdout(t, func() {
		a.copyFiltered(ctx, cancel, card, "all", a.cfg.Destination.Path, a.lastResult, 1)
	})
	if output != "" {
		t.Fatalf("cancelled worker emitted startup/probe output: %q", output)
	}
	if out := waitOutcome(t, a); !errors.Is(out.err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", out.err)
	}
}

func TestCopyLifecycle_ShutdownCancelsActiveCopy(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	started := make(chan struct{})
	copyReturned := make(chan error, 1)
	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, _ cardcopy.Options, _ cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			close(started)
			<-ctx.Done()
			copyReturned <- context.Canceled
			return &cardcopy.Result{FilesCopied: 4}, context.Canceled
		},
	})
	a.detector = fd
	a.currentCard = &detect.Card{Path: cardPath, Name: "CARD"}
	a.phase = phaseReady
	a.lastResult = &analyze.Result{FileCount: 1}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// Start the copy through the event loop.
	a.inputChan <- "a"
	<-started

	// Shut down while the copy is active.
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit on signal")
	}

	select {
	case err := <-copyReturned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("copy err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("copy worker did not observe cancellation")
	}

	if got := a.currentPhase(); got != phaseShuttingDown {
		t.Fatalf("phase = %v, want %v", got, phaseShuttingDown)
	}
}

func TestResumeScanningIfIdle_BlockedDuringActiveCopy(t *testing.T) {
	a := New(Config{Cfg: config.Defaults()})
	a.copyID = 1
	a.resumeScanningIfIdle()
	if a.spinner != nil {
		t.Fatal("spinner must not start while a copy worker is active")
	}
}

func TestResumeScanningIfIdle_BlockedDuringShutdown(t *testing.T) {
	a := New(Config{Cfg: config.Defaults()})
	a.phase = phaseShuttingDown
	a.resumeScanningIfIdle()
	if a.spinner != nil {
		t.Fatal("spinner must not start while shutting down")
	}
}
