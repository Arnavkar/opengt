# unknown

Any command `gt` does not implement and does not refuse is passed to `git`.

## Sub-features

- `unknown-status` runs `git status`.
- `unknown-commit` runs `git commit` (user-typed `gt commit`).

## How to get to it (user POV)

- Run `gt status`, `gt commit`, `gt diff`, or any other git command.

## Driving it with gt-sandbox

Preconditions:

- Isolated repo, clean or with known porcelain.

- **Status.** Run `gt-sandbox run -- status --porcelain`. Exit `0`. stderr contains `$ git status --porcelain`. stdout is git's porcelain (empty when clean).
- **Proof.** Matching `git -C "$REPO" status --porcelain` as a second read.

## Gotchas

- Names in the unsupported map must **not** take this path.
- Named passthroughs (`add`, `rebase`, `reset`, `restore`, `cherry-pick`) are the same mechanism with an explicit command entry. Verify those in their own files.
