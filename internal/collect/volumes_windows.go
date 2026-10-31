package collect

import "os"

// rootMount is the boot volume's mountpoint, sorted first.
func rootMount() string {
	if d := os.Getenv("SystemDrive"); d != "" {
		return d + `\`
	}
	return `C:\`
}

// isRealMount: disk.Partitions(false) on Windows already yields only
// mounted fixed volumes (drive letters and mount points), which is
// exactly what the storage card wants.
func isRealMount(mount string) bool { return mount != "" }

// countDiskIO: IOCounters keys on Windows are physical-drive names;
// there is no slice/container double-counting like APFS.
func countDiskIO(name string) bool { return true }
