//go:build !darwin && !linux

package daemon

import (
	"errors"
	"os"
)

var errProcessLocked = errors.New("process lock is held")

type processLock struct {
	f    *os.File
	path string
}

func acquireProcessLock(path string) (*processLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errProcessLocked
		}
		return nil, err
	}
	return &processLock{f: f, path: path}, nil
}

func (l *processLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	_ = os.Remove(l.path)
	l.f = nil
	return err
}
