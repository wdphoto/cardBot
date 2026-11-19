package cardcopy

import (
	"context"
	"io"
)

// defaultCheckEvery is how often (in bytes) the tracking reader checks for
// context cancellation. 4 MB balances responsiveness with read overhead:
//   - At 200 MB/s → cancel detected within ~20ms
//   - At 10 MB/s  → cancel detected within ~400ms
const defaultCheckEvery = 4 * 1024 * 1024

// copyStream keeps byte tracking/cancellation on the reader while propagating
// short writes and device errors from the destination. It does not publish files.
func copyStream(ctx context.Context, dst io.Writer, src io.Reader, buf []byte, onRead func(int64)) (int64, error) {
	tr := &trackingReader{r: src, ctx: ctx, onRead: onRead, checkEvery: defaultCheckEvery}
	return io.CopyBuffer(dst, tr, buf)
}

// trackingReader counts source bytes and checks for cancellation during a
// large file, rather than waiting until the next file. Its counter measures
// reads, not bytes verified or published at the destination.
type trackingReader struct {
	r          io.Reader
	ctx        context.Context
	bytesRead  int64
	onRead     func(int64) // synchronous, provisional bytes in this file
	checkEvery int64       // bytes between cancellation checks
	sinceCheck int64       // bytes read since last cancellation check
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
		tr.bytesRead += int64(n)
		if tr.onRead != nil {
			tr.onRead(tr.bytesRead)
		}
		tr.sinceCheck += int64(n)
	}
	return n, err
}
