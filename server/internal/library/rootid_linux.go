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
	// Without a creation time the inode alone is not trusted: FUSE pools (mergerfs,
	// Unraid's shfs) number inodes by path, so another, empty folder at the same path can
	// carry the same device and inode. "" then: the emptied library waits for an admin.
	if st.Mask&unix.STATX_INO == 0 || st.Mask&unix.STATX_BTIME == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d:%d:%d:%d.%09d", st.Dev_major, st.Dev_minor, st.Ino, st.Btime.Sec, st.Btime.Nsec), nil
}
