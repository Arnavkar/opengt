# doctor

`gt doctor` compares Git, the shared catalog, worktrees, and (when origin is a network URL) GitHub.

## Sub-features

- `doctor-text` prints a human report and an exit code.
- `doctor-json` prints the same report as JSON, no prompts.
- `doctor-repair` applies safe metadata repairs and asks in a TTY.
- `doctor-repair-yes` applies only safe repairs with no prompt.

## How to get to it (user POV)

- Run `gt doctor`.
- Run `gt doctor --json`.
- Run `gt doctor --repair` or `gt doctor --repair --yes`.

## Driving it with gt-sandbox

Preconditions:

- A healthy two-layer stack on a local origin.

- **Healthy text.** Run `gt-sandbox run -- doctor`. Exit `0`. stdout contains `Git repository detected` and `Summary`.
- **JSON.** Run `gt-sandbox run -- doctor --json`. Exit `0`. stdout JSON has `"status"`.
- **Non-interactive repair.** Run `gt-sandbox run -- doctor --repair` (no TTY). If there is nothing to repair, exit `0`. If a repair was skipped, stdout mentions `non-interactive repair needs --yes` and the exit may be a warning/repairable code.
- **Proof.** Transcript plus `gt-sandbox capture after-doctor`. Catalog SHAs match `git rev-parse` of each stacked branch.

## Gotchas

- Exit `0` healthy, `1` warnings, `2` repairable, `3` ambiguous, `4` missing deps / API / unknown schema. Assert the code, not only the words.
- `--yes` never resolves conflicting stack definitions, prefix disagreements, or Git history. Prefix issues stay for `gh stack modify`.
- File-path origins skip remote GitHub checks (`hasNetworkOrigin` is false). A local sandbox that reports skipped remotes is expected.
- `LEGACY_STATE_UNMIGRATED` appears when `.git/worktrees/<name>/gh-stack` still exists from pre-0.2.0.
