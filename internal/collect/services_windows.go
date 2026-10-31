package collect

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"

	"github.com/ricardotech/minipulse/internal/model"
)

// SystemServices lists *running* Windows services in one
// EnumServicesStatusEx call with connect+enumerate rights only —
// the mgr package asks for SC_MANAGER_ALL_ACCESS, which a
// non-elevated foreground process does not have (the endpoint
// would 500 unless MiniPulse ran elevated). The full service list
// on a typical box runs to hundreds of stopped entries that a
// server dashboard doesn't care about, so — unlike launchd, where
// the owner's own jobs are the short list — running-only is the
// useful view here. model.Service.Status has no Windows analogue
// and stays 0; the UI keys off PID.
func SystemServices(ctx context.Context) ([]model.Service, error) {
	h, err := windows.OpenSCManager(nil, nil,
		windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, fmt.Errorf("open service manager: %w", err)
	}
	defer windows.CloseServiceHandle(h)

	var entries []windows.ENUM_SERVICE_STATUS_PROCESS
	// A service starting between the sizing call and the real one
	// makes the buffer too small; retry with the freshly
	// reported size instead of failing the whole request.
	for attempt := 0; attempt < 3; attempt++ {
		var needed, returned, resume uint32
		err = windows.EnumServicesStatusEx(h, windows.SC_ENUM_PROCESS_INFO,
			windows.SERVICE_WIN32, windows.SERVICE_ACTIVE,
			nil, 0, &needed, &returned, &resume, nil)
		if err != nil && !errors.Is(err, windows.ERROR_MORE_DATA) {
			return nil, fmt.Errorf("enumerate services: %w", err)
		}
		if needed == 0 {
			return nil, nil
		}
		buf := make([]byte, needed)
		returned = 0
		err = windows.EnumServicesStatusEx(h, windows.SC_ENUM_PROCESS_INFO,
			windows.SERVICE_WIN32, windows.SERVICE_ACTIVE,
			&buf[0], uint32(len(buf)), &needed, &returned, &resume, nil)
		if errors.Is(err, windows.ERROR_MORE_DATA) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("enumerate services: %w", err)
		}
		entries = unsafe.Slice((*windows.ENUM_SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buf[0])), returned)
		break
	}
	if entries == nil {
		return nil, fmt.Errorf("enumerate services: buffer kept growing")
	}
	out := make([]model.Service, 0, len(entries))
	for _, e := range entries {
		// SERVICE_ACTIVE also covers start-pending/paused; the
		// view promises *running* (the rest would show as
		// "stopped" with pid 0 in the UI).
		if e.ServiceStatusProcess.CurrentState != windows.SERVICE_RUNNING {
			continue
		}
		out = append(out, model.Service{
			Label: windows.UTF16PtrToString(e.ServiceName),
			PID:   int(e.ServiceStatusProcess.ProcessId),
		})
	}
	return out, nil
}

// Listeners lists TCP ports in LISTEN state via gopsutil's
// connection table (parsing `netstat` output is locale-dependent).
// A non-elevated process sees other users' connections but may not
// resolve every owning process's name; those rows fall back to a
// synthetic "pid-<n>" label.
func Listeners(ctx context.Context) ([]model.Listener, error) {
	conns, err := gnet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return nil, err
	}
	names := map[int32]string{}
	nameOf := func(pid int32) string {
		if n, ok := names[pid]; ok {
			return n
		}
		n := ""
		if p, err := process.NewProcessWithContext(ctx, pid); err == nil {
			n, _ = p.NameWithContext(ctx)
		}
		names[pid] = n
		return n
	}
	var out []model.Listener
	seen := map[string]bool{}
	for _, c := range conns {
		if c.Status != "LISTEN" {
			continue
		}
		key := fmt.Sprintf("%d/%s:%d", c.Pid, c.Laddr.IP, c.Laddr.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		proc := nameOf(c.Pid)
		if proc == "" {
			proc = fmt.Sprintf("pid-%d", c.Pid)
		}
		out = append(out, model.Listener{
			Process: proc, PID: int(c.Pid),
			Addr: c.Laddr.IP, Port: int(c.Laddr.Port),
		})
	}
	return out, nil
}
