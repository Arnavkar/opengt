# sync

`gt sync` is repo-wide. It fetches, fast-forwards trunks and behind branches, cascade-restacks every stack, and can prune finished stacks. It never pushes.

## Sub-features

- `sync-ff-trunk` fast-forwards a behind trunk from origin.
- `sync-restack` restacks stacks onto the updated trunk.
- `sync-no-restack` skips the cascade (`--no-restack`).
- `sync-delete-finished` (`-d`) deletes a stack only when every PR is merged or closed.
- `sync-keeps-open` keeps merged branches on a stack that still has open work.

## How to get to it (user POV)

- Run `gt sync`.
- Run `gt sync -d` to delete finished stacks without a prompt.
- Run `gt sync --no-restack`.

## Driving it with gt-sandbox

Preconditions:

- A stack whose trunk is behind `origin/main` (commit on the bare origin, not locally).
- `GH_TOKEN` unset. GitHub PR lookup may warn and continue.

- **Repo-wide.** Run `gt-sandbox run -- sync`. Exit `0`. Local `main` equals `origin/main`. Stack members are still present. stderr does not contain `$ git push`.
- **No restack.** After moving the trunk again, run `gt-sandbox run -- sync --no-restack`. Exit `0`. Members may still need a rebase (`gt doctor` warning is acceptable).
- **Proof.** `gt-sandbox capture after-sync`. Compare `head` of `main` to `git ls-remote origin main`. Origin must have received **no** new refs from this command.

## Gotchas

- Sync still issues one batched GraphQL PR lookup. A failure prints `could not load pull requests` and continues with refs. That warning is not a failed sync.
- `-d` does not delete ordinary untracked branches.
- A stack with one open PR is not finished. Merged siblings stay so restack keeps the chain.
- Do not prove prune-of-finished-stacks against a local origin; that path needs PR state from GitHub.
