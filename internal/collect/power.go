package collect

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/ricardotech/minipulse/internal/model"
)

// macmonLine mirrors the JSON emitted by `macmon pipe` (v0.10).
// Only the fields MiniPulse displays are decoded.
type macmonLine struct {
	AllPower float64 `json:"all_power"`
	CPUPower float64 `json:"cpu_power"`
	GPUPower float64 `json:"gpu_power"`
	ANEPower float64 `json:"ane_power"`
	RAMPower float64 `json:"ram_power"`
	SysPower float64 `json:"sys_power"`
	Temp     struct {
		CPUAvg float64 `json:"cpu_temp_avg"`
		GPUAvg float64 `json:"gpu_temp_avg"`
	} `json:"temp"`
	Fans []struct {
		RPM int `json:"rpm"`
	} `json:"fans"`
	CPUTiers []struct {
		Label       string  `json:"label"`
		ActiveRatio float64 `json:"active_ratio"`
		FreqMHz     int     `json:"freq_mhz"`
		Cores       []struct {
			CoreID      int     `json:"core_id"`
			ActiveRatio float64 `json:"active_ratio"`
			FreqMHz     int     `json:"freq_mhz"`
		} `json:"cores"`
	} `json:"cpu_tiers"`
}

// ParseMacmonLine converts one macmon JSON line into a Power reading.
func ParseMacmonLine(line []byte) (model.Power, error) {
	var m macmonLine
	if err := json.Unmarshal(line, &m); err != nil {
		return model.Power{}, err
	}
	p := model.Power{
		Available: true,
		SysW:      m.SysPower,
		SoCW:      m.AllPower,
		CPUW:      m.CPUPower,
		GPUW:      m.GPUPower,
		ANEW:      m.ANEPower,
		RAMW:      m.RAMPower,
		CPUTempC:  m.Temp.CPUAvg,
		GPUTempC:  m.Temp.GPUAvg,
	}
	if len(m.Fans) > 0 {
		p.FanRPM = m.Fans[0].RPM
	}
	for _, t := range m.CPUTiers {
		tier := model.Tier{Label: t.Label, ActiveRatio: t.ActiveRatio, FreqMHz: t.FreqMHz}
		for _, c := range t.Cores {
			tier.Cores = append(tier.Cores, model.Core{ID: c.CoreID, ActiveRatio: c.ActiveRatio, FreqMHz: c.FreqMHz})
		}
		p.CPUTiers = append(p.CPUTiers, tier)
	}
	return p, nil
}

// PowerSource keeps a `macmon pipe` subprocess alive and exposes its
// most recent reading. macmon reads Apple Silicon power/thermal data
// through a private API without sudo — MiniPulse deliberately reuses
// it instead of reimplementing IOReport bindings.
type PowerSource struct {
	mu      sync.RWMutex
	latest  model.Power
	updated time.Time
	path    string // macmon binary path, "" when not installed
}

// NewPowerSource locates macmon and starts streaming from it.
func NewPowerSource(ctx context.Context) *PowerSource {
	ps := &PowerSource{}
	path, err := exec.LookPath("macmon")
	if err != nil {
		ps.latest = model.Power{Available: false,
			Reason: "macmon not installed — run: brew install macmon"}
		slog.Warn("power metrics unavailable", "reason", ps.latest.Reason)
		return ps
	}
	ps.path = path
	ps.latest = model.Power{Available: false, Reason: "waiting for first macmon sample"}
	go ps.run(ctx)
	return ps
}

// Latest returns the freshest reading (marked unavailable when stale).
func (ps *PowerSource) Latest() model.Power {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p := ps.latest
	if p.Available && time.Since(ps.updated) > 10*time.Second {
		p.Available = false
		p.Reason = "macmon stream stale"
	}
	return p
}

func (ps *PowerSource) run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := ps.streamOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("macmon stream ended, restarting", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (ps *PowerSource) streamOnce(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, ps.path, "pipe", "-i", "1000")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		p, err := ParseMacmonLine(scanner.Bytes())
		if err != nil {
			continue // partial/first line — keep going
		}
		ps.mu.Lock()
		ps.latest, ps.updated = p, time.Now()
		ps.mu.Unlock()
	}
	if err := scanner.Err(); err != nil {
		// e.g. a line over the 1 MiB cap: we've stopped reading, so
		// the child blocks on a full pipe. Kill it or Wait hangs.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
