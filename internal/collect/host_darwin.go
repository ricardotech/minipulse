package collect

import (
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

// macModel returns the hardware model identifier, e.g. "Mac16,10".
func macModel() string {
	s, err := unix.Sysctl("hw.model")
	if err != nil {
		return ""
	}
	return s
}

// macComputerName returns the friendly name ("Ricardo's Mac mini").
// kern.hostname on a home network is often an IP or a DHCP lease name,
// which makes a poor dashboard title. Absolute path: launchd's PATH
// does not include /usr/sbin.
func macComputerName() string {
	out, err := exec.Command("/usr/sbin/scutil", "--get", "ComputerName").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// macOSVersion returns e.g. "macOS 27.2" from kern.osproductversion.
func macOSVersion() string {
	v, err := unix.Sysctl("kern.osproductversion")
	if err != nil || v == "" {
		return ""
	}
	return "macOS " + v
}

// macBootTime returns the boot time as Unix seconds (kern.boottime),
// read natively instead of via gopsutil (which was observed to fail
// wholesale under launchd).
func macBootTime() int64 {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return 0
	}
	return tv.Sec
}
