# MiniPulse in the menu bar (SwiftBar/xbar)

Phase 0 of the [notch companion issue](https://github.com/ricardotech/minipulse/issues/2):
glanceable MiniPulse stats — **watts first**, then CPU — in the macOS
menu bar, with zero native code. This validates the demand before a
Swift companion (menu-bar runner + notch panel) is built.

The menu bar shows e.g. `31W · 11%`. The dropdown adds SoC/CPU/GPU
watts, temperatures, fan, memory, today's energy (kWh, cost, peak)
and the top CPU processes. Click through to the full dashboard.

## Install

1. Install [SwiftBar](https://github.com/swiftbar/SwiftBar)
   (`brew install --cask swiftbar`) or xbar — the plugin API is shared.
2. Copy `minipulse.5s.py` into your SwiftBar Plugin Folder and make
   it executable:

   ```sh
   cp minipulse.5s.py ~/Library/Application\ Support/SwiftBar/Plugins/
   chmod +x ~/Library/Application\ Support/SwiftBar/Plugins/minipulse.5s.py
   ```

   (Adjust the path if you picked a different Plugin Folder.)
3. Refresh SwiftBar. Done — it talks to `http://127.0.0.1:8848` by
   default. The `5s` in the filename is the refresh interval; rename
   to `minipulse.10s.py` etc. to change it. Going faster than the
   daemon's sample interval (2s default) shows no new data.

## Configuration

Set environment variables on the plugin (SwiftBar: right-click the
menu-bar item → plugin environment variables):

| Variable | Default | Meaning |
|---|---|---|
| `MINIPULSE_URL` | `http://127.0.0.1:8848` | Daemon base URL. For a remote Mac use your Tailscale Serve `*.ts.net` URL (see the main README). |
| `MINIPULSE_TOKEN` | _(empty)_ | API token, only if your daemon has one (`minipulse token`). Sent as the `X-MiniPulse-Token` header. Never commit it. |

## Behaviour notes

- **No watts?** The header shows `—W` when the daemon reports power
  as unavailable (e.g. macmon missing) — never a fake `0W`.
- **Daemon down/warming up:** the header shows `MiniPulse !` with the
  reason in the dropdown instead of stale numbers.
- Requires Python 3 (ships with macOS as `/usr/bin/python3`) and a
  running MiniPulse daemon. No extra dependencies.
