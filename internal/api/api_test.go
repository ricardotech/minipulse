package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ricardotech/minipulse/internal/collect"
	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/store"
)

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
