// Package api serves the dashboard UI, the JSON API, the SSE stream
// and a Prometheus /metrics endpoint. Standard library only.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ricardotech/minipulse/internal/collect"
	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
	"github.com/ricardotech/minipulse/internal/store"
)

// Server bundles the HTTP handlers with their dependencies.
type Server struct {
	cfg   *config.Config
	store *store.Store
	coll  *collect.Collector
	host  model.HostInfo
	web   fs.FS
}

func New(cfg *config.Config, st *store.Store, coll *collect.Collector) *Server {
	return &Server{cfg: cfg, store: st, coll: coll, host: collect.HostInfo(), web: WebFS()}
}

// Handler builds the full route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.Handle("GET /api/overview", s.auth(http.HandlerFunc(s.overview)))
	mux.Handle("GET /api/stream", s.auth(http.HandlerFunc(s.stream)))
	mux.Handle("GET /api/history", s.auth(http.HandlerFunc(s.history)))
	mux.Handle("GET /api/processes", s.auth(http.HandlerFunc(s.processes)))
	mux.Handle("GET /api/energy", s.auth(http.HandlerFunc(s.energy)))
	mux.Handle("GET /api/disk", s.auth(http.HandlerFunc(s.disk)))
	mux.Handle("POST /api/disk/scan", s.auth(http.HandlerFunc(s.diskScan)))
	mux.Handle("GET /api/containers", s.auth(http.HandlerFunc(s.containers)))
	mux.Handle("GET /api/services", s.auth(http.HandlerFunc(s.services)))
	mux.Handle("GET /api/events", s.auth(http.HandlerFunc(s.events)))
	mux.Handle("GET /metrics", s.auth(http.HandlerFunc(s.metrics)))
	mux.Handle("/", http.FileServer(http.FS(s.web)))
	return logRequests(mux)
}

// auth requires the configured token (when set) via Authorization
// header, X-MiniPulse-Token, cookie, or ?token= (which sets a cookie
// so browser users only paste it once).
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.cfg.Token
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-MiniPulse-Token")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if got == "" {
			if c, err := r.Cookie("mp_token"); err == nil {
				got = c.Value
			}
		}
		if q := r.URL.Query().Get("token"); q != "" {
			got = q
			http.SetCookie(w, &http.Cookie{Name: "mp_token", Value: q, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or missing token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	latest, ok := s.coll.Latest()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "warming up"})
		return
	}
	// Refresh uptime cheaply instead of re-reading host info every call.
	host := s.host
	now := time.Now()
	today, _ := s.store.EnergyBetween(startOfDay(now), now.Add(time.Second))
	writeJSON(w, http.StatusOK, map[string]any{
		"host":          host,
		"sample":        latest,
		"energy_today":  energyView(today, s.cfg),
		"currency":      s.cfg.Currency,
		"price_per_kwh": s.cfg.EnergyPricePerKWh,
		"scan_running":  s.coll.ScanRunning(),
		"health":        s.coll.HealthResults(),
	})
}

// stream is the SSE feed: the current sample is sent immediately,
// then one event per sample tick (server-paced), with heartbeats.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, leave := s.coll.Subscribe()
	defer leave()
	if latest, ok := s.coll.Latest(); ok {
		writeSSE(w, latest)
		flusher.Flush()
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case sm := <-ch:
			writeSSE(w, sm)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	dur, step := rangeParams(rangeName)
	now := time.Now()
	pts, err := s.store.History(now.Add(-dur), now, step)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rangeName, "step_s": step, "points": pts})
}

// rangeParams maps a range name to a window and bucket size so charts
// always get a few hundred points at most.
func rangeParams(name string) (time.Duration, int64) {
	switch name {
	case "6h":
		return 6 * time.Hour, 30
	case "24h":
		return 24 * time.Hour, 120
	case "7d":
		return 7 * 24 * time.Hour, 900
	case "30d":
		return 30 * 24 * time.Hour, 3600
	default:
		return time.Hour, 5
	}
}

func (s *Server) processes(w http.ResponseWriter, r *http.Request) {
	byCPU := r.URL.Query().Get("sort") == "cpu"
	limit := intParam(r, "limit", 15)
	procs, err := s.coll.TopProcesses(byCPU, limit)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, procs)
}

func (s *Server) energy(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	var from time.Time
	switch r.URL.Query().Get("range") {
	case "month":
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	case "7d":
		from = startOfDay(now).AddDate(0, 0, -6)
	default:
		from = startOfDay(now)
	}
	e, err := s.store.EnergyBetween(from, now.Add(time.Second))
	if err != nil {
		serverErr(w, err)
		return
	}
	// Per-day breakdown for the chart.
	days := []map[string]any{}
	for d := from; d.Before(now) || sameDay(d, now); d = d.AddDate(0, 0, 1) {
		de, err := s.store.EnergyBetween(d, minTime(d.AddDate(0, 0, 1), now.Add(time.Second)))
		if err == nil && de.Samples > 0 {
			days = append(days, map[string]any{
				"day": d.Format("2006-01-02"), "kwh": de.Wh / 1000,
				"cost": de.Wh / 1000 * s.cfg.EnergyPricePerKWh, "avg_w": de.AvgW,
			})
		}
		if sameDay(d, now) {
			break
		}
	}
	view := energyView(e, s.cfg)
	view["days"] = days
	writeJSON(w, http.StatusOK, view)
}

func energyView(e store.Energy, cfg *config.Config) map[string]any {
	return map[string]any{
		"kwh": e.Wh / 1000, "avg_w": e.AvgW, "peak_w": e.PeakW,
		"cost": e.Wh / 1000 * cfg.EnergyPricePerKWh, "currency": cfg.Currency,
		"co2_g": e.Wh / 1000 * cfg.GridCO2PerKWh,
	}
}

// disk serves the disk deep-dive: ?root=&view=tree|growth|biggest&parent=
func (s *Server) disk(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	roots, err := s.store.ScannedRoots()
	if err != nil {
		serverErr(w, err)
		return
	}
	root := q.Get("root")
	if root == "" && len(roots) > 0 {
		root = roots[0]
		// Prefer the configured roots order when available.
		for _, cr := range s.cfg.DiskScanRoots {
			for _, rr := range roots {
				if rr == cr {
					root = cr
				}
			}
		}
	}
	latest, err := s.store.LatestScan(root)
	if err != nil {
		serverErr(w, err)
		return
	}
	if latest == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"roots": roots, "root": root, "scan": nil,
			"scan_running": s.coll.ScanRunning(),
			"hint":         "no scan yet — POST /api/disk/scan or wait for the scheduled one",
		})
		return
	}
	out := map[string]any{"roots": roots, "root": root, "scan": latest, "scan_running": s.coll.ScanRunning()}
	switch q.Get("view") {
	case "growth":
		prev, err := s.store.ScanBefore(root, latest.Time)
		if err != nil {
			serverErr(w, err)
			return
		}
		if prev == nil {
			out["growth"] = []model.Growth{}
			out["previous_scan"] = nil
			break
		}
		g, err := s.store.Growth(prev.ID, latest.ID, 50)
		if err != nil {
			serverErr(w, err)
			return
		}
		out["growth"], out["previous_scan"] = g, prev
	case "biggest":
		files, err := s.store.BiggestFiles(latest.ID, intParam(r, "limit", 50))
		if err != nil {
			serverErr(w, err)
			return
		}
		out["files"] = files
	default: // tree
		parent := q.Get("parent")
		if parent == "" {
			parent = root
		}
		children, err := s.store.Children(latest.ID, parent, 200)
		if err != nil {
			serverErr(w, err)
			return
		}
		out["parent"] = parent
		out["children"] = children
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) diskScan(w http.ResponseWriter, r *http.Request) {
	started := s.coll.TriggerScan()
	status := http.StatusAccepted
	if !started {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"started": started, "scan_running": true})
}

func (s *Server) containers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, err := s.coll.Containers(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	available := list != nil // nil means no Docker runtime was found
	if list == nil {
		list = []model.Container{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": available, "containers": list})
}

func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	svcs, err := collect.LaunchdServices(ctx)
	if err != nil {
		serverErr(w, err)
		return
	}
	// Server boxes care about their own jobs and anything running.
	var filtered []model.Service
	for _, sv := range svcs {
		if !strings.HasPrefix(sv.Label, "com.apple.") || sv.PID != 0 {
			filtered = append(filtered, sv)
		}
	}
	listeners, _ := collect.Listeners(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"services": filtered, "listeners": listeners, "health": s.coll.HealthResults(),
	})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	ev, err := s.store.Events(intParam(r, "limit", 50))
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

// metrics renders the latest sample in Prometheus text format for
// people who already run Prometheus/Grafana.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	sm, ok := s.coll.Latest()
	if !ok {
		http.Error(w, "warming up", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	var b strings.Builder
	gauge := func(name, help string, v float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", name, help, name, name, v)
	}
	gauge("minipulse_cpu_pct", "Overall CPU usage percent", sm.CPUPct)
	gauge("minipulse_mem_used_bytes", "RAM used in bytes", float64(sm.MemUsed))
	gauge("minipulse_mem_pct", "RAM used percent", sm.MemPct)
	gauge("minipulse_swap_used_bytes", "Swap used in bytes", float64(sm.SwapUsed))
	gauge("minipulse_load1", "1-minute load average", sm.Load1)
	gauge("minipulse_net_rx_bps", "Network receive bytes/sec", sm.NetRxBps)
	gauge("minipulse_net_tx_bps", "Network transmit bytes/sec", sm.NetTxBps)
	gauge("minipulse_disk_read_bps", "Disk read bytes/sec", sm.DiskReadBps)
	gauge("minipulse_disk_write_bps", "Disk write bytes/sec", sm.DiskWritBps)
	if sm.Power.Available {
		gauge("minipulse_power_sys_watts", "Whole-system power draw", sm.Power.SysW)
		gauge("minipulse_power_soc_watts", "SoC power draw", sm.Power.SoCW)
		gauge("minipulse_cpu_temp_celsius", "CPU temperature", sm.Power.CPUTempC)
		gauge("minipulse_gpu_temp_celsius", "GPU temperature", sm.Power.GPUTempC)
		gauge("minipulse_fan_rpm", "Fan speed", float64(sm.Power.FanRPM))
	}
	fmt.Fprintf(&b, "# HELP minipulse_energy_wh_total Cumulative watt-hours\n# TYPE minipulse_energy_wh_total counter\nminipulse_energy_wh_total %g\n", sm.WhTotal)
	for _, v := range sm.Volumes {
		fmt.Fprintf(&b, "minipulse_volume_used_bytes{mount=%q} %d\nminipulse_volume_total_bytes{mount=%q} %d\n", v.Mount, v.Used, v.Mount, v.Total)
	}
	fmt.Fprint(w, b.String())
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func serverErr(w http.ResponseWriter, err error) {
	slog.Warn("api", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stream" {
			slog.Debug("http", "method", r.Method, "path", r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
