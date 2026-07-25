# Contributing

Thanks for helping with Zeta. End-user docs: [README.md](README.md).

## Develop

Requirements: [Go](https://go.dev/dl/) 1.26+.

```bash
git clone https://github.com/axispx/zeta.git
cd zeta
make run       # go run ./cmd/zeta
make build     # → bin/zeta
make tidy      # go mod tidy
make install   # → ~/.local/bin/zeta
go test ./...
```

## Project layout

```
cmd/zeta/            entrypoint
internal/tui/        bubbletea UI
internal/ai/         OpenAI-compatible streaming + tools
internal/agent/      tool loop + permission gate
internal/permission/ allow | deny for side-effect tools
internal/compact/    context compaction
internal/tools/      read / edit / write / grep / glob / bash / websearch / webfetch / ask_user
internal/config/     ~/.zeta/config.json
internal/models/     models.dev catalog → presets
internal/session/    JSONL under ~/.zeta/sessions/
internal/plan/       proposed_plan extract + build seed
internal/paths/      ZETA_HOME
internal/styles/     lipgloss tokens + banner
```

## Versioning

Pre-1.0 (`0.y.z`): no backward compatibility for session transcripts, APIs, or on-disk formats. Prefer deleting legacy paths over dual encodings or migration shims.

## Pull requests

1. Keep changes focused — one concern per PR when practical.
2. Match existing style in the package you touch.
3. Add or update tests for behavior changes.
4. Run `go test ./...` before opening the PR.
