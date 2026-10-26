# bottom

`gt bottom` checks out the lowest stacked branch (not the trunk).

## Sub-features

- `bottom-switch` moves from a higher layer to the bottom member.
- `bottom-stay` prints `Already at the bottom of the stack`.
- `bottom-alias` accepts `gt b`.

## How to get to it (user POV)

- Run `gt bottom` or `gt b` while on any stacked branch.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`. Current branch is `layer-two`.

- **Switch.** Run `gt-sandbox run -- bottom`. Exit `0`. stderr contains `$ git checkout layer-one`. Current branch is `layer-one`.
- **Stay.** Run `gt-sandbox run -- bottom` again. Exit `0`. stdout contains `Already at the bottom of the stack`.
- **Proof.** `gt-sandbox capture after-bottom`. `branch.txt` is `layer-one`. Catalog unchanged.

## Gotchas

- Bottom is the first stacked branch, not `main`. Use `gt trunk` for the trunk.
- Extra positional arguments error.
