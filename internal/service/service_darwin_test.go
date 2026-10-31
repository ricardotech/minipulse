//go:build darwin

package service

import (
	"os"
	"testing"
)

// The absolute launchctl path is a security choice (never resolve
// system tools through PATH); a typo'd path would silently report
// "not loaded" forever, so assert it exists and is executable.
func TestLaunchctlPathExists(t *testing.T) {
	info, err := os.Stat(launchctl)
	if err != nil {
		t.Fatalf("launchctl path %s: %v", launchctl, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		t.Errorf("%s is not an executable file (mode %v)", launchctl, info.Mode())
	}
}
