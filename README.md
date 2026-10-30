# MiniPulse

**A tiny, always-on dashboard for a Mac mini (or any Apple Silicon Mac) used as a home server.**

Live **power draw in watts** with energy cost, CPU/RAM/thermals, a deep
**"where is my disk going?"** view that shows what *grew*, Docker
containers, listening ports, launchd services, health checks and alerts —
in one small Go binary, no sudo, no cloud account, no Electron.

![MiniPulse dashboard](docs/dashboard.png)

## Why?

A base-model Mac mini is now the home server of choice: ~5 W idle,
silent, and fast enough for Plex, Home Assistant, Docker stacks and
local LLMs. But monitoring one is surprisingly awkward:

- **Menu-bar tools** (Stats, iStat Menus) are great on a desktop, but a
  server mini runs *headless* — nobody is logged in to see a menu bar.
  Their remote modes stream your metrics to a third-party cloud.
- **Generic server dashboards** (Glances, Netdata, Beszel) don't know
  Apple Silicon exists: no watts, no Neural Engine, no E/P cores.
- **Power** is the #1 question mini-server owners ask ("what does it
  cost to run 24/7?") and most answer it with a smart plug, because
  software either requires `sudo powermetrics` or shows no history/cost.
- **Storage** is the #2 pain: the base mini has 256 GB, and macOS
  answers "what's eating it?" with a grey *System Data* bar of 100 GB+.
  Caches, Docker images, Ollama models and AI coding tools grow silently
  until the disk is full at 3 a.m.

MiniPulse is built for exactly that machine: a headless mini you want
to check from your phone, over your own network (localhost or
Tailscale), with history and alerts — and nothing leaving the house.

See [docs/RESEARCH.md](docs/RESEARCH.md) for the landscape survey behind
these choices, including the excellent existing tools we build on.

## Features

- ⚡ **Real watts, no sudo** — whole-system + CPU/GPU/ANE power,
  temperatures, fan RPM and per-core E/P/S activity, via
  [macmon](https://github.com/vladkens/macmon)'s `pipe` mode.
- 🔌 **Energy & cost** — kWh today / 7 days / month, cost in your
  currency at your tariff, peak & average draw, optional CO₂.
- 💾 **Where is the disk going?** — scheduled scans of any roots
  (home, external SSDs, `/Applications`…): browse biggest-first,
  see **what grew since the last scan**, and the largest files overall.
- 📈 **History** — every sample in a local SQLite file (30 days by
  default); charts for power, CPU, RAM, network and disk I/O.
- 🐳 **Containers** — Docker Desktop / OrbStack / Colima containers with
  live CPU & memory.
- 🛰️ **Services** — launchd jobs, listening TCP ports, and HTTP health
  checks you configure.
- 🔔 **Alerts without a cloud** — disk %, CPU temperature, RAM, watts and
  health-check failures to an [ntfy](https://ntfy.sh) topic or any webhook.
- 🔒 **Private by design** — binds to `127.0.0.1` by default; refuses to
  listen on a LAN address without a token. Serves a Prometheus
  `/metrics` endpoint if you already run Grafana.
- 🪶 **Small** — one static-ish Go binary, embedded UI (vanilla JS, works
  offline), installed as a `launchd` agent in one command. On the
  author's mini it idles at ~0.5% CPU sampling every 2 s.

## Install

Requirements: an Apple Silicon Mac, macOS 13+, and
[macmon](https://brew.sh) for power metrics (everything else works
without it — the dashboard just shows watts as unavailable).

```sh
brew install macmon
go install github.com/ricardotech/minipulse/cmd/minipulse@latest
minipulse            # serves http://127.0.0.1:8848
```

No Go toolchain? Grab a binary from
[Releases](https://github.com/ricardotech/minipulse/releases), or build
from source:

```sh
git clone https://github.com/ricardotech/minipulse
cd minipulse && make run
```

### Run it forever (launchd)

```sh
minipulse service install     # starts at login, restarts on crash
minipulse service status
minipulse service uninstall
```

Logs land in `~/Library/Logs/minipulse/`.

### See it from your phone (Tailscale)

Keep MiniPulse on loopback and publish it only to your tailnet:

```sh
tailscale serve --bg 8848
# → https://<your-mini>.<tailnet>.ts.net
```

To bind a LAN address instead, set `"token"` in the config first
(`minipulse token` generates one) — MiniPulse refuses to start on a
non-loopback address without it. Open the dashboard once with
`?token=…` and it sets a cookie.

## Configuration

Optional — defaults work out of the box. To customize:

```sh
minipulse init-config   # writes ~/.config/minipulse/config.json
```

| Field | Default | Meaning |
|---|---|---|
| `addr` | `127.0.0.1:8848` | listen address |
| `token` | *(none)* | API token (required off-loopback) |
| `sample_interval_seconds` | `2` | metrics cadence |
| `retention_days` | `30` | history kept in SQLite |
| `energy_price_per_kwh` / `currency` | `0.15` / `USD` | your electricity tariff |
| `disk_scan_roots` | `["~"]` | directories to walk (add `/Volumes/…`, `/Applications`) |
| `disk_scan_interval_minutes` | `60` | scan cadence (`0` = only on demand) |
| `disk_min_entry_bytes` | `10485760` | smaller entries are aggregated, not stored |
| `health_checks` | `[]` | `[{name, url, expect_status?}]` polled every 30 s |
| `alert_disk_pct` / `alert_cpu_temp_c` / `alert_mem_pct` / `alert_watts` | `85`/`95`/`92`/`0` | thresholds (`0` disables) |
| `alert_webhook_url` | *(none)* | ntfy topic URL or webhook |
| `alert_cooldown_minutes` | `30` | repeat interval while a rule keeps firing |

Data lives in `~/.local/share/minipulse/minipulse.db` (SQLite, WAL).

## CLI

```sh
minipulse                      # serve in the foreground
minipulse scan [--top 25] [paths…]   # one-off disk scan: biggest first
minipulse service install|uninstall|status
minipulse token                # print a random API token
minipulse init-config
minipulse version
```

## HTTP API

Everything the UI shows, as JSON (plus a live SSE stream):

```
GET  /api/overview      host + latest sample + energy today
GET  /api/stream        SSE: one event per sample (server-paced)
GET  /api/history?range=1h|6h|24h|7d|30d
GET  /api/processes?sort=mem|cpu&limit=15
GET  /api/energy?range=today|7d|month
GET  /api/disk?root=&view=tree|growth|biggest&parent=
POST /api/disk/scan     trigger a rescan
GET  /api/containers    Docker / OrbStack / Colima
GET  /api/services      launchd jobs + listening ports + health
GET  /api/events        alert/event log
GET  /metrics           Prometheus text
GET  /healthz
```

## How watts work without sudo

`powermetrics` needs root, which is a non-starter for an always-on
agent. [macmon](https://github.com/vladkens/macmon) reads the same
underlying IOReport data through a private-but-unprivileged API.
MiniPulse runs `macmon pipe` as a child process and parses its JSON
stream — credit and thanks to the macmon project; MiniPulse adds the
history, cost accounting, disk deep-dive, services and alerts around it.

## Development

```sh
make check   # gofmt + vet + tests
make build   # ./minipulse
make run     # build & serve on :8848
```

The codebase is deliberately boring: stdlib HTTP, one metrics library
(gopsutil), one SQLite driver, no web build step (the UI is three
static files embedded with `go:embed`). See
[CONTRIBUTING.md](CONTRIBUTING.md).

## Roadmap ideas (PRs welcome)

- SSD health / TBW (SMART) on the storage card
- Per-process energy impact ranking (macmon exposes it in its TUI)
- Homebrew tap formula
- Optional multi-Mac fleet view (several minis on one page)
- Notification via Apple Shortcuts / Pushover natively

## License

MIT. Built on the shoulders of
[macmon](https://github.com/vladkens/macmon) (MIT),
[gopsutil](https://github.com/shirou/gopsutil) (BSD-3) and
[mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) (MIT).
