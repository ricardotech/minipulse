# Security

## Reporting a vulnerability

Please report suspected vulnerabilities privately via GitHub's
[private vulnerability reporting](https://github.com/ricardotech/minipulse/security/advisories/new)
instead of opening a public issue. You should get a reply within a
few days. This is a small hobby project with no SLA and no bug bounty.

## Supported versions

The latest release is the supported version. There are no backports.

## Threat model

MiniPulse is a **read-only monitoring agent** for a Mac you own. It
deliberately has a small attack surface:

- It never runs as root and never asks for `sudo` (wattage comes from
  `macmon`, which uses an unprivileged API).
- It only *reads* system state. The single state-changing API call is
  `POST /api/disk/scan`, which starts a read-only directory walk over
  roots configured by the machine's owner.
- It makes no outbound calls except the health-check URLs and alert
  webhook **you** configure. No telemetry, no update checks.

What it can see — and therefore what a compromise of its HTTP port
would expose — is monitoring data: process names and PIDs (command
lines are deliberately never collected or exposed), file paths and
sizes inside your configured scan roots, listening ports, container
names, and power history. **Treat dashboard access as sensitive.**

## Safe deployment

- The default bind is `127.0.0.1`. Prefer reaching it from other
  devices via [Tailscale Serve](https://tailscale.com/kb/1312/serve)
  (`tailscale serve --bg 8848`), which keeps it end-to-end encrypted
  inside your tailnet with no MiniPulse token strictly required.
- If you bind to a LAN address, MiniPulse refuses to start without an
  API `token` (generate one with `minipulse token`) unless you pass
  `--insecure-no-auth`. LAN traffic is plain HTTP — anyone on the
  network can read it. Don't port-forward 8848 to the internet.
- Any local user — and any process running on the machine — can
  connect to a loopback port. On a shared or multi-user Mac, set a
  `token` even on loopback.
- The data directory is created owner-only (`0700`) and the generated
  config file `0600`, because both can contain sensitive things (your
  token; your file paths). Don't relax those.

## Hardening in the code

- Constant-time token comparison; auth accepts a header token or a
  `?token=` bootstrap link that becomes an HttpOnly, SameSite=Lax
  cookie (the link stays valid until you rotate the token — treat
  it like a password).
- Content-Security-Policy (scripts same-origin only), `nosniff`,
  `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`.
- HTTP server header/idle timeouts against slow clients; all queries
  are parameterized SQL; external tools are a fixed set invoked
  without a shell and with fixed arguments (`macmon` via PATH,
  `/usr/sbin/lsof`, `/bin/launchctl`, `/usr/sbin/scutil`).
- CI runs `go vet` + tests on every push; dependencies are checked
  with `govulncheck`.
