// Package config loads MiniPulse's JSON configuration. Every field has
// a sane default so the binary runs with no config file at all.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// HealthCheckCfg is one user-configured HTTP endpoint to watch.
type HealthCheckCfg struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// ExpectStatus 0 means "any 2xx/3xx".
	ExpectStatus int `json:"expect_status,omitempty"`
}

type Config struct {
	// Addr to listen on. Loopback by default: expose through Tailscale
	// Serve or set a Token before binding to a LAN address.
	Addr string `json:"addr"`
	// Token required for the API when set. Leave empty on loopback.
	Token string `json:"token,omitempty"`

	SampleIntervalSeconds int    `json:"sample_interval_seconds"`
	RetentionDays         int    `json:"retention_days"`
	DataDir               string `json:"data_dir,omitempty"`

	// Per-process history (kept light: only the top consumers, on a
	// slow cadence — a full process walk every sample would cost more
	// CPU than the dashboard is worth).
	ProcessSampleIntervalSeconds int `json:"process_sample_interval_seconds"` // 0 disables
	ProcessRetentionDays         int `json:"process_retention_days"`
	ProcessTopN                  int `json:"process_top_n"` // per ranking (CPU and RAM merged)
	// Log an event when one process stays above this CPU % for two
	// consecutive process samples (100 = one full core). 0 disables.
	AlertProcessCPUPct float64 `json:"alert_process_cpu_pct"`

	// Energy cost estimation.
	EnergyPricePerKWh float64 `json:"energy_price_per_kwh"`
	Currency          string  `json:"currency"`
	// GridCO2PerKWh grams of CO2 per kWh, 0 hides CO2 figures.
	GridCO2PerKWh float64 `json:"grid_co2_per_kwh,omitempty"`

	// Disk deep-dive: roots to walk on a schedule.
	DiskScanRoots           []string `json:"disk_scan_roots"`
	DiskScanIntervalMinutes int      `json:"disk_scan_interval_minutes"`
	// Only store entries at least this big (plus shallow directories).
	DiskMinEntryBytes int64 `json:"disk_min_entry_bytes"`

	HealthChecks []HealthCheckCfg `json:"health_checks,omitempty"`

	// Alerts. Zero disables a rule.
	AlertDiskPct         float64 `json:"alert_disk_pct"`
	AlertCPUTempC        float64 `json:"alert_cpu_temp_c"`
	AlertMemPct          float64 `json:"alert_mem_pct"`
	AlertWatts           float64 `json:"alert_watts"`
	AlertWebhookURL      string  `json:"alert_webhook_url,omitempty"` // ntfy topic URL or any webhook
	AlertCooldownMinutes int     `json:"alert_cooldown_minutes"`
}

// Dir is the per-user config directory.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "minipulse")
}

// DefaultPath is where Load looks when no --config flag is given.
func DefaultPath() string { return filepath.Join(Dir(), "config.json") }

// DefaultDataDir is where the SQLite database lives by default.
func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "minipulse")
}

// Defaults returns a fully-populated default configuration.
func Defaults() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Addr:                         "127.0.0.1:8848",
		SampleIntervalSeconds:        2,
		RetentionDays:                30,
		ProcessSampleIntervalSeconds: 30,
		ProcessRetentionDays:         14,
		ProcessTopN:                  10,
		AlertProcessCPUPct:           200,
		DataDir:                      DefaultDataDir(),
		EnergyPricePerKWh:            0.15,
		Currency:                     "USD",
		DiskScanRoots:                []string{home},
		DiskScanIntervalMinutes:      60,
		DiskMinEntryBytes:            10 << 20, // 10 MiB
		AlertDiskPct:                 85,
		AlertCPUTempC:                95,
		AlertMemPct:                  92,
		AlertCooldownMinutes:         30,
	}
}

// Load reads path if it exists and overlays it on the defaults.
// A missing file is not an error.
func Load(path string) (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	for i, r := range cfg.DiskScanRoots {
		cfg.DiskScanRoots[i] = ExpandHome(r)
	}
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	cfg.DataDir = ExpandHome(cfg.DataDir)
	clampConfig(cfg)
	return cfg, nil
}

// clampConfig keeps hand-edited configs inside sane bounds. Several
// fields are destructive at the extremes — e.g. a retention of 0 (or
// negative) would make the daily prune delete the entire history,
// and an unbounded top_n would persist every process every 30 s.
func clampConfig(cfg *Config) {
	d := Defaults()
	if cfg.SampleIntervalSeconds < 1 {
		cfg.SampleIntervalSeconds = 1
	}
	if cfg.SampleIntervalSeconds > 86400 {
		cfg.SampleIntervalSeconds = 86400
	}
	if cfg.AlertCooldownMinutes < 1 {
		cfg.AlertCooldownMinutes = 1
	}
	if cfg.AlertCooldownMinutes > 10080 {
		cfg.AlertCooldownMinutes = 10080
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = d.RetentionDays
	}
	if cfg.RetentionDays > 3650 {
		cfg.RetentionDays = 3650
	}
	if cfg.ProcessRetentionDays <= 0 {
		cfg.ProcessRetentionDays = d.ProcessRetentionDays
	}
	if cfg.ProcessRetentionDays > 3650 {
		cfg.ProcessRetentionDays = 3650
	}
	if cfg.ProcessTopN <= 0 {
		cfg.ProcessTopN = d.ProcessTopN
	}
	if cfg.ProcessTopN > 100 {
		cfg.ProcessTopN = 100
	}
	switch {
	case cfg.ProcessSampleIntervalSeconds < 0:
		cfg.ProcessSampleIntervalSeconds = 0
	case cfg.ProcessSampleIntervalSeconds > 0 && cfg.ProcessSampleIntervalSeconds < 5:
		cfg.ProcessSampleIntervalSeconds = 5 // a full process walk isn't free
	}
	if cfg.DiskScanIntervalMinutes < 0 {
		cfg.DiskScanIntervalMinutes = 0
	}
	if cfg.DiskScanIntervalMinutes > 10080 {
		cfg.DiskScanIntervalMinutes = 10080
	}
	if cfg.ProcessSampleIntervalSeconds > 86400 {
		cfg.ProcessSampleIntervalSeconds = 86400
	}
	if cfg.EnergyPricePerKWh < 0 {
		cfg.EnergyPricePerKWh = 0
	}
	if cfg.AlertProcessCPUPct < 0 {
		cfg.AlertProcessCPUPct = 0
	}
	if cfg.DiskMinEntryBytes < 0 {
		cfg.DiskMinEntryBytes = 0
	}
}

// ExpandHome expands a leading "~/" (or bare "~") to the home directory.
func ExpandHome(p string) string {
	if p == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// IsLoopback reports whether the configured address only serves
// this machine. An empty host (":8848") binds ALL interfaces in Go,
// so it is explicitly NOT loopback — treating it as loopback would
// skip the token requirement while exposing the LAN.
func (c *Config) IsLoopback() bool {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// WriteExample writes a commented-by-field-name example config.
func WriteExample(path string) error {
	cfg := Defaults()
	cfg.HealthChecks = []HealthCheckCfg{{Name: "example", URL: "http://127.0.0.1:8080/health"}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// 0600: users put their API token in this file. Chmod too —
	// WriteFile's perm only applies when creating the file.
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
