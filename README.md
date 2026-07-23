# Zeta

Command-line AI coding agent.

## Run

```bash
make run     # go run
make build   # bin/zeta
make install # ~/.local/bin/zeta
```

**Keys:** `enter` send · `shift+tab` cycle mode (build / ask / plan) · `shift+enter` / `ctrl+j` / `alt+enter` newline · `ctrl+c` quit · mouse / pgup/pgdn scroll

**Commands:** type `/` for autocomplete · `/clear` new session · `/resume` pick a previous session · `/model` switch model · `/config` manage providers & models

**Modes:** `build` implements with tools (`read` / `edit` / `grep` / `bash` / `websearch` / `webfetch`) · `ask` Q&A with read-only tools · `plan` plans with read-only tools

`shift+enter` needs a terminal that can disambiguate modified keys (Kitty keyboard protocol / CSI-u). Ghostty, Kitty, iTerm2 3.5+, Alacritty, WezTerm (`enable_kitty_keyboard = true`).
If `shift+enter` is remapped in the terminal (common Claude Code / iTerm “send text” setup), fix or remove that binding so the app sees the real key.

## Layout

```
cmd/zeta/            entry
internal/tui/        bubbletea v2 model (viewport + dynamic textarea)
internal/ai/         OpenAI-compatible streaming client + tool calls
internal/tools/      read / edit / grep / bash / websearch / webfetch
internal/config/     ~/.zeta/config.json
internal/models/     models.dev catalog cache → provider presets
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

Launching Zeta always starts a fresh empty session. Prior sessions are available via `/resume`. A session's JSONL and index entry are created on the first message; after that prompt, the model generates a short session title for the picker.

## Config

Path: `$ZETA_HOME/config.json` (default `~/.zeta/config.json`).

```json
{
  "active": "deepseek/deepseek-v4-flash",
  "providers": {
    "deepseek": {
      "name": "DeepSeek",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "sk-...",
      "models": {
        "deepseek-v4-flash": { "name": "V4 Flash", "context_window": 1000000 },
        "deepseek-v4-pro": { "name": "V4 Pro", "context_window": 1000000 }
      }
    },
    "xai": {
      "name": "xAI",
      "base_url": "https://api.x.ai/v1",
      "api_key": "...",
      "models": {
        "grok-3": { "name": "Grok 3", "context_window": 131072 },
        "grok-3-mini": { "name": "Grok 3 Mini", "context_window": 131072 }
      }
    }
  }
}
```

- **`active`** — active model as `provider_id/model_id`
- **`providers`** — map of provider id → `{ "name", "api_key", "base_url", "models" }`
- **`models`** — map of model id → `{ "name": "...", "context_window": N, "disabled": true? }`
- **`context_window`** — required; max tokens for that model (used for the footer context %). DeepSeek V4 is 1M; see [pricing](https://api-docs.deepseek.com/quick_start/pricing/).
- **`disabled`** — optional; when true the model stays configured but is hidden from `/model` (catalog providers use this for toggle).
- **`custom`** — optional; when true the provider is a user-defined endpoint (rename / add / edit / remove models). Catalog providers omit this and only allow enable/disable + API key updates.

Use `/config` to configure providers. The list is loaded from [models.dev](https://models.dev) (cached under `$ZETA_HOME/cache/models.json`, 5‑minute TTL) and filtered to OpenAI-compatible APIs. If models.dev is unreachable, the existing cache is kept and reused. **Configured** opens model activation; new **Providers** ask for an API key first, then let you enable models (`ctrl+a` toggles all). **Custom** is for your own endpoint.

## Web search

`websearch` uses Exa hosted MCP by default (no key required; shared free quota). Set `ZETA_WEBSEARCH_PROVIDER=parallel` to use Parallel instead.

`webfetch` fetches a URL directly (HTML → markdown by default). Blocks private/loopback addresses.

Optional env vars:

- `EXA_API_KEY` — use your Exa quota ([dashboard](https://dashboard.exa.ai/api-keys))
- `PARALLEL_API_KEY` — use your Parallel quota
- `ZETA_WEBSEARCH_PROVIDER=exa|parallel` — select search provider (default: exa)

On search rate limit, the tool returns a message pointing at those keys.
