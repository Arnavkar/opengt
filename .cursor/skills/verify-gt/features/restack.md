# restack

`gt restack` rebases the current stack so each layer sits on the layer below it.

## Sub-features

- `restack-all` rebases the whole stack onto a moved trunk.
- `restack-upstack` (`-u`) restacks this branch and its descendants.
- `restack-downstack` (`-d`) restacks this branch and its ancestors.
- `restack-only` (`-o`) is refused (no `gh stack` equivalent).
- `restack-native` delegates to `gh stack rebase`.

## How to get to it (user POV)

- Run `gt restack` on a stacked branch after the trunk or a parent moved.
- Run `gt restack -u` or `gt restack -d`.

## Driving it with gt-sandbox

Preconditions:

- Stack `layer-one` / `layer-two`.
- On `main`, add a commit `Trunk moves on` containing `trunk-moves.txt`, push it to origin, then check out `layer-two`.

- **Cascade.** Run `gt-sandbox run -- restack`. Exit `0`. stderr does not contain `gh stack rebase`. `git merge-base --is-ancestor main layer-one` succeeds. `layer-two^` equals `layer-one`.
- **`--only`.** Run `gt-sandbox run -- restack --only`. Exit non-zero. stderr contains `no gh stack equivalent`.
- **Proof.** `gt-sandbox capture after-restack`. Log shows the trunk commit under both layers.

## Gotchas

- `-d` and `-u` together error (`--downstack and --upstack conflict`).
- Conflicts leave a paused `git rebase`. Continue with `gt continue` or `gt abort`.
- `--native` is a separate entry point.
