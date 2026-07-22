# zeta

Command-line AI coding agent.

## Run

```bash
make run     # go run
make build   # bin/zeta
make install # ~/.local/bin/zeta
```

**Keys:** `enter` send · `shift+tab` cycle mode (build / ask / plan) · `shift+enter` / `ctrl+j` / `alt+enter` newline · `esc` / `ctrl+c` quit · mouse / pgup/pgdn scroll

**Commands:** type `/` for autocomplete · `/clear` new session · `/resume` pick a previous session · `/model` switch model

**Modes:** `build` implements with tools (`read` / `edit` / `grep`) · `ask` Q&A with read-only tools · `plan` plans with read-only tools

`shift+enter` needs a terminal that can disambiguate modified keys (Kitty keyboard protocol / CSI-u). Ghostty, Kitty, iTerm2 3.5+, Alacritty, WezTerm (`enable_kitty_keyboard = true`).
If `shift+enter` is remapped in the terminal (common Claude Code / iTerm “send text” setup), fix or remove that binding so the app sees the real key.

## Layout

```
cmd/zeta/            entry
internal/tui/        bubbletea v2 model (viewport + dynamic textarea)
internal/ai/         OpenAI-compatible streaming client + tool calls
internal/tools/      read / edit / grep
internal/config/     ~/.zeta/config.json
internal/session/    JSONL transcripts under ~/.zeta/sessions/
internal/paths/      ZETA_HOME resolution
internal/styles/     lipgloss tokens + banner
```

## Home

Everything lives under `~/.zeta` (override with `ZETA_HOME`):

```
~/.zeta/
  config.json
  sessions/<cwd-key>/
    index.json            # [{id, name, created, updated}, ...] for pickers
    <id>.jsonl            # typed events: session + message
```

Launching zeta always starts a fresh empty session. Prior sessions are available via `/resume`. A session's JSONL and index entry are created on the first message; after that prompt, the model generates a short session title for the picker.

## Config

Path: `$ZETA_HOME/config.json` (default `~/.zeta/config.json`).

```json
{
  "model": "deepseek/deepseek-v4-flash",
  "providers": [
    {
      "id": "deepseek",
      "name": "DeepSeek",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "sk-...",
      "models": {
        "deepseek-v4-flash": { "name": "V4 Flash", "context_window": 1000000 },
        "deepseek-v4-pro": { "name": "V4 Pro", "context_window": 1000000 }
      }
    },
    {
      "id": "xai",
      "name": "xAI",
      "base_url": "https://api.x.ai/v1",
      "api_key": "...",
      "models": {
        "grok-3": { "name": "Grok 3", "context_window": 131072 },
        "grok-3-mini": { "name": "Grok 3 Mini", "context_window": 131072 }
      }
    }
  ]
}
```

- **`model`** — active model as `provider_id/model_id`
- Each provider: **`id`**, **`name`** (display, optional), **`api_key`**, **`base_url`**, **`models`** (map of id → `{ "name": "...", "context_window": N }`)
- **`context_window`** — required; max tokens for that model (used for the footer context %). DeepSeek V4 is 1M; see [pricing](https://api-docs.deepseek.com/quick_start/pricing/).
