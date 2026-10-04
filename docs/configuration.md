# Configuration

Use **`/config`** in the app. Settings live at `~/.zeta/config.json` (or `$ZETA_HOME/config.json`).

`/config` has two tabs, **Providers** and **Settings** (`Tab` / `Shift+Tab` to switch). On Providers:

- **Configured** — turn models on/off for providers you already set up
- **Providers** — add a catalog provider (API key, then enable models; `Ctrl+A` toggles all)
- **Custom** — your own OpenAI-compatible endpoint

Optional: set a preferred build model after plan approve with `"defaults": { "build": "provider/model" }` in the config file. Per-model `reasoning_effort` is set from `/model` with Tab, which cycles that model's supported levels (from models.dev; e.g. `low`/`medium`/`high`/`xhigh`/`max`, or a model's shorter list). Models with no catalog effort data fall back to `low`/`medium`/`high`.

Example shape (prefer the UI over hand-editing):

```json
{
  "active": "deepseek/deepseek-v4-flash",
  "providers": {
    "deepseek": {
      "name": "DeepSeek",
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "sk-...",
      "models": {
        "deepseek-v4-flash": {
          "name": "V4 Flash",
          "context_window": 1000000,
          "reasoning_efforts": ["low", "high", "max"],
          "reasoning_effort": "high"
        }
      }
    }
  }
}
```

## Auto review

Optional, off by default; see [Permissions](permissions.md#auto-review) for what it does. Toggle it in `/config` on the **Settings** tab (`Tab` switches tabs; `Enter` toggles Auto review); `Ctrl+K` on that row sets the Jev key. Turning it on says where commands will be sent. Changes apply to the next tool call.

```json
{
  "review": {
    "enabled": true,
    "jev_api_key": "jv_...",
    "allow": ["read_only", "local_reversible"]
  }
}
```

`jev_api_key` (or `TYPESAFE_API_KEY`) selects TypeSafe's Jev model; without one the active chat model reviews. `allow` is optional and defaults to the two labels shown.

## Web search

When the agent searches the web, **Exa** is used by default (shared free quota, no key required). Optional env vars:

| Variable                  | Purpose                                                             |
| ------------------------- | ------------------------------------------------------------------- |
| `EXA_API_KEY`             | Your own Exa quota ([dashboard](https://dashboard.exa.ai/api-keys)) |
| `PARALLEL_API_KEY`        | Parallel search quota                                               |
| `ZETA_WEBSEARCH_PROVIDER` | `exa` (default) or `parallel`                                       |

Fetching a URL is separate and works without those keys. Private/loopback addresses are blocked.

## Where data lives

Everything is under `~/.zeta` (override with `ZETA_HOME`):

```
~/.zeta/
  config.json
  permissions.json    # remembered allow/deny rules
  trusted.json        # folders you confirmed
  sessions/…          # your chat history
```

Sessions appear after the first message; Zeta generates a short title for the `/resume` picker.

Rule file format: [Permissions](permissions.md).
