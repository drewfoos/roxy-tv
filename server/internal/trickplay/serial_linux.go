//go:build linux

package trickplay

import (
	"fmt"
	"os"
	"syscall"
)

// fileSerial is a file's device, number and change time (see fileMark).
func fileSerial(fi os.FileInfo) string {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d.%d", uint64(st.Dev), uint64(st.Ino), int64(st.Ctim.Sec), int64(st.Ctim.Nsec))
}
