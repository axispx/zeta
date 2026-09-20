# Contributing

Thanks for helping with Zeta. End-user docs: [README.md](README.md) and [`docs/`](docs/).

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

`/update` closes zeta, updates in the CLI, then reopens zeta. A dev build
(`dev` version) runs the same handoff with a synthetic update: nothing is
downloaded, so the close → update → restart path stays testable without cutting
a release.

## Project layout

```
cmd/zeta/            entrypoint
internal/cli/        flags, usage, folder-trust prompt
internal/tui/        bubbletea UI
internal/ai/         OpenAI-compatible streaming + tools
internal/agent/      tool loop + permission gate
internal/core/       UI-agnostic runtime: decision gate, request assembly, session state and the durable write path, approvals, usage, client construction
internal/permission/ allow | deny for side-effect tools
internal/policy/     persisted permission rules (~/.zeta/permissions.json)
internal/compact/    context compaction
internal/tools/      read / edit / write / grep / glob / bash / websearch / webfetch / skill / todo / ask_user
internal/todo/       session-scoped checklist store (model-owned)
internal/skill/      bundled playbooks (`skills/*/SKILL.md` via go:embed)
internal/config/     ~/.zeta/config.json
internal/models/     models.dev catalog → presets
internal/session/    JSONL under ~/.zeta/sessions/
internal/workspace/  cwd/git context, AGENTS.md, folder trust (~/.zeta/trusted.json)
internal/search/     fuzzy filter helpers (slash palette, @ files, models)
internal/rg/         shared ripgrep invoke (tools + @ file list)
internal/plan/       proposed_plan extract + build seed
internal/paths/      ZETA_HOME
internal/image/      path normalize, sniff, data: URLs, clipboard (temp only)
internal/styles/     lipgloss tokens + banner
internal/version/    link-time Version string
internal/update/     self-update from GitHub Releases (/update)
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
text (token + optional args); on the invoking turn only, `core.RequestMsgs` appends
the playbook as a developer message. Args after the token stay on the user
message. Slash tokens must not collide with harness commands — `internal/tui`
panics at init if a skill claims `/clear`, `/config`, etc.

Bundled today: `review` (`/review` — thermo-nuclear code quality review).

### Prompt cache

Providers cache the request prefix, so the head of every request stays
byte-identical: system prompt (`internal/prompt`), mode instructions, then
durable history. Anything that changes between turns — slash-skill playbook,
environment, todos checklist — is appended as a trailing developer block in
`core.RequestMsgs`, ordered most stable first. Put new context in that tail, not the
head; a churn in the head re-reads the whole transcript.

That is why AGENTS.md is re-read only at a session boundary — `/clear`,
`/resume`, and compaction (`workspace.Context.ReloadAgents`) — and why only the
git branch is re-read per turn. Editing AGENTS.md mid-session, including with
the agent's own `edit` / `write`, has no effect until one of those boundaries,
because the content heads the cached prefix and changing it invalidates the
whole transcript; compaction is the cheap moment to pick edits up, since the
conversation layer is already being rewritten. No timestamps or other per-turn
values in the head, and no per-turn tool-set changes.

The summarizer rides the same prefix. `compact.Config.Prefix`
(`core.RequestPrefix` + `tools.Defs`) must stay byte-identical to a live turn's head,
with the head and the instruction appended after it — that is what lets the
provider serve the history being summarized from cache instead of charging full
uncached input for it. Hence the summarizer's own steering lives in a trailing
user message, not a system prompt: a different system prompt would diverge at
the first token and forfeit the hit.

Token accounting for images follows vision billing (tiles, not bytes) via
`image.Dimensions`. Do not charge an image by its encoded size: a base64
screenshot is megabytes of text but a few hundred tokens, and an inflated
estimate trips auto-compaction and misreports context fill.

The auto-compact budget check prefers a real provider count over the chars/4
estimate. Keep the pairing intact: `Model.contextTokens` is the provider's
footprint for the last request and `Model.contextMsgs` is how many history
messages it covers, so `compact.usedTokens` measures only what was appended
since. A footprint recorded against a history that has since been rewritten
(compaction, a model or mode switch) describes a request the session will no
longer send — drop it rather than trusting it. `Measured` already includes the
request envelope, so never add `Overhead` to it.

## Versioning

Pre-1.0 (`0.y.z`): no backward compatibility for session transcripts, APIs, or on-disk formats. Prefer deleting legacy paths over dual encodings or migration shims.

## Pull requests

1. Keep changes focused — one concern per PR when practical.
2. Match existing style in the package you touch.
3. Add or update tests for behavior changes.
4. Run `go test ./...` before opening the PR.
