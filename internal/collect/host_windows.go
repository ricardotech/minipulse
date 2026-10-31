package collect

// Windows counterparts of the darwin host helpers in host_darwin.go.
// gopsutil's host.Info() already provides hostname, OS and boot time
// on Windows, so these are deliberately empty: the darwin versions
// exist only because host.Info() can fail wholesale under launchd.

func platformModel() string { return "" }

func platformComputerName() string { return "" }

func platformOSVersion() string { return "" }

func platformBootTime() int64 { return 0 }
