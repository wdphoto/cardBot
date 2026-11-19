//go:build linux

package detect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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

	d.stream.removeMissing(currentCards)
}
