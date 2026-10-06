# Keyboard and terminal

## Shortcuts

| Key                                    | Action                                      |
| -------------------------------------- | ------------------------------------------- |
| `Enter`                                | Send (busy: steer the running turn) |
| `Tab`                                  | Busy: queue the text as a follow-up for after the turn |
| `Alt+↑` / `Alt+↓`                      | Copy queued follow-ups into the input, newest first / back toward newest |
| `↑` / `↓` or `Ctrl+P` / `Ctrl+N`       | Prompt history                              |
| `Shift+Tab`                            | Cycle mode (build → plan)                   |
| `Shift+Enter` / `Ctrl+J` / `Alt+Enter` | Newline                                     |
| `@`                                    | File mention picker (gitignore-aware; Tab/Enter insert) |
| `Tab` (in `/model`)                    | Cycle the model's reasoning levels (off → its supported values) |
| `Esc`                                  | Busy: interrupt and send pending steers, else cancel the turn (queued follow-ups return to the input); denies an open permission prompt |
| `Ctrl+C`                               | Interrupt → clear queue → quit |
| Mouse / `PgUp` / `PgDn`                | Scroll                                      |
| Drag transcript                        | Select text and copy on release |

## Follow-up queue

While the agent is working there are two ways to add a message:

- **Steer** — `Enter`. The message joins the running turn at its next tool boundary (or right after the model's final answer), so the agent sees it without stopping. Until then it is listed above the input under **Steering**. `Esc` interrupts the turn and sends the oldest pending steer right away; any others stay pending.
- **Queue** — `Tab`. Listed under **Queued**. The message waits and starts its own turn once this one ends; queued messages go out one at a time, oldest first. `Alt+↑` copies the newest queued message into the input (again for older ones; `Alt+↓` goes back toward newer and then empties the input) without removing it; sending the copy adds it at the bottom of the queue.

`Esc` with nothing steered cancels the turn and moves any steers and queued messages back into the input, one per line, above a draft already there. A steer the agent never picked up before the turn ended is sent as the next turn. A skill command (`/review …`) is queued rather than steered. `Ctrl+C` interrupts, then clears any remaining queued follow-ups, then quits.

## File mentions

Type `@` in the composer to fuzzy-find a workspace file (respects `.gitignore` via ripgrep). Tab or Enter inserts `@path` and a trailing space; Esc closes the list without clearing the draft.

## File drop

Drop a file (or several) from your file manager onto the window and its path lands in the prompt: a space is added before it when the cursor follows text, and one after it, so the path stays its own token. Dropped image files still attach as `[Image N]`.

## Multiple-choice questions

Sometimes the agent asks a multiple-choice question (plus a freeform row, shown
as **Type an answer**):

| Key         | Action                   |
| ----------- | ------------------------ |
| `↑` / `↓`   | Move                     |
| `Enter`     | Confirm                  |
| `1`–`9`     | Jump to option           |
| Type        | Fill the freeform answer |
| `←` / `→`   | Another question         |
| `Esc`                                  | Busy: interrupt and send pending steers, else cancel the turn (queued follow-ups return to the input); denies an open permission prompt |

Typing goes to the freeform row on its own — there is no key to focus it, and
`↑`/`↓` hand the keys back to the list.

Permission and plan prompts use the same keys (row numbers only move; `Enter`
confirms). No panel binds a bare letter, so typing while a prompt is open never
answers it — see [Permissions](permissions.md). On a permission prompt `Esc`
denies rather than cancelling the turn (`Ctrl+C` aborts the turn); on the ask and
plan panels `Esc` cancels as usual.

## Terminal notes

`Shift+Enter` works best in terminals that report modified keys (Kitty keyboard protocol): Ghostty, Kitty, iTerm2 3.5+, Alacritty, WezTerm (`enable_kitty_keyboard = true`).

If `Shift+Enter` is remapped (common in iTerm), remove that binding or use `Ctrl+J` / `Alt+Enter` for newlines.

**Copy:** drag in the transcript to select; releasing the mouse copies to the clipboard. Leaving the terminal mid-drag counts as release.
