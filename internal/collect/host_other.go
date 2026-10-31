//go:build !darwin && !windows

package collect

// Non-darwin/non-Windows counterparts of the host helpers in
// host_darwin.go. Empty on purpose: gopsutil's host.Info() covers
// hostname, OS and boot time there.

func platformModel() string { return "" }

func platformComputerName() string { return "" }

func platformOSVersion() string { return "" }

func platformBootTime() int64 { return 0 }
