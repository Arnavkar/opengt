package main

import (
	"errors"
	"testing"
)

// withGhView swaps ghStackViewFn for the duration of a test.
func withGhView(t *testing.T, v *ghStackView, err error) {
	t.Helper()
	old := ghStackViewFn
	ghStackViewFn = func() (*ghStackView, error) { return v, err }
	t.Cleanup(func() { ghStackViewFn = old })
}

func TestGhStackForBranch_MemberBuildsStack(t *testing.T) {
	withGhView(t, &ghStackView{
		Trunk:         "main",
		CurrentBranch: "feat/b",
		Branches: []ghStackViewBranch{
			{Name: "feat/a", Base: "sha-main"},
			{Name: "feat/b", Base: "sha-a"},
		},
	}, nil)

	ts, ok := ghStackForBranch("feat/b")
	if !ok {
		t.Fatal("ghStackForBranch = false, want true for a member")
	}
	if ts.Trunk.Branch != "main" || ts.Trunk.Head != "sha-main" {
		t.Errorf("trunk = %#v, want main@sha-main", ts.Trunk)
	}
	if len(ts.Branches) != 2 || ts.Branches[0].Branch != "feat/a" || ts.Branches[1].Branch != "feat/b" {
		t.Errorf("branches = %#v, want feat/a then feat/b", ts.Branches)
	}
}

func TestGhStackForBranch_TrunkIsNotAMember(t *testing.T) {
	withGhView(t, &ghStackView{
		Trunk:         "main",
		CurrentBranch: "main",
		Branches:      []ghStackViewBranch{{Name: "feat/a", Base: "sha-main"}},
	}, nil)

	if _, ok := ghStackForBranch("main"); ok {
		t.Error("ghStackForBranch(main) = true, want false: the trunk is not a member")
	}
}

func TestGhStackForBranch_ViewErrorIsMiss(t *testing.T) {
	withGhView(t, nil, errors.New("current branch is not part of a stack"))
	if _, ok := ghStackForBranch("feat/a"); ok {
		t.Error("ghStackForBranch = true, want false when the view fails")
	}
}

func TestAdoptGhStack_AddsWhenLocalMisses(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	withGhView(t, &ghStackView{
		Trunk: "main",
		Branches: []ghStackViewBranch{
			{Name: "feat/a", Base: "sha-main"},
			{Name: "feat/b", Base: "sha-a"},
		},
	}, nil)

	adoptGhStack(repo, "feat/b")

	if _, ok := findStackForBranch(repo, "feat/b"); !ok {
		t.Fatal("feat/b still not in a stack after adopt")
	}
	if len(repo.Stacks) != 2 {
		t.Errorf("repo.Stacks len = %d, want 2 (local + gh)", len(repo.Stacks))
	}
}

func TestAdoptGhStack_KeepsLocalWhenPresent(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	withGhView(t, &ghStackView{
		Trunk:    "main",
		Branches: []ghStackViewBranch{{Name: "a", Base: "sha-main"}},
	}, nil)

	adoptGhStack(repo, "a")

	if len(repo.Stacks) != 1 {
		t.Errorf("repo.Stacks len = %d, want 1: a local stack must win", len(repo.Stacks))
	}
}

func TestAdoptGhStack_NilRepoIsSafe(t *testing.T) {
	withGhView(t, &ghStackView{Trunk: "main", Branches: []ghStackViewBranch{{Name: "a"}}}, nil)
	adoptGhStack(nil, "a")
}
