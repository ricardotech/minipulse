package store

import (
	"context"
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

func TestProcessHistoryRoundtrip(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := time.Now().Add(-30 * time.Minute).Truncate(time.Second)
	snaps := [][]model.Process{
		{{PID: 1, Name: "hog", CPUPct: 100, RSS: 1000}, {PID: 2, Name: "idle", CPUPct: 1, RSS: 5000}},
		{{PID: 1, Name: "hog", CPUPct: 300, RSS: 2001}, {PID: 9, Name: "hog", CPUPct: 50, RSS: 500}, {PID: 2, Name: "idle", CPUPct: 2, RSS: 6000}}, // same-name instances sum; non-integral AVG(rss) must not break scans
		{{PID: 1, Name: "hog", CPUPct: 200, RSS: 3000}},
	}
	for i, s := range snaps {
		if err := st.InsertProcessSample(base.Add(time.Duration(i)*time.Minute), s); err != nil {
			t.Fatal(err)
		}
	}
	from, to := base.Add(-time.Minute), base.Add(3*time.Minute)
	leaders, err := st.ProcessLeaders(context.Background(), from, to, false, 5)
	if err != nil || len(leaders) != 2 || leaders[0].Name != "hog" {
		t.Fatalf("leaders = %+v err=%v, want hog first", leaders, err)
	}
	// hog per-snapshot sums: cpu 100/350/200, rss 1000/2501/3000.
	if leaders[0].PeakCPU != 350 || leaders[0].Samples != 3 || leaders[0].AvgRSS != 2167 ||
		!leaders[0].PeakCPUAt.Equal(base.Add(time.Minute)) {
		t.Errorf("hog aggregate wrong: %+v", leaders[0])
	}
	byMem, _ := st.ProcessLeaders(context.Background(), from, to, true, 5)
	if byMem[0].Name != "idle" || byMem[0].PeakRSS != 6000 {
		t.Errorf("byMem leader = %+v, want idle peak 6000", byMem[0])
	}
	series, err := st.ProcessSeries(context.Background(), from, to, 60, []string{"hog"})
	if err != nil || len(series["hog"]) != 3 || series["hog"][1].CPU != 350 {
		t.Errorf("series = %+v err=%v", series["hog"], err)
	}
	at, atTime, err := st.ProcessesAt(context.Background(), base.Add(90*time.Second))
	if err != nil || len(at) != 3 || at[0].Name != "hog" || !atTime.Equal(base.Add(time.Minute)) {
		t.Errorf("at = %+v @%v err=%v, want hog snapshot of +1m", at, atTime, err)
	}
	if none, _, _ := st.ProcessesAt(context.Background(), base.Add(-time.Hour)); none != nil {
		t.Errorf("before first snapshot must be empty, got %+v", none)
	}
}

func TestPruneRemovesOldProcessSamplesOnly(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	old := time.Now().AddDate(0, 0, -30)
	recent := time.Now().Add(-time.Hour)
	one := []model.Process{{PID: 1, Name: "hog", CPUPct: 50, RSS: 1}}
	if err := st.InsertProcessSample(old, one); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertProcessSample(recent, one); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSample(model.Sample{Time: recent}); err != nil {
		t.Fatal(err)
	}
	// 30-day retention for samples, 14 for process samples.
	if err := st.Prune(30, 14, 30); err != nil {
		t.Fatal(err)
	}
	at, _, err := st.ProcessesAt(context.Background(), old.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if at != nil {
		t.Errorf("30-day-old process snapshot survived a 14-day prune: %+v", at)
	}
	at, _, err = st.ProcessesAt(context.Background(), time.Now())
	if err != nil || len(at) != 1 {
		t.Errorf("recent snapshot = %+v err=%v, want kept", at, err)
	}
}
