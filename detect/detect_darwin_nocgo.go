//go:build darwin && (!cgo || !cardbot_native)

package detect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Eject unmounts the volume at the given path using macOS diskutil.
func (d *Detector) Eject(path string) error {
	// Use diskutil eject for non-CGO version
	cmd := exec.Command("diskutil", "eject", path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to eject %s: %w", path, err)
	}
	return nil
}

// scanVolumes compares the mount paths that have a DCIM directory now with
// those from the previous poll. Detection triggers a scan, not a copy.
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

			d.processCard(path, vol.Name())
		}
	}
	d.stream.removeMissing(currentCards)
}
