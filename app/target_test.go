package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/analyze"
	"github.com/wdphoto/cardBot/config"
)

func TestTargetPath_SkipsScanningAndAnalyzesImmediately(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Output.Color = false
	fd := newFakeDetector()

	analyzerCalled := false
	a := New(Config{
		Cfg:         cfg,
		TargetPath:  cardPath,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(path string) cardAnalyzer {
			analyzerCalled = true
			if path != cardPath {
				t.Fatalf("analyzer path = %q, want %q", path, cardPath)
			}
			return &fakeAnalyzer{result: &analyze.Result{Bodies: []string{"NIKON Z 9"}, FileCount: 10}}
		},
	})
	a.detector = fd
	t.Cleanup(a.stopScanning)

	// Run should start analyzing the target path immediately.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// Wait for analysis to complete — target path should become currentCard.
	waitForPhase(t, a, phaseReady, 3*time.Second)

	a.mu.Lock()
	current := a.currentCard
	result := a.lastResult
	a.mu.Unlock()

	if current == nil {
		t.Fatal("expected currentCard to be set from target path")
	}
	if current.Path != cardPath {
		t.Fatalf("currentCard.Path = %q, want %q", current.Path, cardPath)
	}
	if !analyzerCalled {
		t.Fatal("expected analyzer to be called for target path")
	}
	if result == nil {
		t.Fatal("expected lastResult to be populated")
	}

	// Shut down.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit on signal")
	}
}

func TestTargetPath_SetsCardNameFromBasename(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Output.Color = false
	fd := newFakeDetector()

	a := New(Config{
		Cfg:         cfg,
		TargetPath:  cardPath,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(_ string) cardAnalyzer {
			return &fakeAnalyzer{result: &analyze.Result{FileCount: 1}}
		},
	})
	a.detector = fd
	t.Cleanup(a.stopScanning)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	waitForPhase(t, a, phaseReady, 3*time.Second)

	a.mu.Lock()
	name := a.currentCard.Name
	a.mu.Unlock()

	expected := filepath.Base(cardPath)
	if name != expected {
		t.Fatalf("card name = %q, want %q", name, expected)
	}

	cancel()
	<-done
}

func TestTargetPath_PopulatesFilesystemUsage(t *testing.T) {
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Output.Color = false
	fd := newFakeDetector()

	a := New(Config{
		Cfg:         cfg,
		TargetPath:  cardPath,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(_ string) cardAnalyzer {
			return &fakeAnalyzer{result: &analyze.Result{FileCount: 1}}
		},
	})
	a.detector = fd
	t.Cleanup(a.stopScanning)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	waitForPhase(t, a, phaseReady, 3*time.Second)

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current == nil {
		t.Fatal("expected currentCard to be set from target path")
	}

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if current.TotalBytes <= 0 {
			t.Fatalf("TotalBytes = %d, want > 0 for target path", current.TotalBytes)
		}
		if current.UsedBytes < 0 {
			t.Fatalf("UsedBytes = %d, want >= 0 for target path", current.UsedBytes)
		}
	}

	cancel()
	<-done
}

func TestTargetPath_Empty_FallsBackToNormalScanning(t *testing.T) {
	cfg := config.Defaults()
	fd := newFakeDetector()

	a := New(Config{
		Cfg:         cfg,
		TargetPath:  "", // no target
		newDetector: func() cardDetector { return fd },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// With no target path and no cards, should remain in scanning phase.
	time.Sleep(200 * time.Millisecond)
	if got := a.currentPhase(); got != phaseScanning {
		t.Fatalf("phase = %v, want phaseScanning when no target path set", got)
	}

	a.mu.Lock()
	current := a.currentCard
	a.mu.Unlock()
	if current != nil {
		t.Fatal("expected no currentCard when no target path set")
	}

	cancel()
	<-done
}

func TestTargetPath_InvalidPath_ShowsError(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "nonexistent")

	cfg := config.Defaults()
	cfg.Output.Color = false
	fd := newFakeDetector()

	a := New(Config{
		Cfg:         cfg,
		TargetPath:  missingPath,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(_ string) cardAnalyzer {
			return &fakeAnalyzer{result: nil, err: os.ErrNotExist}
		},
	})
	a.detector = fd
	t.Cleanup(a.stopScanning)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// Give it time to attempt the target path and handle the error.
	time.Sleep(500 * time.Millisecond)

	cancel()
	<-done
}

func TestTargetPath_MissingDCIM_RetriesBeforeFailing(t *testing.T) {
	cardPath := t.TempDir()

	cfg := config.Defaults()
	cfg.Output.Color = false
	fd := newFakeDetector()

	var attempts atomic.Int32
	a := New(Config{
		Cfg:         cfg,
		TargetPath:  cardPath,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(_ string) cardAnalyzer {
			attempt := attempts.Add(1)
			if attempt < 3 {
				return &fakeAnalyzer{result: nil, err: os.ErrNotExist}
			}
			return &fakeAnalyzer{result: &analyze.Result{FileCount: 5}}
		},
	})
	a.detector = fd
	t.Cleanup(a.stopScanning)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	waitForPhase(t, a, phaseReady, 4*time.Second)
	if got := attempts.Load(); got < 3 {
		t.Fatalf("expected at least 3 analyze attempts, got %d", got)
	}

	cancel()
	<-done
}

func TestTargetPath_StillReceivesDetectorEvents(t *testing.T) {
	// Even with a target path, the detector should still be running
	// so that card removal events are handled.
	cardPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cardPath, "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	fd := newFakeDetector()

	a := New(Config{
		Cfg:         cfg,
		TargetPath:  cardPath,
		newDetector: func() cardDetector { return fd },
		newAnalyzer: func(_ string) cardAnalyzer {
			return &fakeAnalyzer{result: &analyze.Result{FileCount: 5}}
		},
	})
	a.detector = fd
	t.Cleanup(a.stopScanning)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	waitForPhase(t, a, phaseReady, 3*time.Second)

	// Verify detector was started (events should still flow).
	if !fd.started.Load() {
		t.Fatal("detector should be started even with target path")
	}

	cancel()
	<-done
}
