package collect

import (
	"context"
	"fmt"

	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/ricardotech/minipulse/internal/model"
)

// SystemServices lists *running* Windows services via the Service
// Control Manager (no text parsing of `sc query`). The full service
// list on a typical box runs to hundreds of stopped entries that a
// server dashboard doesn't care about, so — unlike launchd, where
// the owner's own jobs are the short list — running-only is the
// useful view here. model.Service.Status has no Windows analogue
// and stays 0; the UI keys off PID.
func SystemServices(ctx context.Context) ([]model.Service, error) {
	m, err := mgr.ConnectRemote("")
	if err != nil {
		return nil, fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()
	names, err := m.ListServices()
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	var out []model.Service
	for _, name := range names {
		if ctx.Err() != nil {
			return out, nil
		}
		s, err := m.OpenService(name)
		if err != nil {
			continue
		}
		st, err := s.Query()
		s.Close()
		if err != nil || st.State != svc.Running {
			continue
		}
		out = append(out, model.Service{Label: name, PID: int(st.ProcessId)})
	}
	return out, nil
}

// Listeners lists TCP ports in LISTEN state via gopsutil's
// connection table (parsing `netstat` output is locale-dependent).
// A non-elevated process sees other users' connections but may not
// resolve every owning process's name; those rows keep the PID
// with an empty process name.
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
