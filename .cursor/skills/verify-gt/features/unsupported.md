# unsupported

Graphite commands that cannot be translated stop with exit `2`, the reason, and a next step. They are never forwarded to git.

## Sub-features

- `unsupported-tui` (`fold`, `move`, `reorder`, `rename`) names `gh stack modify`.
- `unsupported-missing` (`absorb`, `split`, `squash`, `pop`, `revert`, `undo`, `freeze`, `unfreeze`) names a git recipe or says there is none.
- `unsupported-scope` (`untrack`, `unlink`) refuses rather than unstacking the whole stack.
- `unsupported-not-stack` (`config`, `auth`, `info`, and the rest in `unsupported` in `cmd/gt/main.go`) points at `gh` or nowhere.

## How to get to it (user POV)

- Run `gt absorb`, `gt fold`, `gt config`, or any name in that map.

## Driving it with gt-sandbox

Preconditions:

- Any isolated repo.

- **Absorb.** Run `gt-sandbox run -- absorb`. Exit `2`. stderr contains `cannot be translated` and the absorb hint.
- **Fold.** Run `gt-sandbox run -- fold`. Exit `2`. stderr mentions `gh stack modify`.
- **Config.** Run `gt-sandbox run -- config`. Exit `2`. stderr mentions configuring `gh`.
- **Proof.** Repeat for any name you touch. A command that ran `git <name>` failed this feature.

## Gotchas

- Exit `2` is the contract. Exit `0` or a git passthrough means the refuse list regressed.
- `gt track` is supported. `gt untrack` is not. Do not treat them as a pair that both work.
