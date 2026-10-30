# Why MiniPulse exists — landscape notes

Survey from 2026-10-30 (repos read first-hand via the GitHub API and
their READMEs, plus community writing on Mac-mini-as-server setups).
Recorded here so future contributors can see which niches were already
taken and why MiniPulse occupies a different one.

## Existing tools (all worth knowing)

| Project | Form | Apple Silicon watts | Headless/web | History | Disk deep-dive | Notes |
|---|---|---|---|---|---|---|
| [exelban/stats](https://github.com/exelban/stats) (42.9k★, Swift, MIT) | menu bar app | yes | remote mode streams to vendor cloud | limited | no | The desktop standard. Needs a logged-in GUI session. |
| [vladkens/macmon](https://github.com/vladkens/macmon) (2.1k★, Rust, MIT) | TUI + JSON/Prometheus | **yes, sudoless** | `/json` + `/metrics` server | session only | no | Best power data source on macOS. MiniPulse consumes its `pipe` stream. |
| [context-labs/mactop](https://github.com/context-labs/mactop) (2.3k★, Go, MIT) | TUI | via powermetrics (sudo) | no | no | no | |
| [nicolargo/glances](https://github.com/nicolargo/glances) (33.9k★, Python) | TUI + web | no | yes | export only | basic | Generic; nothing Apple-Silicon-specific. |
| [netdata/netdata](https://github.com/netdata/netdata) (81k★, GPL-3) | agent + web | no | yes | yes | basic | Powerful but heavy; cloud upsell; macOS is a second-class target. |
| [henrygd/beszel](https://github.com/henrygd/beszel) (26.7k★, Go, MIT) | hub + agent + web | no | yes | yes | basic | Excellent for Linux fleets; no macOS power/thermal story. |
| [samvladd/vitals](https://github.com/samvladd/vitals) (new, Swift, MIT) | menu bar + phone web view | yes | via Tailscale/SSH | yes | storage totals | Closest in spirit; Swift app-centric (menu bar, fan control, screen sharing), needs the app on a Mac you use. |
| iStat Menus (commercial) | menu bar app | yes | companion apps | yes | no | Closed source, GUI-session bound. |

## Recurring pains from the Mac-mini-server community

1. **"How many watts / what does 24/7 cost?"** — the most-asked mini
   server question. Owners routinely measure with energy-monitoring
   smart plugs (e.g. Kasa) because software options need sudo or keep
   no history. Typical numbers quoted: 4–7 W idle, 15–35 W under real
   work for an M4 mini.
2. **Headless blindness** — the mini sits in a closet; menu-bar tools
   are invisible. People bolt together Netdata + Heimdall + Uptime Kuma
   or accept SSH-only checks.
3. **256 GB anxiety / "System Data"** — base-model storage fills with
   caches, Xcode, Docker/OrbStack images, Ollama models (4–40 GB each)
   and AI-tool data; macOS Storage settings won't say *what*, let alone
   *what changed*. Guides for mini servers explicitly recommend a
   disk-usage alarm at 75–80% because macOS degrades above that.
4. **Docker on a Mac is special** — OrbStack/Colima/Docker Desktop put
   the socket in three different places; GPU workloads (Ollama) must
   run natively, so "containers + native services" is the real unit of
   supervision, not containers alone.
5. **Privacy / no-cloud preference** — a strong norm in the self-hosted
   community; remote features that relay through a vendor account are
   a minus, Tailscale is the accepted transport.

## The gap MiniPulse fills

A *web-first, headless-first* agent that is Apple-Silicon-aware
(watts without sudo, via macmon), keeps local history + cost, treats
**storage growth** as a first-class signal, watches containers *and*
native services/ports, alerts without any cloud, and ships as one
binary with an embedded UI under a launchd agent.

Non-goals (things neighbours already do better): menu-bar widgets,
fan control, remote screen/control, multi-OS fleet management, and
full observability stacks. MiniPulse exposes Prometheus metrics so it
composes with those instead of replacing them.
