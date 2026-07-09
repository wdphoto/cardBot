// Package daemon provides a headless background mode that watches for
// camera card insertions and invokes a callback (typically launching a
// terminal window with cardbot). It reuses the detect package for native
// card detection but has no TUI, spinner, or stdin handling.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/wdphoto/cardBot/detect"
	"github.com/wdphoto/cardBot/term"
)

// ErrAlreadyRunning indicates another daemon owns the process lock.
var ErrAlreadyRunning = errors.New("cardbot daemon is already running")

// detector is the consumer-side interface for card detection.
// Matches the subset of detect.Detector that the daemon needs.
type detector interface {
	Start() error
	Stop()
	Events() <-chan *detect.Card
	Removals() <-chan string
	Eject(path string) error
	Remove(path string)
}

// Config holds the settings for creating a new Daemon.
type Config struct {
	// OnCardInserted is called when a new card is detected.
	// Receives the mount path (e.g. "/Volumes/NIKON Z 9").
	OnCardInserted func(path string)

	// DuplicateCooldown suppresses rapid repeat launch attempts for the same path
	// after a removal/re-appearance cycle (e.g. during sleep/wake churn).
	DuplicateCooldown time.Duration

	// newDetector creates a detector. If nil, uses detect.NewDetector.
	// Unexported: only overridden by tests.
	newDetector func() detector

	// now is an optional time provider for tests.
	now func() time.Time

	// pidPathFn returns the path for the daemon PID file.
	// If nil, uses the default path (~/.cardbot/cardbot.pid).
	pidPathFn func() (string, error)

	// enforceSingleton is enabled in production. Tests using injected detectors
	// opt in only when specifically exercising singleton behavior.
	enforceSingleton bool
}

// Daemon is a long-running background process that watches for card insertions.
type Daemon struct {
	newDetector       func() detector
	onCardInserted    func(path string)
	tracked           map[string]bool
	recentlyProcessed map[string]time.Time
	duplicateCooldown time.Duration
	now               func() time.Time
	pidPath           string
	mu                sync.Mutex
	sigChan           chan os.Signal
	enforceSingleton  bool
}

// New creates a new Daemon instance.
func New(c Config) *Daemon {
	newDetector := c.newDetector
	if newDetector == nil {
		newDetector = func() detector { return detect.NewDetector() }
	}
	onCardInserted := c.OnCardInserted
	if onCardInserted == nil {
		onCardInserted = func(path string) {}
	}
	cooldown := c.DuplicateCooldown
	if cooldown <= 0 {
		cooldown = 5 * time.Second
	}
	now := c.now
	if now == nil {
		now = time.Now
	}
	pidPathFn := c.pidPathFn
	if pidPathFn == nil {
		pidPathFn = PidPath
	}
	pidPath, _ := pidPathFn() // Ignore error; Run() will handle it

	sigChan := make(chan os.Signal, 1)
	enforceSingleton := c.enforceSingleton || c.newDetector == nil

	return &Daemon{
		newDetector:       newDetector,
		onCardInserted:    onCardInserted,
		tracked:           make(map[string]bool),
		recentlyProcessed: make(map[string]time.Time),
		duplicateCooldown: cooldown,
		now:               now,
		pidPath:           pidPath,
		sigChan:           sigChan,
		enforceSingleton:  enforceSingleton,
	}
}

// PidPath returns the default path for the daemon PID file.
func PidPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".cardbot", "cardbot.pid"), nil
}

// Run starts the daemon event loop. It blocks until SIGINT/SIGTERM.
func (d *Daemon) Run() error {
	signal.Notify(d.sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(d.sigChan)

	var lock *processLock
	if d.enforceSingleton && d.pidPath != "" {
		if err := os.MkdirAll(filepath.Dir(d.pidPath), 0o700); err != nil {
			return fmt.Errorf("creating daemon state directory: %w", err)
		}
		var err error
		lock, err = acquireProcessLock(d.pidPath + ".lock")
		if err != nil {
			if errors.Is(err, errProcessLocked) {
				return ErrAlreadyRunning
			}
			return fmt.Errorf("acquiring daemon process lock: %w", err)
		}
		defer lock.Close()
	}

	// Write PID file.
	if d.pidPath != "" {
		if err := os.MkdirAll(filepath.Dir(d.pidPath), 0700); err != nil {
			fmt.Printf("[%s] Warning: could not create PID directory: %v\n", term.Ts(), err)
		} else if err := os.WriteFile(d.pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			fmt.Printf("[%s] Warning: could not write PID file: %v\n", term.Ts(), err)
		}
	}

	// Ensure PID file is cleaned up on exit.
	removePID := func() {
		if d.pidPath != "" {
			_ = os.Remove(d.pidPath)
		}
	}

	detector := d.newDetector()
	if err := detector.Start(); err != nil {
		removePID()
		return fmt.Errorf("starting detector: %w", err)
	}
	defer detector.Stop()
	defer removePID()

	fmt.Printf("[%s] cardBot daemon started — watching for cards...\n", term.Ts())

	for {
		select {
		case card := <-detector.Events():
			d.handleCard(card)

		case path := <-detector.Removals():
			d.handleRemoval(path)

		case <-d.sigChan:
			fmt.Printf("[%s] Daemon shutting down\n", term.Ts())
			return nil
		}
	}
}

func (d *Daemon) handleCard(card *detect.Card) {
	if card == nil || card.Path == "" {
		return
	}

	d.mu.Lock()
	if d.tracked[card.Path] {
		d.mu.Unlock()
		return
	}

	now := d.now()
	for path, last := range d.recentlyProcessed {
		if now.Sub(last) >= d.duplicateCooldown {
			delete(d.recentlyProcessed, path)
		}
	}
	if last, ok := d.recentlyProcessed[card.Path]; ok && now.Sub(last) < d.duplicateCooldown {
		d.mu.Unlock()
		fmt.Printf("[%s] Suppressing duplicate card event for %s (cooldown)\n", term.Ts(), card.Path)
		return
	}

	d.tracked[card.Path] = true
	d.recentlyProcessed[card.Path] = now
	d.mu.Unlock()

	fmt.Printf("[%s] Card detected: %s (%s)\n", term.Ts(), card.Name, card.Path)
	d.onCardInserted(card.Path)
}

func (d *Daemon) handleRemoval(path string) {
	if path == "" {
		return
	}

	d.mu.Lock()
	if !d.tracked[path] {
		d.mu.Unlock()
		return
	}
	delete(d.tracked, path)
	d.mu.Unlock()

	fmt.Printf("[%s] Card removed: %s\n", term.Ts(), path)
}
