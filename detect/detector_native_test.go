//go:build darwin && cgo && cardbot_native

package detect

import (
	"os"
	"testing"
)

// DiskArbitration necessarily talks to the host. Keep this integration test
// explicit; ordinary local/native test runs must not enumerate attached cards.
func TestNativeDetector_Restart(t *testing.T) {
	if os.Getenv("CARDBOT_TEST_LIVE_DETECTOR") != "1" {
		t.Skip("live DiskArbitration test requires CARDBOT_TEST_LIVE_DETECTOR=1")
	}
	d := NewDetector()
	t.Cleanup(d.Stop)
	for range 2 {
		if err := d.Start(); err != nil {
			t.Fatal(err)
		}
		d.Stop()
	}
}
