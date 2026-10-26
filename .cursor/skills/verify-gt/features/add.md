# add

`gt add` is `git add` with the same arguments.

## Sub-features

- `add-all` stages paths via `git add`.

## How to get to it (user POV)

- Run `gt add -A` or `gt add <path>`.

## Driving it with gt-sandbox

Preconditions:

- On a stacked branch. Untracked file `staged.txt` exists.

- **Stage.** Run `gt-sandbox run -- add -A`. Exit `0`. stderr contains `$ git add -A`. `git diff --cached --name-only` contains `staged.txt`.
- **Proof.** That index listing. Do not commit unless a later feature needs it.

## Gotchas

- This is passthrough. Graphite-specific add flags do not exist here.
- Unknown `gt` commands also go to git. `add` is a named passthrough so the announcement is `$ git add ...`.
