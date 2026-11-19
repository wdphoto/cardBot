// Package detect reports card arrivals and removals. App and daemon use the
// same detector API; build constraints select how the host supplies events.
package detect

import "sync"

// Card represents a detected memory card.
type Card struct {
	Path       string        // Mount point (e.g., /Volumes/NIKON Z9)
	Name       string        // Volume name
	TotalBytes int64         // Total capacity (filesystem view)
	UsedBytes  int64         // Used space
	Brand      string        // Guessed brand from DCIM folder naming
	hwMu       sync.Mutex    // protects Hardware from async write
	Hardware   *HardwareInfo // Hardware-level info (may be nil if unavailable)
}

// HW returns the hardware info, safe for concurrent access.
func (c *Card) HW() *HardwareInfo {
	c.hwMu.Lock()
	defer c.hwMu.Unlock()
	return c.Hardware
}

// SetHW sets the hardware info, safe for concurrent access.
func (c *Card) SetHW(hw *HardwareInfo) {
	c.hwMu.Lock()
	defer c.hwMu.Unlock()
	c.Hardware = hw
}
