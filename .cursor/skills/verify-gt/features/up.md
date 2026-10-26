# up

`gt up` checks out the branch above the current one in the catalog.

## Sub-features

- `up-one` moves one layer toward the tip.
- `up-n` takes a step count.
- `up-stay` prints `Already at the top of the stack` and does not move.
- `up-alias` accepts `gt u`.

## How to get to it (user POV)

- Run `gt up` or `gt u` on a stacked branch that is not the tip.
- Run `gt up 2` to skip a layer.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`. Current branch is `layer-one`.

- **One step.** Run `gt-sandbox run -- up`. Exit `0`. stderr contains `$ git checkout layer-two`. stdout contains `✓ Checked out layer-two, 1 branch up`. Current branch is `layer-two`.
- **Stay.** Run `gt-sandbox run -- up` again. Exit `0`. stdout contains `Already at the top of the stack`. Current branch is still `layer-two`.
- **Proof.** `gt-sandbox capture after-up`. `branch.txt` is `layer-two`.

## Gotchas

- Flags (`--native`, `--print-path`) force `gh stack up`. The default path is `git checkout`.
- A non-numeric extra argument errors with `invalid step count` or `unexpected argument`.
