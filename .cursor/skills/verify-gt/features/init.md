# init

`gt init` does nothing. `gh stack` has no repository-level init.

## Sub-features

- `init-explain` errors and tells the user to `gt create` or `gt track`.

## How to get to it (user POV)

- Run `gt init`.

## Driving it with gt-sandbox

Preconditions:

- Any isolated repo.

- **Refuse.** Run `gt-sandbox run -- init`. Exit `1`. stderr contains `no repository-level init` and mentions `gt create` and `gt track`. Catalog is unchanged (still missing, or still the previous stacks).
- **Proof.** That stderr. A later `gt create` on trunk is the real start-of-stack path, verified in `create.md`.

## Gotchas

- Success here is the error. Do not "fix" init to create a stack.
- `gt track` is adoption, not init-of-the-repo.
