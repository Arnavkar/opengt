# modify

`gt modify` commits or amends the current stacked branch and rebases everything above it. There is no `gh stack` equivalent.

## Sub-features

- `modify-amend` amends the current layer and restacks the upstack.
- `modify-commit` (`-c`) creates a new commit instead of amending.
- `modify-empty-branch` creates a commit when the branch has no commits of its own rather than rewriting the parent.
- `modify-top` amends the tip and skips a useless rebase.
- `modify-outside` refuses a branch that is not in a stack.

## How to get to it (user POV)

- Run `gt modify -a -m "<message>"` on a stacked branch.
- Run `gt m` as the alias.
- Run `gt modify -c` to add a commit.
- Run `gt modify --native` to rebase through `gh stack`.

## Driving it with gt-sandbox

Preconditions:

- A two-layer stack `layer-one` / `layer-two` from `gt create`.
- Current branch is `layer-one` (`gt down` from the tip).

- **Amend and restack.** Write `layer-one.txt` with `one, revised`. Run `gt-sandbox run -- modify -a -m "Add layer one, revised"`. Exit `0`. stderr does not contain `gh stack rebase`. `git log -1 --format=%s layer-one` is `Add layer one, revised`. `git rev-list --count layer-one..layer-two` is still `1`. `layer-two^` equals `layer-one`.
- **Outside a stack.** Check out `main`. Run `gt-sandbox run -- modify -a -m "nope"`. Exit non-zero. stderr explains the branch is not in a stack.
- **Proof.** `gt-sandbox capture after-modify`. Catalog still lists both branches in order.

## Gotchas

- Amending a branch with zero own commits would rewrite the parent. The command falls back to a new commit. Assert the parent SHA did not move.
- `--native` is `gh stack rebase --upstack --no-trunk`, not the amend itself.
- A dirty untracked file is not staged unless `-a` / `-u` / `-p` is passed.
