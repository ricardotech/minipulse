package collect

import (
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

// platformModel returns the hardware model identifier, e.g. "Mac16,10".
func platformModel() string {
	s, err := unix.Sysctl("hw.model")
	if err != nil {
		return ""
	}
	return s
}

// platformComputerName returns the friendly name ("Ricardo's Mac mini").
// kern.hostname on a home network is often an IP or a DHCP lease name,
// which makes a poor dashboard title. Absolute path: launchd's PATH
// does not include /usr/sbin.
func platformComputerName() string {
	out, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// platformOSVersion returns e.g. "macOS 27.2" from kern.osproductversion.
func platformOSVersion() string {
	v, err := unix.Sysctl("kern.osproductversion")
	if err != nil || v == "" {
		return ""
	}
	return "macOS " + v
}

// platformBootTime returns the boot time as Unix seconds (kern.boottime),
// read natively instead of via gopsutil (which was observed to fail
// wholesale under launchd).
func platformBootTime() int64 {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return 0
	}
	return tv.Sec
}
