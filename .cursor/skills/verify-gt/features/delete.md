# delete

`gt delete <branch>` deletes that local branch, restacks what was above it, and drops it from the catalog. Bare `gt delete` opens a picker on a TTY.

## Sub-features

- `delete-named` deletes a stacked branch and restacks descendants onto its parent.
- `delete-tip` after deleting HEAD lands on the parent.
- `delete-untracked` deletes a local branch that is not in the catalog.
- `delete-trunk` refuses.
- `delete-bare-no-tty` errors with `needs a branch`.
- `delete-picker` prompt is `Delete a branch (autocomplete or arrow keys)`.

## How to get to it (user POV)

- Run `gt delete <branch>`.
- Run `gt delete` in a terminal and pick a row.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two` / `layer-three`. Current branch is `layer-three`.

- **Bare, no TTY.** Run `gt-sandbox run -- delete`. Exit `1`. stderr contains `needs a branch`.
- **Trunk.** Run `gt-sandbox run -- delete main`. Exit `1`. stderr contains `cannot delete trunk`.
- **Middle.** Run `gt-sandbox run -- delete layer-two`. Exit `0`. `git branch --list layer-two` is empty. Catalog is `layer-one layer-three`. `git rev-list --count layer-one..layer-three` is `1`. Subject of `layer-three` is still `Add layer three`.
- **Tip.** Run `gt-sandbox run -- delete layer-three`. Exit `0`. Current branch is `layer-one`. Catalog is `layer-one`.
- **Proof.** `gt-sandbox capture after-delete`. `catalog.json` matches the remaining chain.

## Gotchas

- Do not drive the picker without a TTY. The helper's `run` is not a TTY.
- A paused rebase blocks delete (`validatePaused`).
- Deleting a branch checked out in another worktree is refused.
