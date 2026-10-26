# get

`gt get <pr>` checks out an existing stack from a branch name, PR number, or PR URL via `gh stack checkout`.

## Sub-features

- `get-missing` errors when no target is given.
- `get-pr` checks out that PR's stack (GitHub).

## How to get to it (user POV)

- Run `gt get 123` or `gt get <url>` or `gt get <branch>`.

## Driving it with gt-sandbox

Preconditions (local):

- Any isolated repo.

- **Missing target.** Run `gt-sandbox run -- get`. Exit `1`. stderr contains `needs a branch, PR number, or PR URL` and mentions `gt sync`.
- **Proof (local).** That refusal transcript. Do not call `gh stack checkout` yourself.

Preconditions (GitHub, `GT_VERIFY_GITHUB=1`):

- A known open stacked PR number on the sandbox origin.

- **Checkout.** Run `gt-sandbox run -- get <number>`. Exit `0`. stderr contains `$ gh stack checkout <number>`. Current branch matches the PR head. Catalog contains that stack.
- If `GT_VERIFY_GITHUB` is unset, record `SKIPPED_MUTATION get`.

## Gotchas

- `gt get` with no args is not "refresh the current stack". That is `gt sync`.
- A local file origin cannot satisfy `get-pr`. Do not substitute `gt checkout <local-branch>` and call it `get`.
