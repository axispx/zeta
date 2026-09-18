# Zeta

Anti vibe-coding AI agent for your terminal.

Use any OpenAI-compatible provider — OpenAI, xAI, DeepSeek, Kimi, and more, plus custom endpoints. 

## Features

- **Build / Ask / Plan** — implement with tools, read-only Q&A, or plan first then approve into Build
- **Permission prompts** — shell, file changes, and reads outside the workspace ask before running
- **Local sessions** — chat history stays on your machine; resume anytime with `/resume`
- **Auto-compaction** — long chats summarize older context when the model window fills up
- **Multi-provider** — API keys and models managed in-app with `/config`. Providers and models come from [models.dev](https://models.dev).

## Install

```bash
curl -fsSL https://zeta.asy.sh/install.sh | sh
```

Then run `zeta` from a project directory.

## Quick start

1. Start Zeta in the repo you care about.
2. Confirm you **trust** the folder when asked.
3. Run **`/config`** and add a provider API key.
4. Type a prompt and press **Enter**.

Each launch opens a **new session**. Use `/resume` to continue an earlier one.

## Modes

Cycle modes with **Shift+Tab**.

| Mode      | What it does                                                                                                                                                    |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Build** | Writes code and runs commands. Shell, file edits, and outside-workspace reads ask first.                                                                |
| **Ask**   | Questions only — reads the codebase, no edits. Outside-workspace reads still prompt.                                                                    |
| **Plan**  | Plans without changing files. When ready: approve, revise, or discard. Approve picks a build model, clears context, switches to Build, and starts implementing. |

## Commands

Type `/` for autocomplete.

| Command    | Description                                      |
| ---------- | ------------------------------------------------ |
| `/clear`   | Start a new session                              |
| `/compact` | Summarize older context now                      |
| `/usage`   | Session token totals, by model (input, output, cached) |
| `/resume`  | Open a previous session                          |
| `/model`   | Switch model; Tab cycles reasoning (Low / Medium / High) |
| `/config`  | Manage providers and models                      |
| `/update`  | Update to the latest release                     |
| `/review`  | Strict code-quality review of the current branch |

Long sessions compact automatically when context runs low; `/compact` does the same on demand.

## Docs

- [Permissions](docs/permissions.md) — prompts, path scopes, remembered rules
- [Configuration](docs/configuration.md) — providers, models, web search, data layout
- [Keyboard and terminal](docs/keyboard.md) — full shortcut list, follow-up queue, terminal quirks

## Contributing

Development setup and project layout: [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
