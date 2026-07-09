//go:build linux

package detect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Detector monitors for memory card insertion/removal on Linux.
// Uses polling since udev/dbus adds complexity and we want zero CGO.
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

// Eject unmounts the volume at the given path.
// Tries udisksctl first (takes a mount point), falls back to umount.
func (d *Detector) Eject(path string) error {
	// udisksctl unmount with --mount-point accepts a mount path directly.
	cmd := exec.Command("udisksctl", "unmount", "--mount-point", path)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Fallback to umount (also accepts mount paths).
	cmd = exec.Command("umount", path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unmount %s: %w", path, err)
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

// getMountPoints returns the standard Linux mount base directories for removable media.
// Modern distros (Fedora, Ubuntu 20+) use /run/media/$USER/CARDNAME (nested).
// Older distros use /media/CARDNAME or /media/$USER/CARDNAME (flat or nested).
// Manual mounts often appear under /mnt/CARDNAME (flat).
func getMountPoints() []string {
	return []string{
		"/run/media", // systemd / modern GNOME, KDE
		"/media",     // traditional / older distros
		"/mnt",       // manual mounts
	}
}

func (d *Detector) scanVolumes() {
	mountPoints := getMountPoints()
	currentCards := make(map[string]bool)

	for _, mountBase := range mountPoints {
		entries, err := os.ReadDir(mountBase)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(mountBase, entry.Name())

			// Check if this entry is a card directly (flat mount: /media/CARD, /mnt/CARD).
			if isMemoryCard(path) {
				currentCards[path] = true
				d.processCard(path, entry.Name())
				continue
			}

			// Otherwise treat it as a user namespace and scan one level deeper
			// (nested mount: /run/media/user/CARD, /media/user/CARD).
			subEntries, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, subEntry := range subEntries {
				if !subEntry.IsDir() {
					continue
				}
				subPath := filepath.Join(path, subEntry.Name())
				if isMemoryCard(subPath) {
					currentCards[subPath] = true
					d.processCard(subPath, subEntry.Name())
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

func (d *Detector) processCard(path, name string) {
	d.mu.RLock()
	_, alreadyTracked := d.cards[path]
	d.mu.RUnlock()

	if alreadyTracked {
		return
	}

	card := buildCard(path, name)
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
