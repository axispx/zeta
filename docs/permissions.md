# Permissions

Zeta asks before side effects. When the agent wants to run a shell command, change a file, or read outside the workspace, you get a prompt.

New folders are gated too: the first open in a directory asks you to **trust** it (the git root when you are in a repo). The choice is saved under `~/.zeta/trusted.json`.

## The prompt

Each prompt names the question, then the thing being asked about — the shell command with a `$` prompt, or the file path, marked `(outside workspace)` when it escapes:

```
   Would you like to run the following command?

   $ cd src && go test ./...

 → 1. Yes, proceed
   2. Yes, and don't ask again for commands that start with `cd src`
   3. Yes, and don't ask again for this command in this session
   4. No, and tell zeta what to do differently (esc)

   Press enter to confirm or esc to cancel
```

Rows read as answers, and a row that remembers something names the exact rule it writes, so nothing about a grant has to be inferred from the verb. The choices per tool:

| Action                   | Choices                                                        |
| ------------------------ | -------------------------------------------------------------- |
| Shell                    | yes · yes, remember one command · yes, this exact command for the session · no |
| Edit / write             | yes · no                                                       |
| Read (outside workspace) | yes · yes, this directory for session · no                     |
| Read (`.env` / `.env.*`) | yes · yes, remember this file · no                             |

Edits and writes are always yes-or-no — every diff gets a review. The **remember** row appears only when a rule can be remembered — see [Remembered rules](#remembered-rules).

## Prompt keys

Prompts list numbered choices. `↑`/`↓` (or the row's number) moves, `Enter` confirms, and clicking a row confirms it too. There are no single-letter shortcuts, so a stray keystroke — typing your next message over an open prompt — never answers it for you. The only key a row advertises is `Esc`, on **deny**, which it also answers.

`Esc` **denies**: a prompt is a yes/no question, so the key that cancels elsewhere answers "no" here and the turn keeps going (the model sees the rejection and can adapt). To abort the whole turn, use `Ctrl+C`.

With **deny** selected, typing starts a reason on that row instead of being swallowed. `Enter` still confirms, so the reason never sends itself, and it reaches the model as `rejected: <reason>` rather than the generic denial — enough to steer the next attempt ("use the helper in foo.go", "don't touch generated files"). `↑`/`↓` leaves the field and returns to the list; `Backspace` on an empty field does the same. An empty reason is a plain deny.

## Scopes

Edit/write/read paths resolve relative to the workspace root, but absolute paths and `..` escapes are allowed — when the target is outside the workspace, the prompt is marked `(outside workspace)`.

- Outside edits/writes need per-call approval.
- In-workspace reads never prompt, except dotenv secrets (`.env`, `.env.*`; `.env.example` is allowed).
- An outside read asks to access that file's directory; a session grant covers later reads under it, not every outside path, and does not skip dotenv files.
- The model must say why it needs an outside path: the `read` call carries a `reason`, shown in the prompt. An outside read without one is refused before any prompt, and the model is told to retry with a reason. A missing path has nothing to approve, so it gets neither.
- `grep`/`glob` and `bash`'s `workdir` stay inside the workspace.

## Read-only commands

A command that only reads and reports runs without a prompt: there is nothing to approve. `ls`, `cat`, `head`, `tail`, `wc`, `grep`, `find`, `sed -n`, `base64`, and the read-only forms of `git` (`status`, `log`, `diff`, `show`, `branch`) are recognised, including their flags — `head -30 go.mod` and `ls -la` never ask.

The recognition is per flag, not per program. `find -exec`, `rg --pre`, `sed -i`, `base64 -o`, `git log --output`, and `git -c` all run another command or write a file, so those still ask.

Two things keep the shortcut honest, because zeta runs shell commands unsandboxed:

- **A path outside the workspace still asks.** `cat /etc/passwd`, `head -5 ../notes`, `ls /tmp`. Read-only or not, the prompt is where you see it.
- **A dotenv secret still asks.** `cat .env`, `head -30 .env.local`, `grep -r x secrets.env`.

An allow rule can still cover these: approving `cat .env` writes a rule for exactly that command. Deciding a path is safe is yours to make explicitly, not the fallback's.

## Chained commands

The agent often runs several commands in one call (`cd src && go test ./...`). zeta splits the string on the shell's separators — `;`, `&&`, `||`, `|`, `|&`, `&`, and newlines — and judges each sub-command on its own, ignoring separators inside quotes. A part has to be allowed, read-only, or explicitly approved:

- every part allowed, read-only, or approved → the chain runs with no prompt;
- any part denied → the whole chain is denied;
- otherwise you get the prompt, for the parts that need one.

So a `go test` rule never approves `go test && rm -rf /`, in either direction: the tail is not carried by the head's allow, and a deny on any part stops the chain. A chain that only reads — `cat go.mod; ls cmd internal; head -30 Makefile` — never prompts at all.

A redirect that names no file is dropped before the command is judged: `2>&1`, `>&2`, `2>&-`, and anything aimed at `/dev/null` (`2>/dev/null`, `&>/dev/null`, `</dev/null`). So `cat go.mod 2>/dev/null | head -20` is `cat go.mod` and `head -20`, and runs with no prompt.

Anything else cannot be split: a redirect to a real file (`>`, `>>`, `<`), here-docs, command substitution (`$(…)`, backticks), subshells, and unbalanced quotes. The whole call is matched as one string and prompts as one unit. That is deliberately conservative — a redirect to a file is where a command can name a path the prompt never showed, so `go test ./... > out.txt` prompts on the full string rather than on `go test`.

Nesting is only ever used to deny. A `deny` rule also fires on a command hidden inside one of those unsplittable forms — `echo "$(git clean -f)"`, `(cd /tmp && git clean -f)`, a `for` body — but an `allow` rule never looks inside them, so `echo $(go test)` still asks.

A remembered rule matches a sub-command from its first word, so it also covers that command with different arguments: remembering `go test ./...` covers `go test ./... -run TestFoo`. It never covers a different chain — that is what the per-sub-command evaluation above is for.

**Remembering** a chain writes exactly one rule, for the first sub-command that still needed a decision, kept whole. `npm install && go test ./...` remembers `` `npm install` ``; approve the same chain again and the next row is `` `go test ./...` ``, so a chain converges one grant at a time. A part that only reads, or that a rule already covers, is skipped — which is why `cd src && go test ./...` offers a rule for `` `go test ./...` `` and not for `cd src`. A call with nothing left to remember offers no remember row at all.

One grant per prompt is deliberate: the row can always name the whole rule on a single line, and approving it never blesses a part of the command that was not on screen.

The model may propose a **prefix** instead — `["git", "pull"]` for a `git pull …` call — which the row shows as ``commands that start with `git pull` ``. A proposal is only offered when it covers the whole call and is not a shell, interpreter, or runner: a rule for `bash`, `python`, `npm run`, or `rm` alone would stand for whatever follows it, so those are refused and the part rule above is offered instead.

## Wrappers

Before a command is judged, a fixed set of wrappers that only run the rest of the command is removed: `timeout`, `time`, `nice`, `nohup`, `stdbuf`, and the builtins `command`, `builtin`, `noglob`. A rule for `go test` therefore covers `timeout 60 go test ./...`, a deny for `rm` still stops `nohup rm x`, and a remembered rule is written for the command that runs, not the wrapper. A wrapper with options zeta does not recognise (`timeout --weird 5 …`, `command -v`) is left as written and asks. `env`, `sudo`, `npx`, `docker exec`, `watch` and `find -exec` are not on the list and keep asking.

## Auto review

On by default, judged by the model you are chatting with. A shell command that rules and the read-only list did not settle is classified before the prompt, and runs without one only when the classifier is confident it is safe. Everything else gets the prompt exactly as it would without review.

It only ever says yes. It is never asked about a call a `deny` rule rejected, it cannot override a rule, and a risky label, a low-confidence answer, a timeout or an error all fall back to the prompt (each of those shows a one-line reason under the command in the prompt; an approval is silent).

A command is classified by what the worst thing in it clearly does:

| Label              | Meaning                                                                 |
| ------------------ | ----------------------------------------------------------------------- |
| `read_only`        | only reads or prints                                                    |
| `local_reversible` | writes inside the project in a way git or a rebuild undoes: builds, tests, formatting, caches, ordinary project scripts, deleting build output |
| `network_fetch`    | uses the network only to download or look up, sending nothing out: `curl` GET, `git fetch` / `pull` / `clone`, installing the project's dependencies |
| `risky`            | could do serious or hard-to-undo harm, or leak data: deleting or rewriting work (`rm -rf` of source, `git reset --hard`, force push), changing remote state (`git push`, publish, deploy, `POST`), sending files or secrets out (`curl -d @file`, `nc`), running code the text does not show (`curl \| sh`), `sudo`, system-wide installs |

The network is not risky in itself: what matters is what goes out and what runs afterwards. A download is `network_fetch`; a request that carries local files or secrets, or changes something remote, is `risky`. Judgement follows what the command text shows, not what it could conceivably do, and anything the classifier cannot place is `risky`.

`read_only`, `local_reversible` and `network_fetch` are approved by default (`allow` in the config narrows or widens that). When a command is not approved, the prompt shows the classifier's one-line reason where the backend gives one. Only the sub-commands no rule covers are put to the classifier, so `go test ./... && make build` asks about `make build` alone.

These never reach the classifier and always prompt, whatever it would say: a command it cannot split (file redirect, here-doc, `$(…)`, subshell), a dotenv secret, and a path outside the workspace.

Two backends, chosen in `/config` (or by `backend` in the config):

- **Active model** (default) — the model you are chatting with. It has no probabilities, so it must answer with a label and `high` certainty.
- **Jev** (TypeSafe), opt-in — needs `jev_api_key` in the config; choosing it without a key prompts for one. It returns a probability per label; a command is approved when the allowed labels together reach 80%, so a command split between `read_only` and `local_reversible` still passes. Jev returns no reason, so its prompts show a generic one.

Having a Jev key does not switch backends: commands go to Jev only when you choose it. Choose **Off** in `/config` (or set `"disabled": true`) to be asked about every unsettled command.

Either way the command, its unsettled parts, the git branch, the working directory and your latest message (capped at 1000 characters) are sent to that backend. Your message is the only text treated as trusted: a command that goes beyond what it asked for is pushed toward a riskier label. Turn it off if you are not comfortable with that.

## Remembered rules

**Allow for session** is scoped to the exact command the prompt showed, not to the shell: approving `npm install` in this session does not approve `rm -rf ~`. The row says "this command in this session" and means it. It follows that the session row is not a way to stop being asked about the shell in general — use **remember** for that, one command at a time.

When a prompt can be remembered it also offers a **remember** row — for one shell sub-command (see [Chained commands](#chained-commands)) or an in-workspace dotenv read. Edits and writes never offer it: each one is approved per call. Out-of-workspace edit/write targets, outside reads, and shell commands that cannot be split keep the plain yes/no prompt (outside reads also offer a directory-scoped session grant).

Remembered rules live in `~/.zeta/permissions.json` (or `$ZETA_HOME/permissions.json`), separate from `config.json`, and are loaded at startup. The prompt only ever writes `allow` rules; add `deny` rules by hand-editing the file.

Rules are evaluated with deny-precedence: any matching `deny` wins (even over an `allow` or a session grant); otherwise a matching `allow` runs; otherwise a command that only reads runs (see [Read-only commands](#read-only-commands)); otherwise zeta asks. Rules are checked for every tool call, so a tool-level (or `"*"`) `deny` also blocks tools that usually auto-run (`grep`, `glob`, in-workspace `read`, …). A tool-level `read` `allow` skips outside-workspace and dotenv read prompts; `allow` for other auto-run tools is redundant. Prompts that are always interactive — `ask_user` — still reach you regardless of rules.

### Rule shape

A rule matches on tool (`"bash"`, `"edit"`, `"write"`, `"read"`, or `"*"`) plus one optional field:

- `command_prefix` (bash) — "commands that start with…", matched on whole words and against one sub-command at a time. Remembering stores the sub-command as it was shown (`go test ./...`), so it covers `go test ./... -v` but not plain `go test -v`. Write a shorter prefix by hand to broaden it — including one the prompt would refuse to propose, such as `rm -rf` or `git`: the guard against a broad `bash`/`python`/`rm` rule is on what the *prompt* offers, not on the file. `bash` is unsandboxed, so a rule is a guardrail, not a sandbox.
- `command` (bash) — a glob against one sub-command's text: `*` matches within a segment, `**` crosses `/`, `?` matches one character, and a pattern without `*` is exact. A chain's raw string is never globbed as a whole, so `git **` cannot cover `git status && rm -rf /`.
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
