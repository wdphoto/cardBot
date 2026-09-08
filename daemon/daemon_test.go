package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/wdphoto/cardBot/detect"
)

// All daemon tests inject detection and PID paths. No test watches real cards
// or writes the user's daemon state, even when several tests run in parallel.
type fakeDetector struct {
	startErr error
	started  chan struct{}
	stopped  atomic.Bool
	events   chan *detect.Card
	removals chan string
}

func newFakeDetector() *fakeDetector {
	return &fakeDetector{
		started:  make(chan struct{}),
		events:   make(chan *detect.Card),
		removals: make(chan string),
	}
}

func (f *fakeDetector) Start() error                { close(f.started); return f.startErr }
func (f *fakeDetector) Stop()                       { f.stopped.Store(true) }
func (f *fakeDetector) Events() <-chan *detect.Card { return f.events }
func (f *fakeDetector) Removals() <-chan string     { return f.removals }
func (f *fakeDetector) Eject(string) error          { panic("daemon must not eject cards") }
func (f *fakeDetector) Remove(string)               { panic("daemon must not remove cards") }

func newTestDaemon(t *testing.T, cfg Config) *Daemon {
	t.Helper()
	if cfg.pidPathFn == nil {
		path := filepath.Join(t.TempDir(), "cardbot.pid")
		cfg.pidPathFn = func() (string, error) { return path, nil }
	}
	if cfg.newDetector == nil {
		cfg.newDetector = func() detector { return newFakeDetector() }
	}
	cfg.enforceSingleton = true
	return New(cfg)
}

// startTestDaemon provides bounded startup/shutdown and cleanup on assertion
// failure. Closing done synchronizes access to runErr and callback results.
func startTestDaemon(t *testing.T, d *Daemon, fd *fakeDetector) func(os.Signal) {
	t.Helper()
	done := make(chan struct{})
	var runErr error
	go func() { defer close(done); runErr = d.Run() }()
	stop := func(sig os.Signal) {
		t.Helper()
		select {
		case <-done:
		default:
			select {
			case d.sigChan <- sig:
			default:
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("daemon did not shut down")
			}
		}
		if runErr != nil {
			t.Fatalf("Run(): %v", runErr)
		}
		if !fd.stopped.Load() {
			t.Error("detector Stop was not called")
		}
	}
	t.Cleanup(func() { stop(os.Interrupt) })
	select {
	case <-fd.started:
	case <-done:
		t.Fatalf("Run exited before startup: %v", runErr)
	case <-time.After(3 * time.Second):
		t.Fatal("detector did not start")
	}
	return stop
}

func sendCard(t *testing.T, fd *fakeDetector, path string) {
	t.Helper()
	select {
	case fd.events <- &detect.Card{Path: path, Name: "synthetic card"}:
	case <-time.After(3 * time.Second):
		t.Fatal("card event was not received")
	}
}

func sendRemoval(t *testing.T, fd *fakeDetector, path string) {
	t.Helper()
	select {
	case fd.removals <- path:
	case <-time.After(3 * time.Second):
		t.Fatal("removal event was not received")
	}
}

func TestDaemon_StartsDetectorAndWaitsForSignal(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			fd := newFakeDetector()
			d := newTestDaemon(t, Config{newDetector: func() detector { return fd }})
			stop := startTestDaemon(t, d, fd)
			stop(sig)
		})
	}
}

func TestDaemon_RejectsConcurrentInstance(t *testing.T) {
	t.Parallel()
	pidPath := filepath.Join(t.TempDir(), "cardbot.pid")
	cfg := Config{pidPathFn: func() (string, error) { return pidPath, nil }}
	fd1 := newFakeDetector()
	cfg.newDetector = func() detector { return fd1 }
	d1 := newTestDaemon(t, cfg)
	stop := startTestDaemon(t, d1, fd1)
	before, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg.newDetector = func() detector { t.Fatal("contender must not create a detector"); return nil }
	if err := newTestDaemon(t, cfg).Run(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("contender error = %v, want ErrAlreadyRunning", err)
	}
	after, err := os.ReadFile(pidPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("contender changed owner's PID file: %q, %v", after, err)
	}
	stop(os.Interrupt)

	// A subsequent daemon can acquire the same lifetime lock after shutdown.
	fd2 := newFakeDetector()
	cfg.newDetector = func() detector { return fd2 }
	stop2 := startTestDaemon(t, newTestDaemon(t, cfg), fd2)
	stop2(syscall.SIGTERM)
}

func TestDaemon_CallsOnCardInserted_WhenCardDetected(t *testing.T) {
	t.Parallel()
	fd := newFakeDetector()
	var paths []string
	d := newTestDaemon(t, Config{
		newDetector:    func() detector { return fd },
		OnCardInserted: func(path string) { paths = append(paths, path) },
	})
	stop := startTestDaemon(t, d, fd)
	// Exact mount whitespace must survive the daemon callback too.
	want := filepath.Join(t.TempDir(), "CARD  ")
	sendCard(t, fd, want)
	stop(os.Interrupt)
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("callbacks = %q, want [%q]", paths, want)
	}
}

func TestDaemon_TracksCards_NoDuplicateCallbacks(t *testing.T) {
	t.Parallel()
	fd := newFakeDetector()
	calls := 0
	d := newTestDaemon(t, Config{
		newDetector:    func() detector { return fd },
		OnCardInserted: func(string) { calls++ },
	})
	stop := startTestDaemon(t, d, fd)
	path := filepath.Join(t.TempDir(), "CARD")
	sendCard(t, fd, path)
	sendCard(t, fd, path)
	stop(os.Interrupt)
	if calls != 1 {
		t.Fatalf("callbacks = %d, want 1", calls)
	}
}

func TestDaemon_CardRemoval_AllowsReinsertCallback(t *testing.T) {
	t.Parallel()
	fd := newFakeDetector()
	var clock atomic.Int64
	callbacks := make(chan string, 2)
	d := newTestDaemon(t, Config{
		newDetector:    func() detector { return fd },
		now:            func() time.Time { return time.Unix(clock.Load(), 0) },
		OnCardInserted: func(path string) { callbacks <- path },
	})
	stop := startTestDaemon(t, d, fd)
	path := filepath.Join(t.TempDir(), "CARD")
	sendCard(t, fd, path)
	select {
	case <-callbacks:
	case <-time.After(3 * time.Second):
		t.Fatal("first callback missing")
	}
	sendRemoval(t, fd, path)
	clock.Store(10) // Beyond the cooldown, without wall-clock sleeps.
	sendCard(t, fd, path)
	stop(os.Interrupt)
	if len(callbacks) != 1 {
		t.Fatal("reinsert callback missing")
	}
}

func TestDaemon_MultipleCards_EachGetsCallback(t *testing.T) {
	t.Parallel()
	fd := newFakeDetector()
	var paths []string
	d := newTestDaemon(t, Config{
		newDetector:    func() detector { return fd },
		OnCardInserted: func(path string) { paths = append(paths, path) },
	})
	stop := startTestDaemon(t, d, fd)
	base := t.TempDir()
	a, b := filepath.Join(base, "A"), filepath.Join(base, "B")
	sendCard(t, fd, a)
	sendCard(t, fd, b)
	stop(os.Interrupt)
	if len(paths) != 2 || paths[0] != a || paths[1] != b {
		t.Fatalf("callbacks = %q", paths)
	}
}

func TestDaemon_Cooldown_SuppressesRapidReinsert(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	calls := 0
	d := newTestDaemon(t, Config{
		now:            func() time.Time { return now },
		OnCardInserted: func(string) { calls++ },
	})
	card := &detect.Card{Path: filepath.Join(t.TempDir(), "CARD")}
	d.handleCard(card)
	d.handleRemoval(card.Path)
	now = now.Add(2 * time.Second)
	d.handleCard(card)
	if calls != 1 {
		t.Fatalf("callbacks during cooldown = %d, want 1", calls)
	}
	now = now.Add(4 * time.Second)
	d.handleCard(card)
	if calls != 2 {
		t.Fatalf("callbacks after cooldown = %d, want 2", calls)
	}
}

func TestDaemon_PIDFile_WrittenAndRemoved(t *testing.T) {
	t.Parallel()
	fd := newFakeDetector()
	d := newTestDaemon(t, Config{newDetector: func() detector { return fd }})
	stop := startTestDaemon(t, d, fd)
	data, err := os.ReadFile(d.pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("PID contents = %q", data)
	}
	stop(syscall.SIGTERM)
	if _, err := os.Stat(d.pidPath); !os.IsNotExist(err) {
		t.Fatalf("PID file remains after shutdown: %v", err)
	}
}

func TestDaemon_DetectorStartError_ReturnsError(t *testing.T) {
	t.Parallel()
	fd := newFakeDetector()
	fd.startErr = os.ErrPermission
	d := newTestDaemon(t, Config{newDetector: func() detector { return fd }})
	if err := d.Run(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Run error = %v", err)
	}
	if _, err := os.Stat(d.pidPath); !os.IsNotExist(err) {
		t.Fatalf("PID file remains after failed startup: %v", err)
	}
	lock, err := acquireProcessLock(d.pidPath + ".lock")
	if err != nil {
		t.Fatalf("failed startup retained lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDaemon_RequiresSingletonStatePath(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"empty", nil}, {"lookup error", os.ErrNotExist},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := newTestDaemon(t, Config{
				pidPathFn:   func() (string, error) { return "", tc.err },
				newDetector: func() detector { t.Fatal("must not start without singleton state"); return nil },
			})
			err := d.Run()
			if err == nil {
				t.Fatal("expected PID path error")
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("Run error = %v, want wrapped %v", err, tc.err)
			}
		})
	}
}

func TestDaemon_UnavailableStateDirectoryFailsBeforeDetection(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	d := newTestDaemon(t, Config{
		pidPathFn:   func() (string, error) { return filepath.Join(blocker, "cardbot.pid"), nil },
		newDetector: func() detector { t.Fatal("must not detect without singleton lock"); return nil },
	})
	if err := d.Run(); err == nil {
		t.Fatal("expected state directory error")
	}
}

func TestDaemon_PIDFile_UnavailablePath_NoError(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fd := newFakeDetector()
	// Only injected, non-singleton test daemons may run without a PID file.
	d := New(Config{
		newDetector: func() detector { return fd },
		pidPathFn:   func() (string, error) { return filepath.Join(blocker, "cardbot.pid"), nil },
	})
	stop := startTestDaemon(t, d, fd)
	stop(os.Interrupt)
}

func TestNew_ProductionEnforcesSingleton(t *testing.T) {
	d := New(Config{pidPathFn: func() (string, error) { return filepath.Join(t.TempDir(), "cardbot.pid"), nil }})
	if !d.enforceSingleton {
		t.Fatal("production daemon must enforce singleton")
	}
}
