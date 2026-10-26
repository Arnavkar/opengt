# rebase

`gt rebase` is `git rebase` with the same arguments.

## Sub-features

- `rebase-passthrough` announces `git rebase` and runs it.

## How to get to it (user POV)

- Run `gt rebase <upstream>` or any `git rebase` invocation.

## Driving it with gt-sandbox

Preconditions:

- A stacked branch. `HEAD` is a valid rebase target for a no-op.

- **No-op.** Run `gt-sandbox run -- rebase HEAD`. Exit `0`. stderr contains `$ git rebase HEAD`.
- **Proof.** `git status` is not a rebase. HEAD SHA is unchanged.

## Gotchas

- This is not `gt restack`. `gt rebase` does not update the catalog. After a real rebase of stacked branches, run `gt doctor`.
- Interactive `gt rebase -i` needs a TTY and an editor. Do not drive it in the helper.
