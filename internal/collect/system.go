package collect

import (
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/ricardotech/minipulse/internal/model"
)

// System gathers CPU/RAM/network/disk/process metrics via gopsutil.
// Rate-based counters (network, disk I/O) are diffed against the
// previous call, so System is stateful and must not be called
// concurrently.
type System struct {
	lastNetRx, lastNetTx     uint64
	lastDiskRead, lastDiskWr uint64
	lastTime                 time.Time

	procMu     sync.Mutex
	procCache  []model.Process
	procAt     time.Time
	procPrev   map[int32]procMark
	lastWalkAt time.Time
}

// procMark remembers a process's cumulative CPU time at the last walk
// so the next walk can compute a true current rate.
type procMark struct {
	total  float64 // user+system seconds
	at     time.Time
	create int64 // ms, detects PID reuse
}

func NewSystem() *System { return &System{} }

// HostInfo identifies the machine (cached data, cheap to re-read).
func HostInfo() model.HostInfo {
	hi := model.HostInfo{Arch: runtime.GOARCH}
	if info, err := host.Info(); err == nil {
		hi.Hostname = info.Hostname
		hi.OS = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
		hi.UptimeS = info.Uptime
		hi.BootTime = int64(info.BootTime)
	}
	if name := platformComputerName(); name != "" {
		hi.Hostname = name
	}
	// Native fallbacks: host.Info() can fail wholesale under launchd,
	// leaving the dashboard header blank.
	if hi.OS == "" || strings.HasPrefix(hi.OS, "darwin") {
		if v := platformOSVersion(); v != "" {
			hi.OS = v
		}
	}
	if hi.BootTime == 0 {
		hi.BootTime = platformBootTime()
	}
	if hi.BootTime > 0 {
		hi.UptimeS = uint64(time.Now().Unix() - hi.BootTime)
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		hi.Chip = infos[0].ModelName
	}
	hi.Model = platformModel()
	if vm, err := mem.VirtualMemory(); err == nil {
		hi.MemTotal = vm.Total
	}
	return hi
}

// Sample takes one system reading. Power/volumes are filled by callers.
func (s *System) Sample(now time.Time) model.Sample {
	sm := model.Sample{Time: now}
	if pcts, err := cpu.Percent(0, true); err == nil && len(pcts) > 0 {
		sm.CPUPerCore = pcts
		sum := 0.0
		for _, p := range pcts {
			sum += p
		}
		sm.CPUPct = sum / float64(len(pcts))
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		sm.MemUsed, sm.MemTotal, sm.MemPct = vm.Used, vm.Total, vm.UsedPercent
	}
	if sw, err := mem.SwapMemory(); err == nil {
		sm.SwapUsed, sm.SwapTotal = sw.Used, sw.Total
	}
	if l, err := load.Avg(); err == nil {
		sm.Load1, sm.Load5, sm.Load15 = l.Load1, l.Load5, l.Load15
	}
	dt := now.Sub(s.lastTime).Seconds()
	if counters, err := gnet.IOCounters(false); err == nil && len(counters) > 0 {
		rx, tx := counters[0].BytesRecv, counters[0].BytesSent
		if dt > 0 && !s.lastTime.IsZero() {
			sm.NetRxBps = rate(rx, s.lastNetRx, dt)
			sm.NetTxBps = rate(tx, s.lastNetTx, dt)
		}
		s.lastNetRx, s.lastNetTx = rx, tx
	}
	if ioMap, err := disk.IOCounters(); err == nil {
		var read, write uint64
		for name, io := range ioMap {
			if !countDiskIO(name) {
				continue
			}
			read += io.ReadBytes
			write += io.WriteBytes
		}
		if dt > 0 && !s.lastTime.IsZero() {
			sm.DiskReadBps = rate(read, s.lastDiskRead, dt)
			sm.DiskWritBps = rate(write, s.lastDiskWr, dt)
		}
		s.lastDiskRead, s.lastDiskWr = read, write
	}
	s.lastTime = now
	sm.Volumes = Volumes()
	return sm
}

func rate(now, prev uint64, dt float64) float64 {
	if now < prev { // counter reset
		return 0
	}
	return float64(now-prev) / dt
}

// Volumes lists real (non-pseudo) mounted filesystems with usage.
func Volumes() []model.Volume {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []model.Volume
	for _, p := range parts {
		if seen[p.Mountpoint] || !isRealMount(p.Mountpoint) {
			continue
		}
		seen[p.Mountpoint] = true
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		used, pct := u.Used, u.UsedPercent
		if p.Fstype == "apfs" {
			// APFS volumes share one container: per-volume "used" for
			// "/" reports only the sealed system snapshot (~13 GB) and
			// badly under-reports the disk. Container used = total-free
			// is the number Finder/`df` users expect. Volumes of the
			// same container share (total, free); keep one row for them.
			dup := false
			for _, v := range out {
				if v.FSType == "apfs" && v.Total == u.Total && v.Free == u.Free {
					dup = true
				}
			}
			if dup {
				continue
			}
			if u.Total > u.Free {
				used = u.Total - u.Free
				pct = float64(used) / float64(u.Total) * 100
			}
		}
		out = append(out, model.Volume{
			Mount: p.Mountpoint, Device: p.Device, FSType: p.Fstype,
			Total: u.Total, Used: used, Free: u.Free, UsedPct: pct,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mount == rootMount() {
			return true
		}
		if out[j].Mount == rootMount() {
			return false
		}
		return out[i].Total > out[j].Total
	})
	return out
}

// TopProcesses returns processes sorted by memory (or CPU when byCPU),
// cached for 2s because a full process walk is the priciest sample.
func (s *System) TopProcesses(byCPU bool, limit int) ([]model.Process, error) {
	s.procMu.Lock()
	defer s.procMu.Unlock()
	if time.Since(s.procAt) > 2*time.Second || s.procCache == nil {
		procs, err := s.walkProcesses()
		if err != nil {
			return nil, err
		}
		s.procCache, s.procAt = procs, time.Now()
	}
	out := make([]model.Process, len(s.procCache))
	copy(out, s.procCache)
	sort.Slice(out, func(i, j int) bool {
		if byCPU {
			return out[i].CPUPct > out[j].CPUPct
		}
		return out[i].RSS > out[j].RSS
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// walkProcesses lists all processes. CPU % is a true rate between
// the previous walk and this one (100 = one full core). Calling
// gopsutil's CPUPercent() on a fresh Process object instead yields a
// lifetime average, which inflates short-lived bursty processes and
// hides what is hot *right now* — measured live as a group of node
// processes "averaging" 880% summed while barely active.
func (s *System) walkProcesses() ([]model.Process, error) {
	ps, err := process.Processes()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	next := make(map[int32]procMark, len(ps))
	out := make([]model.Process, 0, len(ps))
	for _, p := range ps {
		name, err := p.Name()
		if err != nil {
			continue
		}
		mp := model.Process{PID: p.Pid, Name: name}
		mp.CPUPct = s.cpuRate(p, now, next)
		mp.MemPct, _ = p.MemoryPercent()
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			mp.RSS = mi.RSS
		}
		out = append(out, mp)
	}
	s.procPrev = next
	s.lastWalkAt = now
	return out, nil
}

// cpuRate diffs cumulative CPU time against the previous walk.
func (s *System) cpuRate(p *process.Process, now time.Time, next map[int32]procMark) float64 {
	times, err := p.Times()
	if err != nil || times == nil {
		return 0 // no fallback to CPUPercent(): that is the lifetime average this code exists to avoid
	}
	total := times.User + times.System
	create, _ := p.CreateTime()
	next[p.Pid] = procMark{total: total, at: now, create: create}
	var prev *procMark
	if m, ok := s.procPrev[p.Pid]; ok {
		prev = &m
	}
	pct := rateFromMark(prev, total, create, now, s.lastWalkAt)
	if max := float64(runtime.NumCPU()) * 100; pct > max {
		pct = max
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}

// rateFromMark is the pure rate calculation. On first sight there is
// no baseline: only a process born *since the previous walk* gets its
// lifetime average (a fair proxy for a newborn); a pre-existing
// process reports 0 until the next walk. Using the lifetime average
// for old processes is what made long-dead bursts look current —
// observed live as idle node workers "using" ~100% each.
func rateFromMark(prev *procMark, total float64, createMs int64, now, lastWalk time.Time) float64 {
	if lastWalk.IsZero() {
		return 0 // very first walk: no baseline for anyone; "born after zero time" would make every old process a newborn
	}
	if prev != nil && createMs > 0 && prev.create == createMs {
		if dt := now.Sub(prev.at).Seconds(); dt > 0 && total >= prev.total {
			return (total - prev.total) / dt * 100
		}
		return 0
	}
	if createMs > 0 {
		born := time.UnixMilli(createMs)
		if born.After(lastWalk) {
			if age := now.Sub(born).Seconds(); age > 0 {
				return total / age * 100
			}
		}
	}
	return 0
}
