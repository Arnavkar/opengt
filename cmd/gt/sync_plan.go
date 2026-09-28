package main

// BuildSyncPlan computes a SyncPlan purely from already-loaded data. It does
// no I/O.
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
// Stale is the finished stacks: every branch has a pull request and every
// one of those pull requests is merged or closed. groups and trunks are
// loaded by the caller (git branch list and trunk names). The executor
// deletes only this list.
func BuildSyncPlan(repo *repoStackState, snap *RemoteSnapshot, groups [][]localBranch, trunks map[string]bool) SyncPlan {
	var plan SyncPlan
	if repo == nil {
		return plan
	}
	plan.Trunk = buildTrunkPlan(repo, snap)
	plan.Stacks = buildStackSyncPlans(repo)
	prs, prsAvailable := prsFromSnapshot(snap)
	plan.Stale = finishedStackBranches(groups, prs, trunks, prsAvailable)
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
