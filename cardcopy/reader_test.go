package cardcopy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestTrackingReader_CountsBytes(t *testing.T) {
	t.Parallel()

	data := make([]byte, 1024*1024) // 1 MB
	src := bytes.NewReader(data)

	r := &trackingReader{
		r: src,
	}

	buf := make([]byte, 64*1024)
	var total int64
	for {
		n, err := r.Read(buf)
		total += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	if total != int64(len(data)) {
		t.Errorf("total read = %d, want %d", total, len(data))
	}
	if r.bytesRead != int64(len(data)) {
		t.Errorf("counter = %d, want %d", r.bytesRead, len(data))
	}
}

func TestTrackingReader_CancelMidRead(t *testing.T) {
	t.Parallel()

	data := make([]byte, 10*1024*1024) // 10 MB
	src := bytes.NewReader(data)

	ctx, cancel := context.WithCancel(context.Background())

	r := &trackingReader{
		r:          src,
		ctx:        ctx,
		checkEvery: 1024 * 1024, // check every 1 MB
	}

	buf := make([]byte, 64*1024) // 64 KB reads

	// Read 2 MB, then cancel.
	for r.bytesRead < 2*1024*1024 {
		_, err := r.Read(buf)
		if err != nil {
			t.Fatalf("unexpected error before cancel: %v", err)
		}
	}
	cancel()

	// Next read that crosses a check boundary should return context error.
	var hitCancel bool
	for range 200 { // at most 200 more reads
		_, err := r.Read(buf)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				hitCancel = true
				break
			}
			if err == io.EOF {
				break
			}
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if !hitCancel {
		t.Error("expected context.Canceled error after cancel, but read completed")
	}

	// Should have stopped well before reading all 10 MB.
	read := r.bytesRead
	if read >= int64(len(data)) {
		t.Errorf("read all %d bytes despite cancel, should have stopped early", read)
	}
}

func TestTrackingReader_NoCancelContext(t *testing.T) {
	t.Parallel()

	// Without a context, the reader should work normally (no cancel checks).
	data := make([]byte, 512*1024) // 512 KB
	src := bytes.NewReader(data)

	r := &trackingReader{
		r: src,
	}

	buf := make([]byte, 64*1024)
	for {
		_, err := r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	if r.bytesRead != int64(len(data)) {
		t.Errorf("counter = %d, want %d", r.bytesRead, len(data))
	}
}

func TestTrackingReader_SmallCheckEvery(t *testing.T) {
	t.Parallel()

	data := make([]byte, 256*1024) // 256 KB
	src := bytes.NewReader(data)

	ctx, cancel := context.WithCancel(context.Background())

	r := &trackingReader{
		r:          src,
		ctx:        ctx,
		checkEvery: 32 * 1024, // check every 32 KB
	}

	// Read 64 KB then cancel.
	buf := make([]byte, 16*1024)
	for r.bytesRead < 64*1024 {
		_, err := r.Read(buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	cancel()

	// Should stop within a few reads.
	var hitCancel bool
	for range 50 {
		_, err := r.Read(buf)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				hitCancel = true
			}
			break
		}
	}

	if !hitCancel {
		t.Error("expected cancel to propagate with small checkEvery")
	}
}
