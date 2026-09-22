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

// cmdCreate branches off the current position. On the top branch of a stack
// that is `gh stack add`; off a branch in no stack (the trunk, usually) it is
// `gh stack init`, which starts a second stack rather than appending to the
// first. From the middle of a stack it refuses: that would be a fork.
func cmdCreate(args []string) error {
	fs := newFlags("create [name]")
	all := fs.BoolP("all", "a", false, "stage all changes, including untracked files")
	update := fs.BoolP("update", "u", false, "stage updates to tracked files only")
	patch := fs.BoolP("patch", "p", false, "pick hunks to stage before committing")
	insert := fs.BoolP("insert", "i", false, "insert between the current branch and its child")
	msg := fs.StringArrayP("message", "m", nil, "commit message")
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
	pos, _, err := requireStackPosition(branch)
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
		name = fs.Arg(0)
	} else if message != "" {
		name = branchNameFrom(message, time.Now().Format("01-02"))
	} else {
		return fmt.Errorf("gt create needs a branch name or -m <message>")
	}

	// Stage first: an aborted `git add -p` should not leave a new branch behind.
	if err := stage(*all, *update, *patch); err != nil {
		return err
	}

	// gh stack only creates and checks out the branch here. Its own -A/-u/-m
	// staging is deliberately unused: on a parent that carries no commits yet
	// it puts the commit on the parent instead of creating the new branch.
	if pos.inStack {
		err = run("gh", "stack", "add", name)
	} else {
		err = run("gh", "stack", "init", "--base", branch, name)
	}
	if err != nil {
		return err
	}

	if !hasStagedChanges() {
		return nil
	}
	commit := []string{"commit"}
	if message != "" {
		commit = append(commit, "-m", message)
	}
	return run("git", commit...)
}

// cmdModify amends (or adds to) the current branch and restacks everything
// above it. gh stack has no equivalent command, so gt drives git directly and
// then asks gh stack to cascade the rebase.
func cmdModify(args []string) error {
	fs := newFlags("modify")
	commitNew := fs.BoolP("commit", "c", false, "create a new commit instead of amending")
	all := fs.BoolP("all", "a", false, "stage all changes before committing")
	update := fs.BoolP("update", "u", false, "stage updates to tracked files only")
	patch := fs.BoolP("patch", "p", false, "pick hunks to stage before committing")
	edit := fs.BoolP("edit", "e", false, "open an editor for the commit message")
	msg := fs.StringArrayP("message", "m", nil, "commit message")
	if err := parse(fs, args); err != nil {
		return err
	}

	branch, err := currentBranch()
	if err != nil {
		return err
	}
	pos, _, err := requireStackPosition(branch)
	if err != nil {
		return err
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

	if pos.atTop {
		return nil
	}
	return run("gh", "stack", "rebase", "--upstack", "--no-trunk")
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

	repo, err := loadRepoStacks()
	if err != nil {
		return err
	}
	current, err := currentBranch()
	if err != nil {
		return err
	}

	// Pre-mutation validation (decision #10). --native may still proceed
	// here, before any mutation has happened (decision #8); otherwise surface
	// the error and a hint pointing at the fallback.
	if verr := validateStackForMutation(repo, current); verr != nil {
		if *native {
			return nativeSubmit(*edit, *publish, fs.Args())
		}
		fmt.Fprintln(os.Stderr, verr)
		fmt.Fprintln(os.Stderr, "gt: hint — re-run with --native to delegate to gh stack submit")
		return verr
	}
	if *native {
		return nativeSubmit(*edit, *publish, fs.Args())
	}

	if !opts.Stack {
		if stack, ok := findStackForBranch(repo, current); ok {
			if above := upstackOf(stack.trackedStack, current); len(above) > 0 {
				if confirmSubmitUpstack(above) {
					opts.Stack = true
				}
			}
		}
	}

	// Gather branches + knownPRs across the current stack. The snapshot
	// covers the whole stack so any scope (trunk→current or --stack) is
	// satisfied; ResolveSubmitScope narrows the plan.
	branches, knownPRs := submitStackBranches(repo, current)
	snap, snapErr := LoadRemoteSnapshot(branches, knownPRs)
	if snap == nil {
		return snapErr
	}
	if snapErr != nil {
		fmt.Fprintf(os.Stderr, "gt: could not load pull requests (%v); proceeding with refs only\n", snapErr)
	}

	// Live SHAs for the plan: gh stack init / add leave the cached head
	// empty, so both the plan and the PR head SHAs come from git rev-parse.
	refreshStackSHAs(repo)
	plan, err := BuildSubmitPlan(repo, current, snap, opts)
	if err != nil {
		return err
	}

	// No-op fast path (decision #6): zero pushes, zero mutations.
	if plan.IsNoOp() && !opts.Always {
		fmt.Fprintln(os.Stderr, "Stack already up to date")
		return nil
	}

	if opts.DryRun {
		printSubmitPlan(os.Stderr, plan)
		return nil
	}

	mut := MutationState{}

	// Restack before any network I/O (decision #4). Local refs move here; the
	// remote lease ExpectedOld stays from the snapshot.
	if opts.Restack {
		moved, rerr := restackForSubmit(repo, current, opts.Force)
		if rerr != nil {
			return rerr
		}
		if moved {
			mut.GitMutated = true
			for i := range plan.Pushes {
				sha, err := branchHead(plan.Pushes[i].Branch)
				if err != nil {
					return err
				}
				plan.Pushes[i].LocalSHA = sha
			}
		}
	}

	// Submit-safety gate: refuse a remote commit this branch never had
	// before any push or PR create, and leave that commit in place. No
	// fetch and rebase; -f is the explicit override.
	if gerr := gateRemoteReplace(plan.Pushes, opts.Force); gerr != nil {
		return gerr
	}

	// One atomic push (decision #5). No --force fallback on lease failure.
	if len(plan.Pushes) > 0 {
		if perr := AtomicPush(plan.Pushes, PushOpts{
			Force:    opts.Force,
			NoVerify: opts.NoVerify,
			DryRun:   opts.DryRun,
		}); perr != nil {
			explainSwallowedPush("")
			return perr
		}
		mut.GitMutated = true
	}

	// PR mutations via gh subprocess. Create errors abort (state would be
	// inconsistent after a push with no PR); base/publish/automerge failures
	// are warnings and continue.
	if cerr := createPRs(plan.Creates, opts.Draft); cerr != nil {
		return cerr
	}
	if len(plan.Creates) > 0 {
		mut.RemoteMutated = true
	}
	if werr := updatePRBases(plan.BaseUpdates); werr != nil {
		fmt.Fprintf(os.Stderr, "gt: warning: %v\n", werr)
	} else if len(plan.BaseUpdates) > 0 {
		mut.RemoteMutated = true
	}
	if werr := publishPRs(plan.PublishUpdates); werr != nil {
		fmt.Fprintf(os.Stderr, "gt: warning: %v\n", werr)
	} else if len(plan.PublishUpdates) > 0 {
		mut.RemoteMutated = true
	}
	if werr := disableAutoMerges(plan.AutoMergeDisables); werr != nil {
		fmt.Fprintf(os.Stderr, "gt: warning: %v\n", werr)
	} else if len(plan.AutoMergeDisables) > 0 {
		mut.RemoteMutated = true
	}

	// Stack object update. Warn on failure; the push already succeeded.
	if plan.StackUpdate != nil {
		client, cerr := NewStackRemoteClient()
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "gt: warning: stack remote: %v\n", cerr)
		} else {
			changed, serr := syncStackOrder(client, plan.StackUpdate.StackID, plan.StackUpdate.Numbers)
			if serr != nil {
				fmt.Fprintf(os.Stderr, "gt: warning: stack update: %v\n", serr)
			} else if changed {
				mut.RemoteMutated = true
			}
		}
	}

	// Persist local stack state once if restack moved refs.
	if mut.GitMutated {
		refreshStackSHAs(repo)
		if perr := persistSyncState(repo); perr != nil {
			return perr
		}
	}

	return nil
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
	repo, err := loadRepoStacks()
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

	plan := BuildSyncPlan(repo, snap)

	// executeSyncPlan runs the whole sync flow in order: fast-forward the
	// trunk and behind branches, prune merged/closed/gone stack branches
	// (rebasing what was above them onto the trunk), cascade-restack every
	// stack against the updated trunk, and persist the refreshed state.
	_, err = executeSyncPlan(plan, snap, *noRestack, *force, *deleteAll)
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
	add(fallbackTrunk(trunkNames()))
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
func executeSyncPlan(plan SyncPlan, snap *RemoteSnapshot, noRestack bool, force bool, deleteAll bool) (bool, error) {
	moved, err := fastForwardSyncBranches(plan, snap)
	if err != nil {
		return moved, err
	}

	pruned, err := pruneStaleBranches(snap, deleteAll)
	if err != nil {
		return moved, err
	}

	// Prune rewrites the stack chains (it deletes merged branches and rebases
	// what was above them onto the trunk), so the restack must run against
	// the updated chains, not the pre-prune plan.
	var restacked bool
	if !noRestack {
		repo, err := loadRepoStacks()
		if err != nil {
			return moved, err
		}
		restacked, err = restackSyncStacks(repo, force)
		if err != nil {
			return moved, err
		}
	}

	if moved || restacked || pruned {
		repo, err := loadRepoStacks()
		if err != nil {
			return moved, err
		}
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
func fastForwardSyncBranches(plan SyncPlan, snap *RemoteSnapshot) (bool, error) {
	// The plan derives the trunk from repo stacks; when none is tracked it
	// cannot, so resolve the effective trunk here (executor may touch git).
	// The snapshot always carries it because collectSyncBranches adds the
	// fallback trunk.
	trunk := plan.Trunk.Branch
	if trunk == "" {
		trunk = fallbackTrunk(trunkNames())
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
		if err := fetchStackOrigin(); err != nil {
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
		for _, branch := range behindByStack[i] {
			if err := fastForwardBranch(branch); err != nil {
				return moved, err
			}
		}
	}
	return moved, nil
}

// restackSyncStacks cascade-restacks every tracked stack against its trunk,
// skipping stacks whose branches are all checked out in other worktrees. It
// returns moved=true when any local ref changed.
func restackSyncStacks(repo *repoStackState, force bool) (bool, error) {
	var moved bool
	for _, s := range repo.Stacks {
		if stackAllInOtherWorktrees(s.trackedStack) {
			continue
		}
		results, _, err := CascadeRestack(s.trackedStack.Branches, RestackOpts{
			Force: force,
			Trunk: s.trackedStack.Trunk.Branch,
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

// refreshStackSHAs updates the cached head/base SHAs in the repo state from
// the live refs, so the persisted gh-stack state matches the repository after
// sync moved things. base records the parent's head (the trunk head for the
// bottom branch), matching what gh-stack itself writes. Best effort: a
// missing ref leaves its cached value alone.
func refreshStackSHAs(repo *repoStackState) {
	for i := range repo.Stacks {
		s := &repo.Stacks[i].trackedStack
		if h, err := branchHead(s.Trunk.Branch); err == nil {
			s.Trunk.Head = h
		}
		for j := range s.Branches {
			parent := s.Trunk.Branch
			if j > 0 {
				parent = s.Branches[j-1].Branch
			}
			if h, err := branchHead(s.Branches[j].Branch); err == nil {
				s.Branches[j].Head = h
			}
			if h, err := branchHead(parent); err == nil {
				s.Branches[j].Base = h
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
	now, err := capture("git", "rev-parse", branch)
	if err != nil {
		return false
	}
	return now != oldSHA
}

// persistSyncState writes the reconciled repo state back to this worktree's
// gh-stack file. It is called once at the end of sync when a local ref moved.
func persistSyncState(repo *repoStackState) error {
	dir, err := gitStackDir()
	if err != nil {
		return err
	}
	return writeStackFile(filepath.Join(dir, ghStackCompat.StateFileName), repo.asState())
}

func cmdRestack(args []string) error {
	fs := newFlags("restack")
	downstack := fs.BoolP("downstack", "d", false, "restack this branch and its ancestors")
	upstack := fs.BoolP("upstack", "u", false, "restack this branch and its descendants")
	only := fs.BoolP("only", "o", false, "restack this branch only")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *only {
		return fmt.Errorf("gt restack --only has no gh stack equivalent; rebase the single branch with git")
	}
	if *downstack && *upstack {
		return fmt.Errorf("--downstack and --upstack conflict")
	}
	gh := []string{"stack", "rebase"}
	if *downstack {
		gh = append(gh, "--downstack")
	}
	if *upstack {
		gh = append(gh, "--upstack")
	}
	return run("gh", gh...)
}

func cmdContinue(args []string) error { return resume("--continue", args) }
func cmdAbort(args []string) error    { return resume("--abort", args) }

func resume(flag string, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q", args[0])
	}
	op, err := pausedOperation()
	if err != nil {
		return err
	}
	if op == "" {
		return fmt.Errorf("no gh stack rebase or modify is in progress")
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
			return run("git", "checkout", target)
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

func cmdLog(args []string) error {
	form := ""
	if len(args) > 0 && (args[0] == "short" || args[0] == "long") {
		form, args = args[0], args[1:]
	}
	if form == "long" {
		return run("git", append([]string{"log", "--graph", "--oneline", "--decorate", "--all"}, args...)...)
	}
	gh := []string{"stack", "view"}
	if form == "short" {
		gh = append(gh, "--short")
	}
	return run("gh", append(gh, args...)...)
}

func cmdPR(args []string) error {
	return run("gh", append([]string{"pr", "view", "--web"}, args...)...)
}

// cmdTrack adopts existing Git branches into a new stack. Graphite's
// per-branch track becomes `gh stack init` of the listed branches (or the
// current one). It does not create a new branch; that is `gt create`.
func cmdTrack(args []string) error {
	fs := newFlags("track [branches...]")
	base := fs.StringP("base", "b", "", "trunk for the new stack")
	parent := fs.StringP("parent", "p", "", "Graphite parent branch")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *parent != "" {
		return fmt.Errorf("gt track --parent has no gh stack equivalent; pass --base <trunk> and list branches bottom to top")
	}
	trunk := *base
	if trunk == "" {
		trunk = fallbackTrunk(trunkNames())
	}
	branches := fs.Args()
	if len(branches) == 0 {
		cur, err := currentBranch()
		if err != nil {
			return err
		}
		if trunkNames()[cur] {
			return fmt.Errorf("on trunk %q; start a stacked branch with `gt create`, or pass names: `gt track a b`", cur)
		}
		pos, _, err := requireStackPosition(cur)
		if err != nil {
			return err
		}
		if pos.inStack {
			return fmt.Errorf("branch %q is already in a stack", cur)
		}
		branches = []string{cur}
	} else {
		for _, name := range branches {
			if trunkNames()[name] {
				return fmt.Errorf("%q is a trunk; omit it and pass --base %s", name, name)
			}
			pos, _, err := requireStackPosition(name)
			if err != nil {
				return err
			}
			if pos.inStack {
				return fmt.Errorf("branch %q is already in a stack", name)
			}
		}
	}
	gh := append([]string{"stack", "init", "--base", trunk}, branches...)
	return run("gh", gh...)
}

func cmdInit(args []string) error {
	return fmt.Errorf(
		"gh stack has no repository-level init; a stack is created when you branch.\n" +
			"    Run `gt create <name>` on your trunk to start one, or `gt track` to adopt existing branches.")
}
