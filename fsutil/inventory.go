package fsutil

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WalkDCIM visits visible regular files. Analysis and planning share this
// boundary: hidden entries and symlinks are deliberate exclusions; an entry
// we couldn't enumerate or inspect makes the inventory incomplete.
func WalkDCIM(ctx context.Context, root *os.Root, visit func(path, rel string, info fs.FileInfo) error) error {
	const dcim = "DCIM"
	dir, err := root.Lstat(dcim)
	if err != nil {
		return err // preserve a missing DCIM for the app's target retry
	}
	if !dir.IsDir() {
		return fmt.Errorf("DCIM is not a directory: %s", root.Name())
	}
	return fs.WalkDir(root.FS(), dcim, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("incomplete DCIM inventory at %s: %w", path, err)
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
		native := filepath.FromSlash(path)
		info, err := root.Lstat(native)
		if err != nil {
			return fmt.Errorf("incomplete DCIM inventory at %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular source file: %s", path)
		}
		return visit(native, filepath.FromSlash(strings.TrimPrefix(path, dcim+"/")), info)
	})
}
