# abort

`gt abort` abandons a paused `git rebase` (or the matching `gh stack` operation) and restores the pre-rebase refs.

## Sub-features

- `abort-none` errors when nothing is paused.
- `abort-conflict` announces `git rebase --abort` and restores the tip SHA.
- `abort-gh` aborts `gh stack rebase` / `gh stack modify` when that tool left a marker.

## How to get to it (user POV)

- After a conflict, run `gt abort` instead of resolving.

## Driving it with gt-sandbox

Preconditions:

- Same conflicting two-layer setup as `continue`. Record `git rev-parse layer-two` before aborting.

- **Nothing paused.** On a clean stack, run `gt-sandbox run -- abort`. Exit `1`. stderr contains `no rebase is in progress` (or the same paused-operation miss).
- **Restore.** After the conflicting `modify`, run `gt-sandbox run -- abort`. Exit `0`. stderr contains `$ git rebase --abort`. `git status` does not mention rebase. `layer-two` SHA equals the SHA recorded before abort.
- **Proof.** `gt-sandbox capture after-abort`. Catalog still lists both layers.

## Gotchas

- Extra arguments error.
- Abort is the proof that the stack was not rewritten. Compare SHAs, not only the status text.
