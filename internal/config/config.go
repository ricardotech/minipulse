// Package config loads MiniPulse's JSON configuration. Every field has
// a sane default so the binary runs with no config file at all.
package config

import (
	"encoding/json"
	"fmt"
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
		Addr:                    "127.0.0.1:8848",
		SampleIntervalSeconds:   2,
		RetentionDays:           30,
		DataDir:                 DefaultDataDir(),
		EnergyPricePerKWh:       0.15,
		Currency:                "USD",
		DiskScanRoots:           []string{home},
		DiskScanIntervalMinutes: 60,
		DiskMinEntryBytes:       10 << 20, // 10 MiB
		AlertDiskPct:            85,
		AlertCPUTempC:           95,
		AlertMemPct:             92,
		AlertCooldownMinutes:    30,
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
	if cfg.SampleIntervalSeconds < 1 {
		cfg.SampleIntervalSeconds = 1
	}
	if cfg.AlertCooldownMinutes < 1 {
		cfg.AlertCooldownMinutes = 1
	}
	return cfg, nil
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

// IsLoopback reports whether the configured address only serves this machine.
func (c *Config) IsLoopback() bool {
	host := c.Addr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "" || host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// WriteExample writes a commented-by-field-name example config.
func WriteExample(path string) error {
	cfg := Defaults()
	cfg.HealthChecks = []HealthCheckCfg{{Name: "example", URL: "http://127.0.0.1:8080/health"}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
