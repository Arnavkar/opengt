package main

import (
	"errors"
	"fmt"
)

// ResolveSubmitScope returns the ordered branch names (bottom→top) a submit
// touches. Default scope is trunk→current: every member from the bottom of
// the stack up to and including current. With stackFlag the whole stack is in
// scope. With updateOnly (-u) any branch in scope that has no OPEN PR in prs
// is excluded entirely (decision #7: enforced, not advisory).
func ResolveSubmitScope(stack trackedStack, current string, stackFlag bool, updateOnly bool, prs map[string]PRSnapshot) []string {
	members := stackMembers(stack)
	var scope []string
	if stackFlag {
		scope = append(scope, members...)
	} else {
		idx := -1
		for i, m := range members {
			if m == current {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		scope = append(scope, members[:idx+1]...)
	}
	if !updateOnly {
		return scope
	}
	var filtered []string
	for _, b := range scope {
		if pr, ok := prs[b]; ok && pr.State == "OPEN" {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// upstackOf is the members above current (not including current). Empty when
// current is missing or already the tip.
func upstackOf(stack trackedStack, current string) []string {
	members := stackMembers(stack)
	for i, m := range members {
		if m == current {
			return append([]string{}, members[i+1:]...)
		}
	}
	return nil
}

// BuildSubmitPlan computes a SubmitPlan purely from already-loaded repo state,
// the current branch, a remote snapshot, and options. It does no I/O.
func BuildSubmitPlan(repo *repoStackState, current string, snap *RemoteSnapshot, opts SubmitOpts) (SubmitPlan, error) {
	if repo == nil {
		return SubmitPlan{}, errors.New("BuildSubmitPlan: nil repo state")
	}
	if snap == nil {
		return SubmitPlan{}, errors.New("BuildSubmitPlan: nil remote snapshot")
	}
	if hits := repo.ambiguous(current); len(hits) > 0 {
		return SubmitPlan{}, errAmbiguous(current, hits)
	}
	var rs *resolvedStack
	for i := range repo.Stacks {
		s := &repo.Stacks[i]
		if s.Stale {
			continue
		}
		for _, b := range s.Branches {
			if b.Branch == current {
				rs = s
				break
			}
		}
		if rs != nil {
			break
		}
	}
	if rs == nil {
		return SubmitPlan{}, fmt.Errorf("branch %q is not tracked in any stack", current)
	}
	pos := locate(&stackState{Stacks: []trackedStack{rs.trackedStack}}, current)
	if pos.forked {
		return SubmitPlan{}, errForked(current)
	}

	scopeNames := ResolveSubmitScope(rs.trackedStack, current, opts.Stack, opts.UpdateOnly, snap.PRs)
	chain := stackChain(rs.trackedStack)
	parentOf := map[string]string{}
	for i, n := range chain {
		if i == 0 {
			continue
		}
		parentOf[n] = chain[i-1]
	}

	plan := SubmitPlan{}
	var desiredNumbers []int
	for _, name := range stackMembers(rs.trackedStack) {
		if p, ok := snap.PRs[name]; ok && p.State == "OPEN" {
			desiredNumbers = append(desiredNumbers, p.Number)
		}
	}
	for _, name := range scopeNames {
		parent := parentOf[name]
		var localSHA string
		for _, b := range rs.Branches {
			if b.Branch == name {
				localSHA = b.Head
				break
			}
		}
		rr := snap.Refs.Refs[name]
		remoteExists := rr.Exists
		remoteSHA := rr.RemoteSHA
		changed := remoteExists && localSHA != "" && localSHA != remoteSHA

		var pr *PRSnapshot
		if p, ok := snap.PRs[name]; ok && p.State == "OPEN" {
			pc := p
			pr = &pc
		}

		expectedBase := parent
		bsp := BranchSubmitPlan{
			Branch:       name,
			Parent:       parent,
			LocalSHA:     localSHA,
			RemoteSHA:    remoteSHA,
			RemoteExists: remoteExists,
			Changed:      changed,
			PR:           pr,
			ExpectedBase: expectedBase,
		}
		bsp.Push = changed || !remoteExists
		bsp.CreatePR = pr == nil && !opts.UpdateOnly
		bsp.UpdateBase = pr != nil && pr.Base != expectedBase
		bsp.Publish = opts.Publish && pr != nil && pr.Draft
		bsp.DisableAutoMerge = pr != nil && pr.AutoMergeEnabled && (bsp.Push || bsp.UpdateBase)

		plan.Scope = append(plan.Scope, bsp)

		if bsp.Push {
			plan.Pushes = append(plan.Pushes, PushRef{
				Branch:      name,
				LocalSHA:    localSHA,
				ExpectedOld: remoteSHA,
				IsNew:       !remoteExists,
			})
		}
		if bsp.CreatePR {
			plan.Creates = append(plan.Creates, PRCreate{
				Branch:  name,
				Parent:  expectedBase,
				HeadSHA: localSHA,
				Draft:   opts.Draft,
			})
		}
		if bsp.UpdateBase {
			plan.BaseUpdates = append(plan.BaseUpdates, PRBaseUpdate{
				PRNumber: pr.Number,
				NewBase:  expectedBase,
			})
		}
		if bsp.Publish {
			plan.PublishUpdates = append(plan.PublishUpdates, PRPublishUpdate{
				PRNumber: pr.Number,
			})
		}
		if bsp.DisableAutoMerge {
			plan.AutoMergeDisables = append(plan.AutoMergeDisables, PRAutoMergeDisable{
				PRNumber: pr.Number,
			})
		}
	}

	if len(desiredNumbers) > 0 {
		stackID := ""
		remoteNumbers := []int(nil)
		if snap.RemoteStack != nil {
			stackID = snap.RemoteStack.ID
			remoteNumbers = snap.RemoteStack.Numbers
		}
		if !intSlicesEqual(desiredNumbers, remoteNumbers) {
			plan.StackUpdate = &StackMutation{
				StackID: stackID,
				Numbers: desiredNumbers,
			}
		}
	}

	return plan, nil
}

// allowRemoteReplace is the submit-safety gate for an existing-remote push
// (decision #11). It is pure: the caller resolves ancestor (git merge-base
// --is-ancestor) and inReflog (git reflog --format=%H) beforehand.
//
//   - Remote missing: first push, no lease.
//   - Local equals remote: nothing to push.
//   - Remote is an ancestor of local: fast-forward; the lease is safe because
//     no remote commit is dropped.
//   - Remote SHA is in this branch's reflog: our own amend or rebase; allow,
//     keeping --force-with-lease set to that snapshot SHA.
//   - Otherwise the remote has a commit this branch never contained: refuse,
//     and leave that commit in place. No fetch and rebase happens here;
//     -f / --force is the explicit override.
func allowRemoteReplace(local, remote string, ancestor, inReflog bool) bool {
	if remote == "" || local == remote {
		return true
	}
	return ancestor || inReflog
}

// IsNoOp is the fast path: true when the plan performs no git push and no
// remote mutation. Restack results are intentionally excluded (the executor
// runs CascadeRestack separately and opts.Always overrides the fast path).
func (p SubmitPlan) IsNoOp() bool {
	return len(p.Pushes) == 0 &&
		len(p.Creates) == 0 &&
		len(p.BaseUpdates) == 0 &&
		len(p.PublishUpdates) == 0 &&
		len(p.AutoMergeDisables) == 0 &&
		p.StackUpdate == nil
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
