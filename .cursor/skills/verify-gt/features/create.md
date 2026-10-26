# create

`gt create` starts a linear stack off trunk or appends a layer at the tip, then optionally commits. Mid-stack create is refused because that would fork.

## Sub-features

- `create-start` branches off trunk and writes schema v1.
- `create-extend` appends a layer on the tip.
- `create-name-from-message` slugs `-m` into `MM-DD-slug` when no name is given.
- `create-fork-guard` refuses a create that is not at the tip.
- `create-needs-name` refuses a create with neither a name nor `-m`.
- `create-native` delegates to `gh stack init` / `gh stack add`.

## How to get to it (user POV)

- Run `gt create <name> -a -m "<message>"` on trunk or on the tip.
- Run `gt c` as the alias.
- Run `gt create -am "<message>"` and accept the generated branch name.
- Run `gt create --native ...` to force `gh stack`.

## Driving it with gt-sandbox

Preconditions:

- A fresh `gt-sandbox new-repo`.
- On `main`, worktree clean.

- **Start stack.** Write `layer-one.txt` with `one`. Run `gt-sandbox run -- create layer-one -a -m "Add layer one"`. Exit `0`. stderr contains `$ git checkout -b layer-one` and `$ git commit -m 'Add layer one'`. stderr does not contain `gh stack init`. `git branch --show-current` is `layer-one`. Catalog `stacks[0].branches` is `["layer-one"]` and trunk is `main`. `git rev-list --count main..layer-one` is `1`. Subject of `layer-one` is `Add layer one`.
- **Extend stack.** Write `layer-two.txt` with `two`. Run `gt-sandbox run -- create layer-two -a -m "Add layer two"`. Exit `0`. stderr contains `$ git checkout -b layer-two`. Catalog order is `layer-one layer-two`. `git rev-list --count layer-one..layer-two` is `1`.
- **Fork guard.** Run `gt-sandbox run -- down` then write `fork.txt` and run `gt-sandbox run -- create forked -a -m "Fork the stack"`. Exit `1`. stderr contains `not the top of its stack`. Catalog still has two branches. Current branch is still `layer-one`.
- **Missing name.** In a new repo, run `gt-sandbox run -- create`. Exit non-zero. stderr contains `needs a branch name`.
- **Proof.** `gt-sandbox capture after-create`. `catalog.json` is schema v1 and `gh stack view` (run via `gt-sandbox git` is wrong — use `run` only for `gt`; for `gh` use a captured `gt log`) lists both layers. Prefer `gt-sandbox run -- log` and require both names on stdout.

## Gotchas

- `gt create --insert` is unsupported. It errors; do not treat that as a failed happy path.
- Generated names depend on the date (`MM-DD-slug`). Assert the suffix `_add_the_login_form`, not a hard-coded day.
- Staging happens before the branch exists so a failed `git add -p` does not leave a leftover branch. Non-interactive runs should use `-a` or `-u`, not `-p`.
- `--native` is a different entry point. A pass on the default path does not verify `--native`.
