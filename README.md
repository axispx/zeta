# Zeta

Anti vibe-coding AI agent for your terminal.

Use any OpenAI-compatible provider — OpenAI, xAI, DeepSeek, Kimi, and more, plus custom endpoints. Providers and models come from [models.dev](https://models.dev).

## Features

- **Build / Ask / Plan** — implement with tools, read-only Q&A, or plan first then approve into Build
- **Permission prompts** — shell and file changes ask before running (Build mode)
- **Folder trust** — first open in a directory asks before loading project files
- **Local sessions** — chat history stays on your machine; resume anytime with `/resume`
- **Auto-compaction** — long chats summarize older context when the model window fills up
- **Multi-provider** — API keys and models managed in-app with `/config`

## Install

```bash
curl -fsSL https://zeta.asy.sh/install.sh | sh
```

Then run `zeta` from a project directory.

## Quick start

1. Start Zeta in the repo you care about.
2. On first open in a folder, confirm you **trust** it (git root when in a repo). Choice is saved under `~/.zeta/trusted.json`.
3. Run **`/config`**, add a provider API key (or a custom endpoint).
4. Switch models with **`/model`** if you want.
5. Type a prompt and press **Enter**.

Each launch opens a **new session**. Use `/resume` to continue an earlier one.

## Modes

Cycle modes with **Shift+Tab**.

| Mode      | What it does                                                                                                                                                    |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Build** | Writes code and runs commands. Shell and file edits ask for permission first.                                                                                   |
| **Ask**   | Questions only — reads the codebase, no edits.                                                                                                                  |
| **Plan**  | Plans without changing files. When ready: approve, revise, or discard. Approve picks a build model, clears context, switches to Build, and starts implementing. |

## Keyboard shortcuts

| Key                                    | Action                                      |
| -------------------------------------- | ------------------------------------------- |
| `Enter`                                | Send (busy: queue text; empty+queue: send now) |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N`       | Prompt history                              |
| `Ctrl+Q`                               | Manage follow-ups (`↑`/`↓`, Enter send, `e` edit, `d` remove) |
| `Shift+Tab`                            | Cycle mode (build → ask → plan)             |
| `Shift+Enter` / `Ctrl+J` / `Alt+Enter` | Newline                                     |
| `@`                                    | File mention picker (gitignore-aware; Tab/Enter insert) |
| `Esc`                                  | Cancel edit / leave queue / cancel turn (queue kept) |
| `Ctrl+C`                               | Leave edit/focus → interrupt → clear queue → quit |
| Mouse / `PgUp` / `PgDn`                | Scroll                                      |
| Drag transcript                        | Select text and copy on release (no scrollbar) |

Type `@` in the composer to fuzzy-find a workspace file (respects `.gitignore` via ripgrep). Tab or Enter inserts `@path` and a trailing space; Esc closes the list without clearing the draft.

### Permissions (Build)

When the agent wants to run a shell command or change a file:

| Action       | Keys                                                         |
| ------------ | ------------------------------------------------------------ |
| Shell        | `[a]` allow once · `[s]` allow for this session · `[d]` deny |
| Edit / write | `[a]` allow · `[d]` deny (every time)                        |

You can also click, or use `↑`/`↓` + Enter. `Esc` cancels. Ask and Plan never ask for permission.

### Choosing options

Sometimes the agent asks a multiple-choice question (plus freeform **Other**):

| Key       | Action         |
| --------- | -------------- |
| `↑` / `↓` | Move           |
| `Enter`   | Confirm        |
| `1`–`9`   | Jump to option |
| Type      | Fill **Other** |
| `Esc`     | Cancel         |

### Terminal notes

`Shift+Enter` works best in terminals that report modified keys (Kitty keyboard protocol): Ghostty, Kitty, iTerm2 3.5+, Alacritty, WezTerm (`enable_kitty_keyboard = true`).

If `Shift+Enter` is remapped (common in iTerm), remove that binding or use `Ctrl+J` / `Alt+Enter` for newlines.

**Copy:** drag in the transcript to select (the scrollbar is not included); releasing the mouse copies to the clipboard. Leaving the terminal mid-drag counts as release.

While the agent is working, `Enter` with text queues a follow-up. Empty `Enter` (or queue-focus Enter on an item) interrupts the current turn and sends that follow-up immediately. Open the queue with `Ctrl+Q` (`↑`/`↓` move, Enter send selected, `e` edit, `d` remove, Esc back). Queued items also drain one at a time when a turn finishes on its own (unless you are editing the next item or typing a draft). `Esc` cancels an edit, leaves queue focus, or cancels the turn (queue kept). `Ctrl+C` leaves edit/queue focus first, then runs the interrupt ladder (dismiss overlays, cancel turn), then clears any remaining follow-ups, then quits.

## Commands

Type `/` for autocomplete.

| Command    | Description                                      |
| ---------- | ------------------------------------------------ |
| `/clear`   | Start a new session                              |
| `/compact` | Summarize older context now                      |
| `/resume`  | Open a previous session                          |
| `/model`   | Switch model                                     |
| `/config`  | Manage providers and models                      |
| `/review`  | Strict code-quality review of the current branch |

Long sessions compact automatically when context runs low; `/compact` does the same on demand.

## Configuration

Use **`/config`** in the app. Settings live at `~/.zeta/config.json` (or `$ZETA_HOME/config.json`).

In `/config`:

- **Configured** — turn models on/off for providers you already set up
- **Providers** — add a catalog provider (API key, then enable models; `Ctrl+A` toggles all)
- **Custom** — your own OpenAI-compatible endpoint

Optional: set a preferred build model after plan approve with `"defaults": { "build": "provider/model" }` in the config file.

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
        "deepseek-v4-flash": { "name": "V4 Flash", "context_window": 1000000 }
      }
    }
  }
}
```

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
  sessions/…   # your chat history
```

Sessions appear after the first message; Zeta generates a short title for the `/resume` picker.

## Contributing

Development setup and project layout: [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
