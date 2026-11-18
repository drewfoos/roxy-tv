//go:build !linux

package trickplay

import "os"

// fileSerial has nothing to add off Linux (the server runs on Linux): fileMark is
// size and time there.
func fileSerial(os.FileInfo) string { return "" }
