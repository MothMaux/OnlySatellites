//go:build linux || darwin || freebsd || openbsd || netbsd

package handlers

import (
	"golang.org/x/sys/unix"
)

func diskTotalsForPath(path string) (total, free uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(st.Bsize)
	total = uint64(st.Blocks) * blockSize
	free = uint64(st.Bavail) * blockSize
	return total, free, nil
}
