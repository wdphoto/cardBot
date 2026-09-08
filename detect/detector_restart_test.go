//go:build linux || (darwin && (!cgo || !cardbot_native))

package detect

import (
	"sync/atomic"
	"testing"
)

func TestDetector_Restart(t *testing.T) {
	d := NewDetector()
	var scans atomic.Int32
	d.scan = func() { scans.Add(1) } // Never enumerate the host's mounts.
	t.Cleanup(d.Stop)
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	d.Stop()
	if err := d.Start(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	d.Stop()
	if got := scans.Load(); got < 2 {
		t.Fatalf("scans = %d, want an initial scan on each start", got)
	}
}
