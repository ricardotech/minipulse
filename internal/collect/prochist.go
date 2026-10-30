package collect

import (
	"fmt"
	"sort"
	"time"

	"github.com/ricardotech/minipulse/internal/model"
)

// MergeTop returns the union of the top-n processes by CPU and by RSS,
// deduplicated by PID — the small set worth persisting each snapshot.
func MergeTop(procs []model.Process, n int) []model.Process {
	if n <= 0 {
		n = 10
	}
	byCPU := make([]model.Process, len(procs))
	copy(byCPU, procs)
	sort.Slice(byCPU, func(i, j int) bool { return byCPU[i].CPUPct > byCPU[j].CPUPct })
	byMem := make([]model.Process, len(procs))
	copy(byMem, procs)
	sort.Slice(byMem, func(i, j int) bool { return byMem[i].RSS > byMem[j].RSS })

	seen := map[int32]bool{}
	var out []model.Process
	add := func(list []model.Process) {
		for i, p := range list {
			if i >= n {
				break
			}
			if !seen[p.PID] {
				seen[p.PID] = true
				out = append(out, p)
			}
		}
	}
	add(byCPU)
	add(byMem)
	return out
}

// SpikeTracker reports processes that stay above a CPU threshold for
// two consecutive snapshots, with a per-process cooldown — a light
// "what went crazy" log without alert spam from one-off bursts.
type SpikeTracker struct {
	threshold float64
	cooldown  time.Duration
	streak    map[string]int
	lastSent  map[string]time.Time
}

func NewSpikeTracker(threshold float64, cooldown time.Duration) *SpikeTracker {
	return &SpikeTracker{threshold: threshold, cooldown: cooldown,
		streak: map[string]int{}, lastSent: map[string]time.Time{}}
}

// Observe feeds one snapshot; returns spike descriptions to log now.
func (t *SpikeTracker) Observe(procs []model.Process, now time.Time) []string {
	if t.threshold <= 0 {
		return nil
	}
	hot := map[string]float64{}
	for _, p := range procs {
		if p.CPUPct >= t.threshold && p.CPUPct > hot[p.Name] {
			hot[p.Name] = p.CPUPct
		}
	}
	for name := range t.streak {
		if _, ok := hot[name]; !ok {
			delete(t.streak, name)
		}
	}
	// Prune cooled-down entries so lastSent can't grow unbounded with
	// every distinct short-lived process name the box ever spawns.
	for name, sent := range t.lastSent {
		if _, active := t.streak[name]; !active && now.Sub(sent) >= t.cooldown {
			delete(t.lastSent, name)
		}
	}
	var out []string
	for name, pct := range hot {
		t.streak[name]++
		if t.streak[name] >= 2 && now.Sub(t.lastSent[name]) >= t.cooldown {
			t.lastSent[name] = now
			out = append(out, fmt.Sprintf("%s at %.0f%% CPU (≥%.0f%% for 2+ samples)", name, pct, t.threshold))
		}
	}
	sort.Strings(out)
	return out
}
