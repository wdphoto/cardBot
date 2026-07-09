//go:build darwin || linux

package detect

import "testing"

func TestDetector_Restart(t *testing.T) {
	d := NewDetector()
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	d.Stop()
	if err := d.Start(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	d.Stop()
}
