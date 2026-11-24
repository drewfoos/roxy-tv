//go:build linux

package ffmpeg

import "syscall"

// lowPriority makes a started process give way to everything else on the machine (nice
// 10). Its threads are made after this, by a thread that already has the value, and take
// it with them.
func lowPriority(pid int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 10)
}
