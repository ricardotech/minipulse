package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ricardotech/minipulse/internal/collect"
	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
	"github.com/ricardotech/minipulse/internal/store"
)

func TestMiniRouteAuthAndWarmingUp(t *testing.T) {
	cfg := config.Defaults()
	cfg.Token = "secret-token"
	h := testHandler(t, cfg)

	if rec := do(h, "GET", "http://127.0.0.1:8848/api/mini", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}
	// No sample collected yet: same warming-up contract as /api/overview.
	rec := do(h, "GET", "http://127.0.0.1:8848/api/mini", map[string]string{"X-MiniPulse-Token": "secret-token"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before first sample: got %d, want 503", rec.Code)
	}
	// Non-GET falls through to the static catch-all (same as every
	// other GET route in this mux): it must never serve the payload.
	if rec := do(h, "POST", "http://127.0.0.1:8848/api/mini", map[string]string{"X-MiniPulse-Token": "secret-token"}); rec.Code != http.StatusNotFound {
		t.Errorf("POST: got %d, want 404 from static catch-all", rec.Code)
	}
}

func TestMiniViewFlattensGlanceableFields(t *testing.T) {
	sm := model.Sample{
		Time: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC), CPUPct: 11.5, MemPct: 56,
		WhTotal: 196.1, CPUPerCore: []float64{1, 2}, Volumes: []model.Volume{{Mount: "/", Total: 200 << 30, Used: 130 << 30, Free: 70 << 30, UsedPct: 65}},
		Power: model.Power{Available: true, SysW: 31.2, SoCW: 8.4, CPUTempC: 63, GPUTempC: 58, FanRPM: 1000},
	}
	out := miniView(sm, nil)
	raw, _ := json.Marshal(out)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]float64{
		"sys_w": 31.2, "soc_w": 8.4, "cpu_pct": 11.5, "mem_pct": 56,
		"cpu_temp_c": 63, "gpu_temp_c": 58, "fan_rpm": 1000, "wh_total": 196.1,
		"disk_pct": 65, "disk_free_bytes": 70 << 30, "disk_total_bytes": 200 << 30,
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if got["power_available"] != true {
		t.Errorf("power_available = %v, want true", got["power_available"])
	}
	// The whole point of /api/mini: no heavy per-core/volume payloads.
	for _, banned := range []string{"cpu_per_core", "volumes", "cpu_tiers", "top"} {
		if _, ok := got[banned]; ok {
			t.Errorf("/api/mini must not include %q", banned)
		}
	}
	// Power unavailable must be explicit so clients show "—", not 0W.
	sm.Power = model.Power{Available: false, Reason: "no macmon"}
	out = miniView(sm, &model.Process{PID: 42, Name: "node", CPUPct: 120})
	if out["power_available"] != false {
		t.Errorf("power_available = %v, want false", out["power_available"])
	}
	top, ok := out["top"].(map[string]any)
	if !ok || top["name"] != "node" {
		t.Errorf("top = %v, want node process when requested", out["top"])
	}
}

func testHandler(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg.DiskScanRoots = []string{t.TempDir()}
	coll := collect.New(cfg, st)
	return New(cfg, st, coll).Handler()
}

func do(h http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTokenRequiredOnProcessHistory(t *testing.T) {
	cfg := config.Defaults()
	cfg.Token = "secret-token"
	h := testHandler(t, cfg)

	if rec := do(h, "GET", "http://127.0.0.1:8848/api/processes/history", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}
	rec := do(h, "GET", "http://127.0.0.1:8848/api/processes/history", map[string]string{"X-MiniPulse-Token": "secret-token"})
	if rec.Code != http.StatusOK {
		t.Errorf("valid token: got %d, want 200", rec.Code)
	}
}

func TestQueryTokenBootstrapsCookieOnlyWhenValid(t *testing.T) {
	cfg := config.Defaults()
	cfg.Token = "secret-token"
	h := testHandler(t, cfg)

	rec := do(h, "GET", "http://127.0.0.1:8848/?token=secret-token", nil)
	set := rec.Header().Get("Set-Cookie")
	if !strings.Contains(set, "mp_token=secret-token") || !strings.Contains(set, "HttpOnly") {
		t.Errorf("valid query token must set HttpOnly cookie, got %q", set)
	}
	// A wrong token must NOT mint a cookie (fixation: a malicious
	// link would otherwise overwrite the victim's good one).
	rec = do(h, "GET", "http://127.0.0.1:8848/?token=WRONG", nil)
	if set := rec.Header().Get("Set-Cookie"); set != "" {
		t.Errorf("invalid query token set cookie %q, want none", set)
	}
}

func TestHostGuardBlocksRebindingOnLoopback(t *testing.T) {
	cfg := config.Defaults() // loopback, no token
	h := testHandler(t, cfg)

	if rec := do(h, "GET", "http://evil.example:8848/api/overview", nil); rec.Code != http.StatusForbidden {
		t.Errorf("rebound host: got %d, want 403", rec.Code)
	}
	if rec := do(h, "GET", "http://127.0.0.1:8848/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("loopback host: got %d, want 200", rec.Code)
	}
	// Tailscale Serve forwards the tailnet hostname; it must pass.
	if rec := do(h, "GET", "http://mini.tailnet-1234.ts.net:8848/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("ts.net host: got %d, want 200", rec.Code)
	}
}

func TestOriginGuardBlocksCrossSitePost(t *testing.T) {
	cfg := config.Defaults()
	h := testHandler(t, cfg)

	rec := do(h, "POST", "http://127.0.0.1:8848/api/disk/scan", map[string]string{"Origin": "https://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST: got %d, want 403", rec.Code)
	}
	rec = do(h, "POST", "http://127.0.0.1:8848/api/disk/scan", map[string]string{"Origin": "http://127.0.0.1:8848"})
	if rec.Code == http.StatusForbidden {
		t.Errorf("same-origin POST was refused")
	}
}

func TestIntParamClamps(t *testing.T) {
	r := httptest.NewRequest("GET", "http://127.0.0.1/x?limit=100000", nil)
	if got := intParam(r, "limit", 8, 25); got != 25 {
		t.Errorf("limit=100000 → %d, want clamp 25", got)
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1/x?limit=-4", nil)
	if got := intParam(r, "limit", 8, 25); got != 8 {
		t.Errorf("limit=-4 → %d, want default 8", got)
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1/x?limit=abc", nil)
	if got := intParam(r, "limit", 8, 25); got != 8 {
		t.Errorf("limit=abc → %d, want default 8", got)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	h := testHandler(t, config.Defaults())
	rec := do(h, "GET", "http://127.0.0.1:8848/healthz", nil)
	if rec.Header().Get("Content-Security-Policy") == "" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("missing security headers: %v", rec.Header())
	}
}

func TestProcessesJSONHasNoCommandField(t *testing.T) {
	h := testHandler(t, config.Defaults())
	rec := do(h, "GET", "http://127.0.0.1:8848/api/processes?limit=3", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("processes: got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"command"`) {
		t.Error("processes JSON still exposes a command field (argv can carry secrets)")
	}
}

func TestProcessesAtBadTsIs400(t *testing.T) {
	cfg := config.Defaults()
	h := testHandler(t, cfg)
	if rec := do(h, "GET", "http://127.0.0.1:8848/api/processes/at?ts=banana", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad ts: got %d, want 400", rec.Code)
	}
}
