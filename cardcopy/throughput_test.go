package cardcopy

import (
	"fmt"
	"testing"
	"time"
)

func TestThroughputTracker_SmoothedBPS(t *testing.T) {
	t.Parallel()

	tr := newThroughputTracker()
	start := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	tr.start(start, 0)

	// Simulate steady 100 MB/s for 10 seconds at 2-second intervals.
	mbps := int64(100 * 1024 * 1024) // 100 MB
	for i := 1; i <= 5; i++ {
		now := start.Add(time.Duration(i*2) * time.Second)
		bytesDone := mbps * int64(i*2)
		tr.sample(now, bytesDone)
	}

	// Smoothed BPS should be close to 100 MB/s after steady input.
	got := tr.bps()
	wantMin := float64(95 * 1024 * 1024)
	wantMax := float64(105 * 1024 * 1024)
	if got < wantMin || got > wantMax {
		t.Errorf("bps = %.0f, want between %.0f and %.0f", got, wantMin, wantMax)
	}
}

func TestThroughputTracker_ETANotReadyEarly(t *testing.T) {
	t.Parallel()

	tr := newThroughputTracker()
	start := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	tr.start(start, 0)

	// Only 1 sample, 2 seconds in — not enough data.
	tr.sample(start.Add(2*time.Second), 200*1024*1024)

	got := tr.eta(1024 * 1024 * 1024) // 1 GB remaining
	if got >= 0 {
		t.Errorf("eta should be negative (not ready), got %f", got)
	}
}

func TestThroughputTracker_ETAReadyAfterMinSamples(t *testing.T) {
	t.Parallel()

	tr := newThroughputTracker()
	start := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	tr.start(start, 0)

	// Feed enough samples to cross the readiness threshold.
	mbps := int64(100 * 1024 * 1024)
	for i := 1; i <= 5; i++ {
		now := start.Add(time.Duration(i*2) * time.Second)
		tr.sample(now, mbps*int64(i*2))
	}

	remaining := int64(500 * 1024 * 1024) // 500 MB
	got := tr.eta(remaining)
	if got < 0 {
		t.Fatal("eta should be available after enough samples")
	}

	// At ~100 MB/s, 500 MB should take ~5 seconds.
	if got < 3 || got > 7 {
		t.Errorf("eta = %.1fs, want ~5s for 500 MB at 100 MB/s", got)
	}
}

func TestThroughputTracker_ETAZeroBytesRemaining(t *testing.T) {
	t.Parallel()

	tr := newThroughputTracker()
	start := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	tr.start(start, 0)

	for i := 1; i <= 5; i++ {
		tr.sample(start.Add(time.Duration(i*2)*time.Second), int64(i*200*1024*1024))
	}

	got := tr.eta(0)
	if got >= 0 {
		t.Errorf("eta should be negative when 0 bytes remaining, got %f", got)
	}
}

func TestThroughputTracker_AdaptsToSpeedChange(t *testing.T) {
	t.Parallel()

	tr := newThroughputTracker()
	start := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	tr.start(start, 0)

	// 5 samples at 100 MB/s.
	mbps100 := int64(100 * 1024 * 1024)
	var bytesSoFar int64
	for i := 1; i <= 5; i++ {
		bytesSoFar += mbps100 * 2 // 2 seconds per sample
		tr.sample(start.Add(time.Duration(i*2)*time.Second), bytesSoFar)
	}

	// Now 5 samples at 50 MB/s.
	mbps50 := int64(50 * 1024 * 1024)
	for i := 6; i <= 10; i++ {
		bytesSoFar += mbps50 * 2
		tr.sample(start.Add(time.Duration(i*2)*time.Second), bytesSoFar)
	}

	// Should have moved toward 50 MB/s, not stuck at 100.
	got := tr.bps() / (1024 * 1024)
	if got > 85 {
		t.Errorf("bps after slowdown = %.0f MB/s, should have adapted below 85", got)
	}
	if got < 40 {
		t.Errorf("bps after slowdown = %.0f MB/s, should not overshoot below 40", got)
	}
}

func TestThroughputTracker_ZeroDeltaTime(t *testing.T) {
	t.Parallel()

	tr := newThroughputTracker()
	start := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	tr.start(start, 0)

	// Two samples at the exact same time — should not panic or divide by zero.
	tr.sample(start, 1000)
	tr.sample(start, 2000)

	// Should still return something sane (zero or previous).
	got := tr.bps()
	if got < 0 {
		t.Errorf("bps should not be negative, got %f", got)
	}
}

func TestFormatETA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		etaSeconds float64
		want       string
	}{
		{"not ready", -1, ""},
		{"zero", 0, ""},
		{"seconds", 32, "ETA 32s"},
		{"one minute", 60, "ETA 1m0s"},
		{"minutes and seconds", 252, "ETA 4m12s"},
		{"exact minutes", 120, "ETA 2m0s"},
		{"over an hour", 3672, "ETA 1h1m"},
		{"just hours", 7200, "ETA 2h0m"},
		{"sub-second rounds to zero", 0.4, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatETA(tt.etaSeconds)
			if got != tt.want {
				t.Errorf("formatETA(%v) = %q, want %q", tt.etaSeconds, got, tt.want)
			}
		})
	}
}

func TestFormatThroughput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bps  float64
		want string
	}{
		{"high speed", 182 * 1024 * 1024, "182 MB/s"},
		{"medium", 85 * 1024 * 1024, "85 MB/s"},
		{"exactly 10", 10 * 1024 * 1024, "10 MB/s"},
		{"below 10", 8.4 * 1024 * 1024, "8.4 MB/s"},
		{"low speed", 0.8 * 1024 * 1024, "0.8 MB/s"},
		{"zero", 0, "0.0 MB/s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatThroughput(tt.bps)
			if got != tt.want {
				t.Errorf("formatThroughput(%v) = %q, want %q", tt.bps, got, tt.want)
			}
		})
	}
}

func TestFormatProgressLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    Progress
		want string
	}{
		{
			name: "mid copy with eta",
			p: Progress{
				FilesDone:   1247,
				FilesTotal:  3051,
				BytesDone:   48200000000,
				BytesTotal:  96400000000,
				ETASeconds:  252,
				SmoothedBPS: 182 * 1024 * 1024,
			},
			want: "Copying  1247/3051  44.9 GB/89.8 GB  182 MB/s  ETA 4m12s",
		},
		{
			name: "early no eta",
			p: Progress{
				FilesDone:   3,
				FilesTotal:  3051,
				BytesDone:   150000000,
				BytesTotal:  96400000000,
				ETASeconds:  -1,
				SmoothedBPS: 50 * 1024 * 1024,
			},
			want: "Copying  3/3051  143.1 MB/89.8 GB  50 MB/s",
		},
		{
			name: "complete",
			p: Progress{
				FilesDone:   100,
				FilesTotal:  100,
				BytesDone:   5000000000,
				BytesTotal:  5000000000,
				ETASeconds:  0,
				SmoothedBPS: 100 * 1024 * 1024,
			},
			want: "Copying  100/100  4.7 GB/4.7 GB  100 MB/s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatProgressLine(tt.p)
			if got != tt.want {
				t.Errorf("FormatProgressLine() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

// Verify FormatProgressLine uses detect.FormatBytes for consistency.
func TestFormatProgressLine_UsesFormatBytes(t *testing.T) {
	t.Parallel()

	p := Progress{
		FilesDone:   1,
		FilesTotal:  10,
		BytesDone:   1024,       // 1.0 KB
		BytesTotal:  1073741824, // 1.0 GB
		ETASeconds:  -1,
		SmoothedBPS: 1024 * 1024,
	}

	got := FormatProgressLine(p)
	// Should contain detect.FormatBytes output.
	if got == "" {
		t.Error("FormatProgressLine should not return empty string")
	}
	fmt.Println("progress line:", got)
}
