package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/ricardotech/minipulse/internal/config"
)

// ServiceName is the SCM service name.
const ServiceName = "MiniPulse"

// programDataDir is where the service-mode config/data/logs live.
// A LocalSystem service has no useful per-user profile, so — like
// most Windows services — machine-wide state goes to %ProgramData%.
func programDataDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "minipulse")
}

func serviceConfigPath() string { return filepath.Join(programDataDir(), "config.json") }

// Install registers MiniPulse as an automatic Windows Service
// (restart on failure, like launchd's KeepAlive) and starts it.
// The config it runs with lives in %ProgramData%\minipulse: an
// existing per-user config (configPath) is copied there on first
// install, and data_dir is pinned next to it unless the copied
// config sets one explicitly.
func Install(binary, configPath string) error {
	cfgPath, err := prepareServiceConfig(configPath)
	if err != nil {
		return err
	}
	m, err := mgr.ConnectRemote("")
	if err != nil {
		return fmt.Errorf("connect service manager (run as Administrator?): %w", err)
	}
	defer m.Disconnect()
	if old, err := m.OpenService(ServiceName); err == nil {
		_ = stopAndWait(old)
		_ = old.Delete()
		old.Close()
	}
	s, err := m.CreateService(ServiceName, binary, mgr.Config{
		DisplayName: "MiniPulse",
		Description: "Tiny always-on system dashboard (watts/CPU/RAM/disk) — http://127.0.0.1:8848",
		StartType:   mgr.StartAutomatic,
	}, "serve", "--config", cfgPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	// Restart on crash after 5 s, reset the failure counter daily.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, uint32((24 * time.Hour).Seconds()))
	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// prepareServiceConfig creates %ProgramData%\minipulse\config.json
// on first install (copying the user's config when present) and
// returns its path.
func prepareServiceConfig(userConfigPath string) (string, error) {
	dst := serviceConfigPath()
	if _, err := os.Stat(dst); err == nil {
		return dst, nil // reinstall keeps the service config
	}
	if err := os.MkdirAll(programDataDir(), 0o755); err != nil {
		return "", err
	}
	if data, err := os.ReadFile(userConfigPath); err == nil {
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return "", err
		}
	} else {
		if err := config.WriteExample(dst); err != nil {
			return "", err
		}
	}
	// Pin data_dir next to the service config unless the config
	// already names one (the per-user default would resolve into
	// the LocalSystem profile, where nobody will ever find it).
	raw, err := os.ReadFile(dst)
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("parse %s: %w", dst, err)
	}
	if _, ok := m["data_dir"]; !ok {
		m["data_dir"] = filepath.Join(programDataDir(), "data")
		out, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, append(out, '\n'), 0o600); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// Uninstall stops and removes the service. Config/data in
// %ProgramData%\minipulse are left in place.
func Uninstall() error {
	m, err := mgr.ConnectRemote("")
	if err != nil {
		return fmt.Errorf("connect service manager (run as Administrator?): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return nil // not installed
	}
	defer s.Close()
	_ = stopAndWait(s)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func stopAndWait(s *mgr.Service) error {
	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("service did not stop in time")
}

// Status prints the SCM view of the service ("" when not installed,
// mirroring the darwin contract).
func Status() (string, error) {
	m, err := mgr.ConnectRemote("")
	if err != nil {
		return "", nil
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return "", nil
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return "", nil
	}
	state := map[svc.State]string{
		svc.Stopped: "stopped", svc.StartPending: "start pending",
		svc.StopPending: "stop pending", svc.Running: "running",
		svc.ContinuePending: "continue pending", svc.PausePending: "pause pending",
		svc.Paused: "paused",
	}[st.State]
	if state == "" {
		state = fmt.Sprintf("state-%d", st.State)
	}
	return fmt.Sprintf("%s: %s (pid %d)\nconfig: %s\n", ServiceName, state, st.ProcessId, serviceConfigPath()), nil
}

// Describe names the installed unit for CLI output.
func Describe() string {
	return "Windows service '" + ServiceName + "' (config: " + serviceConfigPath() + ")"
}

// RunServe runs serveFn in the foreground (stopped by Ctrl+C /
// SIGTERM) or, when the Service Control Manager launched us, as a
// proper Windows Service. Service mode has no console: logs go to
// %ProgramData%\minipulse\logs\minipulse.log.
func RunServe(serveFn func(ctx context.Context) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return serveFn(ctx)
	}
	if f, err := openServiceLog(); err == nil {
		defer f.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(f, nil)))
	}
	return svc.Run(ServiceName, &serviceHandler{serveFn: serveFn})
}

func openServiceLog() (*os.File, error) {
	dir := filepath.Join(programDataDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "minipulse.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

type serviceHandler struct {
	serveFn func(ctx context.Context) error
}

func (h *serviceHandler) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.serveFn(ctx) }()
	s <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case err := <-done:
			cancel()
			if err != nil {
				slog.Error("serve exited", "err", err)
				return false, 1
			}
			return false, 0
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			default:
				slog.Warn("unexpected service control request", "cmd", c.Cmd)
			}
		}
	}
}
