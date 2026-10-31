package collect

import (
	"os"
	"testing"
)

func TestLaunchctlPathExists(t *testing.T) {
	info, err := os.Stat(launchctlPath)
	if err != nil {
		t.Fatalf("launchctl path %s: %v", launchctlPath, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		t.Errorf("%s is not an executable file (mode %v)", launchctlPath, info.Mode())
	}
}
func TestParseLaunchctl(t *testing.T) {
	out := "PID\tStatus\tLabel\n123\t0\tcom.example.running\n-\t0\tcom.example.stopped\n"
	got := ParseLaunchctl(out)
	if len(got) != 2 || got[0].PID != 123 || got[1].PID != 0 || got[1].Label != "com.example.stopped" {
		t.Errorf("got %+v", got)
	}
}
func TestParseLsof(t *testing.T) {
	out := "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\n" +
		"postgres 1305 alice 7u IPv4 0xabc 0t0 TCP 127.0.0.1:5432 (LISTEN)\n" +
		"redis-ser 1297 alice 12u IPv6 0xdef 0t0 TCP [::1]:6379 (LISTEN)\n"
	got := ParseLsof(out)
	if len(got) != 2 || got[0].Port != 5432 || got[1].Port != 6379 || got[0].Process != "postgres" {
		t.Errorf("got %+v", got)
	}
}
