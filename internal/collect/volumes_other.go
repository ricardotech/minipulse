//go:build !darwin && !windows

package collect

// rootMount is the boot volume's mountpoint, sorted first.
func rootMount() string { return "/" }

// isRealMount accepts every mountpoint gopsutil reports; no
// platform-specific pseudo-volume filtering yet.
func isRealMount(mount string) bool { return mount != "" }

// countDiskIO counts every disk.IOCounters entry.
func countDiskIO(name string) bool { return true }
