# restore

`gt restore` is `git restore` with the same arguments.

## Sub-features

- `restore-staged` unstages a path.

## How to get to it (user POV)

- Run `gt restore --staged <path>` or `gt restore <path>`.

## Driving it with gt-sandbox

Preconditions:

- `staged.txt` is in the index (`gt add -A`).

- **Unstage.** Run `gt-sandbox run -- restore --staged staged.txt`. Exit `0`. stderr contains `$ git restore --staged staged.txt`. `git diff --cached --name-only` no longer lists `staged.txt`.
- **Proof.** Empty cache listing for that path.

## Gotchas

- Restoring a worktree file destroys uncommitted edits. Use `--staged` unless the feature is about the worktree.
