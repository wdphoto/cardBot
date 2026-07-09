//go:build !darwin && !linux

package cardcopy

import "os"

// commitNoReplace uses a hard-link commit on other platforms. Both paths are
// in the same destination directory, and Link fails if dst already exists.
func commitNoReplace(src, dst string) error {
	if err := os.Link(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
