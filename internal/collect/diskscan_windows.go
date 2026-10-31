package collect

import (
	"io/fs"
	"syscall"
)

// isReparsePoint reports NTFS reparse points (junctions, mount
// points). filepath.WalkDir never follows symlinks, but a junction
// *directory* can be descended into — the classic
// "Application Data" junction would loop the walk and double-count.
// Reparse dirs are skipped wholesale, exactly like symlinked dirs.
func isReparsePoint(path string, d fs.DirEntry) bool {
	info, err := d.Info()
	if err != nil {
		return false
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
