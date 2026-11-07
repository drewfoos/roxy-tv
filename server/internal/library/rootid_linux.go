package library

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// dirIdentity names the directory at path, not the path: its device, inode and (where the
// filesystem keeps it) creation time. A drive mounted again shows the same directory; a
// different drive, or the empty folder Docker makes for a missing bind mount, does not,
// even with the same inode number on a fresh drive (the creation time differs). "" when
// it cannot be told.
func dirIdentity(path string) (string, error) {
	var st unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_INO|unix.STATX_BTIME, &st); err != nil {
		return "", err
	}
	if st.Mask&unix.STATX_INO == 0 {
		return "", nil
	}
	id := fmt.Sprintf("%d:%d:%d", st.Dev_major, st.Dev_minor, st.Ino)
	if st.Mask&unix.STATX_BTIME != 0 {
		id += fmt.Sprintf(":%d.%09d", st.Btime.Sec, st.Btime.Nsec)
	}
	return id, nil
}
