# trunk

`gt trunk` checks out the stack trunk (usually `main`).

## Sub-features

- `trunk-switch` checks out the trunk from a stacked branch.
- `trunk-stay` prints `Already on trunk branch <name>` when already there.

## How to get to it (user POV)

- Run `gt trunk` from a stacked branch.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`. Current branch is `layer-two`.

- **Switch.** Run `gt-sandbox run -- trunk`. Exit `0`. stderr contains `$ git checkout main`. stdout contains `✓ Switched to main`. Current branch is `main`.
- **Stay.** Run `gt-sandbox run -- trunk` again. Exit `0`. stdout contains `Already on trunk branch main`.
- **Proof.** `gt-sandbox capture after-trunk`. `branch.txt` is `main`. Catalog still lists both layers.

## Gotchas

- Flags force `gh stack trunk`.
- `gt checkout -t` is a different entry point (`gh stack trunk` even without `--native`).
