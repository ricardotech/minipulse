package config

import (
	"os"
	"path/filepath"
)

// Windows locations follow the platform conventions: roaming
// (%APPDATA%) for the small config file, local (%LOCALAPPDATA%) for
// the ever-growing SQLite database. The Windows Service installs
// with an explicit --config under %ProgramData% instead (a
// LocalSystem service has no useful per-user profile).
func platformConfigDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "minipulse")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "minipulse")
}

func platformDataDir() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "minipulse")
	}
	return platformConfigDir()
}
