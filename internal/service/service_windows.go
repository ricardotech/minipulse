package service

import "fmt"

// Windows Service implementation lands in the service slice; stubs
// for now so the platform compiles.

func Install(binary, configPath string) error {
	return fmt.Errorf("service install: not implemented on Windows yet")
}

func Uninstall() error {
	return fmt.Errorf("service uninstall: not implemented on Windows yet")
}

func Status() (string, error) { return "", nil }

// Describe names the installed unit for CLI output.
func Describe() string { return "Windows service (pending implementation)" }
