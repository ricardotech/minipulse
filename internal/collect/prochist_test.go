package collect

import (
	"testing"
	"time"

	"github.com/ricardotech/minipulse/internal/model"
)

func TestMergeTopUnionDedupes(t *testing.T) {
	procs := []model.Process{
		{PID: 1, Name: "cpu-hog", CPUPct: 300, RSS: 10},
		{PID: 2, Name: "mem-hog", CPUPct: 1, RSS: 9999},
		{PID: 3, Name: "both", CPUPct: 200, RSS: 9000},
		{PID: 4, Name: "tiny", CPUPct: 0.1, RSS: 1},
	}
	got := MergeTop(procs, 2)
	// top2 CPU {1,3} ∪ top2 RSS {2,3} = {1,3,2}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(got), got)
	}
	seen := map[int32]bool{}
	for _, p := range got {
		if seen[p.PID] {
			t.Fatalf("duplicate pid %d", p.PID)
		}
		seen[p.PID] = true
	}
	if !seen[1] || !seen[2] || !seen[3] || seen[4] {
		t.Errorf("merged set wrong: %+v", got)
	}
}

func TestSpikeTrackerNeedsTwoConsecutiveAndCoolsDown(t *testing.T) {
	tr := NewSpikeTracker(200, 30*time.Minute)
	t0 := time.Unix(1_700_000_000, 0)
	hot := []model.Process{{PID: 1, Name: "hog", CPUPct: 250}}
	cool := []model.Process{{PID: 1, Name: "hog", CPUPct: 5}}

	if got := tr.Observe(hot, t0); len(got) != 0 {
		t.Fatalf("first sighting must not fire: %v", got)
	}
	if got := tr.Observe(cool, t0.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("cool sample must reset: %v", got)
	}
	_ = tr.Observe(hot, t0.Add(2*time.Minute))
	if got := tr.Observe(hot, t0.Add(3*time.Minute)); len(got) != 1 {
		t.Fatalf("second consecutive hot sample must fire: %v", got)
	}
	if got := tr.Observe(hot, t0.Add(4*time.Minute)); len(got) != 0 {
		t.Fatalf("cooldown must suppress: %v", got)
	}
	if got := tr.Observe(hot, t0.Add(40*time.Minute)); len(got) != 1 {
		t.Fatalf("after cooldown must fire again: %v", got)
	}
}
