package fsutil

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WalkDCIM visits visible regular files. Analysis and planning share this
// boundary: hidden entries and symlinks are deliberate exclusions; an entry
// we couldn't enumerate or inspect makes the inventory incomplete.
func WalkDCIM(ctx context.Context, dcim string, visit func(path, rel string, info fs.FileInfo) error) error {
	return filepath.WalkDir(dcim, func(path string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if path == dcim && os.IsNotExist(err) {
				return err // the app distinguishes a missing DCIM from a failed scan
			}
			return fmt.Errorf("incomplete DCIM inventory at %s: %w", path, err)
		}
		if path == dcim && !d.IsDir() {
			return fmt.Errorf("DCIM is not a directory: %s", path)
		}
		if IsHidden(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("incomplete DCIM inventory at %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular source file: %s", path)
		}
		rel, err := filepath.Rel(dcim, path)
		if err != nil {
			return err
		}
		return visit(path, rel, info)
	})
}
