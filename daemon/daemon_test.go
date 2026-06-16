package daemon

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/detect"
)

// ---------------------------------------------------------------------------
// fakeDetector — same pattern as app/state_test.go
// ---------------------------------------------------------------------------

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
func (f *fakeDetector) Stop()                       { f.stopped.Store(true) }
func (f *fakeDetector) Events() <-chan *detect.Card { return f.events }
func (f *fakeDetector) Removals() <-chan string     { return f.removals }
func (f *fakeDetector) Eject(path string) error     { return nil }
func (f *fakeDetector) Remove(path string)          {}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDaemon_StartsDetectorAndWaitsForSignal(t *testing.T) {
	t.Parallel()

	fd := newFakeDetector()
	d := New(Config{
		newDetector:    func() detector { return fd },
		OnCardInserted: func(path string) {},
	})

	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	// Wait for detector to start.
	deadline := time.After(2 * time.Second)
	for !fd.started.Load() {
		select {
		case <-deadline:
			t.Fatal("detector Start() was not called")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Shut down.
	d.sigChan <- os.Interrupt

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit on signal")
	}

	if !fd.stopped.Load() {
		t.Fatal("detector Stop() was not called")
	}
}

func TestDaemon_CallsOnCardInserted_WhenCardDetected(t *testing.T) {
	t.Parallel()

	fd := newFakeDetector()

	var mu sync.Mutex
	var insertedPaths []string

	d := New(Config{
		newDetector: func() detector { return fd },
		OnCardInserted: func(path string) {
			mu.Lock()
			insertedPaths = append(insertedPaths, path)
			mu.Unlock()
		},
	})

	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	// Wait for detector to start.
	for !fd.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	// Send a card event.
	fd.events <- &detect.Card{Path: "/Volumes/NIKON Z 9", Name: "NIKON Z 9"}

	// Wait for callback.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(insertedPaths)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("OnCardInserted was not called")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	if len(insertedPaths) != 1 || insertedPaths[0] != "/Volumes/NIKON Z 9" {
		t.Fatalf("insertedPaths = %v, want [\"/Volumes/NIKON Z 9\"]", insertedPaths)
	}
	mu.Unlock()

	d.sigChan <- os.Interrupt
	<-done
}

func TestDaemon_TracksCards_NoDuplicateCallbacks(t *testing.T) {
	t.Parallel()

	fd := newFakeDetector()

	var mu sync.Mutex
	callCount := 0

	d := New(Config{
		newDetector: func() detector { return fd },
		OnCardInserted: func(path string) {
			mu.Lock()
			callCount++
			mu.Unlock()
		},
	})

	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	for !fd.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	// Send the same card twice.
	fd.events <- &detect.Card{Path: "/Volumes/CARD", Name: "CARD"}
	time.Sleep(100 * time.Millisecond)
	fd.events <- &detect.Card{Path: "/Volumes/CARD", Name: "CARD"}
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := callCount
	mu.Unlock()

	if got != 1 {
		t.Fatalf("OnCardInserted called %d times, want 1 (duplicate should be ignored)", got)
	}

	d.sigChan <- os.Interrupt
	<-done
}

func TestDaemon_CardRemoval_AllowsReinsertCallback(t *testing.T) {
	t.Parallel()

	fd := newFakeDetector()

	var mu sync.Mutex
	callCount := 0

	d := New(Config{
		newDetector:       func() detector { return fd },
		DuplicateCooldown: 50 * time.Millisecond,
		OnCardInserted: func(path string) {
			mu.Lock()
			callCount++
			mu.Unlock()
		},
	})

	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	for !fd.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	// Insert card.
	fd.events <- &detect.Card{Path: "/Volumes/CARD", Name: "CARD"}
	time.Sleep(100 * time.Millisecond)

	// Remove card.
	fd.removals <- "/Volumes/CARD"
	time.Sleep(100 * time.Millisecond)

	// Re-insert same card — should fire callback again.
	fd.events <- &detect.Card{Path: "/Volumes/CARD", Name: "CARD"}
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := callCount
	mu.Unlock()

	if got != 2 {
		t.Fatalf("OnCardInserted called %d times, want 2 (after removal + re-insert)", got)
	}

	d.sigChan <- os.Interrupt
	<-done
}

func TestDaemon_DetectorStartError_ReturnsError(t *testing.T) {
	t.Parallel()

	fd := newFakeDetector()
	fd.startErr = os.ErrPermission

	d := New(Config{
		newDetector:    func() detector { return fd },
		OnCardInserted: func(path string) {},
	})

	err := d.Run()
	if err == nil {
		t.Fatal("expected error when detector fails to start")
	}
}

func TestDaemon_MultipleCards_EachGetsCallback(t *testing.T) {
	t.Parallel()

	fd := newFakeDetector()

	var mu sync.Mutex
	var paths []string

	d := New(Config{
		newDetector: func() detector { return fd },
		OnCardInserted: func(path string) {
			mu.Lock()
			paths = append(paths, path)
			mu.Unlock()
		},
	})

	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	for !fd.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	fd.events <- &detect.Card{Path: "/Volumes/CARD_A", Name: "CARD_A"}
	fd.events <- &detect.Card{Path: "/Volumes/CARD_B", Name: "CARD_B"}
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	got := len(paths)
	mu.Unlock()

	if got != 2 {
		t.Fatalf("OnCardInserted called %d times, want 2", got)
	}

	d.sigChan <- os.Interrupt
	<-done
}

func TestDaemon_Cooldown_SuppressesRapidReinsert(t *testing.T) {
	now := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	calls := 0
	d := New(Config{
		DuplicateCooldown: 5 * time.Second,
		now:               func() time.Time { return now },
		OnCardInserted: func(path string) {
			calls++
		},
	})

	card := &detect.Card{Path: "/Volumes/CARD", Name: "CARD"}
	d.handleCard(card)
	d.handleRemoval(card.Path)

	now = now.Add(2 * time.Second)
	d.handleCard(card)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (suppressed by cooldown)", calls)
	}

	now = now.Add(4 * time.Second)
	d.handleCard(card)
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (cooldown elapsed)", calls)
	}
}

func TestDaemon_PIDFile_WrittenAndRemoved(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "cardbot-daemon-pid-test")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidPath := tmpDir + "/cardbot.pid"

	fd := newFakeDetector()
	d := New(Config{
		newDetector:    func() detector { return fd },
		OnCardInserted: func(path string) {},
		pidPathFn:      func() (string, error) { return pidPath, nil },
	})

	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	// Wait for detector to start.
	for !fd.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	// PID file should exist while daemon is running.
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("PID file not found while daemon running: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil || pid <= 0 {
		t.Fatalf("PID file contains invalid data: %s", string(pidData))
	}

	// Shut down daemon.
	d.sigChan <- os.Interrupt

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit on signal")
	}

	// PID file should be removed after daemon exits.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("PID file should be removed after daemon exits, stat error: %v", err)
	}
}

func TestDaemon_PIDFile_UnavailablePath_NoError(t *testing.T) {
	t.Parallel()

	// Use a path that cannot be created.
	pidPath := "/nonexistent/path/cardbot.pid"

	fd := newFakeDetector()
	d := New(Config{
		newDetector:    func() detector { return fd },
		OnCardInserted: func(path string) {},
		pidPathFn:      func() (string, error) { return pidPath, nil },
	})

	done := make(chan error, 1)
	go func() { done <- d.Run() }()

	// Wait for detector to start.
	for !fd.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	// Daemon should still run despite PID file error.
	_ = fd

	d.sigChan <- os.Interrupt

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit on signal")
	}
}
