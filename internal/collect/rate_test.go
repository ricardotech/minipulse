package collect

import (
	"math"
	"testing"
	"time"
)

func TestRateFromMark(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)
	lastWalk := now.Add(-10 * time.Second)
	bornOld := now.Add(-time.Hour).UnixMilli()
	bornNew := now.Add(-2 * time.Second).UnixMilli()

	// Known process: 1.5 CPU-seconds over 10s = 15%.
	prev := &procMark{total: 100, at: lastWalk, create: bornOld}
	if got := rateFromMark(prev, 101.5, bornOld, now, lastWalk); math.Abs(got-15) > 0.001 {
		t.Errorf("diff rate = %v, want 15", got)
	}
	// First sight of a pre-existing process: no baseline → 0, NOT the
	// lifetime average (that is what made idle workers look hot).
	if got := rateFromMark(nil, 3600, bornOld, now, lastWalk); got != 0 {
		t.Errorf("first sight (old) = %v, want 0", got)
	}
	// First sight of a newborn (born after last walk): lifetime proxy.
	if got := rateFromMark(nil, 2, bornNew, now, lastWalk); math.Abs(got-100) > 0.001 {
		t.Errorf("first sight (newborn) = %v, want 100", got)
	}
	// PID reuse: create time differs from the remembered mark.
	if got := rateFromMark(prev, 2, bornNew, now, lastWalk); math.Abs(got-100) > 0.001 {
		t.Errorf("pid reuse = %v, want newborn lifetime 100", got)
	}
	// Counter went backwards: report 0, never negative.
	if got := rateFromMark(prev, 50, bornOld, now, lastWalk); got != 0 {
		t.Errorf("counter reset = %v, want 0", got)
	}
	// Very first walk (lastWalk zero): nothing has a baseline yet.
	// Regression test: without the IsZero guard, every pre-existing
	// process is "born after zero time" and reports its lifetime
	// average, polluting the first history snapshot after a restart.
	if got := rateFromMark(nil, 3600, bornOld, now, time.Time{}); got != 0 {
		t.Errorf("first walk ever = %v, want 0", got)
	}
}
