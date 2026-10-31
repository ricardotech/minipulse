# MiniPulseNotch — the optional GUI companion

The MiniPulse daemon is deliberately **not** a menu-bar app: it is a
headless Go service. This is its optional native companion for a
personal Mac (issue [#2](https://github.com/ricardotech/minipulse/issues/2)),
a separate client of the daemon's HTTP API:

- **Menu bar:** an original pulse runner whose speed tracks CPU
  load, next to **customizable indicators** — pin any subset of
  watts, CPU %, memory %, CPU temperature and fan side by side
  (e.g. `18W · CPU 23% · MEM 61%`) in Preferences. Click for full
  stats, top CPU processes and a dashboard shortcut.
- **Notch / pill:** a Dynamic-Island-style black silhouette with
  concave flare into the menu bar grows out of the physical notch
  on spring physics. At rest the first two enabled indicators ride
  the wings beside the camera housing (nothing is drawn under it);
  hover expands the full stats card (hero watts, CPU/MEM bars,
  temps, top process; click opens the dashboard). On Macs without
  a notch the same panel is a floating pill at the top-centre.
  The window is fixed-size and passes clicks through everywhere
  outside the visible shape.

Original Swift on public AppKit/SwiftUI APIs. Notch geometry is
derived at runtime from `NSScreen.safeAreaInsets` and the
auxiliary top areas — no per-model dimensions, no private
SkyLight/CGSSpace tricks. The silhouette's continuous-corner
extraction is adapted from [Glance](https://github.com/jonnyoo/glance)
(© Jonathan Zhou) under the MIT License, credited in
`NotchShape.swift`; everything else is original to this project.

## Build & run

```sh
cd companion
make app            # release build + ad-hoc signed MiniPulseNotch.app
open MiniPulseNotch.app
```

Requires macOS 13+ and a running MiniPulse daemon. Defaults to
`http://127.0.0.1:8848`; change the host and token in Preferences
(the token is stored in the Keychain, sent as `X-MiniPulse-Token`).
For a remote Mac use its Tailscale Serve `*.ts.net` URL.

Distribution is currently build-from-source / ad-hoc, exactly like
the daemon (clear quarantine once with
`xattr -dr com.apple.quarantine MiniPulseNotch.app` if you move the
app between Macs). Developer ID notarization arrives when the
project gets a signing identity.

## Develop

- `swift test` — unit tests for the API decoders, SSE parser, runner
  cadence mapping and notch geometry (pure core, no display needed).
- No notch on your dev Mac? Simulate one for layout/hover work:

  ```sh
  open MiniPulseNotch.app --args -MiniPulseSimulateNotch 185x32
  ```

  Simulated geometry validates presentation only. Real safe-area,
  fullscreen and scaled-resolution behaviour must be checked on a
  notched MacBook.

## How it talks to the daemon

SSE `GET /api/stream` is the primary feed (server-paced, one event
per sample — the daemon defaults to 2 s samples). Reconnects use
backoff plus one `GET /api/mini?top=1` resync. The click panel
fetches `GET /api/processes?sort=cpu&limit=5` on open. When the
daemon reports power unavailable, surfaces show `—W`, never a
fake `0W`; a down/warming-up daemon shows an explicit stale/offline
state instead of stale numbers. The runner animation pauses while
the display is asleep.
