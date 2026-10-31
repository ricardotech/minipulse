#!/usr/bin/env python3
# <xbar.title>MiniPulse</xbar.title>
# <xbar.version>0.1.0</xbar.version>
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
import urllib.request

BASE = os.environ.get("MINIPULSE_URL", "http://127.0.0.1:8848").rstrip("/")
TOKEN = os.environ.get("MINIPULSE_TOKEN", "")


def fetch(path, timeout=3):
    req = urllib.request.Request(BASE + path)
    if TOKEN:
        req.add_header("X-MiniPulse-Token", TOKEN)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.load(resp)


def clean(text):
    # "|" starts SwiftBar params; process names must not inject them.
    return str(text).replace("|", "/").replace("\n", " ").strip()


def fmt_w(w):
    return f"{w:.0f}W" if w >= 10 else f"{w:.1f}W"


def main():
    try:
        data = fetch("/api/overview")
    except urllib.error.HTTPError as e:
        hint = {401: "token missing/wrong (set MINIPULSE_TOKEN)",
                503: "daemon warming up, retrying…"}.get(e.code, f"HTTP {e.code}")
        print("MiniPulse !")
        print("---")
        print(f"MiniPulse unreachable: {clean(hint)}")
        print(f"Open dashboard | href={BASE}/")
        return
    except Exception:
        print("MiniPulse !")
        print("---")
        print("MiniPulse unreachable: is the daemon running?")
        print(f"Tried {clean(BASE)}/api/overview")
        print(f"Open dashboard | href={BASE}/")
        return

    sample = data.get("sample") or {}
    power = sample.get("power") or {}
    cpu = sample.get("cpu_pct")
    header = []
    if power.get("available"):
        header.append(fmt_w(power.get("sys_w") or 0))
    else:
        header.append("—W")
    if cpu is not None:
        header.append(f"{cpu:.0f}%")
    print(" · ".join(header) + " | font=Menlo size=12")
    print("---")

    host = (data.get("host") or {}).get("hostname") or "MiniPulse"
    print(f"{clean(host)} | size=13")
    if power.get("available"):
        print(f"SoC {fmt_w(power.get('soc_w') or 0)} "
              f"(CPU {fmt_w(power.get('cpu_w') or 0)}, GPU {fmt_w(power.get('gpu_w') or 0)})")
        print(f"CPU temp {(power.get('cpu_temp_c') or 0):.0f}°C · "
              f"GPU temp {(power.get('gpu_temp_c') or 0):.0f}°C")
        if power.get("fan_rpm"):
            print(f"Fan {power['fan_rpm']} rpm")
    else:
        reason = power.get("reason") or "macmon unavailable"
        print(f"Power: — ({clean(reason)})")
    mem = sample.get("mem_pct")
    if mem is not None:
        print(f"Memory {mem:.0f}% used")

    energy = data.get("energy_today") or {}
    if energy.get("kwh") is not None:
        cur = energy.get("currency") or data.get("currency") or ""
        print(f"Today {energy['kwh']:.2f} kWh · {cur} {(energy.get('cost') or 0):.2f} "
              f"· peak {fmt_w(energy.get('peak_w') or 0)}")

    try:
        procs = fetch("/api/processes?sort=cpu&limit=3")
        if procs:
            print("---")
            print("Top CPU now")
            for p in procs:
                print(f"{clean(p.get('name'))} — {(p.get('cpu_pct') or 0):.0f}% | font=Menlo size=12")
    except Exception:
        pass  # top processes are decoration; never fail the plugin for them

    print("---")
    print(f"Open dashboard | href={BASE}/")


if __name__ == "__main__":
    main()
    sys.exit(0)
