package cblog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpen_CreatesFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.log")
	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	if _, err := os.Stat(path); err != nil {
		t.Error("log file should exist after Open")
	}
}

func TestOpen_CreatesParentDirs(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a", "b", "test.log")
	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	if _, err := os.Stat(path); err != nil {
		t.Error("log file should exist after Open")
	}
}

func TestPrintf_WritesTimestampedLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.log")
	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	logger.Printf("hello %s", "world")
	logger.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	if !strings.Contains(line, "hello world") {
		t.Errorf("log line missing message: %q", line)
	}
	if !strings.HasPrefix(line, "[") {
		t.Errorf("log line missing timestamp prefix: %q", line)
	}
}

func TestRaw_NoTimestamp(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.log")
	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	logger.Raw("raw message here")
	logger.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	if line != "raw message here" {
		t.Errorf("Raw() = %q, want exact 'raw message here'", line)
	}
}

func TestRotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	// Write lines until we've definitely exceeded maxSize.
	// Track approximate bytes to know when we've crossed the threshold.
	line := strings.Repeat("x", 200)
	for i := 0; i < 30000; i++ {
		logger.Printf("%s", line)
	}
	logger.Close()

	// After rotation, .old file should exist.
	oldPath := path + ".old"
	if _, err := os.Stat(oldPath); err != nil {
		t.Error("expected .old file after rotation")
	}

	// Current log should be smaller than maxSize (it was rotated).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= maxSize {
		t.Errorf("current log size %d should be < maxSize %d after rotation", info.Size(), maxSize)
	}
}

func TestWrittenTracksSizeAcrossRestart(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.log")

	// First session: write some data.
	logger1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	logger1.Printf("first session")
	logger1.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	size1 := info.Size()

	// Second session: Open should seed `written` from existing file size.
	logger2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	logger2.Printf("second session")
	logger2.Close()

	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= size1 {
		t.Error("second session should have appended to the file")
	}
}

func TestPrintfAndRaw_NoPanicWhenFileHandleNil(t *testing.T) {
	t.Parallel()
	logger := &Logger{path: filepath.Join(t.TempDir(), "test.log"), f: nil}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	logger.Printf("hello")
	logger.Raw("world")
}

func TestLogger_ReportsWriteFailure(t *testing.T) {
	t.Parallel()
	logger, err := Open(filepath.Join(t.TempDir(), "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	logger.mu.Lock()
	if err := logger.f.Close(); err != nil {
		t.Fatal(err)
	}
	logger.mu.Unlock()
	logger.Printf("cannot write")
	if logger.Err() == nil {
		t.Fatal("expected logger write error")
	}
}
