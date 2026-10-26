package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

func newFlags(usage string) *pflag.FlagSet {
	fs := pflag.NewFlagSet("gt "+usage, pflag.ContinueOnError)
	fs.SortFlags = false
	return fs
}

func parse(fs *pflag.FlagSet, args []string) error {
	err := fs.Parse(args)
	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}
	return err
}

// cmdCreate branches off the current position. At the top of a stack it
// appends a layer; off a branch in no stack (the trunk, usually) it starts a
// new one. From the middle of a stack it refuses: that would be a fork.
// --native keeps today's `gh stack add` / `gh stack init`.
func cmdCreate(args []string) error {
	fs := newFlags("create [name]")
	all := fs.BoolP("all", "a", false, "stage all changes, including untracked files")
	update := fs.BoolP("update", "u", false, "stage updates to tracked files only")
	patch := fs.BoolP("patch", "p", false, "pick hunks to stage before committing")
	insert := fs.BoolP("insert", "i", false, "insert between the current branch and its child")
	msg := fs.StringArrayP("message", "m", nil, "commit message")
	native := fs.Bool("native", false, "delegate branch creation to gh stack")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *insert {
		return fmt.Errorf("gt create --insert has no gh stack equivalent; insert a branch with `gh stack modify`")
	}

	branch, err := currentBranch()
	if err != nil {
		return err
	}
	pos, repo, err := requireNativePosition(branch)
	if err != nil {
		return err
	}

	if pos.inStack && !pos.atTop {
		return fmt.Errorf(
			"branch %q is not the top of its stack; branching here would fork the stack, which gh stack cannot represent.\n"+
				"    Run `gt top` first, or restructure with `gh stack modify`.", branch)
	}

	message := joinMessage(*msg)
	name := ""
	if fs.NArg() > 0 {
		// Everything after `gt create` is one string: `gt create fix the bug`
		// is `gt create 'fix the bug'`. Single words pass through untouched;
		// anything with spaces becomes an underscore slug.
		name = strings.Join(fs.Args(), " ")
		if strings.Contains(name, " ") {
			name = slug(name)
		}
	} else if message != "" {
		name = branchNameFrom(message, time.Now().Format("01-02"))
	} else {
		return fmt.Errorf("gt create needs a branch name or -m <message>")
	}

	// Stage first: an aborted `git add -p` should not leave a new branch behind.
	if err := stage(*all, *update, *patch); err != nil {
		return err
	}

	if *native {
		return createViaGhStack(pos, branch, name, message)
	}
	return createNative(repo, pos, branch, name, message)
}

func createViaGhStack(pos position, parent, name, message string) error {
	var err error
	if pos.inStack {
		err = run("gh", "stack", "add", name)
	} else {
		err = run("gh", "stack", "init", "--base", parent, name)
	}
	if err != nil {
		return err
	}
	invalidateGitFacts()
	return commitIfStaged(message)
}

func createNative(repo *repoStackState, pos position, parent, name, message string) error {
	if err := run("git", "checkout", "-b", name); err != nil {
		return err
	}
	invalidateGitFacts()
	if err := applyCreateToRepo(repo, parent, name, pos.inStack); err != nil {
		return err
	}
	if err := persistCreatedStack(repo); err != nil {
		return err
	}
	if err := commitIfStaged(message); err != nil {
		return err
	}
	return persistCreatedStack(repo)
}

func persistCreatedStack(repo *repoStackState) error {
	refreshStackSHAs(repo)
	return persistSyncState(repo)
}

func commitIfStaged(message string) error {
	if !hasStagedChanges() {
		return nil
	}
	commit := []string{"commit"}
	if message != "" {
		commit = append(commit, "-m", message)
	}
	if err := run("git", commit...); err != nil {
		return err
	}
	invalidateGitFacts()
	return nil
}

// cmdModify amends (or adds to) the current branch and restacks everything
// above it. gh stack has no equivalent command, so gt drives git directly.
func cmdModify(args []string) error {
	fs := newFlags("modify")
	commitNew := fs.BoolP("commit", "c", false, "create a new commit instead of amending")
	all := fs.BoolP("all", "a", false, "stage all changes before committing")
	update := fs.BoolP("update", "u", false, "stage updates to tracked files only")
	patch := fs.BoolP("patch", "p", false, "pick hunks to stage before committing")
	edit := fs.BoolP("edit", "e", false, "open an editor for the commit message")
	msg := fs.StringArrayP("message", "m", nil, "commit message")
	native := fs.Bool("native", false, "delegate the upstack rebase to gh stack")
	if err := parse(fs, args); err != nil {
		return err
	}

	branch, err := currentBranch()
	if err != nil {
		return err
	}
	repo, err := loadNativeRepo()
	if err != nil {
		return err
	}
	if hits := repo.ambiguous(branch); len(hits) > 0 {
		return errAmbiguous(branch, hits)
	}
	pos := locate(repo.asState(), branch)
	if pos.forked {
		return errForked(branch)
	}
	if !pos.inStack {
		return errNotInStack(branch)
	}

	if err := stage(*all, *update, *patch); err != nil {
		return err
	}

	// Amending a branch that carries no commits of its own would rewrite the
	// parent's commit, so fall back to a new commit exactly as gt does.
	amend := !*commitNew
	if amend {
		n, err := commitsOn(pos.parent)
		if err != nil {
			return err
		}
		if n == 0 {
			amend = false
		}
	}

	commit := []string{"commit"}
	if amend {
		commit = append(commit, "--amend")
	}
	switch message := joinMessage(*msg); {
	case message != "":
		commit = append(commit, "-m", message)
	case amend && !*edit:
		commit = append(commit, "--no-edit")
	}
	if err := run("git", commit...); err != nil {
		return err
	}
	invalidateGitFacts()

	if pos.atTop {
		return nil
	}
	if *native {
		return run("gh", "stack", "rebase", "--upstack", "--no-trunk")
	}
	return restackCurrentStack(repo, branch, false, true)
}

func errNotInStack(branch string) error {
	if trunkNames()[branch] {
		return fmt.Errorf("branch %q is not part of a stack; start one with `gt create`", branch)
	}
	return fmt.Errorf(
		"branch %q is not part of a stack.\n"+
			"    Adopt this existing branch with `gt track`.\n"+
			"    `gt create` would start a new branch on top of this one.",
		branch)
}

// cmdSubmit is the fast submit: validate locally → load remote snapshot →
// build a pure plan → no-op short-circuit → restack → one atomic push → PR
// mutations → stack object update → persist. --native delegates to the legacy
// `gh stack submit` path, but only before any mutation (decision #8).
//
// Scope (decision #13): trunk→current by default. Upstack branches prompt
// before they are included; --stack / `gt ss` skips the prompt. -u filters
// out branches without open PRs (decision #7, enforced). Clean submit -u is
// zero git push and zero gh mutations (IsNoOp short-circuit, decision #6).
// A changed submit is exactly one `git push --atomic` regardless of stack
// depth (decision #5).
func cmdSubmit(args []string) error {
	fs := newFlags("submit")
	draft := fs.BoolP("draft", "d", false, "create new PRs as drafts")
	publish := fs.BoolP("publish", "p", false, "mark PRs ready for review")
	noEdit := fs.BoolP("no-edit", "n", false, "skip the PR metadata editor (the default)")
	edit := fs.BoolP("edit", "e", false, "open the gh stack submit editor (with --native)")
	stack := fs.Bool("stack", false, "submit the whole stack")
	updateOnly := fs.BoolP("update-only", "u", false, "only update existing PRs; branches without open PRs are skipped")
	dryRun := fs.Bool("dry-run", false, "print the plan without mutating")
	always := fs.Bool("always", false, "run even when the stack is up to date")
	noVerify := fs.Bool("no-verify", false, "pass --no-verify to git push")
	native := fs.Bool("native", false, "delegate to gh stack submit (the legacy path)")
	restack := fs.Bool("restack", true, "cascade restack before pushing (default on)")
	force := fs.BoolP("force", "f", false, "force restack and push even when unchanged")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *draft && *publish {
		return fmt.Errorf("--draft and --publish conflict")
	}
	if *edit && *noEdit {
		return fmt.Errorf("--edit and --no-edit conflict")
	}
	opts := SubmitOpts{
		Stack:      *stack,
		UpdateOnly: *updateOnly,
		Always:     *always,
		Publish:    *publish,
		Draft:      *draft,
		DryRun:     *dryRun,
		Restack:    *restack,
		Force:      *force,
		NoVerify:   *noVerify,
	}

	if *native {
		return executeSubmit(opts, true, *edit, *publish, fs.Args())
	}
	return executeSubmit(opts, false, *edit, *publish, fs.Args())
}

func confirmSubmitUpstack(names []string) bool {
	listed := strings.Join(names, ", ")
	if !(isTerminal(os.Stdin) && isTerminal(os.Stderr)) {
		fmt.Fprintf(os.Stderr, "gt: not submitting upstack %s (pass --stack to include them)\n", listed)
		return false
	}
	return confirm(fmt.Sprintf("gt: also submit %d upstack branch(es) (%s)?", len(names), listed), false)
}

// submitStackBranches gathers every branch (trunk + members) of the stack
// containing current, plus a branch→PR-number map for the snapshot loader.
func submitStackBranches(repo *repoStackState, current string) ([]string, map[string]int) {
	stack, ok := findStackForBranch(repo, current)
	if !ok {
		return nil, nil
	}
	var branches []string
	knownPRs := map[string]int{}
	add := func(b trackedBranch) {
		if b.Branch == "" {
			return
		}
		branches = append(branches, b.Branch)
		if b.PullRequest != nil && b.PullRequest.Number != 0 {
			knownPRs[b.Branch] = b.PullRequest.Number
		}
	}
	add(stack.Trunk)
	for _, b := range stack.Branches {
		add(b)
	}
	return branches, knownPRs
}

// branchReflogHas reports whether sha appears among the branch's reflog tip
// values (git reflog --format=%H). A missing or unreadable reflog counts as
// no, which keeps the gate on the safe side.
func branchReflogHas(branch, sha string) bool {
	out, err := capture("git", "reflog", "--format=%H", branch)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == sha {
			return true
		}
	}
	return false
}

// gateRemoteReplace refuses pushes that would replace a remote commit the
// branch never contained: for each existing-remote push, the snapshot's
// remote SHA must be an ancestor of the local branch or present in its
// reflog (an amend or rebase of our own). Pure allowRemoteReplace decides.
// -f / --force is the explicit override and skips the gate.
func gateRemoteReplace(pushes []PushRef, force bool) error {
	if force {
		return nil
	}
	for _, p := range pushes {
		if p.IsNew {
			continue
		}
		allowed := allowRemoteReplace(p.LocalSHA, p.ExpectedOld,
			isAncestorQuiet(p.ExpectedOld, p.LocalSHA),
			branchReflogHas(p.Branch, p.ExpectedOld))
		if !allowed {
			return fmt.Errorf("refusing to push %s: origin has commit %s, which this branch never contained; fetch and rebase, or use -f to force", p.Branch, p.ExpectedOld)
		}
	}
	return nil
}

// restackForSubmit runs CascadeRestack on the current stack's branches. It
// returns moved=true when any local ref changed.
func restackForSubmit(repo *repoStackState, current string, force bool) (bool, error) {
	stack, ok := findStackForBranch(repo, current)
	if !ok {
		return false, nil
	}
	results, _, err := CascadeRestack(stack.Branches, RestackOpts{Force: force, Trunk: stack.Trunk.Branch})
	if err != nil {
		return false, err
	}
	for _, r := range results {
		if r.Moved {
			return true, nil
		}
	}
	return false, nil
}

// printSubmitPlan writes a human-readable summary of the plan to w.
func printSubmitPlan(w *os.File, plan SubmitPlan) {
	if len(plan.Pushes) > 0 {
		fmt.Fprintln(w, "push:")
		for _, p := range plan.Pushes {
			verb := "update"
			if p.IsNew {
				verb = "create"
			}
			fmt.Fprintf(w, "  %s %s (%s)\n", verb, p.Branch, shortSHA(p.LocalSHA))
		}
	}
	if len(plan.Creates) > 0 {
		fmt.Fprintln(w, "create PR:")
		for _, c := range plan.Creates {
			fmt.Fprintf(w, "  %s -> %s\n", c.Branch, c.Parent)
		}
	}
	if len(plan.BaseUpdates) > 0 {
		fmt.Fprintln(w, "update base:")
		for _, b := range plan.BaseUpdates {
			fmt.Fprintf(w, "  PR %d -> %s\n", b.PRNumber, b.NewBase)
		}
	}
	if len(plan.PublishUpdates) > 0 {
		fmt.Fprintln(w, "publish:")
		for _, p := range plan.PublishUpdates {
			fmt.Fprintf(w, "  PR %d\n", p.PRNumber)
		}
	}
	if len(plan.AutoMergeDisables) > 0 {
		fmt.Fprintln(w, "disable auto-merge:")
		for _, a := range plan.AutoMergeDisables {
			fmt.Fprintf(w, "  PR %d\n", a.PRNumber)
		}
	}
	if plan.StackUpdate != nil {
		fmt.Fprintf(w, "stack update: %s %v\n", plan.StackUpdate.StackID, plan.StackUpdate.Numbers)
	}
	if plan.IsNoOp() {
		fmt.Fprintln(w, "(no changes)")
	}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// createPRs opens one PR per PRCreate via `gh pr create`. A create failure
// aborts: the branch was already pushed, so a missing PR leaves the stack
// inconsistent and a retry of submit would re-push needlessly.
func createPRs(creates []PRCreate, draft bool) error {
	for _, c := range creates {
		title, body := prTitleAndBody(c.Branch, c.Parent)
		args := []string{"pr", "create", "--base", c.Parent, "--head", c.Branch, "--title", title, "--body", body}
		if draft {
			args = append(args, "--draft")
		}
		if err := run("gh", args...); err != nil {
			return fmt.Errorf("create PR for %s: %w", c.Branch, err)
		}
	}
	return nil
}

// prTitleAndBody builds a PR title from the first commit on the branch and a
// body from the full log, both scoped to Parent..Branch.
func prTitleAndBody(branch, parent string) (string, string) {
	log, err := capture("git", "log", "--format=%s", parent+".."+branch)
	if err != nil {
		return branch, ""
	}
	lines := strings.Split(log, "\n")
	title := branch
	if len(lines) > 0 && lines[0] != "" {
		title = lines[0]
	}
	full, err := capture("git", "log", "--format=%H%n  %s%n", parent+".."+branch)
	if err != nil {
		return title, ""
	}
	return title, full
}

// updatePRBases retargets each PR's base via `gh pr edit --base`.
func updatePRBases(updates []PRBaseUpdate) error {
	for _, u := range updates {
		if err := run("gh", "pr", "edit", fmt.Sprintf("%d", u.PRNumber), "--base", u.NewBase); err != nil {
			return fmt.Errorf("update base for PR %d: %w", u.PRNumber, err)
		}
	}
	return nil
}

// publishPRs marks each draft PR ready via `gh pr ready`.
func publishPRs(updates []PRPublishUpdate) error {
	for _, u := range updates {
		if err := run("gh", "pr", "ready", fmt.Sprintf("%d", u.PRNumber)); err != nil {
			return fmt.Errorf("publish PR %d: %w", u.PRNumber, err)
		}
	}
	return nil
}

// disableAutoMerges turns auto-merge off via `gh pr merge --disable-auto`.
func disableAutoMerges(updates []PRAutoMergeDisable) error {
	for _, u := range updates {
		if err := run("gh", "pr", "merge", fmt.Sprintf("%d", u.PRNumber), "--disable-auto"); err != nil {
			return fmt.Errorf("disable auto-merge for PR %d: %w", u.PRNumber, err)
		}
	}
	return nil
}

// nativeSubmit is the --native fallback: it delegates to the legacy
// `gh stack submit` path with the appropriate flags.
func nativeSubmit(edit, publish bool, extra []string) error {
	gh := submitArgs(edit, publish)
	gh = append(gh, extra...)
	captured, err := runTee("gh", gh...)
	if err != nil {
		explainSwallowedPush(captured)
	}
	return err
}

// explainSwallowedPush re-runs a dry-run push when gh stack reports only
// git's generic "failed to push some refs". That line hides pre-push hooks
// and --force-with-lease details.
func explainSwallowedPush(stderr string) {
	if !swallowedPushError(stderr) {
		return
	}
	branch := failedPushBranch(stderr)
	if branch == "" {
		if cur, err := currentBranch(); err == nil {
			branch = cur
		}
	}
	if branch == "" {
		return
	}
	fmt.Fprintln(os.Stderr, "trace:")
	_ = run("git", "push", "--dry-run", "--force-with-lease", "origin", branch)
}

func swallowedPushError(stderr string) bool {
	return strings.Contains(stderr, "failed to push some refs") ||
		(strings.Contains(stderr, "failed to run git") && strings.Contains(stderr, "failed to push"))
}

func failedPushBranch(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "✗"))
		if !strings.HasPrefix(line, "failed to push ") {
			continue
		}
		rest := strings.TrimPrefix(line, "failed to push ")
		if strings.HasPrefix(rest, "some refs") {
			continue
		}
		name, _, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" && !strings.ContainsAny(name, " \t") {
			return name
		}
	}
	return ""
}

// submitArgs builds the `gh stack submit` invocation. It is split out so the
// default -- skipping the editor -- is covered by a test.
func submitArgs(edit, publish bool) []string {
	gh := []string{"stack", "submit"}
	if !edit {
		gh = append(gh, "--auto")
	}
	if publish {
		gh = append(gh, "--open")
	}
	return gh
}

func cmdSync(args []string) error {
	fs := newFlags("sync")
	deleteAll := fs.BoolP("delete-all", "d", false, "delete stale stack branches without prompting")
	noRestack := fs.Bool("no-restack", false, "skip the cascade restack step")
	force := fs.BoolP("force", "f", false, "force restack even when ancestry is unchanged")
	if err := parse(fs, args); err != nil {
		return err
	}

	// Sync is repo-wide (decision #9): every stack, not just HEAD's.
	repo, err := loadNativeRepo()
	if err != nil {
		return err
	}

	branches := collectSyncBranches(repo)
	knownPRs := buildKnownPRs(repo)

	// One concurrent load: targeted ls-remote (refs) + one batched GraphQL
	// (PRs). Clean sync stops here at a single ls-remote.
	snap, snapErr := LoadRemoteSnapshot(branches, knownPRs)
	if snap == nil {
		return snapErr
	}
	if snapErr != nil {
		fmt.Fprintf(os.Stderr, "gt: could not load pull requests (%v); proceeding with refs only\n", snapErr)
	}

	groups, err := listTrackedStackGroupsFrom(repo.asState())
	if err != nil {
		return err
	}
	plan := BuildSyncPlan(repo, snap, groups, trunkNamesFrom(repo))

	// executeSyncPlan runs the whole sync flow in order: fast-forward the
	// trunk and behind branches, delete a stack only when every PR in it is
	// merged or closed, cascade-restack every
	// stack against the updated trunk, and persist the refreshed state.
	_, err = executeSyncPlan(repo, plan, snap, groups, *noRestack, *force, *deleteAll)
	return err
}

// ignorableStackSyncError reports whether a stderr from a stack rebase is
// safe to swallow: a branch not in a stack, or a worktree holding the ref.
// Kept for tests; the rewritten cmdSync no longer calls `gh stack rebase`.
func ignorableStackSyncError(stderr string) bool {
	return strings.Contains(stderr, "is not part of a stack") ||
		strings.Contains(stderr, "cannot force update the branch")
}

// collectSyncBranches gathers every branch the planner needs to know about:
// the trunk of each stack plus all members. Duplicates are removed.
func collectSyncBranches(repo *repoStackState) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	// Always load the trunk even when no stack is tracked, so a bare-trunk
	// sync can still see whether origin/main is ahead (the plan derives the
	// trunk from repo stacks, which is empty here).
	add(fallbackTrunk(trunkNamesFrom(repo)))
	for _, s := range repo.Stacks {
		add(s.Trunk.Branch)
		for _, b := range s.Branches {
			add(b.Branch)
		}
	}
	return out
}

// buildKnownPRs maps branch→PR number from repo stack metadata, so the
// snapshot loader can query GitHub for the PRs we already track.
func buildKnownPRs(repo *repoStackState) map[string]int {
	m := map[string]int{}
	for _, s := range repo.Stacks {
		if s.Trunk.PullRequest != nil && s.Trunk.PullRequest.Number != 0 {
			m[s.Trunk.Branch] = s.Trunk.PullRequest.Number
		}
		for _, b := range s.Branches {
			if b.PullRequest != nil && b.PullRequest.Number != 0 {
				m[b.Branch] = b.PullRequest.Number
			}
		}
	}
	return m
}

// executeSyncPlan runs the trunk fast-forward, per-stack branch fast-forwards,
// and cascade restacks. It returns moved=true when any local ref changed, so
// the caller can persist stack state once. No remote mutations happen here;
// a restack conflict aborts with the error before any push.
func executeSyncPlan(repo *repoStackState, plan SyncPlan, snap *RemoteSnapshot, groups [][]localBranch, noRestack bool, force bool, deleteAll bool) (bool, error) {
	moved, err := fastForwardSyncBranches(repo, plan, snap)
	if err != nil {
		return moved, err
	}

	if snap == nil || snap.PRs == nil {
		fmt.Fprintf(os.Stderr, "gt: no pull request snapshot available; leaving stacks in place\n")
	}
	pruned, err := pruneStaleBranches(plan.Stale, groups, deleteAll)
	if err != nil {
		return moved, err
	}

	// dropBranchesFromStackFiles rewrites the catalog and does not update the
	// in-memory repo. Reload once so restack and the later SHA refresh do not
	// put the deleted stack back. The clean path keeps the repo it was given.
	if pruned {
		reloaded, rerr := loadNativeRepo()
		if rerr != nil {
			return moved, rerr
		}
		repo = reloaded
	}

	// A finished stack is removed whole. A stack that still has open work keeps
	// its merged branches, so the restack runs against that full chain.
	var restacked bool
	if !noRestack {
		restacked, err = restackSyncStacks(repo, snap, force)
		if err != nil {
			return moved, err
		}
	}

	if moved || restacked || pruned {
		refreshStackSHAs(repo)
		if err := persistSyncState(repo); err != nil {
			return moved, err
		}
	}
	return moved || restacked || pruned, nil
}

// fastForwardSyncBranches fetches when anything is behind, then fast-forwards
// the trunk and any stack branches strictly behind their remote. It returns
// moved=true when a local ref changed.
func fastForwardSyncBranches(repo *repoStackState, plan SyncPlan, snap *RemoteSnapshot) (bool, error) {
	// The plan derives the trunk from repo stacks; when none is tracked it
	// cannot, so resolve the effective trunk here (executor may touch git).
	// The snapshot always carries it because collectSyncBranches adds the
	// fallback trunk.
	trunk := plan.Trunk.Branch
	if trunk == "" {
		trunk = fallbackTrunk(trunkNamesFrom(repo))
	}
	trunkBehind := false
	if trunk != "" && !plan.Trunk.FastForward {
		local, _ := branchHead(trunk)
		if snap != nil {
			if ref, ok := snap.Refs.Refs[trunk]; ok && ref.Exists && ref.RemoteSHA != "" && ref.RemoteSHA != local {
				trunkBehind = true
			}
		}
	}

	// Determine whether any branch might need a fast-forward, so we fetch
	// only when necessary (clean sync stays at one ls-remote).
	needFetch := plan.Trunk.FastForward || trunkBehind
	behindByStack := make([][]string, len(plan.Stacks))
	for i := range plan.Stacks {
		behindByStack[i] = branchesBehindRemote(plan.Stacks[i].Stack, snap)
		if len(behindByStack[i]) > 0 {
			needFetch = true
		}
	}
	if needFetch {
		if snap != nil {
			if err := fetchStackOriginFrom(snap.Refs); err != nil {
				return false, err
			}
		} else if err := fetchStackOrigin(); err != nil {
			return false, err
		}
	}

	var moved bool

	if plan.Trunk.FastForward || trunkBehind {
		if err := fastForwardBranch(trunk); err != nil {
			return moved, err
		}
		// fastForwardBranch is a no-op when the move is not a true FF, so
		// re-check the local SHA to decide whether state moved.
		if didMove(trunk, plan.Trunk.LocalSHA) {
			moved = true
		}
	}

	for i := range plan.Stacks {
		catalogHead := map[string]string{}
		for _, b := range plan.Stacks[i].Stack.Branches {
			catalogHead[b.Branch] = b.Head
		}
		for _, branch := range behindByStack[i] {
			if err := fastForwardBranch(branch); err != nil {
				return moved, err
			}
			if didMove(branch, catalogHead[branch]) {
				moved = true
			}
		}
	}
	return moved, nil
}

// restackSyncStacks cascade-restacks every tracked stack against its trunk,
// skipping stacks whose branches are all checked out in other worktrees. It
// returns moved=true when any local ref changed.
func restackSyncStacks(repo *repoStackState, snap *RemoteSnapshot, force bool) (bool, error) {
	merged := mergedForRestack(repo, snap)
	var moved bool
	for _, s := range repo.Stacks {
		if stackAllInOtherWorktrees(s.trackedStack) {
			continue
		}
		results, _, err := CascadeRestack(s.trackedStack.Branches, RestackOpts{
			Force:  force,
			Trunk:  s.trackedStack.Trunk.Branch,
			Merged: merged,
		})
		if err != nil {
			return moved, err
		}
		for _, r := range results {
			if r.Moved {
				moved = true
			}
		}
	}
	return moved, nil
}

// mergedForRestack is the set of stack branches whose pull request is merged.
// Live snapshot state wins over the cached merged flag. Those branches stay
// in the stack and are not rebased.
func mergedForRestack(repo *repoStackState, snap *RemoteSnapshot) map[string]bool {
	out := map[string]bool{}
	if repo == nil {
		return out
	}
	for _, s := range repo.Stacks {
		for _, b := range s.Branches {
			if b.Branch == "" {
				continue
			}
			if snap != nil && snap.PRs != nil {
				if pr, ok := snap.PRs[b.Branch]; ok {
					if pr.Merged || strings.EqualFold(pr.State, "MERGED") {
						out[b.Branch] = true
					}
					continue
				}
			}
			if b.PullRequest != nil && b.PullRequest.Merged {
				out[b.Branch] = true
			}
		}
	}
	return out
}

// refreshStackSHAs updates the cached head/base SHAs in the repo state from
// the live refs, so the persisted gh-stack state matches the repository after
// sync moved things. base records the parent's head (the trunk head for the
// bottom branch), matching what gh-stack itself writes. Best effort: a
// missing ref leaves its cached value alone.
func refreshStackSHAs(repo *repoStackState) {
	if repo == nil {
		return
	}
	// One facts load, then map lookups. The trunk tip is reused as the
	// bottom branch's base. Each later base is the previous branch's tip,
	// already read above.
	_, _ = gitFacts()
	for i := range repo.Stacks {
		s := &repo.Stacks[i].trackedStack
		trunkSHA, trunkOK := "", false
		if s.Trunk.Branch != "" {
			if h, err := branchHead(s.Trunk.Branch); err == nil {
				s.Trunk.Head = h
				trunkSHA, trunkOK = h, true
			}
		}
		var prevSHA string
		var prevOK bool
		for j := range s.Branches {
			if j == 0 {
				if trunkOK {
					s.Branches[j].Base = trunkSHA
				}
			} else if prevOK {
				s.Branches[j].Base = prevSHA
			}
			prevSHA, prevOK = "", false
			if s.Branches[j].Branch == "" {
				continue
			}
			if h, err := branchHead(s.Branches[j].Branch); err == nil {
				s.Branches[j].Head = h
				prevSHA, prevOK = h, true
			}
		}
	}
}

// branchesBehindRemote returns the members of a stack whose tracked Head
// differs from the remote SHA in the snapshot. The snapshot comes from
// LoadRemoteSnapshot's targeted ls-remote, so this is network-free.
func branchesBehindRemote(stack trackedStack, snap *RemoteSnapshot) []string {
	if snap == nil {
		return nil
	}
	var behind []string
	for _, b := range stack.Branches {
		if b.Branch == "" {
			continue
		}
		ref, ok := snap.Refs.Refs[b.Branch]
		if !ok || !ref.Exists {
			continue
		}
		if b.Head != "" && b.Head != ref.RemoteSHA {
			behind = append(behind, b.Branch)
		}
	}
	return behind
}

// stackAllInOtherWorktrees reports whether every member of the stack is
// checked out in a worktree other than this one. Such a stack cannot be
// restacked from here and is skipped.
func stackAllInOtherWorktrees(stack trackedStack) bool {
	cur, err := currentBranch()
	if err != nil {
		return false
	}
	anyLocal := false
	for _, b := range stack.Branches {
		if b.Branch == "" {
			continue
		}
		wt, err := worktreePathForBranch(b.Branch)
		if err != nil || wt == "" || b.Branch == cur {
			anyLocal = true
			break
		}
	}
	return !anyLocal
}

// didMove reports whether the branch's current SHA differs from before.
func didMove(branch, oldSHA string) bool {
	now, err := branchHead(branch)
	if err != nil {
		return false
	}
	return now != oldSHA
}

// persistSyncState writes the reconciled repo state back to the shared
// gh-stack catalog. It is called once at the end of sync when a local ref moved.
func persistSyncState(repo *repoStackState) error {
	dir, err := gitStackDir()
	if err != nil {
		return err
	}
	return writeStackFile(filepath.Join(dir, ghStackCompat.StateFileName), repo.asState())
}

func restackCurrentStack(repo *repoStackState, branch string, down, up bool) error {
	stack, err := stackForRestack(repo, branch)
	if err != nil {
		return err
	}
	scoped, trunk, err := restackScope(stack, branch, down, up)
	if err != nil {
		if !down && !up && stack.Trunk.Branch == branch {
			scoped, trunk = stack.Branches, stack.Trunk.Branch
		} else {
			return err
		}
	}
	results, _, err := CascadeRestack(scoped, RestackOpts{
		Trunk:           trunk,
		PauseOnConflict: true,
	})
	if err != nil {
		return err
	}
	return persistAfterRestack(repo, results)
}

func stackForRestack(repo *repoStackState, branch string) (trackedStack, error) {
	if repo == nil {
		return trackedStack{}, errNotInStack(branch)
	}
	st := repo.asState()
	if stack, ok := stackContaining(st, branch); ok {
		return stack, nil
	}
	var found []trackedStack
	for _, s := range st.Stacks {
		if s.Trunk.Branch == branch {
			found = append(found, s)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return trackedStack{}, errNotInStack(branch)
}

func persistAfterRestack(repo *repoStackState, results []RestackResult) error {
	for _, r := range results {
		if !r.Moved {
			continue
		}
		refreshStackSHAs(repo)
		return persistSyncState(repo)
	}
	return nil
}

func cmdRestack(args []string) error {
	fs := newFlags("restack")
	downstack := fs.BoolP("downstack", "d", false, "restack this branch and its ancestors")
	upstack := fs.BoolP("upstack", "u", false, "restack this branch and its descendants")
	only := fs.BoolP("only", "o", false, "restack this branch only")
	native := fs.Bool("native", false, "delegate to gh stack rebase")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *only {
		return fmt.Errorf("gt restack --only has no gh stack equivalent; rebase the single branch with git")
	}
	if *downstack && *upstack {
		return fmt.Errorf("--downstack and --upstack conflict")
	}
	if *native {
		gh := []string{"stack", "rebase"}
		if *downstack {
			gh = append(gh, "--downstack")
		}
		if *upstack {
			gh = append(gh, "--upstack")
		}
		return run("gh", gh...)
	}
	branch, err := currentBranch()
	if err != nil {
		return err
	}
	repo, err := loadNativeRepo()
	if err != nil {
		return err
	}
	if hits := repo.ambiguous(branch); len(hits) > 0 {
		return errAmbiguous(branch, hits)
	}
	return restackCurrentStack(repo, branch, *downstack, *upstack)
}

func cmdContinue(args []string) error { return resume("--continue", args) }
func cmdAbort(args []string) error    { return resume("--abort", args) }

func resume(flag string, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q", args[0])
	}
	if pausedGitRebase() {
		if flag == "--continue" && os.Getenv("GIT_EDITOR") == "" {
			_ = os.Setenv("GIT_EDITOR", "true")
		}
		return run("git", "rebase", flag)
	}
	op, err := pausedOperation()
	if err != nil {
		return err
	}
	if op == "" {
		return fmt.Errorf("no rebase is in progress")
	}
	return run("gh", "stack", op, flag)
}

func cmdCheckout(args []string) error {
	fs := newFlags("checkout [branch]")
	trunk := fs.BoolP("trunk", "t", false, "check out the trunk")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *trunk {
		return run("gh", "stack", "trunk")
	}
	if fs.NArg() == 0 {
		return checkoutInteractive()
	}
	return checkoutTarget(fs.Arg(0))
}

// checkoutInteractive is Graphite's bare `gt checkout`: a bottom-up tree of
// every locally tracked stack (this worktree and the others). `gh stack switch`
// only lists the current stack; the last row opens `gh stack checkout` for
// stacks that exist only on GitHub.
func checkoutInteractive() error {
	if isTerminal(os.Stdin) && isTerminal(os.Stderr) {
		st, err := loadForestState()
		if err != nil {
			return err
		}
		current, err := currentBranch()
		if err != nil {
			return err
		}
		rows := checkoutRows(st, current)
		rows = append(rows, githubStacksRow())
		chosen, err := pickBranch(rows)
		if err != nil {
			return err
		}
		if chosen.openGithub {
			return run("gh", "stack", "checkout")
		}
		return checkoutTarget(chosen.branch)
	}
	return run("gh", "stack", "checkout")
}

// checkoutTarget sends a named branch to `gh stack checkout`, except for the
// trunk and untracked local branches: a bare name is tried as a stack number
// then a PR, so those would land you on the wrong branch.
func checkoutTarget(target string) error {
	if isLocalBranch(target) {
		pos, _, err := requireStackPosition(target)
		if err != nil {
			return err
		}
		if !pos.inStack {
			if err := run("git", "checkout", target); err != nil {
				return err
			}
			invalidateGitFacts()
			return nil
		}
	}
	return run("gh", "stack", "checkout", target)
}

func cmdGet(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("gt get needs a branch, PR number, or PR URL; to refresh the current stack run `gt sync`")
	}
	return run("gh", "stack", "checkout", args[0])
}

func cmdPR(args []string) error {
	return run("gh", append([]string{"pr", "view", "--web"}, args...)...)
}

// cmdTrack adopts existing Git branches into a new stack. Graphite's
// per-branch track writes them into the schema v1 catalog (or the current
// branch). It does not create a new branch; that is `gt create`. --native
// keeps today's `gh stack init`.
func cmdTrack(args []string) error {
	fs := newFlags("track [branches...]")
	base := fs.StringP("base", "b", "", "trunk for the new stack")
	parent := fs.StringP("parent", "p", "", "Graphite parent branch")
	native := fs.Bool("native", false, "delegate adoption to gh stack")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *parent != "" {
		return fmt.Errorf("gt track --parent has no gh stack equivalent; pass --base <trunk> and list branches bottom to top")
	}
	repo, err := loadNativeRepo()
	if err != nil {
		return err
	}
	trunks := trunkNamesFrom(repo)
	trunk := *base
	if trunk == "" {
		trunk = fallbackTrunk(trunks)
	}
	branches := fs.Args()
	if len(branches) == 0 {
		cur, err := currentBranch()
		if err != nil {
			return err
		}
		if trunks[cur] {
			return fmt.Errorf("on trunk %q; start a stacked branch with `gt create`, or pass names: `gt track a b`", cur)
		}
		if err := refuseAlreadyTracked(repo, cur); err != nil {
			return err
		}
		branches = []string{cur}
	} else {
		for _, name := range branches {
			if trunks[name] {
				return fmt.Errorf("%q is a trunk; omit it and pass --base %s", name, name)
			}
			if err := refuseAlreadyTracked(repo, name); err != nil {
				return err
			}
		}
	}
	if *native {
		gh := append([]string{"stack", "init", "--base", trunk}, branches...)
		return run("gh", gh...)
	}
	if err := applyTrackToRepo(repo, trunk, branches); err != nil {
		return err
	}
	return persistSyncState(repo)
}

func refuseAlreadyTracked(repo *repoStackState, name string) error {
	if hits := repo.ambiguous(name); len(hits) > 0 {
		return errAmbiguous(name, hits)
	}
	pos := locate(repo.asState(), name)
	if pos.forked {
		return errForked(name)
	}
	if pos.inStack {
		return fmt.Errorf("branch %q is already in a stack", name)
	}
	return nil
}

func cmdInit(args []string) error {
	return fmt.Errorf(
		"gh stack has no repository-level init; a stack is created when you branch.\n" +
			"    Run `gt create <name>` on your trunk to start one, or `gt track` to adopt existing branches.")
}
