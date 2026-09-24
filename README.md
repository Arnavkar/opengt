# opengt

> Graphite muscle memory, backed by GitHub's native stacks.

`opengt` installs a `gt` command that translates the Graphite CLI commands
you already know into [GitHub's official `gh stack`][gh-stack] commands and
plain Git operations.

It is intentionally a thin compatibility shim. `gh stack` remains the source
of truth for stack state; `opengt` has no backend, metadata format, or config
of its own. The hot paths — `gt sync` and `gt submit` — are implemented
directly on top of Git and the GitHub API for speed, and fall back to the
legacy `gh stack` implementation with `--native`.

> [!IMPORTANT]
> `opengt` is early software. It currently supports **linear stacks only** and
> targets `gh-stack` **v0.1.1**, state schema **v1**. When an operation cannot
> be translated safely, it stops with an actionable error instead of guessing.

## Quick start

### Requirements

- Git
- [GitHub CLI (`gh`)][gh-cli], authenticated with GitHub
- The [`github/gh-stack`][gh-stack] extension
- Go 1.23 or newer to build from source

Install the extension:

```sh
gh extension install github/gh-stack
```

Install `gt` with Homebrew (macOS). The fully qualified name taps
`hSATAC/toybox` for you:

```sh
brew install hSATAC/toybox/gtstack
```

Or install it with Go:

```sh
go install github.com/Arnavkar/opengt/cmd/gt@latest
```

Or build from source and put `gt` on your `PATH`:

```sh
git clone https://github.com/Arnavkar/opengt.git
cd opengt
mkdir -p bin
go build -o bin/gt ./cmd/gt
export PATH="$PWD/bin:$PATH"
```

Linux builds are attached to each [release][releases] as `tar.gz` archives.

`opengt` deliberately uses the same binary name as Graphite. If Graphite is
still installed, use `type -a gt` to see which binary your shell will run.
You can distinguish them with:

```console
$ gt --version
gt 0.1.0 (opengt)
```

### Use it like Graphite

```sh
# Start a stack and commit the first layer
gt create -am "Add the API client"

# Add another layer
gt create -am "Use the API client"

# Inspect the stack
gt log

# Create or update the native GitHub stack
gt submit --stack
```

Each `gt` invocation operates on the same stack state as `gh stack`, so you can
mix the two CLIs whenever you need a native command:

```sh
gt log
gh stack modify
gt submit --stack
```

## Why not just alias `gh stack` to `gt`?

`gh stack alias gt` only forwards arguments unchanged, but the two CLIs do not
have the same command surface. Some commands have different names:

```text
gt create   → gh stack add
gt log      → gh stack view
gt restack  → gh stack rebase
```

Others have actively conflicting meanings. Graphite's `gt modify` amends the
current branch and restacks its descendants, while `gh stack modify` opens a
TUI for restructuring the stack. `opengt` translates the intent instead of
blindly forwarding the command.

## Command mapping

| Graphite command | What `opengt` runs |
| --- | --- |
| `gt create` / `gt c` | `gh stack init` for a new stack, or `gh stack add` at the top of an existing stack |
| `gt modify` / `gt m` | `git commit [--amend]`, then `gh stack rebase --upstack --no-trunk` |
| `gt submit` / `gt s` / `gt ss` | validate + plan + atomic push + PR create/update + stack update. `--native` falls back to `gh stack submit` |
| `gt sync` | fetch (targeted ls-remote) + fast-forward trunk/branches + cascade restack + prune. `--native` falls back to `gh stack rebase` |
| `gt restack` | `gh stack rebase` (`-d`/`-u` map to `--downstack`/`--upstack`) |
| `gt doctor` | diagnose Git vs local `gh-stack` vs worktrees vs GitHub (`--repair`, `--json`) |
| `gt continue` / `gt abort` | Continue or abort the paused `gh stack rebase` or `gh stack modify` |
| `gt checkout` / `gt co` | tree of local stacks, or `gh stack checkout` / `git checkout` |
| `gt delete` | same tree picker, then delete the chosen branch and restack what was above it |
| `gt get <pr>` | `gh stack checkout <pr>` |
| `gt log` / `gt ls` / `gt ll` | `gh stack view`, `gh stack view --short`, or `git log --graph` |
| `gt up` / `u`, `down` / `d`, `top` / `t`, `bottom` / `b`, `trunk` | The corresponding `gh stack` navigation command |
| `gt add`, `cherry-pick`, `rebase`, `reset`, `restore` | The same `git` command, arguments unchanged |
| `gt track` / `gt tr` | `gh stack init --base <trunk> <branches>` (current branch if none given) |
| `gt merge` | `gh stack merge` |
| `gt switch` | `gh stack switch` |
| `gt pr` | `gh pr view --web` |
| `gt init` | Nothing. Explains that `gh stack` has no repository-level init; use `gt track` to adopt branches |

Run `gt <command> --help` for the supported flags. Unknown flags are rejected
rather than silently dropped.

## Normal Git is supported

`opengt` is a wrapper, not a gate. Any command `gt` does not implement is
passed through to `git` (`gt status` is `git status`, `gt commit` is
`git commit`). Graphite commands that cannot be translated still stop with an
error instead of being forwarded. When Git operations change stack structure,
`gt doctor` explains the disagreement and can repair **metadata**.

```text
git commit          safe
git checkout        safe
git cherry-pick     generally safe; doctor can detect ancestry changes
git rebase / reset / branch -f
                    allowed. May invalidate stack ancestry or cached SHAs.
                    Run `gt doctor` afterwards if you touched stack branches.
```

`gt sync -d` deletes a stack only when every pull request in it is merged or
closed. Merged branches in a stack that still has open work stay in the stack,
so restack keeps the full chain. Ordinary untracked branches are left alone.

### `gt doctor`

```sh
gt doctor              # read-only diagnostics
gt doctor --json       # machine-readable (no prompts)
gt doctor --repair     # safe metadata repairs; asks in a terminal
gt doctor --repair --yes
```

`--yes` applies only repairs classified as safe (refresh cached SHAs, drop
duplicate identical stacks). It never resolves conflicting stack definitions or
rewrites Git history. Prefix disagreements are ambiguous and are never resolved
by `--yes`; use `gh stack modify` to choose the intended stack.

Exit codes:

| Code | Meaning |
| --- | --- |
| 0 | healthy |
| 1 | warnings only (for example, a descendant needs `gt restack -u`) |
| 2 | repairable errors |
| 3 | ambiguous/unsafe state; do not guess |
| 4 | missing git/`gh`/`gh-stack`, GitHub API failure, or an unsupported state schema |

### One state file, at the repo root

`gt` reads and writes gh-stack state only at the shared repository directory
(`.git/gh-stack`), never per worktree. Every worktree sees and updates the same
file, so sync from one worktree is visible everywhere and worktrees cannot
disagree about stack order. `gh stack` itself may still write a per-worktree
copy under `.git/worktrees/<name>` when its own commands run in a linked
worktree; `gt` deliberately ignores those copies.

## Pinning `gh-stack`

This shim targets **v0.1.1** and schema **v1**. CI installs that pin
(`.github/workflows/ci.yml`). The daily `gh-stack-compat` workflow tests
`latest` as an early warning; it does not change what developers should run.

```sh
gh extension install github/gh-stack --pin v0.1.1 --force
```

A newer `gh-stack` with the same schema warns and continues. An unknown
schema is a hard stop.

## Important differences from Graphite

### Linear stacks only

`gh stack` represents a stack as a flat ordered list. `opengt` therefore does
not support forks:

- `gt create` must run from the top of the current stack.
- If a branch belongs to more than one stack, `opengt` stops and asks you to
  use `gh stack` directly.

### `gt submit` stops at the current branch

Graphite's `gt submit` submits the current branch and its downstack. `opengt`
does the same: default scope is trunk→current. If there are branches above
you, it asks before including them (`[y/N]`). `--stack` or `gt ss` submits the
whole stack without asking. Pass `-e` to open the editor (with `--native`)
and deselect branches. `-u` / `--update-only` is enforced: branches without
open PRs are excluded from scope entirely, so `gt ss -u` never pushes or
creates a PR for them.

The fast path validates locally, loads a remote snapshot, builds a pure plan,
and short-circuits when nothing changed ("Stack already up to date", zero
pushes, zero mutations). A changed submit does exactly one
`git push --atomic` with per-ref `--force-with-lease` regardless of stack
depth; on lease failure the entire push fails and no branch moves. Unpublished
branches are pushed before pull requests are opened, and submit stops when
origin has a commit this branch never contained; `-f` remains the override. PR
discovery is a single batched GraphQL call. New PRs are created as drafts;
pass `-p` to mark them ready for review.

> [!NOTE]
> `-p` publishes **new and existing** PRs ready for review. It will publish
> a PR you had deliberately left as a draft, so `gt submit` never passes it
> for you.

#### `gt submit` / `gt ss` flags

| Flag | Meaning |
| --- | --- |
| `--stack` | submit the whole stack without asking (also `gt ss`) |
| `-d`, `--draft` | create new PRs as drafts |
| `-p`, `--publish` | mark PRs ready for review (new and existing) |
| `-u`, `--update-only` | only update existing PRs; branches without open PRs are skipped |
| `-e`, `--edit` | open the `gh stack submit` editor (implies `--native`) |
| `-n`, `--no-edit` | skip the PR metadata editor (the default) |
| `--dry-run` | print the plan without mutating |
| `--always` | run even when the stack is up to date |
| `--restack` / `--no-restack` | cascade restack before pushing (default on) |
| `-f`, `--force` | force restack and push even when unchanged |
| `--no-verify` | pass `--no-verify` to `git push` |
| `--native` | delegate to the legacy `gh stack submit` (see below) |

### `gt sync`

`gt sync` is repo-wide: it fast-forwards every stack's trunk and branches,
cascades a restack across each stack, and prunes stale stack branches. It
does not push. `-d` deletes stale **stack** branches without prompting;
ordinary untracked branches are left alone, even if their upstream is gone.

A clean sync makes zero `gh stack` calls — one targeted `git ls-remote` plus
one batched GraphQL call for PR discovery, then local fast-forwards and
restacks. `--native` falls back to `gh stack rebase` for the legacy behavior.

#### `gt sync` flags

| Flag | Meaning |
| --- | --- |
| `-d`, `--delete-all` | delete stale stack branches without prompting |
| `--no-restack` | skip the cascade restack step |
| `-f`, `--force` | force restack even when ancestry is unchanged |
| `--native` | delegate to the legacy `gh stack rebase` (see below) |

### `--native`: the legacy fallback

`--native` makes `gt sync` and `gt submit` delegate to the legacy `gh stack`
implementation (`gh stack rebase` / `gh stack submit`) instead of the native
fast path. Use it when the native path refuses to proceed — for example,
when validation finds an unsupported state — or when you simply want the
`gh stack` behavior.

`--native` is allowed **before any mutation** has happened (e.g. when
pre-mutation validation fails). It is impossible **after** a mutation has
started (a `git push` or any GitHub mutation): `gt` will not switch
implementations mid-operation, since that could leave the stack in a
half-mutated state.

### `gt modify` is implemented with Git

`gh stack` has no equivalent of Graphite's amend operation. `opengt` commits
or amends with Git, then asks `gh stack` to rebase the upstack branches. If the
current branch has no commits of its own, it creates a commit rather than
rewriting its parent's commit.

### Checkout is a tree, like Graphite

`gt co` without an argument opens a Graphite-style tree of every locally tracked
stack: tips at the top, trunk at the bottom, each stack a continuous vertical
track. Branches that exist locally but are not in `gh-stack` metadata still
appear, dimmed, with `not in a stack · gt track` — adopt them with `gt track`;
`gt create` would start a new branch on top instead. Type to filter, arrows to
move. Enter checks out the highlighted branch via `gh stack checkout`, or
`git checkout` for the trunk. The last row, **All stacks on GitHub**, is bare
`gh stack checkout` — the picker for local *and* remote stacks.

A named argument is `gh stack checkout <target>`, except for the trunk and
untracked local branches, which go through Git. `gt switch` still opens
`gh stack switch` when you only want the current stack.

## Transparent by default

Before running each native operation, `opengt` prints the exact command to
stderr:

```console
$ git add -A
$ gh stack add 07-31-add_the_login_form
$ git commit -m 'Add the login form'
```

This keeps stdout clean for pipelines such as:

```sh
gt log --json | jq
```

Color is disabled when stderr is not a terminal or when `NO_COLOR` is set.

## Unsupported Graphite commands

`opengt` is a workflow bridge, not a complete reimplementation of Graphite.
Each command below stops with the reason and the nearest `git` or `gh stack`
alternative; run it to see the advice for that command.

None of these are merely unfinished. They are grouped by what blocks the
translation.

### Available only in the `gh stack modify` TUI

```text
fold  move  reorder  rename
```

`gh stack` can perform these, but only inside an interactive editor. No flag
drives them, so there is nothing for a one-line `gt` command to call.

### `gh stack` has no equivalent

```text
absorb  split  squash  pop  revert  undo  freeze  unfreeze
```

The operation does not exist in the `gh stack` model. Most of them name a Git
recipe instead — `gt squash` points at `git reset --soft <parent>`, for example.
`freeze` and `unfreeze` have no counterpart at all.

### The scope does not match

```text
untrack  unlink
```

Graphite untracks one branch. `gh stack unstack` drops an entire stack.
`gt track` is the exception: it maps onto `gh stack init` of the named
branches (or the current one). `gt untrack` still refuses rather than
unstacking more than you asked for.

### Not a stack operation

```text
aliases   auth      changelog  children  config  dash
demo      docs      feedback   guide     info    parent
```

`gh` itself, GitHub, or nothing at all handles these. `gt` has no configuration
of its own, so `gt config` points at `gh`.

## Extension and state compatibility

If `github/gh-stack` is missing, `opengt` offers to install it interactively:

```text
gt: the gh stack extension is not installed. Install it now? [Y/n]
```

In a non-interactive environment it never installs software automatically; it
prints the installation command and exits instead.

To decide whether `gt create` should initialize or extend a stack, `opengt`
reads the repository's single `.git/gh-stack` state file at the repo root (the
shared git directory; see [One state file](#one-state-file-at-the-repo-root)).
It refuses to run against an unknown schema version so that a future
`gh-stack` update cannot silently corrupt a stack.

[gh-cli]: https://cli.github.com/
[gh-stack]: https://github.com/github/gh-stack
[releases]: https://github.com/Arnavkar/opengt/releases
