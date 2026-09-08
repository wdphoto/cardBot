//go:build linux

package cardcopy

import (
	"os"

	"golang.org/x/sys/unix"
)

func commitNoReplace(root *os.Root, src, dst string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.Renameat2(int(dir.Fd()), src, int(dir.Fd()), dst, unix.RENAME_NOREPLACE)
}
