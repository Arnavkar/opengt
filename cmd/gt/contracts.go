package main

import "time"

// This file is the shared contract surface for the fast sync/submit refactor.
// It defines the types and interfaces every planner and adapter agrees on.
// Package-level function contracts are documented below as comments; their
// implementations live in their owning files (see docs/refactor-plan.md).
//
// Dependency direction: commands → planner → domain → adapters.
// The planner files (sync_plan.go, submit_plan.go, validate.go) must not
// import "os/exec" or github.com/cli/go-gh. The lint_test.go agent enforces
// this.

// --------------------------------------------------------------------------------
// timing.go contract
// --------------------------------------------------------------------------------

// TimingSpan is one measured step. Sub holds nested spans so a plan can be
// reported as a tree.
type TimingSpan struct {
	Name     string
	Duration time.Duration
	Sub      []TimingSpan
}

// Timer records spans when enabled. When disabled it is a no-op so stdout
// stays clean for pipes (test matrix #14).
//
// Contract (implemented in timing.go):
//
//	func (t *Timer) Measure(name string, fn func() error) error
//	func (t *Timer) Start(name string) func()
type Timer struct {
	Enabled bool
	Spans   []TimingSpan
}

// --------------------------------------------------------------------------------
// remote_refs.go contract
// --------------------------------------------------------------------------------

// RemoteRef is the view of one branch on origin: whether it exists and the SHA
// it points at. Exists is false when ls-remote did not list the branch.
type RemoteRef struct {
	Branch    string
	Exists    bool
	RemoteSHA string
}

// RemoteRefSnapshot is the result of one targeted ls-remote over a set of
// branches. FetchedAt records when the read happened so lease values are
// attributable to a single snapshot.
type RemoteRefSnapshot struct {
	Refs      map[string]RemoteRef
	FetchedAt time.Time
}

// LoadRemoteRefs runs a targeted `git ls-remote --heads origin <branches...>`
// and returns one RemoteRef per requested branch. Missing branches are
// reported with Exists:false rather than dropped. Reuses parseLsRemoteHeads
// (git.go); does not replace fetchStackOrigin's stack-only strategy.
//
// Contract (implemented in remote_refs.go):
//
//	func LoadRemoteRefs(branches []string) (RemoteRefSnapshot, error)

// --------------------------------------------------------------------------------
// github_client.go contract
// --------------------------------------------------------------------------------

// PRSnapshot is the GitHub-side view of one pull request, limited to the
// fields sync/submit need. State is uppercase ("OPEN", "MERGED", "CLOSED").
type PRSnapshot struct {
	Number           int
	NodeID           string
	State            string
	Head             string
	Base             string
	Draft            bool
	Merged           bool
	AutoMergeEnabled bool
	URL              string
}

// GitHubClient wraps go-gh for batched GraphQL reads and PR mutations. It
// reuses gh auth/config and supports GHES. One new dependency only.
//
// Contract (implemented in github_client.go):
//
//	func NewGitHubClient() (*GitHubClient, error)
//	func (c *GitHubClient) BatchPRSnapshot(known map[string]int, heads []string) (map[string]PRSnapshot, error)
type GitHubClient struct {
	repo string
}

// --------------------------------------------------------------------------------
// snapshot.go contract
// --------------------------------------------------------------------------------

// RemoteStackSnapshot is the GitHub-native stack object (gh-stack v0.1.0): an
// id and the ordered PR numbers it contains.
type RemoteStackSnapshot struct {
	ID      string
	Numbers []int
}

// RemoteSnapshot is the unioned remote view loaded once, concurrently: git
// refs and GitHub PRs (and the native stack object when present). Both error
// sources surface so a caller can decide whether to proceed.
type RemoteSnapshot struct {
	Refs        RemoteRefSnapshot
	PRs         map[string]PRSnapshot
	RemoteStack *RemoteStackSnapshot
}

// LoadRemoteSnapshot loads refs and PRs concurrently and returns a single
// snapshot. Both the git and GitHub errors surface; a nil snapshot is returned
// only when both fail in a way that prevents any use.
//
// Contract (implemented in snapshot.go):
//
//	func LoadRemoteSnapshot(branches []string, knownPRs map[string]int) (*RemoteSnapshot, error)

// --------------------------------------------------------------------------------
// restack.go contract
// --------------------------------------------------------------------------------

// RestackResult describes what happened to one branch during a cascade rebase.
// Moved is true when its SHA changed; Skipped is true when it was left alone
// (e.g. checked out in another worktree).
type RestackResult struct {
	Branch  string
	Moved   bool
	OldSHA  string
	NewSHA  string
	Skipped bool
}

// Rollback captures the ref state before a restack so it can be restored on
// conflict. OriginalRefs maps branch→SHA; OriginalHEAD is the pre-restack HEAD.
type Rollback struct {
	OriginalRefs map[string]string
	OriginalHEAD string
}

// RestackOpts toggles force and interactive rebases. Trunk names the stack's
// trunk branch: when set, the bottom branch is rebased onto it (a live ref
// that follows the moved trunk) instead of its cached base SHA, which is a
// snapshot of the trunk from when the stack was written.
type RestackOpts struct {
	Force       bool
	Interactive bool
	Trunk       string
	// Merged branches are left at their current SHA. The stack order is
	// unchanged, so the branch above still rebases onto the merged branch.
	Merged map[string]bool
}

// CascadeRestack rebases each branch onto its parent in stack order, returning
// one RestackResult per branch and a Rollback usable to recover local refs on
// conflict. Zero GitHub API calls happen here. Branches checked out in other
// worktrees are skipped.
//
// Contract (implemented in restack.go):
//
//	func CascadeRestack(stack []trackedBranch, opts RestackOpts) ([]RestackResult, *Rollback, error)

// --------------------------------------------------------------------------------
// push.go contract
// --------------------------------------------------------------------------------

// PushRef is one branch in an atomic push. ExpectedOld is the lease value:
// the SHA the remote is expected to currently hold. IsNew marks a first push
// (no lease, plain refspec).
type PushRef struct {
	Branch      string
	LocalSHA    string
	ExpectedOld string
	IsNew       bool
}

// PushOpts mirrors the git push flags gt needs.
type PushOpts struct {
	Force    bool
	NoVerify bool
	DryRun   bool
}

// AtomicPush runs a single `git push --atomic` with per-ref
// --force-with-lease=<branch>:<expect> for existing refs and plain refspecs
// for new ones. On lease failure the entire push fails; no branch moves.
// There is no fallback to unconditional --force.
//
// Contract (implemented in push.go):
//
//	func AtomicPush(refs []PushRef, opts PushOpts) error

// --------------------------------------------------------------------------------
// stack_remote.go contract
// --------------------------------------------------------------------------------

// StackRemoteClient is the GitHub-native stack object adapter. The real
// implementation (stack_remote.go) is backed by GitHubClient; tests inject a
// fake.
type StackRemoteClient interface {
	GetStack(stackID string) (*RemoteStackSnapshot, error)
	CreateStack(prNumbers []int) (string, error)
	UpdateStack(stackID string, prNumbers []int) error
}

// --------------------------------------------------------------------------------
// submit_plan.go contract
// --------------------------------------------------------------------------------

// PRCreate describes a new pull request to open.
type PRCreate struct {
	Branch  string
	Parent  string
	HeadSHA string
	Draft   bool
	Title   string
	Body    string
}

// PRBaseUpdate changes only the base of an existing PR (branch not pushed).
type PRBaseUpdate struct {
	PRNumber int
	NewBase  string
}

// PRPublishUpdate marks an existing draft PR ready for review.
type PRPublishUpdate struct {
	PRNumber int
}

// PRAutoMergeDisable turns auto-merge off for an existing PR.
type PRAutoMergeDisable struct {
	PRNumber int
}

// StackMutation is the one native-stack update a submit may need: the stack id
// to update (or empty to create) and the ordered PR numbers.
type StackMutation struct {
	StackID string
	Numbers []int
}

// BranchSubmitPlan is the per-branch decision for one branch in the submit
// scope. The boolean flags drive which mutation buckets the branch lands in.
type BranchSubmitPlan struct {
	Branch           string
	Parent           string
	LocalSHA         string
	RemoteSHA        string
	RemoteExists     bool
	Changed          bool
	PR               *PRSnapshot
	ExpectedBase     string
	Push             bool
	CreatePR         bool
	UpdateBase       bool
	Publish          bool
	DisableAutoMerge bool
}

// SubmitPlan is the full set of mutations a submit will perform, computed
// purely from a repo state, the current branch, a remote snapshot, and opts.
// The buckets are populated so the executor can iterate them in order.
type SubmitPlan struct {
	Scope             []BranchSubmitPlan
	Pushes            []PushRef
	Creates           []PRCreate
	BaseUpdates       []PRBaseUpdate
	PublishUpdates    []PRPublishUpdate
	AutoMergeDisables []PRAutoMergeDisable
	StackUpdate       *StackMutation
	Restack           []RestackResult
}

// SubmitOpts mirrors the submit flags. Stack forces whole-stack scope;
// UpdateOnly (-u) excludes branches without open PRs; Always forces a pass
// even when IsNoOp; Publish marks PRs ready; Draft creates new PRs as drafts;
// DryRun plans without mutating; Restack/--no-restack toggles the local restack;
// Force/--no-verify pass through to push.
type SubmitOpts struct {
	Stack      bool
	UpdateOnly bool
	Always     bool
	Publish    bool
	Draft      bool
	DryRun     bool
	Restack    bool
	Force      bool
	NoVerify   bool
}

// BuildSubmitPlan computes a SubmitPlan purely from already-loaded data: the
// repo stack state, the current branch, the remote snapshot, and options. It
// does no I/O. ResolveSubmitScope determines which branches are in scope
// (Graphite semantics: trunk→current by default, whole stack with --stack,
// -u filters out branches without open PRs). IsNoOp is the fast path.
//
// Contract (implemented in submit_plan.go):
//
//	func BuildSubmitPlan(repo *repoStackState, current string, snap *RemoteSnapshot, opts SubmitOpts) (SubmitPlan, error)
//	func ResolveSubmitScope(stack trackedStack, current string, stackFlag bool, updateOnly bool, prs map[string]PRSnapshot) []string
//	func (p SubmitPlan) IsNoOp() bool

// --------------------------------------------------------------------------------
// sync_plan.go contract
// --------------------------------------------------------------------------------

// TrunkPlan describes the trunk fast-forward: whether the local trunk is
// behind origin and should be fast-forwarded.
type TrunkPlan struct {
	Branch      string
	LocalSHA    string
	RemoteSHA   string
	FastForward bool
}

// StackSyncPlan is the per-stack restack decision during sync. SkipReason is
// non-empty when the stack was skipped (e.g. a branch is checked out in
// another worktree).
type StackSyncPlan struct {
	Stack      trackedStack
	Restack    []RestackResult
	SkipReason string
}

// SyncPlan is the pure output of BuildSyncPlan: the trunk fast-forward, the
// per-stack restacks, and the stale branches to prune.
type SyncPlan struct {
	Trunk  TrunkPlan
	Stacks []StackSyncPlan
	Stale  []staleBranch
}

// BuildSyncPlan computes a SyncPlan purely from the repo state and a remote
// snapshot. It does no I/O. Sync is repo-wide: it iterates every stack in the
// repo state, not just HEAD's, and skips branches checked out in other
// worktrees.
//
// Contract (implemented in sync_plan.go):
//
//	func BuildSyncPlan(repo *repoStackState, snap *RemoteSnapshot) SyncPlan

// --------------------------------------------------------------------------------
// validate.go contract
// --------------------------------------------------------------------------------

// validateStackForMutation is the cheap pre-mutation check (not a full
// `gt doctor`): schema support, ambiguity, branch existence, worktree state,
// paused gh-stack op, and ancestry. All local — zero network. Returns nil
// when the branch's stack is safe to mutate.
//
// Contract (implemented in validate.go):
//
//	func validateStackForMutation(repo *repoStackState, branch string) error

// --------------------------------------------------------------------------------
// mutation state
// --------------------------------------------------------------------------------

// MutationState tracks whether any git or remote mutation has happened during
// a command, so the --native fallback can be allowed before any mutation and
// forbidden after one (design decision #8).
type MutationState struct {
	GitMutated    bool
	RemoteMutated bool
}
