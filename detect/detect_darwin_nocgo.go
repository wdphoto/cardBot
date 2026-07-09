//go:build darwin && (!cgo || !cardbot_native)

package detect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Detector monitors for memory card insertion/removal using polling.
// This is the fallback implementation when CGO is not available (no Xcode).
type Detector struct {
	cards    map[string]*Card
	events   chan *Card
	removals chan string
	mu       sync.RWMutex
	started  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewDetector creates a new card detector.
func NewDetector() *Detector {
	return &Detector{
		cards:    make(map[string]*Card),
		events:   make(chan *Card, 10),
		removals: make(chan string, 10),
		stopChan: make(chan struct{}),
	}
}

// Start begins monitoring for card insertion/removal.
func (d *Detector) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return fmt.Errorf("detector already started")
	}

	d.stopChan = make(chan struct{})
	d.started = true
	d.wg.Add(1)
	go d.pollLoop()

	return nil
}

// Stop halts card monitoring.
func (d *Detector) Stop() {
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return
	}
	d.started = false
	close(d.stopChan)
	d.mu.Unlock()

	d.wg.Wait()
}

// Events returns a channel for card insertion events.
func (d *Detector) Events() <-chan *Card { return d.events }

// Removals returns a channel for card removal events.
func (d *Detector) Removals() <-chan string { return d.removals }

// Eject unmounts the volume at the given path using macOS diskutil.
func (d *Detector) Eject(path string) error {
	// Use diskutil eject for non-CGO version
	cmd := exec.Command("diskutil", "eject", path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to eject %s: %w", path, err)
	}
	return nil
}

// Remove removes a card from tracking (used after programmatic eject).
func (d *Detector) Remove(path string) {
	d.mu.Lock()
	delete(d.cards, path)
	d.mu.Unlock()
}

func (d *Detector) pollLoop() {
	defer d.wg.Done()

	// Initial scan
	d.scanVolumes()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			d.scanVolumes()
		}
	}
}

func (d *Detector) scanVolumes() {
	volumes, err := os.ReadDir("/Volumes")
	if err != nil {
		return
	}

	// Build set of currently mounted memory cards
	currentCards := make(map[string]bool)

	for _, vol := range volumes {
		if !vol.IsDir() {
			continue
		}
		path := filepath.Join("/Volumes", vol.Name())

		if isMemoryCard(path) {
			currentCards[path] = true

			d.mu.RLock()
			_, alreadyTracked := d.cards[path]
			d.mu.RUnlock()

			if !alreadyTracked {
				// New card detected
				card := buildCard(path, vol.Name())
				if card != nil {
					d.mu.Lock()
					d.cards[path] = card
					d.mu.Unlock()
					select {
					case d.events <- card:
					default:
					}
				}
			}
		}
	}

	// Check for removed cards — collect first, then process.
	var removed []string
	d.mu.Lock()
	for path := range d.cards {
		if !currentCards[path] {
			removed = append(removed, path)
			delete(d.cards, path)
		}
	}
	d.mu.Unlock()
	for _, path := range removed {
		select {
		case d.removals <- path:
		default:
		}
	}
}
