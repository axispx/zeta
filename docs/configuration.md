# Configuration

Use **`/config`** in the app. Settings live at `~/.zeta/config.json` (or `$ZETA_HOME/config.json`).

`/config` has two tabs, **Providers** and **Settings** (`Tab` / `Shift+Tab` to switch). On Providers:

- **Configured** — turn models on/off for providers you already set up
- **Providers** — add a catalog provider (API key, then enable models; `Ctrl+A` toggles all)
- **Custom** — your own OpenAI-compatible endpoint

## OpenAI

OpenAI is one provider with two ways in, like xAI. Pick **OpenAI** under *Providers*
and choose **API Key** (a platform key from `platform.openai.com`, billed per token,
Chat Completions) or **OAuth** (sign in with your ChatGPT account, Codex models on
your plan). Switching method reconnects the provider: it replaces the base URL,
credentials and model list.

With OAuth, zeta opens `auth.openai.com` and catches the redirect on `localhost:1455`, the loopback
port the OAuth client is registered with. The sign-in covers ChatGPT Plus/Pro and
team plans; usage is metered by your plan's quota, not billed per token. The Codex
backend does not accept a platform key, which is why the two methods use different
endpoints.

On connect, zeta asks the Codex backend which models the account may use and stores
that list, so `/config` can show it later without a fresh token. The list is
entitlement-gated server-side and changes over time (models retire; new ones appear),
so reconnecting from `/config` refreshes it. Because the backend serves the
Responses API rather than Chat Completions, this provider uses a transport of its
own (`internal/ai/codex.go`); the rest of zeta does not change.

`/usage` reports tokens as usual plus the plan quota, when the provider reported it:
the rolling window, the weekly window, and when the window resets. A plan that
reports quota only on request is fetched when you run `/usage`.

For the xAI device-code flow (`xai`), the panel shows a URL and a code to type.

Per-model `reasoning_effort` is set from `/model` with Tab, which cycles that model's supported levels (from models.dev; e.g. `low`/`medium`/`high`/`xhigh`/`max`, or a model's shorter list). Models with no catalog effort data fall back to `low`/`medium`/`high`.

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

An OAuth provider stores tokens instead of a key. For Codex, `account_id` is the
ChatGPT account the tokens are scoped to and is required by the backend:

```json
{
  "active": "openai/gpt-5.5",
  "providers": {
    "openai": {
      "name": "OpenAI",
      "base_url": "https://chatgpt.com/backend-api/codex",
      "oauth": {
        "access_token": "eyJ...",
        "refresh_token": "v1.M...",
        "account_id": "8f4c…",
        "expires_at": 1780000000000,
        "token_type": "bearer"
      },
      "models": {
        "gpt-5.5": { "context_window": 272000, "reasoning_efforts": ["low", "medium", "high"] }
      }
    }
  }
}
```

## Auto review

Optional, off by default; see [Permissions](permissions.md#auto-review) for what it does. Toggle it in `/config` on the **Settings** tab (`Tab` switches tabs; `Enter` toggles Auto review). Turning it on first asks where commands go — **Jev** or the **Active model** — and choosing Jev prompts for its key when none is set; `Ctrl+K` on the row sets the key directly. Changes apply to the next tool call.

```json
{
  "review": {
    "enabled": true,
    "backend": "jev",
    "jev_api_key": "jv_...",
    "allow": ["read_only", "local_reversible", "network_fetch"]
  }
}
```

`backend` is `jev` or `model`; it names where reviewed commands are sent. `jev_api_key` (or `TYPESAFE_API_KEY`) is the Jev key. `allow` is optional and defaults to the three labels shown; the full set is `read_only`, `local_reversible`, `network_fetch` and `risky`.

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
  cache/models.json   # models.dev catalog
  cache/codex-models.json  # ChatGPT models the account may use
```

Sessions appear after the first message; Zeta generates a short title for the `/resume` picker.

Rule file format: [Permissions](permissions.md).
