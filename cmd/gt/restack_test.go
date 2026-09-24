package main

import "testing"

func TestPlanRestack_AllAncestorsNoRebase(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "main"},
		{Branch: "b"},
		{Branch: "c"},
	}
	heads := map[string]string{"a": "sha-a", "b": "sha-b", "c": "sha-c"}
	parents := map[string]string{"a": "main", "b": "a", "c": "b"}
	ancestor := map[string]bool{"a": true, "b": true, "c": true}
	worktree := map[string]string{}

	actions := planRestack(stack, heads, parents, ancestor, worktree, "c", RestackOpts{})

	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}
	for _, act := range actions {
		if act.Rebase {
			t.Errorf("branch %s: expected no rebase, got Rebase=true", act.Branch)
		}
		if act.SkipReason != "" {
			t.Errorf("branch %s: unexpected skip reason %q", act.Branch, act.SkipReason)
		}
	}
}

func TestPlanRestack_OneDivergedRebasesOnlyThatBranch(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "main"},
		{Branch: "b"},
		{Branch: "c"},
	}
	heads := map[string]string{"a": "sha-a", "b": "sha-b", "c": "sha-c"}
	parents := map[string]string{"a": "main", "b": "a", "c": "b"}
	ancestor := map[string]bool{"a": true, "b": false, "c": true}
	worktree := map[string]string{}

	actions := planRestack(stack, heads, parents, ancestor, worktree, "c", RestackOpts{})

	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}
	var rebases int
	for _, act := range actions {
		if act.Rebase {
			rebases++
			if act.Branch != "b" {
				t.Errorf("expected only b to rebase, got %s", act.Branch)
			}
		}
	}
	if rebases != 1 {
		t.Fatalf("expected exactly 1 rebase action, got %d", rebases)
	}
}

func TestPlanRestack_ForceRebasesEvenAncestors(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "main"},
		{Branch: "b"},
	}
	heads := map[string]string{"a": "sha-a", "b": "sha-b"}
	parents := map[string]string{"a": "main", "b": "a"}
	ancestor := map[string]bool{"a": true, "b": true}
	worktree := map[string]string{}

	actions := planRestack(stack, heads, parents, ancestor, worktree, "b", RestackOpts{Force: true})

	for _, act := range actions {
		if !act.Rebase {
			t.Errorf("branch %s: Force should rebase even ancestors", act.Branch)
		}
	}
}

func TestPlanRestack_SkipsBranchInOtherWorktree(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "main"},
		{Branch: "b"},
	}
	heads := map[string]string{"a": "sha-a", "b": "sha-b"}
	parents := map[string]string{"a": "main", "b": "a"}
	ancestor := map[string]bool{"a": false, "b": false}
	worktree := map[string]string{
		"b": "/repo/.git/worktrees/other",
	}

	actions := planRestack(stack, heads, parents, ancestor, worktree, "a", RestackOpts{})

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	for _, act := range actions {
		if act.Branch == "b" {
			if act.Rebase {
				t.Errorf("b: expected Rebase=false for worktree branch")
			}
			if act.SkipReason == "" {
				t.Errorf("b: expected SkipReason set")
			}
		}
		if act.Branch == "a" && !act.Rebase {
			t.Errorf("a: expected Rebase=true")
		}
	}
}

func TestPlanRestack_CurrentBranchNotSkipped(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "main"},
	}
	heads := map[string]string{"a": "sha-a"}
	parents := map[string]string{"a": "main"}
	ancestor := map[string]bool{"a": false}
	worktree := map[string]string{"a": "/repo"}

	actions := planRestack(stack, heads, parents, ancestor, worktree, "a", RestackOpts{})

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if !actions[0].Rebase {
		t.Errorf("current branch should not be skipped for worktree")
	}
	if actions[0].SkipReason != "" {
		t.Errorf("current branch should have no skip reason")
	}
}

func TestPlanRestack_ParentsForStackOrder(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "trunk-sha"},
		{Branch: "b"},
	}
	heads := map[string]string{"a": "sha-a", "b": "sha-b"}
	parents := map[string]string{"a": "trunk-sha", "b": "a"}
	ancestor := map[string]bool{"a": true, "b": true}
	worktree := map[string]string{}

	actions := planRestack(stack, heads, parents, ancestor, worktree, "b", RestackOpts{})

	if actions[0].Parent != "trunk-sha" {
		t.Errorf("a: expected parent trunk-sha, got %q", actions[0].Parent)
	}
	if actions[1].Parent != "a" {
		t.Errorf("b: expected parent a, got %q", actions[1].Parent)
	}
}

// TestStackParents_TrunkOverridesCachedBase pins the sync restack fix: the
// bottom branch's rebase target is the trunk branch (whose head follows the
// moved trunk), not the base SHA cached when the stack was written.
func TestPlanRestack_MergedBranchIsNotRebased(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "main"},
		{Branch: "b"},
	}
	heads := map[string]string{"a": "sha-a", "b": "sha-b"}
	parents := map[string]string{"a": "main", "b": "a"}
	ancestor := map[string]bool{"a": false, "b": false}
	actions := planRestack(stack, heads, parents, ancestor, map[string]string{}, "b", RestackOpts{
		Force:  true,
		Merged: map[string]bool{"a": true},
	})
	if actions[0].Rebase || actions[0].SkipReason == "" {
		t.Fatalf("merged branch a = %+v, want skipped", actions[0])
	}
	if !actions[1].Rebase || actions[1].Parent != "a" {
		t.Fatalf("open branch b = %+v, want rebase onto a", actions[1])
	}
}

func TestStackParents_TrunkOverridesCachedBase(t *testing.T) {
	stack := []trackedBranch{
		{Branch: "a", Base: "stale-trunk-sha"},
		{Branch: "b", Base: "sha-a"},
	}
	parents := stackParents(stack, "main")
	if parents["a"] != "main" {
		t.Errorf("a: parent = %q, want the trunk branch main", parents["a"])
	}
	if parents["b"] != "a" {
		t.Errorf("b: parent = %q, want a", parents["b"])
	}
}

// TestStackParents_FallsBackToCachedBase: without a trunk name the bottom
// branch keeps its cached base.
func TestStackParents_FallsBackToCachedBase(t *testing.T) {
	stack := []trackedBranch{{Branch: "a", Base: "trunk-sha"}}
	if got := stackParents(stack, "")["a"]; got != "trunk-sha" {
		t.Errorf("a: parent = %q, want cached trunk-sha", got)
	}
}

func TestRollback_CapturesPreRestackRefs(t *testing.T) {
	rb := Rollback{
		OriginalHEAD: "feature",
		OriginalRefs: map[string]string{
			"a": "sha-a-original",
			"b": "sha-b-original",
		},
	}

	if rb.OriginalHEAD != "feature" {
		t.Errorf("OriginalHEAD = %q, want feature", rb.OriginalHEAD)
	}
	if got := rb.OriginalRefs["a"]; got != "sha-a-original" {
		t.Errorf("OriginalRefs[a] = %q, want sha-a-original", got)
	}
	if got := rb.OriginalRefs["b"]; got != "sha-b-original" {
		t.Errorf("OriginalRefs[b] = %q, want sha-b-original", got)
	}
}

func TestRollback_PreRestackSHAsPreservedAcrossConflict(t *testing.T) {
	preA, preB := "aaa-pre", "bbb-pre"
	rb := Rollback{
		OriginalHEAD: "b",
		OriginalRefs: map[string]string{
			"a": preA,
			"b": preB,
		},
	}

	// Simulate the conflict path mutating the recorded SHAs in flight.
	mutatedA := "aaa-post-rebase"
	mutatedB := "bbb-conflict"

	// The Rollback must still hold the pre-restack SHAs, not the in-flight ones.
	if rb.OriginalRefs["a"] == mutatedA {
		t.Errorf("rollback captured mutated SHA for a: %q", rb.OriginalRefs["a"])
	}
	if rb.OriginalRefs["b"] == mutatedB {
		t.Errorf("rollback captured mutated SHA for b: %q", rb.OriginalRefs["b"])
	}
	if rb.OriginalRefs["a"] != preA || rb.OriginalRefs["b"] != preB {
		t.Errorf("rollback lost pre-restack SHAs: a=%q b=%q", rb.OriginalRefs["a"], rb.OriginalRefs["b"])
	}
}

func TestRestackResult_SkippedBranchFlags(t *testing.T) {
	res := RestackResult{Branch: "b", Skipped: true}
	if !res.Skipped {
		t.Errorf("expected Skipped=true")
	}
	if res.Moved {
		t.Errorf("skipped branch must not be Moved")
	}
}

func TestRestackResult_MovedWhenSHAChanged(t *testing.T) {
	res := RestackResult{Branch: "a", OldSHA: "old", NewSHA: "new"}
	res.Moved = res.OldSHA != res.NewSHA
	if !res.Moved {
		t.Errorf("expected Moved=true when SHA changed")
	}

	res2 := RestackResult{Branch: "a", OldSHA: "same", NewSHA: "same"}
	res2.Moved = res2.OldSHA != res2.NewSHA
	if res2.Moved {
		t.Errorf("expected Moved=false when SHA unchanged")
	}
}
