//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// On macOS/Linux the historic XDG-style locations are kept exactly
// so existing installs don't move.
func platformConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "minipulse")
}

func platformDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "minipulse")
}
