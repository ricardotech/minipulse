# MiniPulseMenu — the menu-bar companion (issue #2, phase 1)

`MiniPulseMenu` is a tiny SwiftUI/AppKit *agent* app (no Dock icon) that puts
MiniPulse stats in the macOS **menu bar**:

1. **A pulse runner + the indicators you pick** — a load-paced runner
   (an original blinking-heart-style pulse; cadence follows CPU load,
   capped at 10 fps) beside any subset of **watts · CPU % · MEM % ·
   CPU temp · fan rpm**, e.g. `18W · CPU 23% · MEM 61%`. With the
   runner off, the metrics are drawn as real vector text.
2. **A click popover** with the full glanceable strip (big watts,
   CPU/RAM bars, top process) and buttons into the dashboard.

> A MacBook-notch surface for this companion was built and then
> **removed on purpose** (2026-10-31): the menu bar won — it is
> where the eyes already are, works on every Mac and external
> display, and needs no overlay windows. The `/api/mini` endpoint
> it motivated stays (it also serves the ESP32 issue #3).

Every drawn element is **first-party code drawn in this repo**
(system symbols + SwiftUI paths). The daemon remains headless and
untouched; this is just another client of its HTTP API — the open
`/api/stream` (SSE) feeds live samples, `GET /api/processes` is
fetched on popover open, and `/api/mini` covers stream warm-up and
recovery.

## Build

```bash
cd companion
swift build           # debug binary
swift test            # MiniPulseCore unit tests
make run              # build, assemble and run (no .app needed)
make app              # ad-hoc signed MiniPulseMenu.app (unsigned: local use)
make clean            # remove the assembled app
```

## Run

Launch the app, then open Preferences from the status item's
right-click menu (left-click opens the popover):

- **Daemon base URL** — default `http://127.0.0.1:8848`; a
  `https://<mac>.<tailnet>.ts.net` from `tailscale serve` works too.
  Plain `http` outside loopback is allowed but warns — the token
  would travel in clear.
- **Token** — generate with `minipulse token`, paste it here
  (stored in the Keychain, never in UserDefaults or logs). Leave
  empty for a loopback daemon with no token set.
- **Menu bar** section — the pulse runner and/or the numeric
  indicators: watts, CPU %, memory %, CPU temp and fan are
  toggled independently and shown side by side in canonical order.

## Status

Verified by unit tests (SSE parsing, cadence, menu-bar text,
staleness, host validation) and by running it against a live
daemon. **Open item:** real-hardware polish across display
changes (clamshell, external monitors) — please file issues with
screenshots.
