# checkout

`gt checkout` moves HEAD. A named stacked branch goes through `gh stack checkout`. Trunk and untracked local branches go through `git checkout`. Bare `gt checkout` opens a tree picker on a TTY.

## Sub-features

- `checkout-tracked` runs `gh stack checkout <name>`.
- `checkout-untracked` runs `git checkout <name>`.
- `checkout-trunk` runs `git checkout` of the trunk.
- `checkout-t` (`-t`) runs `gh stack trunk`.
- `checkout-picker` opens `Checkout a branch (autocomplete or arrow keys)` on a TTY.
- `checkout-alias` accepts `gt co`.

## How to get to it (user POV)

- Run `gt checkout <branch>` or `gt co <branch>`.
- Run `gt co` with no argument in a terminal to pick from the tree.
- Run `gt co -t` to go to the trunk.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`. An extra local branch `scratch-co` that is not in the catalog.

- **Tracked.** Run `gt-sandbox run -- checkout layer-one`. Exit `0`. stderr contains `$ gh stack checkout layer-one`. Current branch is `layer-one`.
- **Untracked.** Run `gt-sandbox run -- checkout scratch-co`. Exit `0`. stderr contains `$ git checkout scratch-co`. Current branch is `scratch-co`.
- **Trunk.** Run `gt-sandbox run -- checkout main`. Exit `0`. stderr contains `$ git checkout main`.
- **`-t`.** On `layer-two`, run `gt-sandbox run -- co -t`. Exit `0`. stderr contains `$ gh stack trunk`. Current branch is `main`.
- **Tree picker (PTY).** Run `bin/gt-pty --gt "$GT_BIN" --repo "$REPO" --out ... --keys ESC -- checkout`. The `.plain` screen contains `Checkout a branch`, `layer-two`, `main`, `All stacks on GitHub`, and `not in a stack · gt track` when an untracked local branch exists. A second run with `--keys "layer-one ENTER"` leaves HEAD on `layer-one`.
- **Bare, no TTY.** `gt-sandbox run -- checkout` is **not** the picker. It falls through to `gh stack checkout`. That path does not verify this feature.
- **Proof.** The `.plain` screen plus `git branch --show-current` after Enter. A named `gt checkout layer-one` is a different entry point.

## Gotchas

- A bare name sent to `gh stack checkout` is tried as a stack number, then a PR. That is why untracked locals and the trunk go through git.
- The picker prompt is `Checkout a branch (autocomplete or arrow keys)`. Last row is **All stacks on GitHub**. Driving that row needs a TTY and may talk to GitHub.
- Untracked local branches show dimmed in the picker with `not in a stack · gt track`. Adopting is `gt track`, not `gt create`.
