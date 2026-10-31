package collect

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
)

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
