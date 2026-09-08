//go:build linux

package detect

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetVolumeUUIDFromDir exercises the production lookup behavior with real
// temporary symlinks. The sda10 entry sorts first; the old substring matcher
// would wrongly pick it for device "sda1". Compiled on Linux only; runtime is
// exercised on CI (not on the Darwin development host).
func TestGetVolumeUUIDFromDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("../../sda10", filepath.Join(dir, "a-sda10")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../sda1", filepath.Join(dir, "b-sda1")); err != nil {
		t.Fatal(err)
	}
	// A non-matching regular entry must be skipped.
	if err := os.WriteFile(filepath.Join(dir, "not-a-link"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	if got := getVolumeUUIDFromDir("sda1", dir); got != "b-sda1" {
		t.Fatalf("getVolumeUUIDFromDir(sda1) = %q, want b-sda1 (not a-sda10)", got)
	}
	if got := getVolumeUUIDFromDir("sda10", dir); got != "a-sda10" {
		t.Fatalf("getVolumeUUIDFromDir(sda10) = %q, want a-sda10", got)
	}
	if got := getVolumeUUIDFromDir("sdb1", dir); got != "" {
		t.Fatalf("getVolumeUUIDFromDir(sdb1) = %q, want empty", got)
	}
}
