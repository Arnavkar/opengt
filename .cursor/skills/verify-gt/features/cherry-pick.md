# cherry-pick

`gt cherry-pick` is `git cherry-pick` with the same arguments.

## Sub-features

- `cherry-pick-passthrough` announces `git cherry-pick`.
- `cherry-pick-bad` forwards git's error for a missing commit.

## How to get to it (user POV)

- Run `gt cherry-pick <commit>`.

## Driving it with gt-sandbox

Preconditions:

- Any repo.

- **Missing commit.** Run `gt-sandbox run -- cherry-pick not-a-commit`. Non-zero exit from git. stderr contains `$ git cherry-pick not-a-commit`. stderr does not contain `unknown command` or `cannot be translated`.
- **Proof.** The announcement plus git's own error. A successful pick (optional) copies a real commit SHA onto the current branch; then `gt doctor` if that branch is stacked.

## Gotchas

- Cherry-picking onto a stacked branch can change ancestry. The catalog may need `gt doctor`.
- Do not confuse a refused Graphite command with this passthrough.
