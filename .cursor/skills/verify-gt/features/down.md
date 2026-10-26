# down

`gt down` checks out the branch below the current one in the catalog.

## Sub-features

- `down-one` moves one layer toward the bottom.
- `down-n` takes a step count.
- `down-stay` prints `Already at the bottom of the stack`.
- `down-alias` accepts `gt d`.

## How to get to it (user POV)

- Run `gt down` or `gt d` on a stacked branch that is not the bottom.
- Run `gt down 2` to skip a layer.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`. Current branch is `layer-two`.

- **One step.** Run `gt-sandbox run -- down`. Exit `0`. stderr contains `$ git checkout layer-one`. stdout contains `✓ Checked out layer-one, 1 branch down`. Current branch is `layer-one`.
- **Stay.** Run `gt-sandbox run -- down` again. Exit `0`. stdout contains `Already at the bottom of the stack`.
- **Proof.** `gt-sandbox capture after-down`. `branch.txt` is `layer-one`.

## Gotchas

- Flags force `gh stack down`. The default path is `git checkout`.
- `down` from trunk is not a stack member move; start on a stacked branch.
