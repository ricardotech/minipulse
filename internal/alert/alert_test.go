package alert

import (
	"testing"
	"time"

	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
)

func TestEvaluateFiresOnceRecoversAndRepeatsAfterCooldown(t *testing.T) {
	cfg := config.Defaults()
	cfg.AlertMemPct = 90
	cfg.AlertCooldownMinutes = 30
	e := New(cfg)
	t0 := time.Unix(1_700_000_000, 0)

	hot := model.Sample{MemPct: 95}
	if got := e.Evaluate(hot, t0); len(got) != 1 || got[0].Recovery {
		t.Fatalf("first breach = %+v, want one alert", got)
	}
	if got := e.Evaluate(hot, t0.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("during cooldown = %+v, want none", got)
	}
	if got := e.Evaluate(hot, t0.Add(31*time.Minute)); len(got) != 1 {
		t.Fatalf("after cooldown = %+v, want repeat", got)
	}
	cool := model.Sample{MemPct: 50}
	if got := e.Evaluate(cool, t0.Add(32*time.Minute)); len(got) != 1 || !got[0].Recovery {
		t.Fatalf("recovery = %+v, want one recovery", got)
	}
	if got := e.Evaluate(cool, t0.Add(33*time.Minute)); len(got) != 0 {
		t.Fatalf("steady ok = %+v, want none", got)
	}
}

func TestEvaluateDiskPerVolume(t *testing.T) {
	cfg := config.Defaults()
	cfg.AlertDiskPct = 85
	e := New(cfg)
	sm := model.Sample{Volumes: []model.Volume{
		{Mount: "/", UsedPct: 50},
		{Mount: "/Volumes/Data", UsedPct: 91},
	}}
	got := e.Evaluate(sm, time.Now())
	if len(got) != 1 || got[0].Rule != "disk:/Volumes/Data" {
		t.Fatalf("got %+v, want one disk alert for /Volumes/Data", got)
	}
}
