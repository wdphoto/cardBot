//go:build darwin || linux

package daemon

import (
	"errors"
	"os"
	"syscall"
)

var errProcessLocked = errors.New("process lock is held")

type processLock struct{ f *os.File }

func acquireProcessLock(path string) (*processLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errProcessLocked
		}
		return nil, err
	}
	return &processLock{f: f}, nil
}

func (l *processLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
