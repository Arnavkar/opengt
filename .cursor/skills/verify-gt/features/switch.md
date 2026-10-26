# switch

`gt switch` is passed through to `gh stack switch`, a TUI for the current stack.

## Sub-features

- `switch-tui` opens the `gh stack switch` TUI.

## How to get to it (user POV)

- Run `gt switch` in a terminal.

## Driving it with gt-sandbox

Preconditions (local):

- Isolated repo. Helper `run` is not a TTY.

- Do **not** run `gt switch` in the helper. It opens a TUI and will block or fail.
- Record `SKIPPED_MUTATION switch: opens the gh stack switch TUI`.
- The named-argument substitute for moving around a stack is `gt checkout <branch>` / `gt up` / `gt down`. Those do not verify `switch`.

Preconditions (PTY only):

- A real TTY or tmux pane attached to `REPO`.
- Run `gt switch`, pick a branch, confirm HEAD moved. Prompt and keybindings are `gh stack`'s, not opengt's.

## Gotchas

- `gt checkout` with no args is a different picker (all local stacks, plus a GitHub row). Do not conflate them.
- `gh stack modify` is another TUI (`fold` / `move` / `reorder` / `rename` live there). It is not `gt switch`.
