package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrSourceChanged means the mount path no longer identifies the card acquired
// for this operation. It is distinct from a write error on the correct card.
var ErrSourceChanged = errors.New("source card changed or is unavailable")

// Source identifies a card between phases without holding a mount open while
// the app waits for input. Each I/O phase opens and closes its own pinned root.
type Source struct {
	path string
	info os.FileInfo
}

func IdentifySource(path string) (*Source, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source is not a directory: %s", path)
	}
	return &Source{path: path, info: info}, nil
}

func (s *Source) Path() string { return s.path }

// Check detects a removal/replacement. A check followed by pathname I/O is
// not a pin; use Open for the actual reads, writes and rename.
func (s *Source) Check() error {
	info, err := os.Stat(s.path)
	if err != nil || !os.SameFile(s.info, info) {
		return fmt.Errorf("%w: %s", ErrSourceChanged, s.path)
	}
	return nil
}

func (s *Source) Open() (*os.Root, error) {
	root, err := os.OpenRoot(s.path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(s.info, info) {
		root.Close()
		return nil, fmt.Errorf("%w: %s", ErrSourceChanged, s.path)
	}
	return root, nil
}
