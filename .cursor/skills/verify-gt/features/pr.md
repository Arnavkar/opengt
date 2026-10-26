# pr

`gt pr` opens the current pull request in a browser via `gh pr view --web`.

## Sub-features

- `pr-web` announces `gh pr view --web` and opens the browser.

## How to get to it (user POV)

- Run `gt pr` on a branch that has a GitHub PR.

## Driving it with gt-sandbox

Preconditions (local):

- Isolated repo.

- Do **not** run `gt pr`. It opens a browser.
- Record `SKIPPED_MUTATION pr: opens a browser via gh pr view --web`.

Preconditions (GitHub, and only when a browser is acceptable):

- A submitted stack, current branch has a PR.

- **Open.** Run `gt-sandbox run -- pr`. Exit `0`. stderr contains `$ gh pr view --web`. The opened URL is that branch's PR. Capture the announced command and the PR URL from `gh pr view --json url` as a second read — do not treat a browser flash as proof.

## Gotchas

- Local-suite and headless agents must skip this. Opening a browser on the user's machine is not isolation.
- `gt pr --help` is not a verified open.
