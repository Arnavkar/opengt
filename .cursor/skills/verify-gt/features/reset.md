# reset

`gt reset` is `git reset` with the same arguments.

## Sub-features

- `reset-soft` moves HEAD and leaves the index.

## How to get to it (user POV)

- Run `gt reset --soft HEAD~1` (or any `git reset` invocation).

## Driving it with gt-sandbox

Preconditions:

- Current branch has at least two commits. Record `git rev-parse HEAD~1` as `before`.

- **Soft reset.** Run `gt-sandbox run -- reset --soft HEAD~1`. Exit `0`. stderr contains `$ git reset --soft 'HEAD~1'` or `$ git reset --soft HEAD~1`. `git rev-parse HEAD` equals `before`.
- **Proof.** That SHA comparison. Then restore the commit if later steps need the old tip (`git commit` of the leftover index) — or use a disposable repo.

## Gotchas

- `--hard` is allowed and destructive. Prefer `--soft` in verification.
- Resetting a stacked branch can invalidate catalog SHAs. Run `gt doctor` afterwards if you keep using the stack.
