# continue

`gt continue` resumes a paused `git rebase` (or the matching `gh stack` operation).

## Sub-features

- `continue-none` errors when nothing is paused.
- `continue-conflict` announces `git rebase --continue` and, after the user resolves, finishes the rebase.
- `continue-gh` resumes `gh stack rebase` / `gh stack modify` when that tool left a marker.

## How to get to it (user POV)

- After a conflict from `gt modify` or `gt restack`, resolve files, `git add`, then run `gt continue`.

## Driving it with gt-sandbox

Preconditions for the happy path:

- Two layers that both edit `shared.txt`. On `layer-one`, rewrite the file and `gt modify` so the cascade stops.

- **Nothing paused.** On a clean stack, run `gt-sandbox run -- continue`. Exit `1`. stderr contains `no rebase is in progress`.
- **Conflict then resolve.** After `modify` leaves a rebase, run `gt-sandbox run -- continue` once (may stop again). Write `shared.txt` with `resolved`, `git add shared.txt`, run `gt-sandbox run -- continue` again. Exit `0`. stderr contains `$ git rebase --continue`. `git status` does not mention rebase. `layer-two^` equals `layer-one`.
- **Proof.** `gt-sandbox capture after-continue`. Log shows a linear chain.

## Gotchas

- Extra arguments error (`unexpected argument`).
- If `GIT_EDITOR` is unset, continue sets it to `true` so the rebase does not open an editor.
- Do not send `git rebase --continue` yourself and then claim `gt continue` worked.
