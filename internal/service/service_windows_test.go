package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPrepareServiceConfigGenerated(t *testing.T) {
	pd := t.TempDir()
	t.Setenv("ProgramData", pd)
	dst, err := prepareServiceConfig(filepath.Join(pd, "no-user-config.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	m := readConfig(t, dst)
	if got, want := m["data_dir"], filepath.Join(pd, "minipulse", "data"); got != want {
		t.Errorf("data_dir = %v, want %v (generated config must be pinned under ProgramData)", got, want)
	}
	roots, _ := m["disk_scan_roots"].([]any)
	if len(roots) != 1 || !strings.HasSuffix(roots[0].(string), "Users") {
		t.Errorf("disk_scan_roots = %v, want the Users root for a generated service config", roots)
	}
}

// A config found in %ProgramData% with no previous install is
// foreign (standard users can create directories there) and must
// be rewritten; a reinstall keeps the previous install's config.
func TestPrepareServiceConfigForeignVsReinstall(t *testing.T) {
	pd := t.TempDir()
	t.Setenv("ProgramData", pd)
	dir := filepath.Join(pd, "minipulse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(dir, "config.json")
	if err := os.WriteFile(planted, []byte(`{"token":"attacker-token-123456","addr":"127.0.0.1:9999"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dst, err := prepareServiceConfig(filepath.Join(pd, "none.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if m := readConfig(t, dst); m["token"] == "attacker-token-123456" {
		t.Fatal("pre-planted config was honored on a fresh install")
	}
	// The directory must end up admin-owned, or the pre-planter
	// keeps WRITE_DAC and can reopen it (requires an elevated
	// test process — CI runners and admin shells qualify).
	if !programDataOwnedByUs() {
		t.Error("ProgramData dir is not SYSTEM/Administrators-owned after prepare")
	}
	// Now it is our own config: a reinstall must keep it.
	if err := os.WriteFile(dst, []byte(`{"token":"ours-12345678901234"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareServiceConfig(filepath.Join(pd, "none.json"), true); err != nil {
		t.Fatal(err)
	}
	if m := readConfig(t, dst); m["token"] != "ours-12345678901234" {
		t.Errorf("reinstall rewrote the service config: %v", m)
	}
}
