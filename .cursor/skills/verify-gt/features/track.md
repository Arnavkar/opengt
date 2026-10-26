# track

`gt track` adopts existing Git branches into the schema v1 catalog. It does not create a branch.

## Sub-features

- `track-current` adopts the current untracked branch.
- `track-named` adopts a listed bottom-to-top chain with `--base`.
- `track-on-trunk` refuses when HEAD is a trunk.
- `track-already` refuses a branch already in a stack.
- `track-native` delegates to `gh stack init --base <trunk>`.

## How to get to it (user POV)

- Check out an untracked branch and run `gt track`.
- Run `gt track a b --base main`.
- Run `gt tr` as the alias.

## Driving it with gt-sandbox

Preconditions:

- A fresh repo, or a repo whose current branch is not yet in the catalog.

- **On trunk.** On `main`, run `gt-sandbox run -- track`. Exit `1`. stderr contains `gt create` and the trunk name.
- **Adopt current.** `git checkout -b scratch`, then `gt-sandbox run -- track`. Exit `0`. stderr does not contain `gh stack init`. Catalog branches include `scratch`.
- **Already tracked.** Run `gt-sandbox run -- track` again. Exit `1`. stderr contains `already in a stack`.
- **Proof.** `gt-sandbox capture after-track`. `catalog.json` lists `scratch` under trunk `main`.

## Gotchas

- `--parent` is a Graphite flag with no translation. It errors. Use `--base`.
- `gt create` on an untracked branch would start a *new* branch on top. Users who meant to adopt must use `gt track`.
- Tracking a trunk name as a member errors. Omit the trunk and pass `--base`.
