# Changelog

All notable changes are documented here. Format loosely follows
[Keep a Changelog](https://keepachangelog.com); versions are tagged
GitHub releases.

## [0.2.0] — 2026-10-30

### Added
- **Per-process detail drawer**: click the CPU or Memory card for a
  live top-15 table (bars, CPU %, RAM), and a History tab with a
  stacked chart of top consumers, leader boards with peak times, and
  click-the-chart "what was running at that moment" snapshots.
- **Process history**: top consumers sampled every 30 s (configurable)
  into SQLite, 14-day retention. New endpoints:
  `GET /api/processes/history`, `GET /api/processes/at`.
- **Process spike events**: a process holding ≥200% CPU (2 cores,
  configurable) for two consecutive samples is logged to Events.
- New config keys: `process_sample_interval_seconds`,
  `process_top_n`, `process_retention_days`, `alert_process_cpu_pct`.

### Changed
- **Full-screen app shell UI**: fixed sidebar with section navigation
  and scrollspy, sticky topbar with a live watts pill, KPI strip and
  a dense 12-column grid using the whole viewport (sidebar collapses
  to a top strip on narrow screens).
- Per-process CPU % is now a true rate between process walks instead
  of gopsutil's lifetime average (which made idle helpers look hot).
  Same-name instances are summed in history.
- Landing position of sidebar anchors fixed (`scroll-margin-top`);
  scrollspy is deterministic and click-locked.

### Security
- CSP + `nosniff` + `Referrer-Policy` + frame-denial headers on all
  responses; HTTP header/idle timeouts.
- Data directory created `0700`; generated config `0600`.
- launchd plist paths are XML-escaped; `lsof` is invoked by absolute
  path (it was silently missing from the LaunchAgent PATH, which had
  left Listening ports empty when running as a service).

## [0.1.0] — 2026-10-30

Initial public release.

- Live Apple Silicon power (system / CPU / GPU / ANE), temperatures
  and fan without sudo, via macmon; energy integration with kWh and
  cost (today / 7d / month).
- System sampling every 2 s into SQLite with 30-day retention and
  bucketed history charts; SSE live stream; Prometheus `/metrics`.
- Disk deep-dive: scheduled scans, biggest-first browsing, growth
  diffs between scans, largest files.
- Docker/OrbStack/Colima containers, launchd services, listening
  ports, HTTP health checks.
- Threshold alerts (disk, CPU temperature, RAM, watts) to ntfy or a
  generic webhook, with cooldowns and recovery notices; event log.
- launchd service install; loopback-by-default with token auth for
  LAN binds; designed for access via Tailscale Serve.
