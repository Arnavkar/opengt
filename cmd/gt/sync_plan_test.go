package main

import (
	"testing"
	"time"
)

// snapWith builds a RemoteSnapshot from ref and PR maps. Tests in this file
// never touch the network.
func snapWith(refs map[string]RemoteRef, prs map[string]PRSnapshot) *RemoteSnapshot {
	return &RemoteSnapshot{
		Refs: RemoteRefSnapshot{Refs: refs, FetchedAt: time.Unix(1700000000, 0)},
		PRs:  prs,
	}
}

// repoWithStacks builds a repoStackState from bare trackedStacks, the way
// reconcileSources does once it has grouped definitions.
func repoWithStacks(stacks ...trackedStack) *repoStackState {
	r := &repoStackState{}
	for _, s := range stacks {
		r.Stacks = append(r.Stacks, resolvedStack{trackedStack: s})
	}
	return r
}

func TestBuildSyncPlan_TrunkFastForward(t *testing.T) {
	for _, tc := range []struct {
		name      string
		localSHA  string
		remoteSHA string
		wantFF    bool
	}{
		{"differ", "sha-local", "sha-remote", true},
		{"equal", "sha-same", "sha-same", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := repoWithStacks(trackedStack{
				Trunk:    trackedBranch{Branch: "main", Head: tc.localSHA},
				Branches: []trackedBranch{{Branch: "feat/a"}},
			})
			snap := snapWith(map[string]RemoteRef{
				"main": {Branch: "main", Exists: true, RemoteSHA: tc.remoteSHA},
			}, nil)

			plan := BuildSyncPlan(repo, snap, nil, nil)

			if plan.Trunk.Branch != "main" {
				t.Fatalf("Trunk.Branch = %q, want main", plan.Trunk.Branch)
			}
			if plan.Trunk.LocalSHA != tc.localSHA {
				t.Errorf("Trunk.LocalSHA = %q, want %q", plan.Trunk.LocalSHA, tc.localSHA)
			}
			if plan.Trunk.RemoteSHA != tc.remoteSHA {
				t.Errorf("Trunk.RemoteSHA = %q, want %q", plan.Trunk.RemoteSHA, tc.remoteSHA)
			}
			if plan.Trunk.FastForward != tc.wantFF {
				t.Errorf("Trunk.FastForward = %v, want %v", plan.Trunk.FastForward, tc.wantFF)
			}
		})
	}
}

func TestBuildSyncPlan_RepoWideIteratesAllStacks(t *testing.T) {
	repo := repoWithStacks(
		trackedStack{
			Trunk:    trackedBranch{Branch: "main", Head: "sha-main"},
			Branches: []trackedBranch{{Branch: "feat/a"}},
		},
		trackedStack{
			Trunk:    trackedBranch{Branch: "main", Head: "sha-main"},
			Branches: []trackedBranch{{Branch: "feat/b"}, {Branch: "feat/c"}},
		},
	)
	snap := snapWith(map[string]RemoteRef{
		"main":   {Branch: "main", Exists: true, RemoteSHA: "sha-main"},
		"feat/a": {Branch: "feat/a", Exists: true, RemoteSHA: "sha-a"},
		"feat/b": {Branch: "feat/b", Exists: true, RemoteSHA: "sha-b"},
		"feat/c": {Branch: "feat/c", Exists: true, RemoteSHA: "sha-c"},
	}, nil)

	plan := BuildSyncPlan(repo, snap, nil, nil)

	if len(plan.Stacks) != 2 {
		t.Fatalf("Stacks len = %d, want 2 (one per stack, repo-wide)", len(plan.Stacks))
	}
	if plan.Stacks[0].Stack.Branches[0].Branch != "feat/a" {
		t.Errorf("Stacks[0] first branch = %q, want feat/a", plan.Stacks[0].Stack.Branches[0].Branch)
	}
	if len(plan.Stacks[1].Stack.Branches) != 2 {
		t.Errorf("Stacks[1] branches len = %d, want 2", len(plan.Stacks[1].Stack.Branches))
	}
	// The planner leaves Restack and SkipReason empty; the executor fills them.
	for i, s := range plan.Stacks {
		if s.SkipReason != "" {
			t.Errorf("Stacks[%d].SkipReason = %q, want empty (executor sets it)", i, s.SkipReason)
		}
		if len(s.Restack) != 0 {
			t.Errorf("Stacks[%d].Restack len = %d, want 0 (executor runs CascadeRestack)", i, len(s.Restack))
		}
	}
}

func TestBuildSyncPlan_FinishedStack(t *testing.T) {
	repo := repoWithStacks(trackedStack{
		Trunk: trackedBranch{Branch: "main", Head: "sha-main"},
		Branches: []trackedBranch{
			{Branch: "feat/a"},
			{Branch: "feat/b"},
		},
	})
	snap := snapWith(nil, map[string]PRSnapshot{
		"feat/a": {Number: 1, Head: "feat/a", State: "MERGED"},
		"feat/b": {Number: 2, Head: "feat/b", State: "CLOSED"},
	})
	groups := [][]localBranch{{
		{name: "feat/a", local: true},
		{name: "feat/b", local: true},
	}}

	plan := BuildSyncPlan(repo, snap, groups, map[string]bool{"main": true})

	if len(plan.Stale) != 2 {
		t.Fatalf("Stale len = %d, want 2; got %+v", len(plan.Stale), plan.Stale)
	}
	if plan.Stale[0].name != "feat/a" || plan.Stale[1].name != "feat/b" {
		t.Errorf("Stale = %+v, want feat/a then feat/b", plan.Stale)
	}
}

func TestBuildSyncPlan_EmptyRepoEmptyPlan(t *testing.T) {
	repo := &repoStackState{}
	snap := snapWith(map[string]RemoteRef{}, nil)

	plan := BuildSyncPlan(repo, snap, nil, nil)

	if plan.Trunk.Branch != "" {
		t.Errorf("Trunk.Branch = %q, want empty for empty repo", plan.Trunk.Branch)
	}
	if len(plan.Stacks) != 0 {
		t.Errorf("Stacks len = %d, want 0", len(plan.Stacks))
	}
	if len(plan.Stale) != 0 {
		t.Errorf("Stale len = %d, want 0", len(plan.Stale))
	}
}

func TestBuildSyncPlan_TrunkFastForwardFalseWhenRemoteMissing(t *testing.T) {
	repo := repoWithStacks(trackedStack{
		Trunk:    trackedBranch{Branch: "main", Head: "sha-local"},
		Branches: []trackedBranch{{Branch: "feat/a"}},
	})
	// Snapshot has no entry for main: remote is missing.
	snap := snapWith(map[string]RemoteRef{
		"feat/a": {Branch: "feat/a", Exists: true, RemoteSHA: "sha-a"},
	}, nil)

	plan := BuildSyncPlan(repo, snap, nil, nil)

	if plan.Trunk.Branch != "main" {
		t.Fatalf("Trunk.Branch = %q, want main", plan.Trunk.Branch)
	}
	if plan.Trunk.RemoteSHA != "" {
		t.Errorf("Trunk.RemoteSHA = %q, want empty when remote missing", plan.Trunk.RemoteSHA)
	}
	if plan.Trunk.FastForward {
		t.Errorf("Trunk.FastForward = true, want false when RemoteSHA empty")
	}
}

// TestBuildSyncPlan_NilSnapIsSafe guards the pure entry point against a nil
// snapshot (e.g. both adapters failed): it must return a plan with no stale
// entries rather than panic.
func TestBuildSyncPlan_NilSnapIsSafe(t *testing.T) {
	repo := repoWithStacks(trackedStack{
		Trunk:    trackedBranch{Branch: "main", Head: "sha-local"},
		Branches: []trackedBranch{{Branch: "feat/a"}},
	})

	plan := BuildSyncPlan(repo, nil, nil, nil)

	if plan.Trunk.FastForward {
		t.Errorf("Trunk.FastForward = true, want false with nil snap")
	}
	if len(plan.Stacks) != 1 {
		t.Errorf("Stacks len = %d, want 1 (repo-wide plan is independent of snap)", len(plan.Stacks))
	}
	if len(plan.Stale) != 0 {
		t.Errorf("Stale len = %d, want 0 when snap is nil", len(plan.Stale))
	}
}

// TestBuildSyncPlan_NilRepoIsSafe guards the pure entry point against a nil
// repo: it must return a zero plan rather than panic.
func TestBuildSyncPlan_NilRepoIsSafe(t *testing.T) {
	plan := BuildSyncPlan(nil, snapWith(map[string]RemoteRef{}, nil), nil, nil)

	if plan.Trunk.Branch != "" {
		t.Errorf("Trunk.Branch = %q, want empty for nil repo", plan.Trunk.Branch)
	}
	if len(plan.Stacks) != 0 {
		t.Errorf("Stacks len = %d, want 0", len(plan.Stacks))
	}
	if len(plan.Stale) != 0 {
		t.Errorf("Stale len = %d, want 0", len(plan.Stale))
	}
}

// TestBuildSyncPlan_OpenBranchKeepsChain ensures one open pull request keeps
// every branch on that stack, including a merged one.
func TestBuildSyncPlan_OpenBranchKeepsChain(t *testing.T) {
	repo := repoWithStacks(trackedStack{
		Trunk: trackedBranch{Branch: "main", Head: "sha-main"},
		Branches: []trackedBranch{
			{Branch: "feat/merged"},
			{Branch: "feat/open"},
		},
	})
	snap := snapWith(nil, map[string]PRSnapshot{
		"feat/merged": {Number: 1, Head: "feat/merged", State: "MERGED"},
		"feat/open":   {Number: 2, Head: "feat/open", State: "OPEN"},
	})
	groups := [][]localBranch{{
		{name: "feat/merged", local: true},
		{name: "feat/open", local: true},
	}}

	plan := BuildSyncPlan(repo, snap, groups, map[string]bool{"main": true})

	if len(plan.Stale) != 0 {
		t.Fatalf("Stale len = %d, want 0; got %+v", len(plan.Stale), plan.Stale)
	}
}
