// Package cblog provides simple file-based logging with size-based rotation.
package cblog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxSize = 5 * 1024 * 1024 // 5 MB

// Logger writes log lines to a file, rotating when it exceeds maxSize.
type Logger struct {
	mu      sync.Mutex
	path    string
	f       *os.File
	written int64 // bytes written since open, avoids Stat() on every line
	lastErr error // most recent persistent write/rotation error
}

// Open opens (or creates) the log file at path.
func Open(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}
	// Seed written from current file size so rotation works on restart.
	var written int64
	if info, err := f.Stat(); err == nil {
		written = info.Size()
	}
	return &Logger{path: path, f: f, written: written}, nil
}

// Printf writes a formatted log line with a timestamp prefix.
func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	line := fmt.Sprintf("[%s] %s\n",
		time.Now().Format("2006-01-02T15:04:05"),
		fmt.Sprintf(format, args...))

	l.writeLineLocked(line)
}

// Raw writes a pre-formatted line to the log (no timestamp added).
func (l *Logger) Raw(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writeLineLocked(line + "\n")
}

// Close flushes and closes the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var closeErr error
	if l.f != nil {
		closeErr = l.f.Close()
		l.f = nil
	}
	return errors.Join(l.lastErr, closeErr)
}

// Err returns the most recent logging error, if any.
func (l *Logger) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastErr
}

// writeLineLocked writes a single line while holding l.mu.
func (l *Logger) writeLineLocked(line string) {
	if l.f == nil {
		return
	}

	if l.written >= maxSize {
		l.rotate()
		if l.f == nil {
			return
		}
	}

	n, err := l.f.WriteString(line)
	if err != nil {
		l.lastErr = err
		return
	}
	l.written += int64(n)
}

// rotate renames the current log to .old and opens a fresh file.
func (l *Logger) rotate() {
	if l.f != nil {
		if err := l.f.Close(); err != nil {
			l.lastErr = err
		}
	}
	if err := os.Rename(l.path, l.path+".old"); err != nil && !os.IsNotExist(err) {
		l.lastErr = err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		l.lastErr = err
		l.f = nil
		return
	}
	l.f = f
	l.written = 0
}
