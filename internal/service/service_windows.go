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
	"unsafe"

	"github.com/Microsoft/go-winio"
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
// service. The owner matters as much as the DACL: an owner keeps
// WRITE_DAC implicitly whatever the DACL says, so ownership is
// forced to SYSTEM too (SeTakeOwnershipPrivilege). POSIX
// 0o600/0o700 modes are no-ops on NTFS. Taking ownership of a
// directory area an admin is actively installing a service under
// is proportionate; failing to do so fails the install closed
// rather than installing into an attacker-controlled tree.
func restrictProgramData() error {
	dir := programDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Owner goes to Administrators, not SYSTEM: assigning SYSTEM
	// as owner needs SeRestorePrivilege, while an admin may
	// always assign a group they belong to. Either way the
	// pre-planter stops being owner and loses implicit WRITE_DAC.
	sd, err := windows.SecurityDescriptorFromString("O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return fmt.Errorf("build security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read security descriptor: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read security descriptor owner: %w", err)
	}
	apply := func() error {
		return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
			windows.OWNER_SECURITY_INFORMATION|
				windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			owner, nil, dacl, nil)
	}
	if err := winio.RunWithPrivilege("SeTakeOwnershipPrivilege", func() error { return apply() }); err != nil {
		return fmt.Errorf("restrict ACL on %s: %w", dir, err)
	}
	return nil
}

// programDataOwnedByUs reports whether the existing
// %ProgramData%\minipulse directory belongs to SYSTEM or
// Administrators — the verifiable provenance of a previous
// install of ours (a pre-planted or foreign directory is owned
// by whoever created it).
func programDataOwnedByUs() bool {
	sd, err := windows.GetNamedSecurityInfo(programDataDir(), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false
	}
	return owner.String() == "S-1-5-18" || owner.String() == "S-1-5-32-544"
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

	var old *mgr.Service
	if o, err := m.OpenService(ServiceName); err == nil {
		old = o
		defer old.Close()
	}
	// Prepare everything fallible while the old service is still
	// intact, so a bad config or a locked binary cannot leave the
	// machine with no service at all. An existing service config is
	// only kept on a reinstall over a live service record: with no
	// service installed, a config found in %ProgramData% is foreign
	// (standard users can create directories there, and a directory
	// left behind by an uninstall is already admin-owned, so
	// ownership alone proves nothing).
	cfgPath, err := prepareServiceConfig(configPath, old != nil)
	if err != nil {
		return err
	}
	staged, exe, err := stageBinary(binary)
	if err != nil {
		return err
	}
	if old != nil {
		if err := stopAndWait(old); err != nil {
			return fmt.Errorf("stop existing service: %w", err)
		}
		if err := old.Delete(); err != nil {
			return fmt.Errorf("delete existing service: %w", err)
		}
	}
	if staged {
		if err := moveStagedBinary(exe); err != nil {
			return err
		}
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
		_ = s.Delete() // roll back: no half-registered service
		return fmt.Errorf("set recovery actions: %w", err)
	}
	if err := s.Start(); err != nil {
		_ = s.Delete() // roll back: no crash-looping auto-start service
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// stageBinary copies the running executable next to its final
// %ProgramFiles%\minipulse location as minipulse.exe.new (staged
// reports whether finalizeBinary still has to move it into place,
// which must wait until the old service is stopped).
func stageBinary(binary string) (staged bool, finalPath string, err error) {
	dst := programFilesBinary()
	srcInfo, err := os.Stat(binary)
	if err != nil {
		return false, "", err
	}
	if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(srcInfo, dstInfo) {
		return false, dst, nil // reinstalling from the installed binary itself
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, "", err
	}
	src, err := os.Open(binary)
	if err != nil {
		return false, "", err
	}
	defer src.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return false, "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return false, "", err
	}
	return true, dst, nil
}

func moveStagedBinary(dst string) error {
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		// A just-stopped previous service may hold the file briefly.
		if err = windows.MoveFileEx(windows.StringToUTF16Ptr(dst+".new"),
			windows.StringToUTF16Ptr(dst), windows.MOVEFILE_REPLACE_EXISTING); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("install binary to %s: %w", dst, err)
}

// prepareServiceConfig creates %ProgramData%\minipulse\config.json
// (copying the user's config when present) behind a restricted
// DACL and returns its path. keepExisting may only be true when
// the directory verifiably came from a previous install (see
// programDataOwnedByUs).
func prepareServiceConfig(userConfigPath string, keepExisting bool) (string, error) {
	if err := restrictProgramData(); err != nil {
		return "", err
	}
	dst := serviceConfigPath()
	if keepExisting {
		if _, err := os.Stat(dst); err == nil {
			if err := normalizeServiceConfig(dst, false); err != nil {
				return "", err
			}
			return dst, nil
		}
	}
	// Keep a backup of whatever is being replaced (a config left
	// behind by an uninstall is foreign for trust purposes, but
	// its token/settings may still be wanted back), then unlink:
	// truncating a pre-planted file would preserve its DACL (and
	// its owner's read/write access).
	if prev, err := os.ReadFile(dst); err == nil {
		_ = os.WriteFile(dst+".bak", prev, 0o600)
	}
	_ = os.Remove(dst)
	generated := false
	if data, err := os.ReadFile(userConfigPath); err == nil {
		// Validate before anything downstream trusts this copy.
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return "", fmt.Errorf("parse %s: %w", userConfigPath, err)
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return "", err
		}
	} else {
		if err := config.WriteExample(dst); err != nil {
			return "", err
		}
		generated = true
	}
	if err := normalizeServiceConfig(dst, generated); err != nil {
		return "", err
	}
	return dst, nil
}

// normalizeServiceConfig pins service-mode paths in the config at
// path: data_dir next to the service config (unless the user named
// a real absolute one that is not simply their per-user default,
// which under LocalSystem would bury the database in the
// installer's profile), and — for generated configs — the disk
// roots at every user's home (the example names the installer's).
func normalizeServiceConfig(path string, generated bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	changed := false
	dd, _ := m["data_dir"].(string)
	expanded := config.ExpandHome(strings.TrimSpace(dd))
	if generated || dd == "" || expanded != strings.TrimSpace(dd) ||
		!filepath.IsAbs(expanded) || samePath(expanded, config.DefaultDataDir()) {
		m["data_dir"] = filepath.Join(programDataDir(), "data")
		changed = true
	}
	if generated {
		drive := os.Getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		m["disk_scan_roots"] = []string{filepath.Join(drive+`\`, "Users")}
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
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
	if st, err := s.Query(); err == nil && st.State == svc.Stopped {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return nil
		}
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

// Status prints the SCM view of the service ("" when not
// installed, mirroring the darwin contract). Uses connect +
// query-status rights only, so it works from a non-elevated
// shell — the mgr package would ask for SC_MANAGER_ALL_ACCESS.
func Status() (string, error) {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "", fmt.Errorf("connect service manager: %w", err)
	}
	defer windows.CloseServiceHandle(h)
	namePtr, err := windows.UTF16PtrFromString(ServiceName)
	if err != nil {
		return "", err
	}
	s, err := windows.OpenService(h, namePtr, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return "", nil // not installed (darwin contract)
		}
		return "", fmt.Errorf("open service: %w", err)
	}
	defer windows.CloseServiceHandle(s)
	var needed uint32
	buf := make([]byte, 1024)
	if err := windows.QueryServiceStatusEx(s, windows.SC_STATUS_PROCESS_INFO,
		&buf[0], uint32(len(buf)), &needed); err != nil {
		return "", fmt.Errorf("query service: %w", err)
	}
	st := (*windows.SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buf[0]))
	state := map[uint32]string{
		windows.SERVICE_STOPPED: "stopped", windows.SERVICE_START_PENDING: "start pending",
		windows.SERVICE_STOP_PENDING: "stop pending", windows.SERVICE_RUNNING: "running",
		windows.SERVICE_CONTINUE_PENDING: "continue pending", windows.SERVICE_PAUSE_PENDING: "pause pending",
		windows.SERVICE_PAUSED: "paused",
	}[st.CurrentState]
	if state == "" {
		state = fmt.Sprintf("state-%d", st.CurrentState)
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
		_ = os.Remove(path + ".old") // os.Rename does not replace on Windows
		_ = os.Rename(path, path+".old")
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
