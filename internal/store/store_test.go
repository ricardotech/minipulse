package store

import (
	"testing"
	"time"

	"github.com/ricardotech/minipulse/internal/model"
)

func TestSampleEnergyAndGrowthRoundtrip(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := 0; i < 3; i++ {
		sm := model.Sample{
			Time:    base.Add(time.Duration(i) * time.Minute),
			WhTotal: float64(i * 10), MemPct: 50,
			Power:   model.Power{SysW: 20, CPUTempC: 40},
			Volumes: []model.Volume{{Mount: "/", Used: 100, Total: 200}},
		}
		if err := st.InsertSample(sm); err != nil {
			t.Fatal(err)
		}
	}
	if wh, _ := st.LastWhTotal(); wh != 20 {
		t.Errorf("LastWhTotal = %v, want 20", wh)
	}
	e, err := st.EnergyBetween(base.Add(time.Minute), base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if e.Wh != 20 || e.AvgW != 20 || e.Samples != 2 {
		t.Errorf("energy = %+v, want 20Wh/20W/2 samples", e)
	}
	pts, err := st.History(base.Add(-time.Minute), base.Add(3*time.Minute), 60)
	if err != nil || len(pts) != 3 {
		t.Errorf("history = %d points, err %v; want 3", len(pts), err)
	}

	mk := func(ts time.Time, sizes map[string]int64) int64 {
		var entries []model.DiskEntry
		for p, s := range sizes {
			entries = append(entries, model.DiskEntry{Path: p, Size: s, IsDir: true})
		}
		id, err := st.SaveDiskScan(model.DiskScan{Time: ts, Root: "/r"}, entries)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	old := mk(base, map[string]int64{"/r/a": 100, "/r/b": 50, "/r/gone": 70})
	newID := mk(base.Add(time.Hour), map[string]int64{"/r/a": 400, "/r/b": 50, "/r/new": 10})
	g, err := st.Growth(old, newID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 3 || g[0].Path != "/r/a" || g[0].Delta != 300 {
		t.Fatalf("growth = %+v, want /r/a +300 first", g)
	}
	found := map[string]int64{}
	for _, x := range g {
		found[x.Path] = x.Delta
	}
	if found["/r/gone"] != -70 || found["/r/new"] != 10 {
		t.Errorf("growth deltas = %+v", found)
	}
}
