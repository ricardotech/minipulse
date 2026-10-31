//go:build !darwin && !windows

package service

import "fmt"

func Install(binary, configPath string) error {
	return fmt.Errorf("service install: unsupported on this platform (use systemd manually)")
}

func Uninstall() error {
	return fmt.Errorf("service uninstall: unsupported on this platform")
}

func Status() (string, error) {
	return "", fmt.Errorf("service status: unsupported on this platform")
}

// Describe names the installed unit for CLI output.
func Describe() string { return "unsupported platform" }
