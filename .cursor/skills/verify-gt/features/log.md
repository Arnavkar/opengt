# log

`gt log` prints the current stack. `gt ls` is the short catalog. `gt ll` is `git log --graph`.

## Sub-features

- `log-full` prints the catalog with commit subject lines, tip at the top.
- `log-short` (`gt ls` or `--short`) prints names only.
- `log-json` prints `stackViewJSON` on stdout.
- `log-long` (`gt ll`) runs `git log --graph --oneline --decorate --all`.
- `log-native` delegates to `gh stack view`.

## How to get to it (user POV)

- Run `gt log` on a stacked branch.
- Run `gt ls` or `gt log --short`.
- Run `gt ll`.
- Run `gt log --json` and pipe it.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`, current branch `layer-two`.

- **Full.** Run `gt-sandbox run -- log`. Exit `0`. stdout contains `layer-two`, `layer-one`, and `main`. stderr does not contain `gh stack view`.
- **Short.** Run `gt-sandbox run -- ls`. Exit `0`. stdout contains `layer-two` and `layer-one`.
- **JSON.** Run `gt-sandbox run -- log --json`. Exit `0`. stdout is JSON with `"currentBranch": "layer-two"` and a `branches` entry named `layer-two`.
- **Long.** Run `gt-sandbox run -- ll`. Exit `0`. stderr contains `$ git log --graph --oneline --decorate --all`.
- **Proof.** Transcripts show stack names on stdout for `log` / `ls` / `--json`. `ll` proof is the announced git graph plus those names in stdout.

## Gotchas

- Unknown flags force a delegate to `gh stack view`. Do not pass leftover Graphite flags.
- `gt log` is the view people read. Keep stdout clean; assertions belong on stdout, not stderr.
- `--native` is a separate entry point.
