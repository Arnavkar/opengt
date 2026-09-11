package main

import "fmt"

// BuildSyncPlan computes a SyncPlan purely from the repo stack state and a
// remote snapshot. It does no I/O.
//
// Sync is repo-wide (design decision #9): it iterates every stack in
// repo.Stacks, not just HEAD's. For each stack it produces a StackSyncPlan
// whose Restack and SkipReason are left empty; the executor (cmdSync) runs
// CascadeRestack and fills them, skipping branches checked out in other
// worktrees at execution time. Worktree skip is an executor concern because
// worktreePathForBranch hits git and cannot run in a pure planner.
//
// The trunk is derived from the repo state (the first non-empty
// stack.Trunk.Branch) rather than trunkNames()/fallbackTrunk(), which hit
// git. Its LocalSHA is the trunk's tracked Head from the repo state; its
// RemoteSHA comes from snap.Refs. FastForward is set from a SHA inequality
// only — ancestry is confirmed by the executor, since isAncestor is not
// pure.
//
// Stale candidates are tracked members (non-trunk) whose remote ref no
// longer exists and which have no open PR in snap.PRs. The executor
// confirms via the existing prune logic.
func BuildSyncPlan(repo *repoStackState, snap *RemoteSnapshot) SyncPlan {
	var plan SyncPlan
	if repo == nil {
		return plan
	}
	plan.Trunk = buildTrunkPlan(repo, snap)
	plan.Stacks = buildStackSyncPlans(repo)
	plan.Stale = buildStaleCandidates(repo, snap)
	return plan
}

// buildTrunkPlan derives the trunk from the first stack with a non-empty
// trunk branch. LocalSHA is the tracked trunk Head; RemoteSHA is read from
// the snapshot. FastForward is true only when both SHAs are present and
// differ — the executor confirms ancestry.
func buildTrunkPlan(repo *repoStackState, snap *RemoteSnapshot) TrunkPlan {
	var tp TrunkPlan
	for _, s := range repo.Stacks {
		if s.Trunk.Branch != "" {
			tp.Branch = s.Trunk.Branch
			tp.LocalSHA = s.Trunk.Head
			break
		}
	}
	if tp.Branch == "" {
		return tp
	}
	if snap != nil {
		if ref, ok := snap.Refs.Refs[tp.Branch]; ok {
			tp.RemoteSHA = ref.RemoteSHA
		}
	}
	tp.FastForward = tp.LocalSHA != "" && tp.RemoteSHA != "" && tp.LocalSHA != tp.RemoteSHA
	return tp
}

// buildStackSyncPlans emits one StackSyncPlan per stack in repo order. The
// planner does not precompute Restack results or SkipReason: CascadeRestack
// is not pure, and worktree skips are an executor concern. The plan shape
// (one entry per stack) is what makes the repo-wide iteration testable.
func buildStackSyncPlans(repo *repoStackState) []StackSyncPlan {
	if len(repo.Stacks) == 0 {
		return nil
	}
	plans := make([]StackSyncPlan, 0, len(repo.Stacks))
	for _, s := range repo.Stacks {
		plans = append(plans, StackSyncPlan{Stack: s.trackedStack})
	}
	return plans
}

// buildStaleCandidates collects tracked members whose remote ref is gone
// and which have no open PR. A branch claimed by multiple stacks is listed
// once. Trunks are never stale. When snap is nil, no staleness can be
// computed from refs, so nothing is returned.
func buildStaleCandidates(repo *repoStackState, snap *RemoteSnapshot) []staleBranch {
	if snap == nil {
		return nil
	}
	var stale []staleBranch
	seen := map[string]bool{}
	for _, s := range repo.Stacks {
		for _, b := range s.Branches {
			branch := b.Branch
			if branch == "" || seen[branch] {
				continue
			}
			seen[branch] = true
			ref, listed := snap.Refs.Refs[branch]
			if listed && ref.Exists {
				continue
			}
			if hasOpenPR(snap.PRs, branch) {
				continue
			}
			stale = append(stale, staleBranch{name: branch, reason: staleReasonMissingRemote(branch, listed)})
		}
	}
	return stale
}

// hasOpenPR reports whether snap.PRs holds an OPEN PR for the given head.
// A nil PR map means PR data is unavailable; the caller treats that as
// "no open PR known" and lets the executor confirm.
func hasOpenPR(prs map[string]PRSnapshot, head string) bool {
	if prs == nil {
		return false
	}
	pr, ok := prs[head]
	if !ok {
		return false
	}
	return pr.State == "OPEN"
}

func staleReasonMissingRemote(branch string, listed bool) string {
	if !listed {
		return fmt.Sprintf("%s has no remote ref and no open PR", branch)
	}
	return fmt.Sprintf("%s is gone on remote and has no open PR", branch)
}
