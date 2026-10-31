// Package service installs MiniPulse as a launchd LaunchAgent so the
// dashboard starts at login and restarts if it crashes — the standard
// "always-on" mechanism on a Mac used as a server.
package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Label is the launchd job label.
const Label = "io.github.ricardotech.minipulse"

// Absolute path: system tools are never resolved through PATH.
const launchctl = "/bin/launchctl"

// PlistPath is where the LaunchAgent plist lives.
func PlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

func logPaths() (stdout, stderr string) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Logs", "minipulse")
	return filepath.Join(dir, "minipulse.log"), filepath.Join(dir, "minipulse.err.log")
}

// RenderPlist builds the LaunchAgent plist for the given binary/config.
func RenderPlist(binary, configPath string) string {
	stdout, stderr := logPaths()
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, Label, xmlEsc(binary), xmlEsc(configPath), xmlEsc(stdout), xmlEsc(stderr))
}

// xmlEsc escapes text interpolated into the plist so a path containing
// & or < cannot break (or inject into) the XML document.
func xmlEsc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Install writes the plist and (re)loads the job.
func Install(binary, configPath string) error {
	stdout, _ := logPaths()
	// Logs can name processes, roots and spike offenders: owner-only.
	if err := os.MkdirAll(filepath.Dir(stdout), 0o700); err != nil {
		return err
	}
	_ = os.Chmod(filepath.Dir(stdout), 0o700)
	plist := PlistPath()
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plist, []byte(RenderPlist(binary, configPath)), 0o644); err != nil {
		return err
	}
	_ = run(launchctl, "unload", plist) // ok if not loaded yet
	if err := run(launchctl, "load", "-w", plist); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}
	return nil
}

// Uninstall unloads the job and removes the plist.
func Uninstall() error {
	plist := PlistPath()
	_ = run(launchctl, "unload", plist)
	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Status prints launchctl's view of the job ("" when not loaded).
func Status() (string, error) {
	out, err := exec.Command(launchctl, "list", Label).CombinedOutput()
	if err != nil {
		return "", nil // not loaded
	}
	return string(out), nil
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, out)
	}
	return nil
}
