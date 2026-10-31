package collect

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/ricardotech/minipulse/internal/model"
)

// Absolute path, like lsof/scutil below: never resolve system
// tools through a PATH an attacker-writable directory could lead.
const launchctlPath = "/bin/launchctl"

// SystemServices lists user-domain launchd jobs (`launchctl list`).
// System com.apple.* jobs are included but the UI filters them by
// default — a server box cares about the jobs its owner installed.
func SystemServices(ctx context.Context) ([]model.Service, error) {
	out, err := exec.CommandContext(ctx, launchctlPath, "list").Output()
	if err != nil {
		return nil, err
	}
	return ParseLaunchctl(string(out)), nil
}

// ParseLaunchctl parses `launchctl list` output ("PID Status Label").
func ParseLaunchctl(text string) []model.Service {
	var out []model.Service
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] == "PID" {
			continue
		}
		pid, _ := strconv.Atoi(f[0]) // "-" → 0 (not running)
		status, _ := strconv.Atoi(f[1])
		out = append(out, model.Service{Label: f[2], PID: pid, Status: status})
	}
	return out
}

// lsofLine matches: COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
var lsofName = regexp.MustCompile(`^(.*):(\d+)$`)

// Listeners lists TCP ports in LISTEN state via lsof (present on every
// Mac). Without sudo only the current user's processes are visible —
// acceptable for a single-user server box, and documented.
func Listeners(ctx context.Context) ([]model.Listener, error) {
	// Absolute path: lsof lives in /usr/sbin, which a launchd agent's
	// minimal PATH does not include.
	out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iTCP", "-sTCP:LISTEN").Output()
	if err != nil {
		return nil, err
	}
	return ParseLsof(string(out)), nil
}

// ParseLsof parses default lsof output.
func ParseLsof(text string) []model.Listener {
	var out []model.Listener
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 9 || f[0] == "COMMAND" {
			continue
		}
		name := f[len(f)-1]
		// "(LISTEN)" trails the NAME column on some macOS versions.
		if name == "(LISTEN)" && len(f) >= 10 {
			name = f[len(f)-2]
		}
		m := lsofName.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		port, _ := strconv.Atoi(m[2])
		pid, _ := strconv.Atoi(f[1])
		key := f[0] + "/" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, model.Listener{Process: f[0], PID: pid, Addr: m[1], Port: port})
	}
	return out
}
