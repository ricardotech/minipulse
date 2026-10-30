package collect

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ricardotech/minipulse/internal/alert"
	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
	"github.com/ricardotech/minipulse/internal/store"
)

// Collector owns the sampling loops: system metrics every interval,
// disk scans on a slower cadence, health checks, alert evaluation and
// daily pruning. Subscribers (SSE clients) receive every new sample.
type Collector struct {
	cfg    *config.Config
	store  *store.Store
	sys    *System
	power  *PowerSource
	health *HealthWatcher
	docker *Docker
	eval   *alert.Evaluator

	mu       sync.RWMutex
	latest   model.Sample
	haveData bool
	subs     map[chan model.Sample]struct{}

	whTotal   float64
	lastPower time.Time

	scanMu      sync.Mutex
	scanRunning bool
}

func New(cfg *config.Config, st *store.Store) *Collector {
	c := &Collector{
		cfg: cfg, store: st, sys: NewSystem(),
		eval: alert.New(cfg), subs: map[chan model.Sample]struct{}{},
	}
	if wh, err := st.LastWhTotal(); err == nil {
		c.whTotal = wh
	}
	c.health = NewHealthWatcher(cfg.HealthChecks, func(r model.HealthCheck) {
		kind := "alert"
		title := "Service down: " + r.Name
		if r.Up {
			kind, title = "ok", "Service up: "+r.Name
		}
		_ = st.AddEvent(kind, title, r.URL+" → "+r.Error)
		if cfg.AlertWebhookURL != "" {
			_ = alert.Send(context.Background(), cfg.AlertWebhookURL,
				alert.Notice{Title: title, Detail: r.URL, Recovery: r.Up})
		}
	})
	return c
}

// Run blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	c.power = NewPowerSource(ctx)
	c.docker = NewDocker()
	go c.health.Run(ctx, 30*time.Second)
	go c.diskLoop(ctx)
	go c.pruneLoop(ctx)

	interval := time.Duration(c.cfg.SampleIntervalSeconds) * time.Second
	c.tick(ctx) // first sample immediately so the UI never starts empty
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

func (c *Collector) tick(ctx context.Context) {
	now := time.Now()
	sm := c.sys.Sample(now)
	sm.Power = c.power.Latest()
	// Integrate system power into the cumulative watt-hour counter.
	// Cap dt so a long sleep doesn't invent energy that never happened.
	if sm.Power.Available && !c.lastPower.IsZero() {
		dt := now.Sub(c.lastPower).Seconds()
		if dt > 0 && dt <= 120 {
			c.whTotal += sm.Power.SysW * dt / 3600
		}
	}
	c.lastPower = now
	sm.WhTotal = c.whTotal

	if err := c.store.InsertSample(sm); err != nil {
		slog.Warn("store sample", "err", err)
	}
	for _, n := range c.eval.Evaluate(sm, now) {
		kind := "alert"
		if n.Recovery {
			kind = "ok"
		}
		_ = c.store.AddEvent(kind, n.Title, n.Detail)
		if err := alert.Send(ctx, c.cfg.AlertWebhookURL, n); err != nil {
			slog.Warn("send alert", "err", err)
		}
	}

	c.mu.Lock()
	c.latest, c.haveData = sm, true
	c.mu.Unlock()
	c.broadcast(sm)
}

func (c *Collector) broadcast(sm model.Sample) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for ch := range c.subs {
		select {
		case ch <- sm:
		default: // slow consumer: drop, it can resync via /api/overview
		}
	}
}

// Subscribe registers an SSE consumer. Call the returned func to leave.
func (c *Collector) Subscribe() (<-chan model.Sample, func()) {
	ch := make(chan model.Sample, 4)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

// Latest returns the most recent sample.
func (c *Collector) Latest() (model.Sample, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest, c.haveData
}

// TopProcesses proxies the (cached) process walk.
func (c *Collector) TopProcesses(byCPU bool, limit int) ([]model.Process, error) {
	return c.sys.TopProcesses(byCPU, limit)
}

// Containers returns Docker containers, or nil when no runtime is up.
func (c *Collector) Containers(ctx context.Context) ([]model.Container, error) {
	if c.docker == nil {
		c.docker = NewDocker() // runtime may have started after us
		if c.docker == nil {
			return nil, nil
		}
	}
	out, err := c.docker.Containers(ctx)
	if err != nil {
		c.docker = nil // socket went away; rediscover next call
		return nil, nil
	}
	return out, nil
}

// HealthResults returns the latest health-check results.
func (c *Collector) HealthResults() []model.HealthCheck { return c.health.Results() }

// ScanRunning reports whether a disk scan is in progress.
func (c *Collector) ScanRunning() bool {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	return c.scanRunning
}

// TriggerScan starts a disk scan of every configured root in the
// background. Returns false when one is already running.
func (c *Collector) TriggerScan() bool {
	c.scanMu.Lock()
	if c.scanRunning {
		c.scanMu.Unlock()
		return false
	}
	c.scanRunning = true
	c.scanMu.Unlock()
	go func() {
		defer func() {
			c.scanMu.Lock()
			c.scanRunning = false
			c.scanMu.Unlock()
		}()
		for _, root := range c.cfg.DiskScanRoots {
			c.scanOne(root)
		}
	}()
	return true
}

func (c *Collector) scanOne(root string) {
	slog.Info("disk scan started", "root", root)
	res := ScanDir(root, c.cfg.DiskMinEntryBytes)
	id, err := c.store.SaveDiskScan(res.Scan, res.Entries)
	if err != nil {
		slog.Warn("save disk scan", "root", root, "err", err)
		return
	}
	slog.Info("disk scan done", "root", root, "scan_id", id,
		"bytes", res.Scan.TotalBytes, "files", res.Scan.FileCount,
		"ms", res.Scan.DurationMs)
	// A multi-million-file walk inflates the heap; hand it back to the
	// OS so an always-on agent stays small between scans.
	debug.FreeOSMemory()
}

// scansStale reports whether any configured root lacks a scan newer
// than the configured interval.
func (c *Collector) scansStale() bool {
	interval := time.Duration(c.cfg.DiskScanIntervalMinutes) * time.Minute
	for _, root := range c.cfg.DiskScanRoots {
		latest, err := c.store.LatestScan(root)
		if err != nil || latest == nil {
			return true
		}
		if interval > 0 && time.Since(latest.Time) > interval {
			return true
		}
	}
	return false
}

func (c *Collector) diskLoop(ctx context.Context) {
	// Scan soon after boot only when the newest scan is missing or
	// stale — restarting the agent must not re-walk millions of files.
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}
	if c.scansStale() {
		c.TriggerScan()
	}
	every := time.Duration(c.cfg.DiskScanIntervalMinutes) * time.Minute
	if every <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.TriggerScan()
		}
	}
}

func (c *Collector) pruneLoop(ctx context.Context) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		if err := c.store.Prune(c.cfg.RetentionDays, 30); err != nil {
			slog.Warn("prune", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
