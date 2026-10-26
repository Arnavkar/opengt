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

func TestCascadeRestack_LoadedFactsDoNotListWorktreesOrMergeBase(t *testing.T) {
	resetProcessCaches(t)
	cachedFacts = restackFacts()
	var worktreeCalls, ancestorCalls, rebaseCalls int
	results, _, err := cascadeRestack(restackStack(), RestackOpts{Trunk: "main"}, restackGit{
		current: func() (string, error) { return "c", nil },
		head: func(name string) (string, error) {
			return cachedFacts.heads[name], nil
		},
		worktree: func(name string) (string, error) {
			worktreeCalls++
			t.Fatalf("worktree %s", name)
			return "", nil
		},
		ancestor: func(parent, child string) bool {
			ancestorCalls++
			t.Fatalf("ancestor %s %s", parent, child)
			return false
		},
		rebase: func(parent, oldBase, branch string, interactive bool) error {
			rebaseCalls++
			t.Fatalf("rebase %s %s", parent, branch)
			return nil
		},
		noteHead: func(branch, sha string) {
			cachedFacts.heads[branch] = sha
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	want := []RestackResult{
		{Branch: "a", OldSHA: "sha-a", NewSHA: "sha-a"},
		{Branch: "b", OldSHA: "sha-b", NewSHA: "sha-b"},
		{Branch: "c", OldSHA: "sha-c", NewSHA: "sha-c"},
	}
	for i, r := range results {
		if r.Branch != want[i].Branch || r.OldSHA != want[i].OldSHA || r.NewSHA != want[i].NewSHA || r.Moved || r.Skipped {
			t.Fatalf("result %d = %+v, want %+v", i, r, want[i])
		}
	}
	if worktreeCalls != 0 || ancestorCalls != 0 || rebaseCalls != 0 {
		t.Fatalf("worktree=%d ancestor=%d rebase=%d", worktreeCalls, ancestorCalls, rebaseCalls)
	}
}

func TestCascadeRestack_EmptyBaseChecksAncestorOnce(t *testing.T) {
	resetProcessCaches(t)
	cachedFacts = restackFacts()
	stack := restackStack()
	stack[1].Base = ""
	var worktreeCalls, ancestorCalls, rebaseCalls int
	results, _, err := cascadeRestack(stack, RestackOpts{Trunk: "main"}, restackGit{
		current: func() (string, error) { return "c", nil },
		head: func(name string) (string, error) {
			return cachedFacts.heads[name], nil
		},
		worktree: func(name string) (string, error) {
			worktreeCalls++
			t.Fatalf("worktree %s", name)
			return "", nil
		},
		ancestor: func(parent, child string) bool {
			ancestorCalls++
			return true
		},
		rebase: func(parent, oldBase, branch string, interactive bool) error {
			rebaseCalls++
			t.Fatalf("rebase %s %s", parent, branch)
			return nil
		},
		noteHead: func(branch, sha string) {
			cachedFacts.heads[branch] = sha
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for _, r := range results {
		if r.Moved || r.Skipped {
			t.Fatalf("%s moved=%v skipped=%v", r.Branch, r.Moved, r.Skipped)
		}
	}
	if ancestorCalls != 1 || rebaseCalls != 0 || worktreeCalls != 0 {
		t.Fatalf("ancestor=%d rebase=%d worktree=%d", ancestorCalls, rebaseCalls, worktreeCalls)
	}
}

func TestCascadeRestack_RebaseNotesNewHead(t *testing.T) {
	resetProcessCaches(t)
	cachedFacts = restackFacts()
	stack := restackStack()
	stack[1].Base = ""
	var worktreeCalls int
	var noted string
	var g restackGit
	g.current = func() (string, error) { return "c", nil }
	g.worktree = func(name string) (string, error) {
		worktreeCalls++
		t.Fatalf("worktree %s", name)
		return "", nil
	}
	g.ancestor = func(parent, child string) bool {
		return child != "b"
	}
	g.noteHead = func(branch, sha string) {
		cachedFacts.heads[branch] = sha
		if branch == "b" {
			noted = sha
		}
	}
	g.rebase = func(parent, oldBase, branch string, interactive bool) error {
		if branch == "b" {
			g.noteHead(branch, "sha-b2")
		}
		return nil
	}
	g.head = func(name string) (string, error) {
		if name == "b" && noted != "" {
			return noted, nil
		}
		sha, ok := cachedFacts.heads[name]
		if name == "b" && noted == "" {
			sha = "sha-b"
			ok = true
		}
		if !ok {
			t.Fatalf("head %s", name)
		}
		return sha, nil
	}
	results, _, err := cascadeRestack(stack, RestackOpts{Trunk: "main"}, g)
	if err != nil {
		t.Fatal(err)
	}
	if worktreeCalls != 0 {
		t.Fatalf("worktree calls = %d", worktreeCalls)
	}
	var sawB bool
	for _, r := range results {
		if r.Branch != "b" {
			if r.Moved || r.Skipped {
				t.Fatalf("%s = %+v, want unmoved", r.Branch, r)
			}
			continue
		}
		sawB = true
		if !r.Moved || r.NewSHA != "sha-b2" || r.Skipped {
			t.Fatalf("b = %+v, want moved to sha-b2", r)
		}
	}
	if !sawB {
		t.Fatal("missing result for b")
	}
	if cachedFacts.heads["b"] != "sha-b2" {
		t.Fatalf("facts head b = %q, want sha-b2", cachedFacts.heads["b"])
	}
}

func TestRestoreFactsAfterRebase_CurrentFollowsRebasedBranch(t *testing.T) {
	resetProcessCaches(t)
	saved := GitFacts{
		heads:     map[string]string{"a": "old", "b": "sha-b"},
		worktrees: map[string]string{},
		current:   "b",
		ready:     true,
	}
	restoreFactsAfterRebase(saved, "a", "new")
	if cachedFacts.current != "a" {
		t.Fatalf("current = %q, want a", cachedFacts.current)
	}
	if cachedFacts.heads["a"] != "new" || cachedFacts.heads["b"] != "sha-b" {
		t.Fatalf("heads = %#v, want a=new b=sha-b", cachedFacts.heads)
	}
}

func restackFacts() GitFacts {
	return GitFacts{
		heads: map[string]string{
			"main": "sha-main",
			"a":    "sha-a",
			"b":    "sha-b",
			"c":    "sha-c",
		},
		worktrees: map[string]string{},
		current:   "c",
		ready:     true,
	}
}

func TestRestackScope(t *testing.T) {
	stack := trackedStack{
		Trunk:    trackedBranch{Branch: "main"},
		Branches: restackStack(),
	}
	full, trunk, err := restackScope(stack, "b", false, false)
	if err != nil || trunk != "main" || len(full) != 3 || full[0].Branch != "a" {
		t.Fatalf("full = %v trunk=%s err=%v", namesOf(full), trunk, err)
	}
	down, trunk, err := restackScope(stack, "b", true, false)
	if err != nil || trunk != "main" || len(down) != 2 || down[1].Branch != "b" {
		t.Fatalf("down = %v trunk=%s err=%v", namesOf(down), trunk, err)
	}
	up, trunk, err := restackScope(stack, "b", false, true)
	if err != nil || trunk != "a" || len(up) != 2 || up[0].Branch != "b" || up[1].Branch != "c" {
		t.Fatalf("up = %v trunk=%s err=%v", namesOf(up), trunk, err)
	}
	if _, _, err := restackScope(stack, "missing", true, false); err == nil {
		t.Fatal("missing branch should error")
	}
}

func namesOf(stack []trackedBranch) []string {
	out := make([]string, len(stack))
	for i, b := range stack {
		out[i] = b.Branch
	}
	return out
}

func restackStack() []trackedBranch {
	return []trackedBranch{
		{Branch: "a", Base: "sha-main"},
		{Branch: "b", Base: "sha-a"},
		{Branch: "c", Base: "sha-b"},
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
