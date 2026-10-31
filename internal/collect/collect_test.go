package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
	"github.com/ricardotech/minipulse/internal/store"
)

func TestScansStaleOnDemandOnly(t *testing.T) {
	cfg := config.Defaults()
	cfg.DiskScanIntervalMinutes = 0 // documented as "on demand only"
	c := &Collector{cfg: cfg}       // nil store: must return before touching it
	if c.scansStale() {
		t.Error("interval 0 must never trigger a boot scan")
	}
}

// Positive control for the test above: scheduled mode still asks for
// a boot scan when none exists, and stops asking once a fresh one is
// stored.
func TestScansStaleScheduledMode(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Defaults()
	cfg.DiskScanIntervalMinutes = 60
	cfg.DiskScanRoots = []string{"/tmp/mp-test-root"}
	c := &Collector{cfg: cfg, store: st}
	if !c.scansStale() {
		t.Fatal("no scan stored + scheduled mode: want stale")
	}
	if _, err := st.SaveDiskScan(model.DiskScan{Time: time.Now(), Root: "/tmp/mp-test-root"}, nil); err != nil {
		t.Fatal(err)
	}
	if c.scansStale() {
		t.Error("fresh scan stored: want not stale")
	}
}

func TestLaunchctlPathExists(t *testing.T) {
	info, err := os.Stat(launchctlPath)
	if err != nil {
		t.Fatalf("launchctl path %s: %v", launchctlPath, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		t.Errorf("%s is not an executable file (mode %v)", launchctlPath, info.Mode())
	}
}

// One real line captured from `macmon pipe` 0.10 on an Apple M6.
const macmonSample = `{"all_power":0.17742818593978882,"ane_power":0.0,"cpu_power":0.15622924268245697,"cpu_tiers":[{"active_ratio":0.11169984191656113,"cores":[{"active_ratio":0.24637316167354584,"core_id":0,"die_id":0,"freq_mhz":1198,"scaled_ratio":0.1}],"freq_mhz":1284,"label":"E","scaled_ratio":0.047}],"fans":[{"max_rpm":4900,"name":"fan0","rpm":999}],"gpu_power":0.021198948845267296,"gpu_ram_power":0.0,"memory":{"ram_total":17179869184,"ram_usage":10640392192,"swap_total":0,"swap_usage":0},"ram_power":0.0,"sys_power":19.483932495117188,"temp":{"cpu_temp_avg":38.75801467895508,"gpu_temp_avg":38.63107681274414},"timestamp":"2026-10-30T20:59:15.630084+00:00"}`

func TestParseMacmonLine(t *testing.T) {
	p, err := ParseMacmonLine([]byte(macmonSample))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Available || p.SysW < 19 || p.SysW > 20 {
		t.Errorf("sys power = %v, want ~19.48", p.SysW)
	}
	if p.CPUTempC < 38 || p.CPUTempC > 39 {
		t.Errorf("cpu temp = %v, want ~38.7", p.CPUTempC)
	}
	if p.FanRPM != 999 {
		t.Errorf("fan = %d, want 999", p.FanRPM)
	}
	if len(p.CPUTiers) != 1 || p.CPUTiers[0].Label != "E" || len(p.CPUTiers[0].Cores) != 1 {
		t.Errorf("tiers = %+v", p.CPUTiers)
	}
}

func TestScanDirAggregatesAndFilters(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, size int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("big/file1.bin", 3000)
	write("big/sub/file2.bin", 2000)
	write("small/tiny.txt", 5)
	// Symlink to the big dir must not be followed (no double count).
	_ = os.Symlink(filepath.Join(root, "big"), filepath.Join(root, "link"))

	res := ScanDir(root, 1000)
	if res.Scan.TotalBytes != 5005 {
		t.Fatalf("total = %d, want 5005 (symlink not followed)", res.Scan.TotalBytes)
	}
	if res.Scan.FileCount != 3 {
		t.Fatalf("files = %d, want 3", res.Scan.FileCount)
	}
	byPath := map[string]int64{}
	for _, e := range res.Entries {
		byPath[e.Path] = e.Size
	}
	if byPath[filepath.Join(root, "big")] != 5000 {
		t.Errorf("big/ = %d, want 5000", byPath[filepath.Join(root, "big")])
	}
	if _, ok := byPath[filepath.Join(root, "small", "tiny.txt")]; ok {
		t.Error("tiny file below threshold should be filtered out")
	}
	if _, ok := byPath[filepath.Join(root, "small")]; !ok {
		t.Error("shallow small directory should still appear for tree navigation")
	}
}

func TestParseLaunchctl(t *testing.T) {
	out := "PID\tStatus\tLabel\n123\t0\tcom.example.running\n-\t0\tcom.example.stopped\n"
	got := ParseLaunchctl(out)
	if len(got) != 2 || got[0].PID != 123 || got[1].PID != 0 || got[1].Label != "com.example.stopped" {
		t.Errorf("got %+v", got)
	}
}

func TestParseLsof(t *testing.T) {
	out := "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\n" +
		"postgres 1305 alice 7u IPv4 0xabc 0t0 TCP 127.0.0.1:5432 (LISTEN)\n" +
		"redis-ser 1297 alice 12u IPv6 0xdef 0t0 TCP [::1]:6379 (LISTEN)\n"
	got := ParseLsof(out)
	if len(got) != 2 || got[0].Port != 5432 || got[1].Port != 6379 || got[0].Process != "postgres" {
		t.Errorf("got %+v", got)
	}
}
