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

// CascadeRestack rebases each branch onto its parent in stack order, returning
// one RestackResult per branch and a Rollback usable to recover local refs on
// conflict. Zero GitHub API calls happen here. Branches checked out in other
// worktrees are skipped.
func CascadeRestack(stack []trackedBranch, opts RestackOpts) ([]RestackResult, *Rollback, error) {
	head, err := currentBranch()
	if err != nil {
		return nil, nil, err
	}

	rb := &Rollback{
		OriginalHEAD: head,
		OriginalRefs: make(map[string]string, len(stack)),
	}
	heads := make(map[string]string, len(stack))
	for _, b := range stack {
		sha, err := branchHead(b.Branch)
		if err != nil {
			return nil, nil, err
		}
		rb.OriginalRefs[b.Branch] = sha
		heads[b.Branch] = sha
	}

	parents := stackParents(stack, opts.Trunk)

	ancestor := make(map[string]bool, len(stack))
	worktree := make(map[string]string, len(stack))
	for _, b := range stack {
		ancestor[b.Branch] = isAncestor(parents[b.Branch], b.Branch)
		wt, err := worktreePathForBranch(b.Branch)
		if err != nil {
			return nil, nil, err
		}
		worktree[b.Branch] = wt
	}

	actions := planRestack(stack, heads, parents, ancestor, worktree, head, opts)

	results := make([]RestackResult, 0, len(stack))
	for _, act := range actions {
		res := RestackResult{Branch: act.Branch, OldSHA: heads[act.Branch]}

		if act.SkipReason != "" {
			res.Skipped = true
			res.NewSHA = res.OldSHA
			results = append(results, res)
			continue
		}

		// planRestack decided from ancestry captured before any branch moved.
		// An ancestor rebased earlier in this cascade invalidates that
		// decision (the parent's old SHA was an ancestor; its new head is
		// not), so the live refs are authoritative unless Force says
		// rebase regardless.
		rebase := act.Rebase
		if !opts.Force {
			rebase = !isAncestor(act.Parent, act.Branch)
		}

		if !rebase {
			res.NewSHA = res.OldSHA
			results = append(results, res)
			continue
		}

		args := []string{"rebase"}
		if opts.Interactive {
			args = append(args, "--interactive")
		}
		args = append(args, act.Parent, act.Branch)
		if err := run("git", args...); err != nil {
			if rbErr := applyRollback(*rb); rbErr != nil {
				return results, rb, fmt.Errorf("rebase %s onto %s failed: %w (rollback also failed: %v)",
					act.Branch, act.Parent, err, rbErr)
			}
			return results, rb, fmt.Errorf("rebase %s onto %s failed: %w\n"+
				"    local refs were rolled back (no remote changes were made)\n"+
				"    resolve with `git rebase %s %s` (or `gt restack`), then re-run",
				act.Branch, act.Parent, err, act.Parent, act.Branch)
		}

		newSHA, err := branchHead(act.Branch)
		if err != nil {
			return results, rb, err
		}
		res.NewSHA = newSHA
		res.Moved = res.OldSHA != res.NewSHA
		results = append(results, res)
	}

	return results, rb, nil
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
// restores HEAD. It performs no remote mutations.
func applyRollback(rb Rollback) error {
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
