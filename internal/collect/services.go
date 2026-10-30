package collect

import (
	"context"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
)

// LaunchdServices lists user-domain launchd jobs (`launchctl list`).
// System com.apple.* jobs are included but the UI filters them by
// default — a server box cares about the jobs its owner installed.
func LaunchdServices(ctx context.Context) ([]model.Service, error) {
	// Absolute path, like lsof/scutil below: never resolve system
	// tools through a PATH an attacker-writable directory could lead.
	out, err := exec.CommandContext(ctx, "/bin/launchctl", "list").Output()
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

// HealthWatcher polls configured HTTP endpoints on an interval and
// reports state changes through the onChange callback.
type HealthWatcher struct {
	checks   []config.HealthCheckCfg
	client   *http.Client
	mu       sync.RWMutex
	results  []model.HealthCheck
	onChange func(model.HealthCheck)
}

func NewHealthWatcher(checks []config.HealthCheckCfg, onChange func(model.HealthCheck)) *HealthWatcher {
	return &HealthWatcher{
		checks:   checks,
		client:   &http.Client{Timeout: 5 * time.Second},
		onChange: onChange,
	}
}

// Run polls until ctx is cancelled. First round runs immediately.
func (h *HealthWatcher) Run(ctx context.Context, interval time.Duration) {
	if len(h.checks) == 0 {
		return
	}
	h.poll(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.poll(ctx)
		}
	}
}

// Results returns the latest result per check.
func (h *HealthWatcher) Results() []model.HealthCheck {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]model.HealthCheck, len(h.results))
	copy(out, h.results)
	return out
}

func (h *HealthWatcher) poll(ctx context.Context) {
	h.mu.Lock()
	prev := map[string]bool{}
	for _, r := range h.results {
		prev[r.Name] = r.Up
	}
	h.mu.Unlock()

	results := make([]model.HealthCheck, 0, len(h.checks))
	for _, c := range h.checks {
		r := h.checkOne(ctx, c)
		if old, known := prev[c.Name]; known && old != r.Up && h.onChange != nil {
			h.onChange(r)
		}
		results = append(results, r)
	}
	h.mu.Lock()
	h.results = results
	h.mu.Unlock()
}

func (h *HealthWatcher) checkOne(ctx context.Context, c config.HealthCheckCfg) model.HealthCheck {
	r := model.HealthCheck{Name: c.Name, URL: c.URL, Checked: time.Now()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	start := time.Now()
	resp, err := h.client.Do(req)
	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer resp.Body.Close()
	r.Status = resp.StatusCode
	if c.ExpectStatus != 0 {
		r.Up = resp.StatusCode == c.ExpectStatus
	} else {
		r.Up = resp.StatusCode >= 200 && resp.StatusCode < 400
	}
	return r
}
