//go:build !windows

package collect

import "io/fs"

// isReparsePoint is a Windows/NTFS concept; symlinks (already
// skipped by the walk) cover the unix case.
func isReparsePoint(path string, d fs.DirEntry) bool { return false }
