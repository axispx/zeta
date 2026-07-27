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
internal/tools/      read / edit / write / grep / glob / bash / websearch / webfetch / skill / todo / ask_user
internal/todo/       session-scoped checklist store (model-owned)
internal/skill/      bundled playbooks (`skills/*/SKILL.md` via go:embed)
internal/config/     ~/.zeta/config.json
internal/models/     models.dev catalog → presets
internal/session/    JSONL under ~/.zeta/sessions/
internal/plan/       proposed_plan extract + build seed
internal/paths/      ZETA_HOME
internal/image/      path normalize, sniff, data: URLs, clipboard (temp only)
internal/styles/     lipgloss tokens + banner
```

Images attach as inline `data:` URLs on user turns in session JSONL (no `attachments/` side store). Composer ↑/↓ walks the session UI transcript (not a separate history file).

### Bundled skills

First-party playbooks only (no user/repo skill dirs). Bodies live at
`internal/skill/skills/<name>/SKILL.md`. Metadata is a Go table in
`internal/skill/skill.go` (`bundled`):

```go
//go:embed skills/my-skill/SKILL.md
var mySkillMD string

// in bundled:
{
    Name:        "my-skill",
    Description: "Use when the user asks to …",
    Slash:       "/my-skill", // empty = tool-only (no palette entry)
    Content:     mySkillMD,
},
```

Rebuild picks up embeds. Every bundled skill is:

- listed in the system prompt catalog
- loadable by the model via the `skill` tool (build and ask/plan)

Optional `Slash: "/name"` also registers a palette entry (`command.skill`).
Palette Enter/Tab always fills `"/name "` into the input (never runs) so the
user can add args; a second Enter submits. Durable history stores the user
text (token + optional args); on the invoking turn only, `requestMsgs` appends
the playbook as a developer message. Args after the token stay on the user
message. Slash tokens must not collide with harness commands — `internal/tui`
panics at init if a skill claims `/clear`, `/config`, etc.

Bundled today: `review` (`/review` — thermo-nuclear code quality review).

## Versioning

Pre-1.0 (`0.y.z`): no backward compatibility for session transcripts, APIs, or on-disk formats. Prefer deleting legacy paths over dual encodings or migration shims.

## Pull requests

1. Keep changes focused — one concern per PR when practical.
2. Match existing style in the package you touch.
3. Add or update tests for behavior changes.
4. Run `go test ./...` before opening the PR.
