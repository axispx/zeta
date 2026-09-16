# Keyboard and terminal

## Shortcuts

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

## Follow-up queue

While the agent is working, `Enter` with text queues a follow-up. Empty `Enter` (or queue-focus Enter on an item) interrupts the current turn and sends that follow-up immediately. Open the queue with `Ctrl+Q` (`↑`/`↓` move, Enter send selected, `e` edit, `d` remove, Esc back). Queued items also drain one at a time when a turn finishes on its own (unless you are editing the next item or typing a draft). `Esc` cancels an edit, leaves queue focus, or cancels the turn (queue kept). `Ctrl+C` leaves edit/queue focus first, then runs the interrupt ladder (dismiss overlays, cancel turn), then clears any remaining follow-ups, then quits.

## File mentions

Type `@` in the composer to fuzzy-find a workspace file (respects `.gitignore` via ripgrep). Tab or Enter inserts `@path` and a trailing space; Esc closes the list without clearing the draft.

## File drop

Drop a file (or several) from your file manager onto the window and its path lands in the prompt: a space is added before it when the cursor follows text, and one after it, so the path stays its own token. Dropped image files still attach as `[Image N]`.

## Multiple-choice questions

Sometimes the agent asks a multiple-choice question (plus freeform **Other**):

| Key       | Action         |
| --------- | -------------- |
| `↑` / `↓` | Move           |
| `Enter`   | Confirm        |
| `1`–`9`   | Jump to option |
| Type      | Fill **Other** |
| `Esc`     | Cancel         |

## Terminal notes

`Shift+Enter` works best in terminals that report modified keys (Kitty keyboard protocol): Ghostty, Kitty, iTerm2 3.5+, Alacritty, WezTerm (`enable_kitty_keyboard = true`).

If `Shift+Enter` is remapped (common in iTerm), remove that binding or use `Ctrl+J` / `Alt+Enter` for newlines.

**Copy:** drag in the transcript to select (the scrollbar is not included); releasing the mouse copies to the clipboard. Leaving the terminal mid-drag counts as release.
