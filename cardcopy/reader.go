package cardcopy

import (
	"context"
	"io"
	"sync/atomic"
)

// defaultCheckEvery is how often (in bytes) the tracking reader checks for
// context cancellation. 4 MB balances responsiveness with read overhead:
//   - At 200 MB/s → cancel detected within ~20ms
//   - At 10 MB/s  → cancel detected within ~400ms
const defaultCheckEvery = 4 * 1024 * 1024

// copyStream keeps byte tracking/cancellation on the reader while propagating
// short writes and device errors from the destination. It does not publish files.
func copyStream(ctx context.Context, dst io.Writer, src io.Reader, buf []byte, counter *atomic.Int64) (int64, error) {
	tr := &trackingReader{r: src, ctx: ctx, counter: counter, checkEvery: defaultCheckEvery}
	return io.CopyBuffer(dst, tr, buf)
}

// trackingReader wraps an io.Reader to provide:
//  1. Live byte counting via an atomic counter (for intra-file progress)
//  2. Periodic context cancellation checks (for mid-file abort)
//
// The counter is atomic so the copy goroutine can write it while the progress
// callback reads it from the same goroutine (or a different one if needed).
type trackingReader struct {
	r          io.Reader
	ctx        context.Context
	counter    *atomic.Int64 // cumulative bytes read, updated on every Read
	checkEvery int64         // bytes between cancellation checks
	sinceCheck int64         // bytes read since last cancellation check
}

func (tr *trackingReader) Read(p []byte) (int, error) {
	// Check for cancellation if we've crossed a check boundary.
	if tr.ctx != nil && tr.checkEvery > 0 && tr.sinceCheck >= tr.checkEvery {
		select {
		case <-tr.ctx.Done():
			return 0, tr.ctx.Err()
		default:
		}
		tr.sinceCheck = 0
	}

	n, err := tr.r.Read(p)
	if n > 0 {
		tr.counter.Add(int64(n))
		tr.sinceCheck += int64(n)
	}
	return n, err
}
