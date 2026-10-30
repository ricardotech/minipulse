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

	procMu    sync.Mutex
	procCache []model.Process
	procAt    time.Time
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
	if name := macComputerName(); name != "" {
		hi.Hostname = name
	}
	// Native fallbacks: host.Info() can fail wholesale under launchd,
	// leaving the dashboard header blank.
	if hi.OS == "" || strings.HasPrefix(hi.OS, "darwin") {
		if v := macOSVersion(); v != "" {
			hi.OS = v
		}
	}
	if hi.BootTime == 0 {
		hi.BootTime = macBootTime()
	}
	if hi.BootTime > 0 {
		hi.UptimeS = uint64(time.Now().Unix() - hi.BootTime)
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		hi.Chip = infos[0].ModelName
	}
	hi.Model = macModel()
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
			// Whole disks only (disk0, disk4…), not APFS slices (disk3s1).
			if strings.Contains(name, "s") && !isWholeDisk(name) {
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

func isWholeDisk(name string) bool {
	// "disk0" yes, "disk3s1" no.
	for _, r := range strings.TrimPrefix(name, "disk") {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
		if out[i].Mount == "/" {
			return true
		}
		if out[j].Mount == "/" {
			return false
		}
		return out[i].Total > out[j].Total
	})
	return out
}

// isRealMount filters APFS system slices that would double-count "/".
func isRealMount(mount string) bool {
	if mount == "/" {
		return true
	}
	if strings.HasPrefix(mount, "/Volumes/") {
		return mount != "/Volumes/Recovery"
	}
	return false // /System/Volumes/* etc. share the root container
}

// TopProcesses returns processes sorted by memory (or CPU when byCPU),
// cached for 2s because a full process walk is the priciest sample.
func (s *System) TopProcesses(byCPU bool, limit int) ([]model.Process, error) {
	s.procMu.Lock()
	defer s.procMu.Unlock()
	if time.Since(s.procAt) > 2*time.Second || s.procCache == nil {
		procs, err := walkProcesses()
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

func walkProcesses() ([]model.Process, error) {
	ps, err := process.Processes()
	if err != nil {
		return nil, err
	}
	out := make([]model.Process, 0, len(ps))
	for _, p := range ps {
		name, err := p.Name()
		if err != nil {
			continue
		}
		mp := model.Process{PID: p.Pid, Name: name}
		mp.CPUPct, _ = p.CPUPercent()
		mp.MemPct, _ = p.MemoryPercent()
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			mp.RSS = mi.RSS
		}
		mp.User, _ = p.Username()
		if cmd, err := p.Cmdline(); err == nil {
			mp.Command = truncate(cmd, 160)
		}
		out = append(out, mp)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
