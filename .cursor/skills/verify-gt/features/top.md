# top

`gt top` checks out the tip of the current stack.

## Sub-features

- `top-switch` moves from a lower layer to the tip.
- `top-stay` prints `Already at the top of the stack`.
- `top-alias` accepts `gt t`.

## How to get to it (user POV)

- Run `gt top` or `gt t` while on any stacked branch.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`. Current branch is `layer-one`.

- **Switch.** Run `gt-sandbox run -- top`. Exit `0`. stderr contains `$ git checkout layer-two`. Current branch is `layer-two`.
- **Stay.** Run `gt-sandbox run -- top` again. Exit `0`. stdout contains `Already at the top of the stack`.
- **Proof.** `gt-sandbox capture after-top`. `branch.txt` is `layer-two`.

## Gotchas

- Extra positional arguments error (`unexpected argument`).
- Flags force `gh stack top`.
