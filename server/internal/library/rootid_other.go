//go:build !linux

package library

// dirIdentity is not known off Linux (the server runs on Linux): an emptied library's
// records are then only removed once an admin confirms it (Library.AcceptEmpty).
func dirIdentity(path string) (string, error) { return "", nil }
