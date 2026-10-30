// Package model holds the shared data types passed between collectors,
// the store and the HTTP API. It has no dependencies on the other
// internal packages so everything can import it safely.
package model

import "time"

// Power is one Apple Silicon power/thermal reading. Values come from
// macmon (https://github.com/vladkens/macmon) when it is installed;
// the same private IOReport data powermetrics exposes, without sudo.
type Power struct {
	Available bool    `json:"available"`
	Reason    string  `json:"reason,omitempty"`
	SysW      float64 `json:"sys_w"` // whole-system power draw, watts
	SoCW      float64 `json:"soc_w"` // CPU+GPU+ANE+RAM, watts
	CPUW      float64 `json:"cpu_w"`
	GPUW      float64 `json:"gpu_w"`
	ANEW      float64 `json:"ane_w"`
	RAMW      float64 `json:"ram_w"`
	CPUTempC  float64 `json:"cpu_temp_c"`
	GPUTempC  float64 `json:"gpu_temp_c"`
	FanRPM    int     `json:"fan_rpm"`
	CPUTiers  []Tier  `json:"cpu_tiers,omitempty"`
}

// Tier is one CPU cluster (E, P, and S on newer chips).
type Tier struct {
	Label       string  `json:"label"`
	ActiveRatio float64 `json:"active_ratio"`
	FreqMHz     int     `json:"freq_mhz"`
	Cores       []Core  `json:"cores"`
}

type Core struct {
	ID          int     `json:"id"`
	ActiveRatio float64 `json:"active_ratio"`
	FreqMHz     int     `json:"freq_mhz"`
}

// Volume is one mounted filesystem.
type Volume struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	FSType  string  `json:"fs_type"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	UsedPct float64 `json:"used_pct"`
}

// Sample is one point-in-time system reading, persisted to SQLite and
// streamed to the dashboard.
type Sample struct {
	Time        time.Time `json:"time"`
	CPUPct      float64   `json:"cpu_pct"`
	CPUPerCore  []float64 `json:"cpu_per_core,omitempty"`
	MemUsed     uint64    `json:"mem_used"`
	MemTotal    uint64    `json:"mem_total"`
	MemPct      float64   `json:"mem_pct"`
	SwapUsed    uint64    `json:"swap_used"`
	SwapTotal   uint64    `json:"swap_total"`
	Load1       float64   `json:"load1"`
	Load5       float64   `json:"load5"`
	Load15      float64   `json:"load15"`
	NetRxBps    float64   `json:"net_rx_bps"`
	NetTxBps    float64   `json:"net_tx_bps"`
	DiskReadBps float64   `json:"disk_read_bps"`
	DiskWritBps float64   `json:"disk_write_bps"`
	WhTotal     float64   `json:"wh_total"` // cumulative watt-hours since first run
	Power       Power     `json:"power"`
	Volumes     []Volume  `json:"volumes,omitempty"`
}

// Process is one row of the top-processes table.
type Process struct {
	PID     int32   `json:"pid"`
	Name    string  `json:"name"`
	CPUPct  float64 `json:"cpu_pct"`
	MemPct  float32 `json:"mem_pct"`
	RSS     uint64  `json:"rss"`
	User    string  `json:"user,omitempty"`
	Command string  `json:"command,omitempty"`
}

// DiskEntry is one directory or file in a disk scan snapshot.
type DiskEntry struct {
	Path   string `json:"path"`
	Parent string `json:"parent"`
	Name   string `json:"name"`
	IsDir  bool   `json:"is_dir"`
	Size   int64  `json:"size"`
}

// DiskScan summarises one completed walk of a root directory.
type DiskScan struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	Root       string    `json:"root"`
	DurationMs int64     `json:"duration_ms"`
	TotalBytes int64     `json:"total_bytes"`
	FileCount  int64     `json:"file_count"`
	ErrCount   int64     `json:"err_count"`
}

// Growth is the size delta of one path between two scans.
type Growth struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Was   int64  `json:"was"`
	Now   int64  `json:"now"`
	Delta int64  `json:"delta"`
}

// Container is one Docker container (Docker Desktop, OrbStack or Colima).
type Container struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Image   string  `json:"image"`
	State   string  `json:"state"`
	Status  string  `json:"status"`
	CPUPct  float64 `json:"cpu_pct"`
	MemUsed uint64  `json:"mem_used"`
	MemMax  uint64  `json:"mem_max"`
}

// Service is one launchd job in the user domain.
type Service struct {
	Label  string `json:"label"`
	PID    int    `json:"pid"` // 0 when not running
	Status int    `json:"status"`
}

// Listener is one TCP port in LISTEN state.
type Listener struct {
	Process string `json:"process"`
	PID     int    `json:"pid"`
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
}

// HealthCheck is the live result of one configured HTTP check.
type HealthCheck struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Up        bool      `json:"up"`
	Status    int       `json:"status"`
	LatencyMs int64     `json:"latency_ms"`
	Checked   time.Time `json:"checked"`
	Error     string    `json:"error,omitempty"`
}

// Event is one alert/incident log line.
type Event struct {
	ID     int64     `json:"id"`
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"` // alert | ok | info
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
}

// HostInfo describes the machine being monitored.
type HostInfo struct {
	Hostname string `json:"hostname"`
	Model    string `json:"model"`
	Chip     string `json:"chip"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	MemTotal uint64 `json:"mem_total"`
	UptimeS  uint64 `json:"uptime_s"`
	BootTime int64  `json:"boot_time"`
}
