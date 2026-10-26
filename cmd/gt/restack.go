package main

import "fmt"

// restackAction is the pure planner's decision for one branch: whether to
// rebase it onto Parent, or leave it alone with a SkipReason.
type restackAction struct {
	Branch     string
	Parent     string
	Rebase     bool
	SkipReason string
}

// planRestack decides per branch whether a rebase is needed, purely from
// precomputed ancestry and worktree info. It does no I/O.
//
//   - heads     maps branch→current SHA (used only for presence; the executor
//     captures OldSHA from here).
//   - parents   maps branch→parent ref (trunk SHA for stack[0], previous
//     branch otherwise).
//   - ancestor  maps branch→true when parent is already an ancestor of branch
//     (no rebase needed unless Force).
//   - worktree  maps branch→worktree path when checked out somewhere.
//   - currentBranch is the branch in this worktree; a branch equal to it is
//     not treated as "in another worktree".
func planRestack(
	stack []trackedBranch,
	heads map[string]string,
	parents map[string]string,
	ancestor map[string]bool,
	worktree map[string]string,
	currentBranch string,
	opts RestackOpts,
) []restackAction {
	actions := make([]restackAction, 0, len(stack))
	for _, b := range stack {
		branch := b.Branch
		parent := parents[branch]
		act := restackAction{Branch: branch, Parent: parent}

		if opts.Merged[branch] {
			act.SkipReason = "pull request is merged"
			actions = append(actions, act)
			continue
		}

		if wt := worktree[branch]; wt != "" && branch != currentBranch {
			act.SkipReason = "checked out in another worktree: " + wt
			actions = append(actions, act)
			continue
		}

		if ancestor[branch] && !opts.Force {
			actions = append(actions, act)
			continue
		}

		act.Rebase = true
		actions = append(actions, act)
	}
	return actions
}

// stackParents maps each branch to the ref it must sit on: the previous
// branch in the stack, or — for the bottom branch — the trunk. The trunk
// wins over the branch's cached Base: that Base is the trunk SHA recorded
// when the stack was written, which stays an ancestor of the branch even
// after the trunk has moved, so rebasing onto it can never pull in new
// trunk commits. An empty trunk falls back to the cached Base.
func stackParents(stack []trackedBranch, trunk string) map[string]string {
	parents := make(map[string]string, len(stack))
	for i, b := range stack {
		if i == 0 {
			parents[b.Branch] = trunk
			if parents[b.Branch] == "" {
				parents[b.Branch] = b.Base
			}
		} else {
			parents[b.Branch] = stack[i-1].Branch
		}
	}
	return parents
}

// restackScope is the slice CascadeRestack should run. down includes this
// branch and its ancestors (onto the trunk). up includes this branch and
// its descendants; the parent of the current branch is passed as trunk so
// the bottom of that slice still sits on the live parent, not a cached SHA.
func restackScope(stack trackedStack, current string, down, up bool) ([]trackedBranch, string, error) {
	if !down && !up {
		return stack.Branches, stack.Trunk.Branch, nil
	}
	idx := -1
	for i, b := range stack.Branches {
		if b.Branch == current {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, "", fmt.Errorf("branch %q is not a member of this stack", current)
	}
	if down && up {
		return nil, "", fmt.Errorf("--downstack and --upstack conflict")
	}
	if down {
		return stack.Branches[:idx+1], stack.Trunk.Branch, nil
	}
	trunk := stack.Trunk.Branch
	if idx > 0 {
		trunk = stack.Branches[idx-1].Branch
	}
	return stack.Branches[idx:], trunk, nil
}

// restackGit is the git seam CascadeRestack uses. Tests pass a fake.
// Production funcs read GitFacts and run git rebase.
type restackGit struct {
	current  func() (string, error)
	head     func(name string) (string, error)
	worktree func(name string) (string, error)
	ancestor func(parent, child string) bool
	rebase   func(parent, oldBase, branch string, interactive bool) error
	noteHead func(branch, sha string)
}

func realRestackGit() restackGit {
	return restackGit{
		current:  currentBranch,
		head:     branchHead,
		worktree: worktreePathForBranch,
		ancestor: isAncestor,
		rebase:   gitRebaseBranch,
		noteHead: noteHead,
	}
}

// gitRebaseBranch drops the snapshot before git moves refs, so a failed
// rebase is not observed as the old tips. On success the pre-rebase snapshot
// is put back and only this branch's SHA is replaced. That keeps the cascade
// from running for-each-ref once per branch.
func gitRebaseBranch(parent, oldBase, branch string, interactive bool) error {
	saved := cachedFacts
	invalidateGitFacts()
	args := []string{"rebase"}
	if interactive {
		args = append(args, "--interactive")
	}
	if oldBase != "" {
		args = append(args, "--onto", parent, oldBase, branch)
	} else {
		args = append(args, parent, branch)
	}
	if err := run("git", args...); err != nil {
		return err
	}
	sha, err := capture("git", "rev-parse", "--verify", "--quiet", branch)
	if err != nil {
		return err
	}
	restoreFactsAfterRebase(saved, branch, sha)
	return nil
}

// restoreFactsAfterRebase puts the pre-rebase snapshot back, then records the
// one ref git just moved. git rebase <parent> <branch> leaves HEAD on branch,
// so the cached current branch has to follow or the next stack in this
// process treats that checkout as a foreign worktree.
func restoreFactsAfterRebase(saved GitFacts, branch, sha string) {
	cachedFacts = saved
	if !cachedFacts.ready {
		return
	}
	noteHead(branch, sha)
	cachedFacts.current = branch
}

// CascadeRestack rebases each branch onto its parent in stack order, returning
// one RestackResult per branch and a Rollback usable to recover local refs on
// conflict. Zero GitHub API calls happen here. Branches checked out in other
// worktrees are skipped.
func CascadeRestack(stack []trackedBranch, opts RestackOpts) ([]RestackResult, *Rollback, error) {
	return cascadeRestack(stack, opts, realRestackGit())
}

func cascadeRestack(stack []trackedBranch, opts RestackOpts, g restackGit) ([]RestackResult, *Rollback, error) {
	if !cachedFacts.ready {
		_, _ = gitFacts()
	}

	var head string
	var err error
	if cachedFacts.ready && cachedFacts.current != "" {
		head = cachedFacts.current
	} else if g.current != nil {
		head, err = g.current()
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("HEAD is detached; check out a branch first")
	}

	rb := &Rollback{
		OriginalHEAD: head,
		OriginalRefs: make(map[string]string, len(stack)),
	}
	heads := make(map[string]string, len(stack))
	for _, b := range stack {
		sha, herr := restackHead(g, b.Branch)
		if herr != nil {
			return nil, nil, herr
		}
		rb.OriginalRefs[b.Branch] = sha
		heads[b.Branch] = sha
	}

	parents := stackParents(stack, opts.Trunk)
	worktree := make(map[string]string, len(stack))
	if cachedFacts.ready {
		for _, b := range stack {
			worktree[b.Branch] = cachedFacts.worktrees[b.Branch]
		}
	} else {
		for _, b := range stack {
			wt, werr := g.worktree(b.Branch)
			if werr != nil {
				return nil, nil, werr
			}
			worktree[b.Branch] = wt
		}
	}

	// The setup ancestry pass is dead. planRestack still applies worktree
	// and merged skips. The execute loop decides whether to rebase.
	actions := planRestack(stack, heads, parents, map[string]bool{}, worktree, head, opts)

	bases := make(map[string]string, len(stack))
	for _, b := range stack {
		bases[b.Branch] = b.Base
	}

	results := make([]RestackResult, 0, len(stack))
	for _, act := range actions {
		res := RestackResult{Branch: act.Branch, OldSHA: heads[act.Branch]}

		if act.SkipReason != "" {
			res.Skipped = true
			res.NewSHA = res.OldSHA
			results = append(results, res)
			continue
		}

		// Catalog Base is the parent SHA from the last write. When it still
		// equals the parent's live tip, merge-base would only confirm that.
		// A raw-SHA parent misses facts.heads, and a parent noteHead just
		// moved disagrees with the child's cached Base, so those ask
		// merge-base once. Force rebases without asking.
		rebase := act.Rebase
		if !opts.Force {
			if catalogBaseMatches(bases[act.Branch], act.Parent) {
				rebase = false
			} else {
				rebase = !g.ancestor(act.Parent, act.Branch)
			}
		}

		if !rebase {
			res.NewSHA = res.OldSHA
			results = append(results, res)
			continue
		}

		if err := g.rebase(act.Parent, bases[act.Branch], act.Branch, opts.Interactive); err != nil {
			if opts.PauseOnConflict {
				return results, rb, fmt.Errorf("rebase %s onto %s failed: %w\n"+
					"    resolve the conflict, then `gt continue` (or `gt abort`)",
					act.Branch, act.Parent, err)
			}
			if rbErr := applyRollback(*rb); rbErr != nil {
				return results, rb, fmt.Errorf("rebase %s onto %s failed: %w (rollback also failed: %v)",
					act.Branch, act.Parent, err, rbErr)
			}
			return results, rb, fmt.Errorf("rebase %s onto %s failed: %w\n"+
				"    local refs were rolled back (no remote changes were made)\n"+
				"    resolve with `git rebase %s %s` (or `gt restack`), then re-run",
				act.Branch, act.Parent, err, act.Parent, act.Branch)
		}

		newSHA, err := g.head(act.Branch)
		if err != nil {
			return results, rb, err
		}
		if g.noteHead != nil {
			g.noteHead(act.Branch, newSHA)
		}
		res.NewSHA = newSHA
		res.Moved = res.OldSHA != res.NewSHA
		results = append(results, res)
	}

	return results, rb, nil
}

func restackHead(g restackGit, name string) (string, error) {
	if cachedFacts.ready {
		if sha, ok := cachedFacts.heads[name]; ok {
			return sha, nil
		}
	}
	if g.head == nil {
		return "", fmt.Errorf("missing head for %s", name)
	}
	return g.head(name)
}

func catalogBaseMatches(base, parent string) bool {
	if base == "" || !cachedFacts.ready {
		return false
	}
	live, ok := cachedFacts.heads[parent]
	return ok && live == base
}

// rebaseDescendantsOnto drops oldBaseSHA from each descendant by rebasing
// onto the rewritten parent, bottom to top. On conflict it restores the
// descendant refs and HEAD.
func rebaseDescendantsOnto(descendants []trackedBranch, onto, oldBaseSHA string) error {
	if len(descendants) == 0 {
		return nil
	}
	head, err := currentBranch()
	if err != nil {
		return err
	}
	rb := Rollback{OriginalHEAD: head, OriginalRefs: make(map[string]string, len(descendants))}
	for _, b := range descendants {
		sha, err := branchHead(b.Branch)
		if err != nil {
			return err
		}
		rb.OriginalRefs[b.Branch] = sha
	}
	prevOld, prevNew := oldBaseSHA, onto
	for _, b := range descendants {
		invalidateGitFacts()
		if err := run("git", "rebase", "--onto", prevNew, prevOld, b.Branch); err != nil {
			if rbErr := applyRollback(rb); rbErr != nil {
				return fmt.Errorf("rebase %s onto %s failed: %w (rollback also failed: %v)",
					b.Branch, prevNew, err, rbErr)
			}
			return fmt.Errorf("rebase %s onto %s failed: %w\n"+
				"    local refs were rolled back (branch was not deleted)\n"+
				"    resolve with `git rebase --onto %s %s %s`, then re-run `gt delete`",
				b.Branch, prevNew, err, prevNew, prevOld, b.Branch)
		}
		prevOld = rb.OriginalRefs[b.Branch]
		prevNew = b.Branch
	}
	return nil
}

// applyRollback restores local refs captured before a restack. It aborts any
// in-progress rebase, resets each tracked branch to its original SHA, and
// restores HEAD. It performs no remote mutations. Many refs move, so the
// process-local git snapshot is dropped.
func applyRollback(rb Rollback) error {
	invalidateGitFacts()
	_ = run2("git", "rebase", "--abort")

	for branch, sha := range rb.OriginalRefs {
		if sha == "" {
			continue
		}
		if err := run("git", "update-ref", "refs/heads/"+branch, sha); err != nil {
			return err
		}
	}

	if rb.OriginalHEAD != "" {
		if err := run("git", "checkout", rb.OriginalHEAD); err != nil {
			if err := run("git", "symbolic-ref", "HEAD", "refs/heads/"+rb.OriginalHEAD); err != nil {
				return err
			}
		}
	}
	return nil
}
