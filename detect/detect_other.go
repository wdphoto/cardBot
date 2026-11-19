//go:build !darwin && !linux

package detect

import (
	"fmt"
)

// Detector monitors for memory card insertion/removal.
type Detector struct {
	events chan Event
}

// NewDetector creates a new card detector.
func NewDetector() *Detector {
	return &Detector{
		events: make(chan Event),
	}
}

// Start begins monitoring for card insertion/removal.
func (d *Detector) Start() error {
	return fmt.Errorf("memory card detection not supported on this platform")
}

// Stop halts card monitoring.
func (d *Detector) Stop() {}

// Events is inert on unsupported platforms.
func (d *Detector) Events() <-chan Event {
	return d.events
}

// Remove is a no-op on unsupported platforms.
func (d *Detector) Remove(path string) {}

// Eject is not supported on this platform.
func (d *Detector) Eject(path string) error {
	return fmt.Errorf("eject not supported on this platform")
}
