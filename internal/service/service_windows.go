package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
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

// programFilesBinary is where Install copies the executable: a
// LocalSystem service must never point at a binary in a
// user-writable directory (Downloads etc.) — any process running
// as that user could replace it and inherit SYSTEM at next boot.
func programFilesBinary() string {
	base := os.Getenv("ProgramFiles")
	if base == "" {
		base = `C:\Program Files`
	}
	return filepath.Join(base, "minipulse", "minipulse.exe")
}

// restrictProgramData locks %ProgramData%\minipulse down to SYSTEM
// + Administrators. %ProgramData% itself is world-readable and
// standard users can create (and then own) new subdirectories
// there, so without a protected DACL the service config (API
// token), database and logs would be readable by every local user,
// and a pre-planted config would be honored by a LocalSystem
// service. POSIX 0o600/0o700 modes are no-ops on NTFS.
func restrictProgramData() error {
	dir := programDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("O:SYD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return fmt.Errorf("build security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read security descriptor: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("restrict ACL on %s: %w", dir, err)
	}
	return nil
}

// Install registers MiniPulse as an automatic Windows Service
// (restart on failure, like launchd's KeepAlive) and starts it.
// The binary is copied to %ProgramFiles%\minipulse (admin-only
// writable) and the config it runs with lives in
// %ProgramData%\minipulse behind a SYSTEM+Administrators DACL.
func Install(binary, configPath string) error {
	m, err := mgr.ConnectRemote("")
	if err != nil {
		return fmt.Errorf("connect service manager (run as Administrator?): %w", err)
	}
	defer m.Disconnect()

	reinstall := false
	if old, err := m.OpenService(ServiceName); err == nil {
		reinstall = true
		if err := stopAndWait(old); err != nil {
			old.Close()
			return fmt.Errorf("stop existing service: %w", err)
		}
		if err := old.Delete(); err != nil {
			old.Close()
			return fmt.Errorf("delete existing service: %w", err)
		}
		old.Close()
	}
	// Config prep happens only after elevation is proven, and a
	// config found in %ProgramData% with no installed service is
	// foreign (possibly pre-planted by a standard user, who can
	// create directories there) — only a reinstall may keep it.
	cfgPath, err := prepareServiceConfig(configPath, reinstall)
	if err != nil {
		return err
	}
	exe, err := installBinary(binary)
	if err != nil {
		return err
	}
	s, err := m.CreateService(ServiceName, exe, mgr.Config{
		DisplayName: "MiniPulse",
		Description: "Tiny always-on system dashboard (CPU/RAM/disk) — http://127.0.0.1:8848",
		StartType:   mgr.StartAutomatic,
	}, "serve", "--config", cfgPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	// Restart on crash after 5 s, reset the failure counter daily.
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, uint32((24 * time.Hour).Seconds())); err != nil {
		return fmt.Errorf("set recovery actions: %w", err)
	}
	if err := s.Start(); err != nil {
		_ = s.Delete() // roll back: no crash-looping auto-start service
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// installBinary copies the running executable into
// %ProgramFiles%\minipulse (writable by Administrators only) and
// returns the path the SCM should register.
func installBinary(binary string) (string, error) {
	dst := programFilesBinary()
	if filepath.Clean(binary) == filepath.Clean(dst) {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	src, err := os.Open(binary)
	if err != nil {
		return "", err
	}
	defer src.Close()
	var out *os.File
	for attempt := 0; attempt < 10; attempt++ {
		// A just-stopped previous service may hold the file briefly.
		out, err = os.Create(dst)
		if err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		return "", fmt.Errorf("install binary to %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return "", err
	}
	return dst, nil
}

// prepareServiceConfig creates %ProgramData%\minipulse\config.json
// (copying the user's config when present) behind a restricted
// DACL and returns its path. keepExisting may only be true on a
// reinstall, when the file's provenance is our own previous install.
func prepareServiceConfig(userConfigPath string, keepExisting bool) (string, error) {
	if err := restrictProgramData(); err != nil {
		return "", err
	}
	dst := serviceConfigPath()
	if keepExisting {
		if _, err := os.Stat(dst); err == nil {
			return dst, nil
		}
	}
	generated := false
	if data, err := os.ReadFile(userConfigPath); err == nil {
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return "", err
		}
	} else {
		if err := config.WriteExample(dst); err != nil {
			return "", err
		}
		generated = true
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		return "", err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("parse %s: %w", dst, err)
	}
	changed := false
	// Pin data_dir next to the service config unless the *user's*
	// config names a real one. Empty and ~-relative values would
	// resolve into the LocalSystem profile (or the System32 CWD),
	// and a generated example carries the installing user's
	// per-user default.
	if dd, _ := m["data_dir"].(string); generated || strings.TrimSpace(dd) == "" || strings.HasPrefix(dd, "~") {
		m["data_dir"] = filepath.Join(programDataDir(), "data")
		changed = true
	}
	// A generated example scans the installer's home; the service
	// equivalently wants every user's home (it runs as SYSTEM).
	if generated {
		drive := os.Getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		m["disk_scan_roots"] = []string{filepath.Join(drive+`\`, "Users")}
		changed = true
	}
	if changed {
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
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil // not installed
		}
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()
	if err := stopAndWait(s); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
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
		return "", fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return "", nil // not installed (darwin contract)
		}
		return "", fmt.Errorf("open service: %w", err)
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return "", fmt.Errorf("query service: %w", err)
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
	path := filepath.Join(dir, "minipulse.log")
	if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
		_ = os.Rename(path, path+".old") // one generation is enough
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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
