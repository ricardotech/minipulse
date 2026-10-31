#!/usr/bin/env python3
# <xbar.title>MiniPulse</xbar.title>
# <xbar.version>0.2.0</xbar.version>
# <xbar.author>Ricardo Domene</xbar.author>
# <xbar.desc>Glanceable MiniPulse stats (watts, CPU, temps) in the menu bar.</xbar.desc>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideLastUpdated>true</swiftbar.hideLastUpdated>
# <swiftbar.refreshOnOpen>true</swiftbar.refreshOnOpen>
#
# Phase 0 of issue #2 (notch companion): validate the "glanceable"
# demand before any native code exists. Polls the MiniPulse HTTP API
# every 5s (the interval is in the filename; rename to change it).
# Prefers GET /api/mini (cheap flat payload); daemons without it
# (pre-/api/mini builds answer 404) fall back to /api/overview.
#
# Config (environment):
#   MINIPULSE_URL    base URL, default http://127.0.0.1:8848
#                    (remote: use your Tailscale Serve *.ts.net URL)
#   MINIPULSE_TOKEN  API token, only if the daemon has one configured.
#                    Set it per-plugin in SwiftBar (right-click the
#                    item > set environment variables) -- never commit it.
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

TOKEN = os.environ.get("MINIPULSE_TOKEN", "")


def valid_base(raw):
    """Only a clean http(s) base URL may reach an href= parameter."""
    raw = (raw or "").strip().rstrip("/")
    if not raw or any(c in raw for c in " |\t\n"):
        return None
    p = urllib.parse.urlparse(raw)
    if p.scheme not in ("http", "https") or not p.netloc or p.username or p.query:
        return None
    return raw


BASE = valid_base(os.environ.get("MINIPULSE_URL", "http://127.0.0.1:8848"))


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    # Never follow redirects: urllib would re-send the token header
    # to the redirect target, possibly a different origin.
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


_OPENER = urllib.request.build_opener(_NoRedirect)


def fetch(path, timeout=3):
    req = urllib.request.Request(BASE + path)
    if TOKEN:
        req.add_header("X-MiniPulse-Token", TOKEN)
    with _OPENER.open(req, timeout=timeout) as resp:
        return json.load(resp)


def clean(text):
    # "|" starts SwiftBar params; server-provided strings (host and
    # process names, currency, reasons) must never inject them.
    return str(text).replace("|", "/").replace("\n", " ").strip()


def num(v):
    return float(v) if isinstance(v, (int, float)) and not isinstance(v, bool) else None


def dget(d, key):
    """Nested-dict access that tolerates wrong container types."""
    v = d.get(key) if isinstance(d, dict) else None
    return v if isinstance(v, dict) else {}


def fmt_w(w):
    return f"{w:.0f}W" if w >= 10 else f"{w:.1f}W"


def error_state(msg):
    print("MiniPulse !")
    print("---")
    print(f"MiniPulse: {clean(msg)}")
    if BASE:
        print(f"Open dashboard | href={BASE}/")


def from_mini(m):
    if not isinstance(m, dict):
        m = {}
    p = dget(m, "top")
    top = [{"name": p.get("name"), "cpu_pct": num(p.get("cpu_pct"))}] if p.get("name") else []
    return {
        "sys_w": num(m.get("sys_w")), "soc_w": num(m.get("soc_w")),
        "cpu_pct": num(m.get("cpu_pct")), "mem_pct": num(m.get("mem_pct")),
        "cpu_temp_c": num(m.get("cpu_temp_c")), "gpu_temp_c": num(m.get("gpu_temp_c")),
        "fan_rpm": num(m.get("fan_rpm")), "power_available": bool(m.get("power_available")),
        "wh_total": num(m.get("wh_total")), "top": top, "today": None, "host": None,
    }


def from_overview(d):
    if not isinstance(d, dict):
        d = {}
    s = dget(d, "sample")
    p = dget(s, "power")
    e = dget(d, "energy_today")
    today = None
    if num(e.get("kwh")) is not None:
        today = {"kwh": num(e.get("kwh")), "cost": num(e.get("cost")) or 0,
                 "peak_w": num(e.get("peak_w")) or 0,
                 "currency": e.get("currency") or d.get("currency") or ""}
    return {
        "sys_w": num(p.get("sys_w")), "soc_w": num(p.get("soc_w")),
        "cpu_w": num(p.get("cpu_w")), "gpu_w": num(p.get("gpu_w")),
        "cpu_pct": num(s.get("cpu_pct")), "mem_pct": num(s.get("mem_pct")),
        "cpu_temp_c": num(p.get("cpu_temp_c")), "gpu_temp_c": num(p.get("gpu_temp_c")),
        "fan_rpm": num(p.get("fan_rpm")), "power_available": bool(p.get("available")),
        "power_reason": p.get("reason"), "wh_total": num(s.get("wh_total")),
        "top": [], "today": today, "host": dget(d, "host").get("hostname"),
    }


def load():
    try:
        v = from_mini(fetch("/api/mini?top=1"))
        if not v["top"]:
            try:
                procs = fetch("/api/processes?sort=cpu&limit=3")
                v["top"] = [{"name": x.get("name"), "cpu_pct": num(x.get("cpu_pct"))}
                            for x in procs if isinstance(x, dict)]
            except Exception:
                pass
        return v
    except urllib.error.HTTPError as e:
        if e.code != 404:  # auth/warming-up errors are real; 404 = old daemon
            raise
    v = from_overview(fetch("/api/overview"))
    try:
        procs = fetch("/api/processes?sort=cpu&limit=3")
        v["top"] = [{"name": x.get("name"), "cpu_pct": num(x.get("cpu_pct"))}
                    for x in procs if isinstance(x, dict)]
    except Exception:
        pass
    return v


def render(v):
    header = [fmt_w(v["sys_w"]) if v["power_available"] and v["sys_w"] is not None else "—W"]
    if v["cpu_pct"] is not None:
        header.append(f"{v['cpu_pct']:.0f}%")
    print(" · ".join(header) + " | font=Menlo size=12")
    print("---")
    if v.get("host"):
        print(f"{clean(v['host'])} | size=13")
    if v["power_available"]:
        soc = f"SoC {fmt_w(v['soc_w'] or 0)}"
        if v.get("cpu_w") is not None:
            soc += f" (CPU {fmt_w(v['cpu_w'] or 0)}, GPU {fmt_w(v.get('gpu_w') or 0)})"
        print(soc)
        if v["cpu_temp_c"] is not None:
            print(f"CPU temp {v['cpu_temp_c']:.0f}°C · GPU temp {(v['gpu_temp_c'] or 0):.0f}°C")
        if v["fan_rpm"]:
            print(f"Fan {v['fan_rpm']:.0f} rpm")
    else:
        print(f"Power: — ({clean(v.get('power_reason') or 'unavailable')})")
    if v["mem_pct"] is not None:
        print(f"Memory {v['mem_pct']:.0f}% used")
    if v.get("today"):
        t = v["today"]
        print(f"Today {t['kwh']:.2f} kWh · {clean(t['currency'])} {t['cost']:.2f} "
              f"· peak {fmt_w(t['peak_w'])}")
    elif v["wh_total"] is not None:
        print(f"Energy total {v['wh_total']:.1f} Wh")
    tops = [t for t in v.get("top") or [] if t.get("name")]
    if tops:
        print("---")
        print("Top CPU now")
        for t in tops[:3]:
            cpu = f"{t['cpu_pct']:.0f}%" if t.get("cpu_pct") is not None else "—"
            print(f"{clean(t['name'])} — {cpu} | font=Menlo size=12")
    print("---")
    print(f"Open dashboard | href={BASE}/")


def main():
    if BASE is None:
        error_state("invalid MINIPULSE_URL (want http(s)://host[:port])")
        return
    try:
        render(load())
    except urllib.error.HTTPError as e:
        hint = {401: "token missing/wrong (set MINIPULSE_TOKEN)",
                503: "daemon warming up, retrying…"}.get(e.code, f"HTTP {e.code}")
        error_state(f"unreachable: {hint}")
    except Exception:
        # Bad JSON, wrong types, connection refused: one honest error
        # state, never a traceback in the menu.
        error_state("unreachable: is the daemon running?")


if __name__ == "__main__":
    main()
    sys.exit(0)
