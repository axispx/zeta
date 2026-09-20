# Permissions

Zeta asks before side effects. When the agent wants to run a shell command, change a file, or read outside the workspace, you get a prompt.

New folders are gated too: the first open in a directory asks you to **trust** it (the git root when you are in a repo). The choice is saved under `~/.zeta/trusted.json`.

## Prompt keys

Prompts list numbered choices. `↑`/`↓` (or the row's number) moves, `Enter` confirms, and clicking a row confirms it too. There are no single-letter shortcuts, so a stray keystroke — typing your next message over an open prompt — never answers it for you.

`Esc` **denies**: a prompt is a yes/no question, so the key that cancels elsewhere answers "no" here and the turn keeps going (the model sees the rejection and can adapt). To abort the whole turn, use `Ctrl+C`.

With **deny** selected, typing starts a reason on that row instead of being swallowed. `Enter` still confirms, so the reason never sends itself, and it reaches the model as `rejected: <reason>` rather than the generic denial — enough to steer the next attempt ("use the helper in foo.go", "don't touch generated files"). `↑`/`↓` leaves the field and returns to the list; `Backspace` on an empty field does the same. An empty reason is a plain deny.

| Action                   | Choices                                              |
| ------------------------ | ---------------------------------------------------- |
| Shell                    | allow once · always allow · allow for session · deny |
| Edit / write             | allow · deny                                         |
| Read (outside workspace) | allow once · allow this directory for session · deny |
| Read (`.env` / `.env.*`) | allow · always allow this file · deny                |

Ask and Plan have no shell or edit tools, but they still prompt for outside-workspace reads. Edits and writes are always allow-once-or-deny — every diff gets a review. The **always allow** row appears only when a rule can be remembered — see [Remembered rules](#remembered-rules).

## Scopes

Edit/write/read paths resolve relative to the workspace root, but absolute paths and `..` escapes are allowed — when the target is outside the workspace, the prompt is marked `(outside workspace)`.

- Outside edits/writes need per-call approval.
- In-workspace reads never prompt, except dotenv secrets (`.env`, `.env.*`; `.env.example` is allowed).
- An outside read asks to access that file's directory; a session grant covers later reads under it, not every outside path, and does not skip dotenv files.
- `grep`/`glob` and `bash`'s `workdir` stay inside the workspace.

## Remembered rules

When a prompt can be remembered it also offers **always allow** — for a shell command prefix or an in-workspace dotenv read. Edits and writes never offer it: each one is approved per call. Out-of-workspace edit/write targets, outside reads, and shell commands with chaining (`&&`, `|`, `;`, redirects, `$(…)`), keep the plain allow/deny prompt (outside reads also offer a directory-scoped session grant).

Remembered rules live in `~/.zeta/permissions.json` (or `$ZETA_HOME/permissions.json`), separate from `config.json`, and are loaded at startup. The prompt only ever writes `allow` rules; add `deny` rules by hand-editing the file.

Rules are evaluated with deny-precedence: any matching `deny` wins (even over an `allow` or a session grant); otherwise a matching `allow` runs; otherwise zeta asks as usual. Rules are checked for every tool call, so a tool-level (or `"*"`) `deny` also blocks tools that usually auto-run (`grep`, `glob`, in-workspace `read`, …). A tool-level `read` `allow` skips outside-workspace and dotenv read prompts; `allow` for other auto-run tools is redundant. Prompts that are always interactive — `ask_user` — still reach you regardless of rules.

### Rule shape

A rule matches on tool (`"bash"`, `"edit"`, `"write"`, `"read"`, or `"*"`) plus one optional field:

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
