package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8848":   true,
		"[::1]:8848":       true,
		"localhost:8848":   true,
		":8848":            false, // binds ALL interfaces in Go — must require a token
		"0.0.0.0:8848":     false,
		"192.168.1.5:8848": false,
		"100.64.0.1:8848":  false, // CGNAT/Tailscale-range IP is reachable remotely, not loopback
		"garbage":          false,
		"":                 false,
	}
	for addr, want := range cases {
		cfg := Defaults()
		cfg.Addr = addr
		if got := cfg.IsLoopback(); got != want {
			t.Errorf("IsLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestLoadClampsDestructiveZeros(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"retention_days": 0,
		"process_retention_days": -3,
		"process_top_n": 100000,
		"process_sample_interval_seconds": 1
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Defaults()
	if cfg.RetentionDays != d.RetentionDays {
		t.Errorf("RetentionDays = %d, want default %d (0 would prune everything)", cfg.RetentionDays, d.RetentionDays)
	}
	if cfg.ProcessRetentionDays != d.ProcessRetentionDays {
		t.Errorf("ProcessRetentionDays = %d, want default %d", cfg.ProcessRetentionDays, d.ProcessRetentionDays)
	}
	if cfg.ProcessTopN != 100 {
		t.Errorf("ProcessTopN = %d, want clamped 100", cfg.ProcessTopN)
	}
	if cfg.ProcessSampleIntervalSeconds != 5 {
		t.Errorf("ProcessSampleIntervalSeconds = %d, want floor 5", cfg.ProcessSampleIntervalSeconds)
	}
}

func TestWriteExampleIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		// POSIX mode bits don't exist on NTFS (Go reports 0666);
		// access there is governed by the per-user profile ACL.
		t.Skip("posix permissions are not meaningful on windows")
	}
	path := filepath.Join(t.TempDir(), "minipulse", "config.json")
	if err := WriteExample(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("example config perm = %o, want 600 (it will hold the API token)", perm)
	}
}
