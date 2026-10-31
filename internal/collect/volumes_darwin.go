package collect

import "strings"

// rootMount is the boot volume's mountpoint, sorted first.
func rootMount() string { return "/" }

// isRealMount filters APFS system slices that would double-count "/".
func isRealMount(mount string) bool {
	if mount == "/" {
		return true
	}
	if strings.HasPrefix(mount, "/Volumes/") {
		return mount != "/Volumes/Recovery"
	}
	return false // /System/Volumes/* etc. share the root container
}

// countDiskIO reports whether a disk.IOCounters entry is a whole
// disk (disk0, disk4…) rather than an APFS slice (disk3s1) that
// would double-count its container's I/O.
func countDiskIO(name string) bool {
	return !strings.Contains(name, "s") || isWholeDisk(name)
}

func isWholeDisk(name string) bool {
	// "disk0" yes, "disk3s1" no.
	for _, r := range strings.TrimPrefix(name, "disk") {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
