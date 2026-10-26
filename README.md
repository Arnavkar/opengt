# opengt

`opengt` installs a `gt` command that speaks Graphite and writes GitHub's
native stacks.

You type the Graphite CLI you already know. `gt` turns those commands into
Git operations, GitHub API calls, and [GitHub's official `gh stack`][gh-stack]
commands. `gh stack` stays the source of truth. `opengt` has no backend,
metadata format, or config of its own.

> [!IMPORTANT]
> Early software. **Linear stacks only.** Targets `gh-stack` **v0.2.1**,
> state schema **v1**. When a command cannot be translated safely, `gt`
> stops with an error instead of guessing.

## What this does

Five jobs. Nothing else.

### 1. Keep one linear stack catalog

State lives in `.git/gh-stack` at the shared repository directory — the same
file `gh stack` reads and writes. Schema v1 is an ordered list of branches
on a trunk. No forks, no per-worktree copies, no opengt-owned store.

Writes are atomic and take `gh-stack.lock`, so `gt` and `gh stack` can share
the file without clobbering each other. You can mix the two CLIs:

```sh
gt log
gh stack modify
gt submit --stack
```

A branch in more than one stack is a hard stop. Use `gh stack` directly.

### 2. Grow and amend the stack locally

| You type | What happens |
| --- | --- |
| `gt create` / `gt c` | `git checkout -b`, append the branch to the catalog, optionally commit. Must run from the tip or off trunk — mid-stack would fork. |
| `gt track` / `gt tr` | Adopt existing Git branches into the catalog. Does not create a branch. |
| `gt modify` / `gt m` | Commit or amend this layer, then rebase everything above it. A branch with no commits of its own gets a new commit, not a rewrite of its parent. |
| `gt restack` | `git rebase` cascade. `-d` / `-u` scope downstack / upstack. |
| `gt continue` / `gt abort` | Finish or abandon a paused rebase. |

`gt init` does nothing. `gh stack` has no repository-level init; use
`gt track` to adopt branches.

### 3. Push the stack to GitHub as native stacked PRs

`gt submit` / `gt s` (or `gt ss` for the whole stack) validates locally,
loads a remote snapshot, builds a plan, then mutates.

Default scope is trunk → current branch. Branches above you get a `[y/N]`
prompt; `--stack` / `gt ss` skips it. `-u` / `--update-only` drops branches
that have no open PR, so `gt ss -u` never creates one.

A no-op prints `Stack already up to date` and does nothing. A changed
submit restacks (default on), then does **one** `git push --atomic` with
per-ref `--force-with-lease`. Lease failure moves no branch. Unpublished
branches are pushed before PRs open. New PRs are drafts unless you pass
`-p`. `-p` also publishes existing drafts; `gt` never adds it for you.

Origin having a commit this branch never contained is a stop. `-f` is the
override.

### 4. Pull GitHub back onto every local stack

`gt sync` is repo-wide and never pushes.

1. One targeted `git ls-remote` plus one batched GraphQL PR lookup.
2. Fast-forward trunks and behind branches.
3. Cascade-restack every stack against the updated trunk.
4. Prune a stack only when **every** PR in it is merged or closed. A stack
   with one open PR keeps its merged branches so restack still has the
   chain. Untracked branches are left alone.

`-d` / `--delete-all` deletes those finished stacks without prompting.

### 5. Navigate, inspect, and repair

| You type | What happens |
| --- | --- |
| `gt log` / `gt ls` / `gt ll` | Catalog view, short catalog, or `git log --graph`. |
| `gt up` / `down` / `top` / `bottom` / `trunk` | Check out the catalog neighbor. |
| `gt checkout` / `gt co` | Tree of local stacks (tips at top, trunk at bottom). Type to filter. Untracked local branches show dimmed — adopt with `gt track`. Last row opens `gh stack checkout` for remote stacks too. |
| `gt delete` | Same tree, then delete the chosen branch and restack what was above it. |
| `gt get <pr>` | `gh stack checkout <pr>`. |
| `gt merge` / `gt switch` | Passed to `gh stack`. |
| `gt pr` | `gh pr view --web`. |
| `gt doctor` | Compare Git, the catalog, worktrees, and GitHub. `--repair` fixes safe metadata only. |

Anything `gt` does not implement is passed to `git` (`gt status` is
`git status`). Graphite commands with no safe translation stop with the
reason and a next step — they are never forwarded.

`--native` on create, track, modify, restack, submit, log, and navigation
delegates to `gh stack` **before** any mutation. After a push or GitHub
write has started, `gt` will not switch implementations mid-operation.

## Why not `gh stack alias gt`

`gh stack alias gt` forwards arguments unchanged. The two CLIs do not share
a command surface.

```text
gt create   → git checkout -b + catalog write
gt log      → catalog view
gt restack  → git rebase cascade
gt modify   → amend this layer and rebase the upstack
gh stack modify → TUI for restructuring the stack
```

`opengt` translates intent. A blind alias would not.

## Quick start

Needs Git, an authenticated [GitHub CLI (`gh`)][gh-cli], the
[`github/gh-stack`][gh-stack] extension, and Go 1.25+ to build from source.

```sh
gh extension install github/gh-stack --pin v0.2.1 --force
```

Homebrew (macOS). The fully qualified name taps `hSATAC/toybox`:

```sh
brew install hSATAC/toybox/gtstack
```

Or Go:

```sh
go install github.com/Arnavkar/opengt/cmd/gt@latest
```

Or from source:

```sh
git clone https://github.com/Arnavkar/opengt.git
cd opengt
mkdir -p bin
go build -o bin/gt ./cmd/gt
export PATH="$PWD/bin:$PATH"
```

Linux builds are attached to each [release][releases] as `tar.gz` archives.

The binary is named `gt`, same as Graphite. `type -a gt` shows which one
your shell will run.

```console
$ gt --version
gt 0.1.0 (opengt)
```

### Everyday loop

```sh
gt create -am "Add the API client"
gt create -am "Use the API client"
gt log
gt submit --stack
```

## Verify it

The suite is the Go integration tests. They build `gt`, drive it in a
throwaway repo with a bare git remote, and never prompt.

```sh
go test -tags=integration -timeout 15m ./test/integration/
```

`go test ./...` skips this tag. See `test/integration/README.md` for what
is covered. Submit/merge/get/pr against GitHub stay skipped unless
`GT_TEST_GITHUB=1`.

## Command map

Run `gt <command> --help` for flags. Unknown flags are rejected, not dropped.

| Graphite | What `opengt` runs |
| --- | --- |
| `gt create` / `gt c` | `git checkout -b` + schema v1 write. `--native` is `gh stack init` / `gh stack add` |
| `gt modify` / `gt m` | `git commit [--amend]`, then rebase the upstack. `--native` is `gh stack rebase --upstack --no-trunk` |
| `gt submit` / `gt s` / `gt ss` | validate → plan → atomic push → PR create/update → stack update. `--native` is `gh stack submit` |
| `gt sync` | targeted fetch + fast-forward + cascade restack + prune |
| `gt restack` | `git rebase` cascade. `--native` is `gh stack rebase` |
| `gt doctor` | diagnose Git vs catalog vs worktrees vs GitHub (`--repair`, `--json`) |
| `gt continue` / `gt abort` | paused `git rebase`, else `gh stack rebase` / `gh stack modify` |
| `gt checkout` / `gt co` | tree of local stacks, or `gh stack checkout` / `git checkout` |
| `gt delete` | same tree, then delete and restack above |
| `gt get <pr>` | `gh stack checkout <pr>` |
| `gt log` / `gt ls` / `gt ll` | catalog, short catalog, or `git log --graph`. `--native` is `gh stack view` |
| `gt up` / `u`, `down` / `d`, `top` / `t`, `bottom` / `b`, `trunk` | catalog neighbor. `--native` is the matching `gh stack` command |
| `gt add`, `cherry-pick`, `rebase`, `reset`, `restore` | the same `git` command, arguments unchanged |
| `gt track` / `gt tr` | write listed branches (current if none) into schema v1. `--native` is `gh stack init --base <trunk>` |
| `gt merge` | `gh stack merge` |
| `gt switch` | `gh stack switch` |
| `gt pr` | `gh pr view --web` |
| `gt init` | explains that there is no repo-level init; use `gt track` |

### `gt submit` flags

| Flag | Meaning |
| --- | --- |
| `--stack` | whole stack, no prompt (also `gt ss`) |
| `-d`, `--draft` | create new PRs as drafts |
| `-p`, `--publish` | mark PRs ready for review (new **and** existing) |
| `-u`, `--update-only` | skip branches without an open PR |
| `-e`, `--edit` | open the `gh stack submit` editor (implies `--native`) |
| `-n`, `--no-edit` | skip the PR metadata editor (the default) |
| `--dry-run` | print the plan, mutate nothing |
| `--always` | run even when the stack is up to date |
| `--restack` / `--no-restack` | cascade restack before push (default on) |
| `-f`, `--force` | force restack and push even when unchanged |
| `--no-verify` | pass `--no-verify` to `git push` |
| `--native` | delegate to `gh stack submit` |

### `gt sync` flags

| Flag | Meaning |
| --- | --- |
| `-d`, `--delete-all` | delete finished stacks without prompting |
| `--no-restack` | skip the cascade restack |
| `-f`, `--force` | restack even when ancestry is unchanged |

### `gt doctor`

```sh
gt doctor                 # read-only
gt doctor --json          # machine-readable, no prompts
gt doctor --repair        # safe metadata repairs; asks in a terminal
gt doctor --repair --yes  # same, no prompts
```

`--yes` only applies repairs classified as safe (refresh cached SHAs, drop
duplicate identical stacks). It never resolves conflicting stack definitions
or rewrites Git history. Prefix disagreements stay unresolved; use
`gh stack modify`.

| Exit | Meaning |
| --- | --- |
| 0 | healthy |
| 1 | warnings only (for example, a descendant needs `gt restack -u`) |
| 2 | repairable errors |
| 3 | ambiguous or unsafe; do not guess |
| 4 | missing git / `gh` / `gh-stack`, GitHub API failure, or unknown schema |

## Ordinary Git is allowed

`gt` is a wrapper, not a gate.

```text
git commit          safe
git checkout        safe
git cherry-pick     generally safe; doctor can detect ancestry changes
git rebase / reset / branch -f
                    allowed. May invalidate stack ancestry or cached SHAs.
                    Run `gt doctor` afterwards if you touched stack branches.
```

`gt doctor` can repair **metadata**. It does not rewrite history.

## Transparent by default

Before each native operation, `gt` prints the exact command to stderr:

```console
$ git add -A
$ gh stack add 07-31-add_the_login_form
$ git commit -m 'Add the login form'
```

Stdout stays clean for pipelines (`gt log --json | jq`). Color is off when
stderr is not a terminal or when `NO_COLOR` is set.

## Unsupported Graphite commands

Not unfinished — blocked by the `gh stack` model. Each one stops with the
reason and the nearest `git` or `gh stack` alternative.

**Only inside `gh stack modify`:** `fold`, `move`, `reorder`, `rename`.

**No `gh stack` equivalent:** `absorb`, `split`, `squash`, `pop`, `revert`,
`undo`, `freeze`, `unfreeze`. Most name a Git recipe (`gt squash` points at
`git reset --soft <parent>`). Freeze has no counterpart.

**Scope mismatch:** `untrack`, `unlink`. Graphite untracks one branch;
`gh stack unstack` drops the whole stack. `gt` refuses rather than
unstacking more than you asked.

**Not a stack operation:** `aliases`, `auth`, `changelog`, `children`,
`config`, `dash`, `demo`, `docs`, `feedback`, `guide`, `info`, `parent`.
`gt` has no config; `gt config` points at `gh`.

## Compatibility

Missing `github/gh-stack`? Interactive `gt` offers to install it. Non-
interactive environments print the install command and exit.

Pin the tested extension:

```sh
gh extension install github/gh-stack --pin v0.2.1 --force
```

Minimum is **v0.2.0** (shared catalog). CI pins **v0.2.1**. A newer
`gh-stack` with schema v1 warns and continues. An unknown schema is a hard
stop. The daily `gh-stack-compat` workflow tests `latest` as an early
warning; it does not change what you should run.

Repos that ran `gh-stack` before 0.2.0 may still have per-worktree catalogs
under `.git/worktrees/<name>/gh-stack`. `gh stack` migrates those on its
next command. If migration is blocked, `gt doctor` warns that those files
remain and that `gt` is reading only the — possibly stale — shared file.

[gh-cli]: https://cli.github.com/
[gh-stack]: https://github.com/github/gh-stack
[releases]: https://github.com/Arnavkar/opengt/releases
