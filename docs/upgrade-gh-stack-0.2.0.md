# gh-stack 0.2.0 upgrade plan

**Status: executed.** MinVersion chosen as {0,2,0}, so phase 3 deletions
(`adoptGhStack`, stack_view.go) shipped. Phase 1.3 flock also shipped
(`lock.go`, x/sys/unix, no-op off Unix).

Target key: opengt pins `TestedVersion = 0.1.1`; gh-stack 0.2.0 is installed and
warns on every invocation. 0.2.0 keeps schema v1 and moves to a single shared
catalog at `<git-common-dir>/gh-stack` — the same file gt already reads — but
it changes paused-op recovery, catalog atomicity, and lock files around it.

## Evidence (verified, 2026-10, local + upstream source)

- `gh stack init` run in a **linked worktree** now writes the state file to the
  common git dir (`/tmp/.../main-repo/.git/gh-stack`), not
  `.git/worktrees/<name>/gh-stack`. `schemaVersion` stays `1`.
- New files beside the catalog: `gh-stack.lock` (flock, `internal/stack/lock.go`)
  and `gh-stack-operation.lock` (cross-command exclusion).
- gh-stack's own catalog writes are atomic (`internal/stack/atomic_unix.go`,
  tmp + rename + fsync).
- Paused-op markers: `gh-stack-rebase-state` (unchanged name) and
  `gh-stack-modify-state`; the binary also contains a suffixed variant
  `gh-stack-modify-state_%s`. Markers now live in the common dir.
- 0.1.1-era per-worktree catalogs are auto-migrated on the next `gh stack`
  command. If definitions conflict, gh-stack stops with guidance and leaves
  the repo-root file unmigrated (`internal/stack/migration.go`,
  `MigrationBlockedError`). A `gh-stack-migration` backup file may linger.
- `init`/`add` can adopt a branch checked out in **another** worktree.
  The same-worktree refusal (`current branch "X" is already part of a stack`)
  is unchanged — the planned `gt track --parent` (see end) still needs the
  trunk-hop.
- Cross-worktree `rebase`/`sync`/`modify`: changes run in each branch's
  owning worktree; dirty/unfinished-op checks up front; `--continue`/`--abort`
  work from any worktree via the shared recovery record. Requires git 2.36+.
- Update notifier: background, ≤1/day, stderr only, off via
  `GH_STACK_NO_UPDATE_NOTIFIER=1`.

## What gt does today that interacts with this

| opengt behavior | file | interaction with 0.2.0 |
|---|---|---|
| Reads one catalog: `--git-common-dir`/gh-stack | `git.go:421` | Correct upstream behavior now; comments describing 0.1.1 per-worktree divergence are stale |
| `pausedOperation` exact-name stat of `gh-stack-{rebase,modify}-state` | `stack.go:173` | **Misses suffixed modify marker** → gt mutates during a paused cross-worktree modify |
| `writeStackFile` = plain `os.WriteFile` | `stack.go:66` | **Torn-write risk** now that gh-stack assumes atomic catalogs and both tools share one file |
| `adoptGhStack` asks `gh stack view --json` when the repo-root file doesn't place the current branch | `stack_view.go` | Fallback for 0.1.1 per-worktree copies; no longer fires under 0.2.0 |
| `withWorktreeStacks` synthesizes forest rows for branches in other worktrees | `forest.go:237` | Partially stale (see phase 3) |
| `requireGhStack` version gate, `MinVersion 0.1.0`, `TestedVersion 0.1.1` | `compat.go:12` | Emits the stale warning |

## Phases

### Phase 1 — correctness fixes (ship first, still on TestedVersion 0.1.1)

1. **Fix paused-op detection** (`stack.go:173-202`).
   - Change exact `os.Stat` to a prefix match on `gh-stack-rebase-state` and
     `gh-stack-modify-state` (incl. suffixed `..._...` variants) in both
     `pausedMarkerDirs` (per-git-dir + common dir).
   - Unit test `stack_test.go`: seed `gh-stack-modify-state_wt1`-style file in a
     temp git dir, assert `pausedOperation()` reports `modify`.
2. **Make `writeStackFile` atomic** (`stack.go:66-77`).
   - Write to `<path>.gt-tmp-<pid>`, `fsync`, `os.Rename` over the target; clean
     up tmp on error. Same-dir rename = atomic on POSIX; matches gh-stack's own
     guarantee so neither tool can read a torn catalog.
   - Unit test: path in unreadable dir errors; success leaves no tmp files.
3. **Interop lock (stretch, same PR allowed)**.
   - Acquire `syscall.Flock` (stdlib, unix build tag; noop file for windows)
     on `<common-dir>/gh-stack.lock` around `writeStackFile`, mirroring
     `internal/stack/lock.go`. Eliminates the remaining lost-update race when a
     gt write coincides with a gh-stack mutation. If deferred, document the
     last-writer-wins window in the README caveats.

### Phase 2 — compat bump

4. Run the full suite with 0.2.1/0.2.0 installed; `go test ./...` plus
   `go test -tags=integration -timeout 15m ./test/integration/`.
5. `compat.go:12-18`: `TestedVersion {0,1,1} → {0,2,0}`. Update
   `compat_test.go` expectations and the warning text tests.
6. `.github/workflows/ci.yml`: `--pin v0.1.1` → `--pin v0.2.0` in the
   integration job. Check `.github/workflows/gh-stack-compat.yml` (daily
   latest-test) for any hard-coded versions; update its docs comment if it
   references 0.1.1.
7. `README.md`: "targets gh-stack **v0.1.1**, state schema **v1**" → v0.2.0.
   Refresh the "Pinning `gh-stack`" section.
8. **Doctor additions** (`doctor.go`):
   - Detect un-migrated legacy catalogs: any `.git/worktrees/*/gh-stack` files
     existing alongside the common-dir catalog → warn "gh-stack has not
     migrated old per-worktree state yet; run any `gh stack` command, or
     inspect the migration conflict". (gt reads only the repo-root file, so a
     blocked migration means gt sees stale state silently.)
   - Report git < 2.36 when the repo has worktrees (cross-worktree ops
     require it upstream).
9. Child env: set `GH_STACK_NO_UPDATE_NOTIFIER=1` on the env gt uses to spawn
   `gh` (`git.go` `exec1`/`run` path) so gt-owned output stays clean; gt has
   its own version warning.

### Phase 3 — deletions (requires MinVersion decision)

10. Decide `MinVersion`. Recommend **{0,2,0}**: gh-stack auto-migrates old
    catalogs on its next command, and with it:
    - `git.go:98` `requireGhStack` floor bump; error text unchanged.
    - **Delete `adoptGhStack`** (`stack_view.go`) + its tests: under 0.2.0 every
      write lands in the one file gt reads. Remove the README "adoption
      fallback" paragraph (~lines 185-194).
    - **`withWorktreeStacks`** (`forest.go:237-290`): it serves two cases —
      (a) tracked branches whose tracking lived in another git-dir (dead
      under 0.2.0) and (b) plain untracked branches checked out in other
      worktrees (still real). Keep (b), delete (a), update comments in
      `git.go:421-466` that describe per-worktree files as active upstream
      behavior.
    - If MinVersion stays {0,1,0} instead: skip 10 entirely, keep everything.
      Call out in the PR description which was chosen and why.

### Phase 4 — integration tests (`test/integration/`, `integration` tag)

11. New scenario: init a stack **in a linked worktree** (0.2.0), then from the
    main worktree run `gt ls`, `gt doctor`, `gt sync`. Expect: single catalog
    read, no `CONFLICTING_STACK`, no AdoptGhStack round-trip.
12. New scenario: force a paused cross-worktree modify with 0.2.0
    (conflicting restack), then assert `gt restack` and `gt sync` refuse with
    the paused-operation error from any worktree; `gt continue` resumes via
    `gh stack modify --continue`.
13. Regression for phase 1.2: concurrent-ish writes — run `gt sync` while a
    background `gh stack rebase` holds the operation lock in a scratch repo;
    assert no corrupt catalog (best-effort, allow flakiness guard).
14. Version-warning integration: install pinned 0.1.1 in one test env (if the
    MinVersion answer in step 10 is {0,2,0}, assert the NOT-TESTED warning
    appears; else assert both pass).

## Risks

- **Blocked migration on user machines**: repos with conflicting per-worktree
  0.1.1 catalogs keep an unmigrated repo-root file; gt would read stale state.
  Mitigated by phase 2.8 (doctor detection). Do not have gt migrate.
- **Marker name drift**: prefix matching (phase 1.1) tolerates suffix changes,
  but rely on it minimally — `--continue`/`--abort` still delegate to
  `gh stack` itself.
- **Lock semantics divergence**: if gt implements phase 1.3 with flock and
  gh-stack moves to a different lock, worst case is gt's lock being ignored —
  no corruption risk (its writes are still atomic). Acceptable.

## Out of scope (this PR)

- Chained-stack support (`gt track --parent` → `gh stack init --base <parent>`).
  0.2.0 changed nothing there: same-worktree refusal and opengt's `forked`
  rejection (`stack.go:134`) stand. Separate change.
- gh-stack GitHub-side features (`link`) — no local tracking to shim.

## Acceptance criteria

- No "newer than the tested 0.1.1" warning with 0.2.0 installed.
- `gt doctor` clean on a repo whose stacks were created across two worktrees
  with 0.2.0.
- Unit + integration suites green on CI pin v0.2.0; daily latest-compat job
  green.
- README reflects v0.2.0 and, if phase 3 runs, no mention of `gh stack view
  --json` adoption fallback.
