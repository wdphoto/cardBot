package cardcopy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

type faultWriter func([]byte) (int, error)

func (w faultWriter) Write(p []byte) (int, error) { return w(p) }

func TestCopyStream_WriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name              string
		writeErr, wantErr error
	}{
		{"short write", nil, io.ErrShortWrite},
		{"disk full", syscall.ENOSPC, syscall.ENOSPC},
		{"closed destination", os.ErrClosed, os.ErrClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var counter atomic.Int64
			writer := faultWriter(func(p []byte) (int, error) { return min(2, len(p)), tc.writeErr })
			n, err := copyStream(context.Background(), writer, strings.NewReader("photo"), make([]byte, 32), &counter)
			if n != 2 || !errors.Is(err, tc.wantErr) {
				t.Fatalf("copyStream = %d, %v; want 2, %v", n, err, tc.wantErr)
			}
			// Progress counts source reads, not committed/successful bytes.
			if counter.Load() != 5 {
				t.Fatalf("bytes read = %d, want 5", counter.Load())
			}
		})
	}
}

type faultSource struct {
	*bytes.Reader
	readErr, seekErr error
	onRead           func()
}

func (r *faultSource) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if r.onRead != nil {
		r.onRead()
	}
	if r.readErr != nil {
		err = r.readErr
	}
	return n, err
}
func (r *faultSource) Seek(offset int64, whence int) (int64, error) {
	if r.seekErr != nil {
		return 0, r.seekErr
	}
	return r.Reader.Seek(offset, whence)
}

func TestCopyToDir_FaultCleanup(t *testing.T) {
	for _, name := range []string{"source error", "short source", "grown source", "seek error", "cancel during copy", "late conflict"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dest := t.TempDir()
			root, err := os.OpenRoot(dest)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			const sentinel = "another ingest owns this partial"
			if err := root.WriteFile("unrelated.NEF.part", []byte(sentinel), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			src := &faultSource{Reader: bytes.NewReader([]byte("photo"))}
			var reader io.ReadSeeker = src
			size := int64(5)
			var wantErr error
			switch name {
			case "source error":
				src.readErr, wantErr = syscall.EIO, syscall.EIO
			case "short source":
				size = 6
			case "grown source":
				size = 4
			case "seek error":
				src.seekErr, wantErr = syscall.EIO, syscall.EIO
			case "cancel during copy":
				src.onRead, wantErr = cancel, context.Canceled
			case "late conflict":
				wantErr = ErrDestinationConflict
				reader = &rewindReader{Reader: src.Reader, onRewind: func() {
					// A competing publisher wins after copying but before commit.
					if err := root.WriteFile("IMG.NEF", []byte("owner"), 0o600); err != nil {
						t.Fatal(err)
					}
				}}
			}
			err = copyToDir(ctx, root, "IMG.NEF", reader, size, make([]byte, 8192), new(atomic.Int64), true)
			if err == nil || (wantErr != nil && !errors.Is(err, wantErr)) {
				t.Fatalf("copy error = %v, want %v", err, wantErr)
			}
			if wantErr == nil && !strings.Contains(err.Error(), "size mismatch") {
				t.Fatalf("expected size mismatch, got %v", err)
			}
			got, err := root.ReadFile("unrelated.NEF.part")
			if err != nil || string(got) != sentinel {
				t.Fatalf("unrelated partial changed: %q, %v", got, err)
			}
			wantEntries := 1
			if name == "late conflict" {
				wantEntries++
				got, err := root.ReadFile("IMG.NEF")
				if err != nil || string(got) != "owner" {
					t.Fatalf("competing destination changed: %q, %v", got, err)
				}
			}
			entries, err := os.ReadDir(dest)
			if err != nil || len(entries) != wantEntries {
				t.Fatalf("failure left files: %v, %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(dest, "IMG.NEF.part")); !os.IsNotExist(err) {
				t.Fatalf("own partial remains: %v", err)
			}
		})
	}
}

func TestVerifyBytes_ReadErrorsCannotPass(t *testing.T) {
	for _, sourceFault := range []bool{false, true} {
		name := "destination"
		if sourceFault {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			good := strings.NewReader("photo")
			bad := &faultSource{Reader: bytes.NewReader([]byte("photo")), readErr: syscall.EIO}
			var src, dst io.Reader = good, bad
			if sourceFault {
				src, dst = bad, good
			}
			if err := verifyBytes(context.Background(), src, dst, nil); !errors.Is(err, syscall.EIO) {
				t.Fatalf("read failure accepted or lost: %v", err)
			}
		})
	}
}
