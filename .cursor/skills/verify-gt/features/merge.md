# merge

`gt merge` is passed through to `gh stack merge`.

## Sub-features

- `merge-passthrough` announces `gh stack merge` and merges the native GitHub stack.

## How to get to it (user POV)

- Run `gt merge` on a submitted stack.

## Driving it with gt-sandbox

Preconditions (local):

- Isolated repo. No GitHub stack object.

- Do **not** run `gt merge` against a local origin. It will call GitHub.
- Record `SKIPPED_MUTATION merge` unless `GT_VERIFY_GITHUB=1`.

Preconditions (GitHub, `GT_VERIFY_GITHUB=1`):

- A submitted stack whose PRs are mergeable.

- **Merge.** Run `gt-sandbox run -- merge` with whatever flags `gh stack merge --help` documents. Exit `0`. stderr contains `$ gh stack merge`. GitHub shows the stacked PRs merged. Local follow-up is `gt sync -d` to drop the finished stack.

## Gotchas

- This is not `git merge`. Do not prove it by merging locally.
- `gt merge --help` going to `gh` is not a verified merge.
