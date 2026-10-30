# Contributing

Thanks for considering a contribution! MiniPulse aims to stay small,
boring and dependable on a machine that runs 24/7.

## Ground rules

- **One static-ish binary.** No new heavy dependencies without a
  discussion first. Current non-stdlib deps: gopsutil (system metrics)
  and mattn/go-sqlite3 (storage). The UI is vanilla HTML/CSS/JS with
  no build step on purpose.
- **No sudo, ever.** If a metric needs root, it doesn't ship.
- **No cloud, no telemetry.** Nothing may phone home.
- `gofmt`, `go vet` and `go test ./...` must pass (`make check`).
- New parsers/collectors get a test with a real captured sample where
  practical (see `internal/collect/collect_test.go`).

## Layout

```
cmd/minipulse/     CLI: serve / service / scan / token / init-config
internal/model/    shared types (no deps)
internal/config/   JSON config + defaults
internal/collect/  system, macmon power, disk scanner, docker, services
internal/store/    SQLite (the only package touching the DB)
internal/alert/    threshold rules + ntfy/webhook delivery
internal/api/      HTTP API, SSE, Prometheus, embedded UI (web/)
internal/service/  launchd LaunchAgent install
```

## Testing on real hardware

Power/thermal paths can only be exercised on Apple Silicon with
`macmon` installed. If your change touches them, note in the PR which
chip you tested on (M1–M6 report slightly different tier layouts).

## License

By contributing you agree your work is licensed under the MIT license
in this repo.
