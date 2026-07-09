//go:build darwin

package cardcopy

import "golang.org/x/sys/unix"

func commitNoReplace(src, dst string) error {
	return unix.RenamexNp(src, dst, unix.RENAME_EXCL)
}
