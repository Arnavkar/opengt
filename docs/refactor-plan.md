# Fast sync/submit refactor plan

Stop calling `gh stack rebase` / `gh stack submit` as monolithic hot-path
operations. Keep `gh-stack` as the state/schema and GitHub-stack compatibility
layer, but implement the common sync/submit workflows directly and
efficiently. `--native` remains as the documented fallback.

This is a local vibe-coded repo — no PRs. The plan is structured so a swarm
of Composer subagents can implement it in parallel against agreed interfaces.

## Architectural endpoint

```
commands.go (thin: parse flags, dispatch)
        │
        ▼
planner (sync_plan.go, submit_plan.go)   ← pure, unit-testable
        │
        ▼
domain models (stack.go, forest.go, reconcile.go)   ← existing, lightly extended
        │
        ▼
adapters: git (git.go, remote_refs.go, push.go, restack.go)
          github (github_client.go, stack_remote.go)   ← new, go-gh backed
        │
        ▼
.git/gh-stack state  +  GitHub native stacks
```

Dependency direction: `commands → planner → domain → adapters`. The planners
must not import `os/exec` or `github.com/cli/go-gh`.

## Swarm execution strategy

Three waves. Within each wave, tasks run in parallel.

### Wave 0 — Contracts (one agent, fast, blocks everything)

Define all shared interfaces and types in a single file so downstream agents
agree on shapes. No logic, just signatures and types.

**File:** `cmd/gt/contracts.go` (new)

Must contain:

```go
// timing.go contract
type TimingSpan struct { Name string; Duration time.Duration; Sub []TimingSpan }
type Timer struct { Enabled bool; Spans []TimingSpan }
func (t *Timer) Measure(name string, fn func() error) error
func (t *Timer) Start(name string) func()

// remote_refs.go contract
type RemoteRef struct { Branch string; Exists bool; RemoteSHA string }
type RemoteRefSnapshot struct { Refs map[string]RemoteRef; FetchedAt time.Time }
func LoadRemoteRefs(branches []string) (RemoteRefSnapshot, error)

// github_client.go contract
type PRSnapshot struct {
    Number int; NodeID string; State string; Head string; Base string
    Draft bool; Merged bool; AutoMergeEnabled bool; URL string
}
type GitHubClient struct { repo string }
func NewGitHubClient() (*GitHubClient, error)
func (c *GitHubClient) BatchPRSnapshot(known map[string]int, heads []string) (map[string]PRSnapshot, error)

// snapshot.go contract
type RemoteStackSnapshot struct { ID string; Numbers []int }
type RemoteSnapshot struct {
    Refs RemoteRefSnapshot
    PRs  map[string]PRSnapshot
    RemoteStack *RemoteStackSnapshot
}
func LoadRemoteSnapshot(branches []string, knownPRs map[string]int) (*RemoteSnapshot, error)

// restack.go contract
type RestackResult struct { Branch string; Moved bool; OldSHA string; NewSHA string; Skipped bool }
type Rollback struct { OriginalRefs map[string]string; OriginalHEAD string }
type RestackOpts struct { Force bool; Interactive bool }
func CascadeRestack(stack []trackedBranch, opts RestackOpts) ([]RestackResult, *Rollback, error)

// push.go contract
type PushRef struct { Branch string; LocalSHA string; ExpectedOld string; IsNew bool }
type PushOpts struct { Force bool; NoVerify bool; DryRun bool }
func AtomicPush(refs []PushRef, opts PushOpts) error

// stack_remote.go contract
type StackRemoteClient interface {
    GetStack(stackID string) (*RemoteStackSnapshot, error)
    CreateStack(prNumbers []int) (string, error)
    UpdateStack(stackID string, prNumbers []int) error
}

// submit_plan.go contract
type BranchSubmitPlan struct {
    Branch string; Parent string; LocalSHA string; RemoteSHA string
    RemoteExists bool; Changed bool; PR *PRSnapshot; ExpectedBase string
    Push bool; CreatePR bool; UpdateBase bool; Publish bool; DisableAutoMerge bool
}
type SubmitPlan struct {
    Scope []BranchSubmitPlan; Pushes []PushRef; Creates []PRCreate
    BaseUpdates []PRBaseUpdate; PublishUpdates []PRPublishUpdate
    AutoMergeDisables []PRAutoMergeDisable; StackUpdate *StackMutation
    Restack []RestackResult
}
type SubmitOpts struct {
    Stack bool; UpdateOnly bool; Always bool; Publish bool; Draft bool
    DryRun bool; Restack bool; Force bool; NoVerify bool
}
func BuildSubmitPlan(repo *repoStackState, current string, snap *RemoteSnapshot, opts SubmitOpts) (SubmitPlan, error)
func ResolveSubmitScope(stack trackedStack, current string, stackFlag bool, updateOnly bool, prs map[string]PRSnapshot) []string
func (p SubmitPlan) IsNoOp() bool

// sync_plan.go contract
type TrunkPlan struct { Branch string; LocalSHA string; RemoteSHA string; FastForward bool }
type StackSyncPlan struct { Stack trackedStack; Restack []RestackResult; SkipReason string }
type SyncPlan struct { Trunk TrunkPlan; Stacks []StackSyncPlan; Stale []staleBranch }
func BuildSyncPlan(repo *repoStackState, snap *RemoteSnapshot) SyncPlan

// validate.go contract
func validateStackForMutation(repo *repoStackState, branch string) error

// mutation state
type MutationState struct { GitMutated bool; RemoteMutated bool }
```

### Wave 1 — Parallel implementation (many agents)

Each agent owns one file + its test file. Agents depend only on
`contracts.go` and existing code. No agent edits another agent's file.

| Agent | File(s) | Depends on | Tests assert |
|---|---|---|---|
| A-timing | `timing.go`, `timing_test.go` | contracts | spans nest, error path records, stdout clean |
| A-remote-refs | `remote_refs.go`, `remote_refs_test.go` | contracts, `git.go` parser | parse ls-remote, missing→Exists:false |
| A-github-client | `github_client.go`, `github_client_test.go` | contracts, `go get github.com/cli/go-gh/v2` | query builder (no network), alias shape |
| A-snapshot | `snapshot.go`, `snapshot_test.go` | contracts, remote_refs, github_client | concurrent load, both errors surface |
| A-restack | `restack.go`, `restack_test.go` | contracts, `git.go` (`isAncestor`, `branchHead`) | ancestor→0 rebases, 1 diverged→1 rebase, conflict→rollback |
| A-push | `push.go`, `push_test.go` | contracts | refspec new vs existing, lease from snapshot, --no-verify |
| A-stack-remote | `stack_remote.go`, `stack_remote_test.go` | contracts, github_client | idempotent UpdateStack skips, query mirrors gh-stack v0.1.0 |
| A-submit-plan | `submit_plan.go`, `submit_plan_test.go` | contracts, `reconcile.go`, `stack.go` | all 14 scope cases, -u filters missing PRs, IsNoOp |
| A-sync-plan | `sync_plan.go`, `sync_plan_test.go` | contracts, `reconcile.go` | pure plan from fake repo+snapshot, worktree skip |
| A-validate | `validate.go`, `validate_test.go` | contracts, `stack.go`, `reconcile.go`, `git.go` | all 6 checks, no network |

### Wave 2 — Integration (fewer agents, after Wave 1 merges)

| Agent | File(s) | Task |
|---|---|---|
| B-sync | `commands.go` (`cmdSync`), `git.go` (generalize `fastForwardTrunk`) | rewrite cmdSync: concurrent snapshot → plan → ff trunk → ff branches → cascade restack → prune → persist once. Add `--no-restack`, `-f` flags. |
| B-submit | `commands.go` (`cmdSubmit`), `main.go` | rewrite cmdSubmit: validate → build plan → no-op short circuit → restack → atomic push → create PRs → update bases → disable automerge → publish → update stack → persist. Add `--dry-run`, `--always`, `--no-verify`, `--native`, `--restack`/`--no-restack`, `-f` flags. Make `ss` alias meaningful. |
| B-prune | `prune.go` | consume `*RemoteSnapshot` instead of `listPullRequests`; delete `gh pr list` from common path. |
| B-lint | `lint_test.go` | assert `*_plan.go` imports neither `os/exec` nor `github.com/cli/go-gh`. |

### Wave 3 — Tests + docs (after Wave 2)

| Agent | File(s) | Task |
|---|---|---|
| C-integration | `test/integration/workflow_test.go` | add cases: clean sync (0 gh calls), 5-stack 2-changed (1 push), lease failure (no partial), -u missing PR (no push/create), no-op submit, --native fallback pre/post mutation |
| C-bench | `test/integration/bench_test.go` (build tag `integration`) | 1/3/5/10-branch matrix, assert subprocess invocation counts not wall time |
| C-readme | `README.md` | update command mapping table, document `--native`, `--dry-run`, `--always`, `--no-verify` |

## Key design decisions (binding for all agents)

1. **One new dependency only:** `github.com/cli/go-gh/v2`. Added by the
   github-client agent. Reuses `gh` auth/config, supports GHES.

2. **Targeted `ls-remote` preserved.** Do not replace
   `fetchStackOrigin`'s stack-only ref strategy (`git.go:299`) with a full
   `git fetch`. The remote-refs agent reuses `parseLsRemoteHeads` (`git.go:354`).

3. **Planners are pure.** `BuildSyncPlan`, `BuildSubmitPlan`,
   `ResolveSubmitScope` take already-loaded data, do no I/O. This is what
   makes them unit-testable and what lets the lint agent enforce the
   dependency direction.

4. **Restack before any network I/O.** Submit restacks locally, recomputes
   SHAs, then reads remote. Sync reads remote concurrently, plans, then
   restacks. Zero GitHub calls during restack.

5. **One atomic push.** `git push --atomic` with per-ref
   `--force-with-lease=<branch>:<expect>`. No fallback to unconditional
   `--force`. On lease failure, entire push fails, no branch moves.

6. **No-op fast path.** `SubmitPlan.IsNoOp()` → print "Stack already up to
   date", exit 0. Zero pushes, zero mutations. Git + GitHub reads are
   concurrent so latency floor is `max(GitRTT, GitHubRTT)`.

7. **`-u` is enforced, not advisory.** The current note at
   `commands.go:209-213` is deleted. Branches without open PRs are excluded
   from scope entirely when `--update-only` is set.

8. **`--native` fallback.** Pre-mutation: unsupported state → error message
   pointing to `--native`. Post-mutation (`MutationState.GitMutated ||
   RemoteMutated`): never switch implementations.

9. **`gt sync` is repo-wide.** Iterates every stack in
   `loadRepoStackState()`, not just HEAD's stack. Skips branches checked
   out in other worktrees (reuse `worktreePathForBranch`, `git.go:375`).

10. **Cheap validator, not full doctor.** `validateStackForMutation` checks
    only: schema, ambiguity, branch existence, worktree state, paused
    gh-stack op, ancestry. All local. Full `gt doctor` only on abnormal state.

## Performance acceptance (structural, CI-enforced)

| Operation | Asserted via |
|---|---|
| Clean `sync` | zero `gh stack` invocations, one `git ls-remote` |
| Clean `submit -u` | zero `git push`, zero `gh` mutations |
| Changed `submit` | exactly one `git push` regardless of stack depth |
| PR discovery | exactly one GraphQL call (test interceptor) |
| Stack update | zero mutations when order unchanged |
| Local restack | zero GitHub API calls |
| 5 vs 10 branches | network RTT count approximately constant |

## Test matrix (spec, must be covered)

1. No-op submit: zero push, zero mutations
2. One changed branch in 5-stack: one atomic push, refspec has only that branch
3. Three changed branches: one push, three refspecs
4. Lease failure: entire push fails, no partial remote state
5. `gt ss -u` with missing PR: branch not pushed, no PR created
6. Regular submit with missing PR: branch pushed, PR created, stack updated
7. Wrong PR base: base alone updated, branch not pushed
8. No-op stack object: no stack mutation
9. Middle branch changed: descendants restacked locally, changed descendants in atomic push
10. Rebase conflict: no remote mutations, local refs recover cleanly
11. Other-worktree branch: sync does not mutate it
12. Unrelated normal Git branch: never touched
13. Graphite scope: submit=trunk→current, --stack=entire stack, -u filters missing PRs
14. Timing: output doesn't contaminate stdout
15. Fallback: native allowed before mutation, impossible after

## Attribution

If substantial code is adapted from `gh-stack` (MIT-licensed), preserve the
required license/attribution notices in the adapted files.
