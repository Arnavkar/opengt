# What the integration suite still does not prove

`go test -tags=integration ./test/integration/` is the suite. A pass does
not mean the items below were verified. Do not mark them done via `--help`.

## Interactive UIs (need a real TTY)

- **`gt checkout` / `gt co` with no argument** — Graphite tree: tips at top,
  trunk at bottom, `◉`/`◯`, type-to-filter, arrows, Enter, Esc,
  dim `not in a stack · gt track`, last row `…  All stacks on GitHub`.
  Suite only drove `gt checkout <name>`.
- **`gt delete` with no argument** — same tree minus trunk/GitHub row.
  Prompt `Delete a branch (autocomplete or arrow keys)`.
- **`gt sync` prune prompt** — confirm one stale branch, or multi-select
  (`space toggle · enter delete selected`).
- **`gt submit` upstack `[y/N]`** — TTY-only. Non-TTY skips upstack.
- **`gt doctor --repair` confirm** — TTY prompt before safe repairs.
- **`gt create -p` / `gt modify -p`** — `git add -p` hunk picker.
- **`gt switch`** — `gh stack switch` TUI. Not ours; still a user path.
- **`gt pr`** — opens a browser.

## `--native` fallback (never driven)

`create`, `track`, `modify`, `restack`, `submit`, `log`, `up`/`down`/`top`/
`bottom`/`trunk`. Also: `--native` refused after a mutation has started.

## Flags and aliases never driven

- `gt create` name-from-`-m` (`MM-DD-slug`), `-u`, `--insert` refuse, `--native`
- `gt c` alias
- `gt modify -c`, `-e`, `--native`; `gt m` alias
- `gt restack -u`, `-d`, `-o` refuse, `--native`
- `gt sync -d`, `--no-restack`, `-f`
- `gt submit` / `gt s` / `gt ss`: `--stack`, `-d`, `-p`, `-u`, `-e`, `-n`,
  `--always`, `--restack`/`--no-restack`, `-f`, `--no-verify`, `--native`,
  lease-failure, no-op `already up to date`
- `gt checkout -t` / `gt co -t` (`gh stack trunk`)
- `gt log --native` and unknown-flag delegate to `gh stack view`
- `gt up 2` / `gt down 2`
- `gt track a b --base <trunk>`, `--parent` refuse, `--native`, `gt tr`
- `gt doctor --repair`, `--repair --yes`, exit codes 1/2/3/4
- Extra-arg refusals on `continue` / `abort` / `top`

## GitHub (needs `GT_VERIFY_GITHUB=1`)

- `gt submit` atomic push + draft PR create/update + stack object
- `gt get <pr>`
- `gt merge`
- Picker row **All stacks on GitHub** (`gh stack checkout` with no args)
- Sync prune of a **finished** stack (every PR merged/closed)
- Sync keep-merged-sibling when one PR is still open

## Repo shapes never built

- Two stacks in one catalog (forest columns, `ambiguous` branch)
- A branch in two stacks (hard stop)
- Linked worktree sharing `.git/gh-stack`
- Legacy `.git/worktrees/<name>/gh-stack` (`LEGACY_STATE_UNMIGRATED`)
- Unknown schema (hard stop)
- Missing `gh stack` (install prompt `[Y/n]`)
- `NO_COLOR` off (color path of the picker)
- `gh stack modify` TUI (`fold` / `move` / `reorder` / `rename`)

## Unsupported Graphite names (only `absorb` was run)

`fold`, `move`, `reorder`, `rename`, `split`, `squash`, `pop`, `revert`,
`undo`, `freeze`, `unfreeze`, `untrack`, `unlink`, `info`, `children`,
`parent`, `dash`, `auth`, `config`, `aliases`, `demo`, `guide`, `docs`,
`feedback`, `changelog`.
