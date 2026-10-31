# MiniPulseNotch — the optional GUI companion

The MiniPulse daemon is deliberately **not** a menu-bar app: it is a
headless Go service. This is its optional native companion for a
personal Mac (issue [#2](https://github.com/ricardotech/minipulse/issues/2)),
a separate client of the daemon's HTTP API:

- **Menu bar:** an original pulse runner whose speed tracks CPU
  load, next to the live wattage. Click for full stats, top CPU
  processes and a dashboard shortcut.
- **Notch / pill:** on a notched MacBook the panel hugs the notch —
  watts on the left wing, CPU % on the right (the camera housing is
  opaque; nothing is drawn under it). Hover expands a stats flyout.
  On Macs without a notch (mini, iMac, external displays) the same
  panel is a small floating pill at the top-centre.

Clean-room Swift on public AppKit/SwiftUI APIs only. Notch geometry
is derived at runtime from `NSScreen.safeAreaInsets` and the
auxiliary top areas — no per-model dimensions, no private
SkyLight/CGSSpace tricks, no third-party code or assets.

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
