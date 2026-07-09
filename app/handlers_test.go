package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/cardcopy"
	"github.com/wdphoto/cardBot/config"
	"github.com/wdphoto/cardBot/detect"
)

func TestHandleCopyCmd_NotReady(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatalf("mkdir dcim: %v", err)
	}

	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	called := 0
	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, opts cardcopy.Options, onProgress cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			called++
			return &cardcopy.Result{}, nil
		},
	})
	a.detector = fd

	card := &detect.Card{Path: cardPath, Name: "CARD"}
	a.currentCard = card
	a.phase = phaseAnalyzing

	out := captureStdout(t, func() {
		a.handleCopyCmd(card, "all")
	})

	if called != 0 {
		t.Fatalf("copy runner called %d times, want 0", called)
	}
	if !strings.Contains(out, "Still scanning card. Please wait.") {
		t.Fatalf("expected readiness warning, got:\n%s", out)
	}
}

func TestHandleCopyCmd_Allowed(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatalf("mkdir dcim: %v", err)
	}

	cfg := config.Defaults()
	cfg.Destination.Path = t.TempDir()
	fd := newFakeDetector()

	called := 0
	a := New(Config{
		Cfg:         cfg,
		DryRun:      true,
		newDetector: func() cardDetector { return fd },
		runCopy: func(ctx context.Context, opts cardcopy.Options, onProgress cardcopy.ProgressFunc) (*cardcopy.Result, error) {
			called++
			return &cardcopy.Result{}, nil
		},
	})
	a.detector = fd

	card := &detect.Card{Path: cardPath, Name: "CARD"}
	a.currentCard = card
	a.phase = phaseReady
	a.copiedModes = make(map[string]bool)

	a.handleCopyCmd(card, "all")
	a.copyWG.Wait()

	if called != 1 {
		t.Fatalf("copy runner called %d times, want 1", called)
	}
}

func TestIsTracked(t *testing.T) {
	t.Parallel()

	a := &App{
		currentCard: &detect.Card{Path: "/card/current"},
		cardQueue: []*detect.Card{
			{Path: "/card/queued1"},
			{Path: "/card/queued2"},
		},
	}

	if !a.isTracked("/card/current") {
		t.Fatal("expected current card to be tracked")
	}
	if !a.isTracked("/card/queued2") {
		t.Fatal("expected queued card to be tracked")
	}
	if a.isTracked("/card/unknown") {
		t.Fatal("did not expect unknown card to be tracked")
	}
}

func TestIsTracked_NormalizesPath(t *testing.T) {
	t.Parallel()

	a := &App{
		currentCard: &detect.Card{Path: "/Volumes/NIKON Z 9  "},
	}

	if !a.isTracked("/Volumes/NIKON Z 9  /") {
		t.Fatal("expected trailing-slash variant to be tracked")
	}
	if a.isTracked("/Volumes/NIKON Z 9") {
		t.Fatal("did not expect trimmed path variant to be tracked")
	}
}

func TestResumeScanningIfIdle(t *testing.T) {
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})

	a.resumeScanningIfIdle()
	if a.spinner == nil {
		t.Fatal("expected spinner when idle")
	}
	a.stopScanning()

	a.currentCard = &detect.Card{Path: "/card"}
	a.resumeScanningIfIdle()
	if a.spinner != nil {
		t.Fatal("did not expect spinner when card is active")
	}
}

// ---------------------------------------------------------------------------
// Helpers shared across handler tests
// ---------------------------------------------------------------------------

// fakeAnalyzer implements cardAnalyzer for tests.
type fakeAnalyzer struct {
	result *analyze.Result
	err    error
}

func (f *fakeAnalyzer) SetWorkers(_ int)                                   {}
func (f *fakeAnalyzer) OnProgress(_ analyze.ProgressFunc)                  {}
func (f *fakeAnalyzer) Analyze(_ context.Context) (*analyze.Result, error) { return f.result, f.err }

// waitForPhase polls a until it reaches want or timeout expires.
func waitForPhase(t *testing.T, a *App, want appPhase, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.currentPhase() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("phase did not reach %v within %v (current: %v)", want, timeout, a.currentPhase())
}

// ---------------------------------------------------------------------------
// handleCardEvent
// ---------------------------------------------------------------------------

func TestHandleCardEvent_FirstCard_BecomesCurrentAndAnalyzes(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Output.Color = false
	fd := newFakeDetector()
	a := New(Config{
		Cfg:         cfg,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(_ string) cardAnalyzer {
			return &fakeAnalyzer{result: &analyze.Result{Bodies: []string{"NIKON Z 9"}}}
		},
	})
	a.detector = fd
	t.Cleanup(a.stopScanning)

	a.handleCardEvent(&detect.Card{Path: cardPath, Name: "CARD"})

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current == nil || current.Path != cardPath {
		t.Fatalf("currentCard not set: got %v", current)
	}

	// displayCard goroutine transitions phase to ready once analysis completes.
	waitForPhase(t, a, phaseReady, 2*time.Second)

	a.mu.Lock()
	result := a.lastResult
	a.mu.Unlock()
	if result == nil {
		t.Fatal("expected lastResult to be populated after analysis")
	}
}

func TestHandleCardEvent_SecondCard_GetsQueued(t *testing.T) {
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/card/first", Name: "FIRST"}
	a.phase = phaseReady

	_ = captureStdout(t, func() {
		a.handleCardEvent(&detect.Card{Path: "/card/second", Name: "SECOND"})
	})

	a.mu.Lock()
	qLen := len(a.cardQueue)
	var qPath string
	if qLen > 0 {
		qPath = a.cardQueue[0].Path
	}
	a.mu.Unlock()

	if qLen != 1 {
		t.Fatalf("queue length = %d, want 1", qLen)
	}
	if qPath != "/card/second" {
		t.Fatalf("queue[0].Path = %q, want /card/second", qPath)
	}
}

func TestHandleCardEvent_AlreadyTrackedAsCurrent_Ignored(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/card/current"}
	a.phase = phaseReady

	a.handleCardEvent(&detect.Card{Path: "/card/current"})

	a.mu.Lock()
	qLen := len(a.cardQueue)
	a.mu.Unlock()
	if qLen != 0 {
		t.Fatalf("already-tracked card should not be queued, got queue length %d", qLen)
	}
}

func TestHandleCardEvent_AlreadyTrackedAsCurrent_WithTrailingSlash_Ignored(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/Volumes/NIKON Z 9"}
	a.phase = phaseReady

	a.handleCardEvent(&detect.Card{Path: "/Volumes/NIKON Z 9/"})

	a.mu.Lock()
	qLen := len(a.cardQueue)
	a.mu.Unlock()
	if qLen != 0 {
		t.Fatalf("already-tracked card should not be queued, got queue length %d", qLen)
	}
}

func TestHandleCardEvent_AlreadyQueued_Ignored(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/card/current"}
	a.cardQueue = []*detect.Card{{Path: "/card/queued"}}
	a.phase = phaseReady

	a.handleCardEvent(&detect.Card{Path: "/card/queued"})

	a.mu.Lock()
	qLen := len(a.cardQueue)
	a.mu.Unlock()
	if qLen != 1 {
		t.Fatalf("already-queued card should not be duplicated, got queue length %d", qLen)
	}
}

// ---------------------------------------------------------------------------
// handleRemoval
// ---------------------------------------------------------------------------

func TestHandleRemoval_CurrentCard_NoQueue_ClearsCard(t *testing.T) {
	cfg := config.Defaults()
	fd := newFakeDetector()
	a := New(Config{
		Cfg:         cfg,
		newDetector: func() cardDetector { return fd },
	})
	a.detector = fd
	a.currentCard = &detect.Card{Path: "/card/main", Name: "MAIN"}
	a.phase = phaseReady
	t.Cleanup(a.stopScanning)

	// captureStdout is safe: the deferred resumeScanningIfIdle goroutine sleeps
	// 2 seconds before it could write anything, well after captureStdout returns.
	_ = captureStdout(t, func() {
		a.handleRemoval("/card/main")
	})

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard should be nil after removal, got %v", current)
	}
	if got := a.currentPhase(); got != phaseScanning {
		t.Fatalf("phase = %v, want phaseScanning", got)
	}
}

func TestHandleRemoval_QueuedCard_RemovesFromQueue(t *testing.T) {
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/card/current"}
	a.cardQueue = []*detect.Card{
		{Path: "/card/q1"},
		{Path: "/card/q2"},
	}
	a.phase = phaseReady

	_ = captureStdout(t, func() {
		a.handleRemoval("/card/q1")
	})

	a.mu.Lock()
	qLen := len(a.cardQueue)
	var remaining string
	if qLen > 0 {
		remaining = a.cardQueue[0].Path
	}
	a.mu.Unlock()

	if qLen != 1 {
		t.Fatalf("queue length = %d, want 1", qLen)
	}
	if remaining != "/card/q2" {
		t.Fatalf("remaining card = %q, want /card/q2", remaining)
	}
}

func TestHandleRemoval_UnknownPath_NoStateChange(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	card := &detect.Card{Path: "/card/main", Name: "MAIN"}
	a.currentCard = card
	a.phase = phaseReady

	a.handleRemoval("/card/other")

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current != card {
		t.Fatal("currentCard should be unchanged for untracked removal path")
	}
}

// ---------------------------------------------------------------------------
// finishCard
// ---------------------------------------------------------------------------

func TestFinishCard_EmptyQueue_ClearsCardAndScans(t *testing.T) {
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/card/main"}
	a.phase = phaseReady
	t.Cleanup(a.stopScanning)

	// Do not use captureStdout: finishCard calls StartScanning synchronously
	// which starts a spinner goroutine that would race with pipe closure.
	a.finishCard()

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard should be nil after finishCard, got %v", current)
	}
	if got := a.currentPhase(); got != phaseScanning {
		t.Fatalf("phase = %v, want phaseScanning", got)
	}
}

// ---------------------------------------------------------------------------
// handleInput
// ---------------------------------------------------------------------------

func TestHandleInput_Empty_IsNoOp(t *testing.T) {
	a := &App{copiedModes: make(map[string]bool)}
	out := captureStdout(t, func() { a.handleInput("") })
	if out != "" {
		t.Fatalf("empty input should produce no output, got: %q", out)
	}
}

func TestHandleInput_NoCard_NonblankInput(t *testing.T) {
	a := &App{copiedModes: make(map[string]bool)}
	out := captureStdout(t, func() { a.handleInput("a") })
	// Commands with no card present are silently ignored.
	if out != "" {
		t.Fatalf("expected no output when no card present, got: %q", out)
	}
}

func TestHandleInput_Help_ShowsCommands(t *testing.T) {
	a := &App{copiedModes: make(map[string]bool)}
	out := captureStdout(t, func() { a.handleInput("?") })
	if !strings.Contains(out, "[a]  Copy All") {
		t.Fatalf("expected help output, got:\n%s", out)
	}
}

func TestHandleInput_Unknown_WithCard(t *testing.T) {
	cfg := config.Defaults()
	cfg.Output.Color = false
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/card", Name: "CARD"}
	a.phase = phaseReady
	out := captureStdout(t, func() { a.handleInput("z") })
	if !strings.Contains(out, "Unknown command") {
		t.Fatalf("expected unknown command message, got:\n%s", out)
	}
}

func TestHandleInput_Exit_ClearsCurrentCard(t *testing.T) {
	cfg := config.Defaults()
	a := New(Config{Cfg: cfg})
	a.currentCard = &detect.Card{Path: "/card/main", Name: "MAIN"}
	a.phase = phaseReady
	t.Cleanup(a.stopScanning)

	a.handleInput("x")

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard should be nil after [x], got %v", current)
	}
}

func TestHandleInput_Eject_ClearsCurrentCard(t *testing.T) {
	cfg := config.Defaults()
	fd := newFakeDetector()
	a := New(Config{
		Cfg:         cfg,
		newDetector: func() cardDetector { return fd },
	})
	a.detector = fd
	a.currentCard = &detect.Card{Path: "/card/main", Name: "MAIN"}
	a.phase = phaseReady
	t.Cleanup(a.stopScanning)

	a.handleInput("e")

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current != nil {
		t.Fatalf("currentCard should be nil after [e], got %v", current)
	}
}
