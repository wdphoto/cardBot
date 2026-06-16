package cardcopy

import (
	"fmt"
	"time"

	"github.com/wdphoto/cardBot/fsutil"
)

// throughputTracker computes smoothed throughput and ETA using an exponential
// moving average (EMA). Designed for large media file transfers where raw
// throughput fluctuates due to OS caching, card thermal throttling, and
// directory boundaries.
//
// Not goroutine-safe — intended to be called from a single goroutine
// (the copy progress path).
type throughputTracker struct {
	alpha       float64   // EMA smoothing factor (0–1); higher = more reactive
	smoothedBPS float64   // current smoothed bytes-per-second
	lastBytes   int64     // bytesDone at last sample
	lastTime    time.Time // wall clock at last sample
	startTime   time.Time // copy start time
	samples     int       // number of samples taken
	ready       bool      // true once we have enough data for meaningful estimates
}

const (
	// etaMinSamples is the minimum sample count before ETA is reported.
	etaMinSamples = 3

	// etaMinElapsed is the minimum elapsed time before ETA is shown.
	etaMinElapsed = 5 * time.Second

	// defaultAlpha is the EMA smoothing factor. 0.3 gives a ~10-second
	// effective window at 2-second sample intervals.
	defaultAlpha = 0.3
)

func newThroughputTracker() *throughputTracker {
	return &throughputTracker{alpha: defaultAlpha}
}

// start initialises the tracker at the beginning of a copy operation.
func (t *throughputTracker) start(now time.Time, initialBytes int64) {
	t.startTime = now
	t.lastTime = now
	t.lastBytes = initialBytes
	t.smoothedBPS = 0
	t.samples = 0
	t.ready = false
}

// sample records a progress observation and returns the updated smoothed BPS.
// Call on every progress tick (typically every 2 seconds).
func (t *throughputTracker) sample(now time.Time, bytesDone int64) float64 {
	dt := now.Sub(t.lastTime).Seconds()
	if dt <= 0 {
		return t.smoothedBPS
	}

	instantBPS := float64(bytesDone-t.lastBytes) / dt
	t.lastTime = now
	t.lastBytes = bytesDone

	if t.samples == 0 {
		t.smoothedBPS = instantBPS
	} else {
		t.smoothedBPS = t.alpha*instantBPS + (1-t.alpha)*t.smoothedBPS
	}
	t.samples++

	if !t.ready && t.samples >= etaMinSamples && now.Sub(t.startTime) >= etaMinElapsed {
		t.ready = true
	}

	return t.smoothedBPS
}

// bps returns the current smoothed bytes-per-second.
func (t *throughputTracker) bps() float64 {
	return t.smoothedBPS
}

// eta returns the estimated seconds remaining, or -1 if not enough data.
func (t *throughputTracker) eta(bytesRemaining int64) float64 {
	if !t.ready || t.smoothedBPS <= 0 || bytesRemaining <= 0 {
		return -1
	}
	return float64(bytesRemaining) / t.smoothedBPS
}

// formatETA returns a human-readable ETA string, or "" if unavailable.
func formatETA(seconds float64) string {
	if seconds < 0 {
		return ""
	}
	secs := int(seconds + 0.5)
	if secs <= 0 {
		return ""
	}

	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60

	switch {
	case h > 0:
		return fmt.Sprintf("ETA %dh%dm", h, m)
	case m > 0:
		return fmt.Sprintf("ETA %dm%ds", m, s)
	default:
		return fmt.Sprintf("ETA %ds", s)
	}
}

// formatThroughput returns a human-readable throughput string from bytes/sec.
//
//	≥ 10 MB/s → "182 MB/s"  (no decimal)
//	<  10 MB/s → "8.4 MB/s" (one decimal)
func formatThroughput(bps float64) string {
	mbps := bps / (1024 * 1024)
	if mbps >= 9.95 { // rounds to 10 or above
		return fmt.Sprintf("%d MB/s", int(mbps+0.5))
	}
	return fmt.Sprintf("%.1f MB/s", mbps)
}

// FormatProgressLine renders the copy progress as a single-line string.
// Example: "Copying  1247/3051  48.2 GB/96.4 GB  182 MB/s  ETA 4m12s"
func FormatProgressLine(p Progress) string {
	line := fmt.Sprintf("Copying  %d/%d  %s/%s  %s",
		p.FilesDone, p.FilesTotal,
		fsutil.FormatBytes(p.BytesDone),
		fsutil.FormatBytes(p.BytesTotal),
		formatThroughput(p.SmoothedBPS),
	)

	if eta := formatETA(p.ETASeconds); eta != "" {
		line += "  " + eta
	}

	return line
}
