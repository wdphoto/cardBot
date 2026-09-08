package cardcopy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func verifyTestFiles(src, dst string, buf []byte) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := os.Open(dst)
	if err != nil {
		return err
	}
	defer df.Close()
	return verifyBytes(context.Background(), sf, df, buf)
}

func TestPlanCopy_RejectsDestinationSymlinkEscape(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		name := "missing file"
		if existing {
			name = "existing size match"
		}
		t.Run(name, func(t *testing.T) {
			card := createTestCard(t, map[string]testFileSpec{
				"IMG.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
			})
			dest, outside := t.TempDir(), t.TempDir()
			if existing {
				if err := os.WriteFile(filepath.Join(outside, "IMG.JPG"), []byte("photo"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, filepath.Join(dest, "2026-03-08")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			_, err := PlanCopy(context.Background(), Options{CardPath: card, DestBase: dest})
			if err == nil {
				t.Fatal("planning accepted an escaping destination symlink")
			}
			entries, err := os.ReadDir(dest)
			if err != nil || len(entries) != 1 {
				t.Fatalf("planning changed destination: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestExecute_RejectsDestinationSymlinkSwap(t *testing.T) {
	t.Parallel()
	for _, skipped := range []bool{false, true} {
		name := "copy"
		if skipped {
			name = "skip"
		}
		t.Run(name, func(t *testing.T) {
			card := createTestCard(t, map[string]testFileSpec{
				"IMG.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
			})
			dest, outside := t.TempDir(), t.TempDir()
			opts := Options{CardPath: card, DestBase: dest}
			if skipped {
				if _, err := Run(context.Background(), opts, nil); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := PlanCopy(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			day := filepath.Join(dest, "2026-03-08")
			if skipped {
				if err := os.Rename(day, filepath.Join(dest, "original")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, day); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			result, err := Execute(context.Background(), plan, nil)
			if err == nil {
				t.Fatal("execution accepted an escaping destination symlink")
			}
			if result != nil && (result.FilesCopied != 0 || result.FilesSkipped != 0) {
				t.Fatalf("reported success through escaped path: %+v", result)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("wrote outside destination: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestExecute_RejectsDestinationBaseReplacement(t *testing.T) {
	t.Parallel()
	for _, initiallyExists := range []bool{false, true} {
		name := "initially missing"
		if initiallyExists {
			name = "initially present"
		}
		t.Run(name, func(t *testing.T) {
			card := createTestCard(t, map[string]testFileSpec{
				"IMG.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
			})
			parent, outside := t.TempDir(), t.TempDir()
			base := filepath.Join(parent, "destination")
			dest := base
			if initiallyExists {
				if err := os.Mkdir(base, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				// Exercise a missing intermediate directory as well as the base.
				dest = filepath.Join(base, "nested")
			}
			plan, err := PlanCopy(context.Background(), Options{CardPath: card, DestBase: dest})
			if err != nil {
				t.Fatal(err)
			}
			if initiallyExists {
				if err := os.Rename(base, filepath.Join(parent, "original")); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(base); !os.IsNotExist(err) {
				t.Fatalf("planning created the destination: %v", err)
			}
			if err := os.Symlink(outside, base); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := Execute(context.Background(), plan, nil); err == nil {
				t.Fatal("execution followed a replaced destination base")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("execution wrote outside: entries=%v err=%v", entries, err)
			}
		})
	}
}

// Rewinding marks the exact boundary between copying and full verification.
// Tests can inject a source change/cancellation without racing filesystem I/O.
type rewindReader struct {
	*bytes.Reader
	onRewind func()
}

func (r *rewindReader) Seek(offset int64, whence int) (int64, error) {
	if r.onRewind != nil {
		r.onRewind()
	}
	return r.Reader.Seek(offset, whence)
}

func TestCopyToDir_VerificationFailureNeverPublishes(t *testing.T) {
	t.Parallel()
	for _, cancelVerify := range []bool{false, true} {
		name := "mismatch"
		if cancelVerify {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			root, err := os.OpenRoot(dest)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			data := []byte("original photograph")
			src := &rewindReader{Reader: bytes.NewReader(data), onRewind: func() {
				if cancelVerify {
					cancel()
				} else {
					data[0] = '!'
				}
			}}
			err = copyToDir(ctx, root, "IMG.JPG", src, int64(len(data)), make([]byte, 8192), new(atomic.Int64), true)
			if err == nil {
				t.Fatal("expected verification failure")
			}
			if cancelVerify && !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if !cancelVerify && !strings.Contains(err.Error(), "content mismatch") {
				t.Fatalf("error = %v, want content mismatch", err)
			}
			entries, err := os.ReadDir(dest)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed verification left files: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestCopyToDir_PinsParentThroughCommit(t *testing.T) {
	t.Parallel()
	dest, outside := t.TempDir(), t.TempDir()
	parent := filepath.Join(dest, "day")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	moved := filepath.Join(dest, "original-day")
	src := &rewindReader{Reader: bytes.NewReader([]byte("photo")), onRewind: func() {
		if err := os.Rename(parent, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, parent); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}}
	if err := copyToDir(context.Background(), root, "IMG.JPG", src, 5, make([]byte, 8192), new(atomic.Int64), true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(moved, "IMG.JPG"))
	if err != nil || string(got) != "photo" {
		t.Fatalf("commit did not use pinned parent: data=%q err=%v", got, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("commit escaped to replacement symlink: entries=%v err=%v", entries, err)
	}
}

type verifyReadFunc func([]byte) (int, error)

func (f verifyReadFunc) Read(p []byte) (int, error) { return f(p) }

func TestVerifyBytes_CancellationBetweenReads(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := verifyReadFunc(func(p []byte) (int, error) {
		cancel()
		return copy(p, "photo"), io.EOF
	})
	dst := verifyReadFunc(func(p []byte) (int, error) {
		t.Fatal("destination read started after cancellation")
		return 0, io.EOF
	})
	if err := verifyBytes(ctx, src, dst, make([]byte, 8192)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestExecute_RevalidatesFullVerificationSkips(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"IMG.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
	})
	opts := Options{CardPath: card, DestBase: t.TempDir(), VerifyMode: "full"}
	if _, err := Run(context.Background(), opts, nil); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanCopy(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Files[0].DestPath, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(context.Background(), plan, nil)
	if !errors.Is(err, ErrDestinationConflict) || result.FilesSkipped != 0 {
		t.Fatalf("result=%+v error=%v, want conflict without skip", result, err)
	}
}

func TestExecute_RevalidatesSizeSkips(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"removed", "resized", "symlink"} {
		t.Run(change, func(t *testing.T) {
			card := createTestCard(t, map[string]testFileSpec{
				"IMG.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
			})
			opts := Options{CardPath: card, DestBase: t.TempDir()}
			if _, err := Run(context.Background(), opts, nil); err != nil {
				t.Fatal(err)
			}
			plan, err := PlanCopy(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			dst := plan.Files[0].DestPath
			switch change {
			case "resized":
				err = os.WriteFile(dst, []byte("changed size"), 0600)
			case "removed", "symlink":
				err = os.Remove(dst)
				if err == nil && change == "symlink" {
					err = os.Symlink(filepath.Join(card, "DCIM", "IMG.JPG"), dst)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := Execute(context.Background(), plan, nil)
			if !errors.Is(err, ErrDestinationConflict) || result.FilesSkipped != 0 || result.FilesCopied != 0 {
				t.Fatalf("result=%+v error=%v, want conflict without success", result, err)
			}
		})
	}
}

func TestCopy_PreservesPreexistingPartial(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"IMG.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
	})
	plan, err := PlanCopy(context.Background(), Options{CardPath: card, DestBase: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	part := plan.Files[0].DestPath + ".part"
	if err := os.MkdirAll(filepath.Dir(part), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, []byte("another copy owns this"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), plan, nil); !errors.Is(err, os.ErrExist) {
		t.Fatalf("error = %v, want existing partial error", err)
	}
	got, err := os.ReadFile(part)
	if err != nil || string(got) != "another copy owns this" {
		t.Fatalf("existing partial changed: data=%q err=%v", got, err)
	}
}

func TestExecute_ConcurrentPlansNeverOverwrite(t *testing.T) {
	t.Parallel()
	card := createTestCard(t, map[string]testFileSpec{
		"IMG.JPG": {data: []byte("photo"), mtime: date(2026, 3, 8)},
	})
	plan, err := PlanCopy(context.Background(), Options{CardPath: card, DestBase: t.TempDir(), VerifyMode: "full"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int64
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := Execute(context.Background(), plan, nil); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, os.ErrExist) && !errors.Is(err, ErrDestinationConflict) {
				t.Errorf("unexpected copy failure: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful copies = %d, want exactly one", successes.Load())
	}
	got, err := os.ReadFile(plan.Files[0].DestPath)
	if err != nil || string(got) != "photo" {
		t.Fatalf("destination changed: data=%q err=%v", got, err)
	}
	if _, err := os.Stat(plan.Files[0].DestPath + ".part"); !os.IsNotExist(err) {
		t.Fatalf("partial left behind: %v", err)
	}
}
