# submit

`gt submit` validates, plans, restacks, does one atomic `git push --force-with-lease`, then creates or updates PRs and the GitHub stack. `gt ss` is `--stack`. Default scope is trunk → current.

## Sub-features

- `submit-dry-run` prints the plan and must not move origin refs.
- `submit-help` documents `-u`, `--stack`, `-p`, `-d`.
- `submit-ss` is the whole-stack alias.
- `submit-update-only` skips branches with no open PR.
- `submit-push` one atomic push, then PR create/update (GitHub).
- `submit-noop` prints `Stack already up to date` (GitHub, existing PRs).
- `submit-native` delegates to `gh stack submit` only before any mutation.

## How to get to it (user POV)

- Run `gt submit` or `gt s` on a stacked branch.
- Run `gt ss` or `gt submit --stack` for the whole stack.
- Run `gt submit --dry-run` to print the plan.
- Run `gt submit -u` to update existing PRs only.

## Driving it with gt-sandbox

Preconditions (local):

- A two-layer stack that has **not** been pushed.
- `GH_TOKEN` unset.

- **Help.** Run `gt-sandbox run -- submit --help`. Exit `0`. Output contains `update-only` and `-u`.
- **Dry-run.** Run `gt-sandbox run -- submit --dry-run`. Record exit and transcript. Run `git ls-remote --heads origin`. `refs/heads/layer-one` and `refs/heads/layer-two` must be absent. stderr must not show a completed `$ git push` that created those refs.
- **Proof (local).** `gt-sandbox capture after-dry-run` plus the `ls-remote` output saved next to the transcript.

Preconditions (GitHub, `GT_VERIFY_GITHUB=1` only):

- A sandbox whose `origin` is a real GitHub repo the token can write.
- First `gt submit` opens draft PRs. Second `gt submit -u` on an unchanged stack prints `Stack already up to date` and announces zero `git push`.

- **Push.** After changing two layers, `gt submit` announces exactly one `$ git push`. All stacked branches have PRs. New PRs are drafts unless `-p` was passed.
- **`-p`.** Proves existing drafts became ready. Never add `-p` yourself to "help" the user.

If `GT_VERIFY_GITHUB` is unset, record `SKIPPED_MUTATION submit-push` and stop. Do not mark push/PR verified from `--dry-run` or `--help`.

## Gotchas

- `--dry-run` still loads remotes (ls-remote + GraphQL). Prove the skip by reading origin refs.
- Non-TTY `gt submit` will not include upstack (`[y/N]` defaults to no). Use `--stack` or `gt ss`.
- `-p` publishes **new and existing** PRs. It will un-draft a PR left draft on purpose.
- After a `git push` or GitHub write has started, `--native` is refused.
- Lease failure must move **no** branch. Compare every origin SHA.
