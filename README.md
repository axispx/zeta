# Zeta

Anti vibe-coding AI agent for your terminal.

Use any OpenAI-compatible provider — OpenAI, xAI, DeepSeek, Kimi, and more, plus custom endpoints. Providers and models come from [models.dev](https://models.dev).

## Features

- **Build / Ask / Plan** — implement with tools, read-only Q&A, or plan first then approve into Build
- **Permission prompts** — shell, file changes, and reads outside the workspace ask before running, with optional remembered rules
- **Folder trust** — first open in a directory asks before loading project files
- **File drop** — drag files from your file manager onto the window to add their paths to the prompt
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
| **Build** | Writes code and runs commands. Shell, file edits, and outside-workspace reads ask first.                                                                |
| **Ask**   | Questions only — reads the codebase, no edits. Outside-workspace reads still prompt.                                                                    |
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
| `Tab` (in `/model`)                    | Cycle reasoning (low → medium → high)       |
| `Esc`                                  | Cancel edit / leave queue / cancel turn (queue kept) |
| `Ctrl+C`                               | Leave edit/focus → interrupt → clear queue → quit |
| Mouse / `PgUp` / `PgDn`                | Scroll                                      |
| Drag transcript                        | Select text and copy on release (no scrollbar) |

Type `@` in the composer to fuzzy-find a workspace file (respects `.gitignore` via ripgrep). Tab or Enter inserts `@path` and a trailing space; Esc closes the list without clearing the draft.

Drop a file (or several) from your file manager onto the window and its path lands in the prompt: a space is added before it when the cursor follows text, and one after it, so the path stays its own token. Dropped image files still attach as `[Image N]`.

### Permissions

When the agent wants to run a shell command, change a file, or read outside the workspace:

| Action                         | Keys                                                                            |
| ------------------------------ | ------------------------------------------------------------------------------- |
| Shell                          | `[a]` allow once · `[p]` always allow · `[s]` allow for session · `[d]` deny     |
| Edit / write                   | `[a]` allow · `[p]` always allow · `[d]` deny                                   |
| Read (outside workspace)       | `[a]` allow once · `[s]` allow this directory for session · `[d]` deny          |
| Read (`.env` / `.env.*`)       | `[a]` allow · `[p]` always allow this file · `[d]` deny                         |

You can also click, or use `↑`/`↓` + Enter. `Esc` cancels. Ask and Plan have no shell or edit tools, but they still prompt for outside-workspace reads. The `[p]` row appears only when a rule can be remembered — see [Remembered rules](#remembered-rules).

Edit/write/read paths resolve relative to the workspace root, but absolute paths and `..` escapes are allowed — when the target is outside the workspace, the prompt is marked `(outside workspace)`. Outside edits/writes need per-call approval. In-workspace reads never prompt, except dotenv secrets (`.env`, `.env.*`; `.env.example` is allowed). An outside read asks to access that file's directory; a session grant covers later reads under it, not every outside path, and does not skip dotenv files. `grep`/`glob` and `bash`'s `workdir` stay inside the workspace.

#### Remembered rules

When a prompt can be remembered it also offers `[p]` **always allow** — for a shell command prefix, an in-workspace edit/write file, or an in-workspace dotenv read. Out-of-workspace edit/write targets, outside reads, and shell commands with chaining (`&&`, `|`, `;`, redirects, `$(…)`), keep the plain allow/deny prompt (outside reads also offer a directory-scoped session grant).

Remembered rules live in `~/.zeta/permissions.json` (or `$ZETA_HOME/permissions.json`), separate from `config.json`, and are loaded at startup. The prompt only ever writes `allow` rules; add `deny` rules by hand-editing the file.

Rules are evaluated with deny-precedence: any matching `deny` wins (even over an `allow` or a session grant); otherwise a matching `allow` runs; otherwise zeta asks as usual. Rules are checked for every tool call, so a tool-level (or `"*"`) `deny` also blocks tools that usually auto-run (`grep`, `glob`, in-workspace `read`, …). A tool-level `read` `allow` skips outside-workspace and dotenv read prompts; `allow` for other auto-run tools is redundant. Prompts that are always interactive — `ask_user` — still reach you regardless of rules. A rule matches on tool (`"bash"`, `"edit"`, `"write"`, `"read"`, or `"*"`) plus one optional field:

- `command_prefix` (bash) — "commands that start with…", matched on whole words. Remembering `go test ./...` stores `go test` and also covers `go test -v`. A command whose second word is a flag keeps the whole string (`rm -rf /`), so it never broadens to every `rm`. `bash` is unsandboxed, so this is a guardrail, not a sandbox.
- `command` (bash) — a glob against the raw command string: `*` matches within a segment, `**` crosses `/`, `?` matches one character, and a pattern without `*` is exact.
- `path` (edit/write/read) — a glob against the workspace-relative path; out-of-workspace edit/write never match one. Out-of-workspace reads match the absolute path instead, so a deny like `**/.ssh/**` still applies.

```json
{
  "rules": [
    { "tool": "bash", "command_prefix": "go test", "action": "allow" },
    { "tool": "bash", "command_prefix": "git push", "action": "deny" },
    { "tool": "edit", "path": "src/**", "action": "deny" },
    { "tool": "read", "path": "**/.ssh/**", "action": "deny" }
  ]
}
```

Hand-edit the file to add rules or to broaden one beyond what the prompt remembers.

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
| `/model`   | Switch model; Tab cycles reasoning (low / medium / high) |
| `/config`  | Manage providers and models                      |
| `/update`  | Update to the latest release                     |
| `/review`  | Strict code-quality review of the current branch |

Long sessions compact automatically when context runs low; `/compact` does the same on demand.

## Configuration

Use **`/config`** in the app. Settings live at `~/.zeta/config.json` (or `$ZETA_HOME/config.json`).

In `/config`:

- **Configured** — turn models on/off for providers you already set up
- **Providers** — add a catalog provider (API key, then enable models; `Ctrl+A` toggles all)
- **Custom** — your own OpenAI-compatible endpoint

Optional: set a preferred build model after plan approve with `"defaults": { "build": "provider/model" }` in the config file. Per-model `reasoning_effort` (`low` / `medium` / `high`) is set from `/model` with Tab.

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
        "deepseek-v4-flash": { "name": "V4 Flash", "context_window": 1000000, "reasoning_effort": "medium" }
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
  permissions.json
  sessions/…   # your chat history
```

Sessions appear after the first message; Zeta generates a short title for the `/resume` picker.

## Contributing

Development setup and project layout: [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
