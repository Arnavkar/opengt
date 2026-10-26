package main

import "testing"

func TestApplyCreate_StartsStackWithoutID(t *testing.T) {
	st := &stackState{SchemaVersion: 1}
	if err := applyCreate(st, "main", "a", "sha-main", "sha-a", false); err != nil {
		t.Fatal(err)
	}
	if len(st.Stacks) != 1 {
		t.Fatalf("stacks = %d, want 1", len(st.Stacks))
	}
	s := st.Stacks[0]
	if s.ID != "" || s.Number != 0 {
		t.Fatalf("id/number should be empty, got id=%q number=%d", s.ID, s.Number)
	}
	if s.Trunk.Branch != "main" || s.Trunk.Head != "sha-main" {
		t.Fatalf("trunk = %+v", s.Trunk)
	}
	if len(s.Branches) != 1 || s.Branches[0] != (trackedBranch{Branch: "a", Head: "sha-a", Base: "sha-main"}) {
		t.Fatalf("branches = %+v", s.Branches)
	}
}

func TestApplyCreate_ExtendsExistingStack(t *testing.T) {
	st := &stackState{SchemaVersion: 1}
	if err := applyCreate(st, "main", "a", "sha-main", "sha-a", false); err != nil {
		t.Fatal(err)
	}
	if err := applyCreate(st, "a", "b", "sha-a", "sha-b", true); err != nil {
		t.Fatal(err)
	}
	if len(st.Stacks) != 1 || len(st.Stacks[0].Branches) != 2 {
		t.Fatalf("stacks = %+v", st.Stacks)
	}
	if got := st.Stacks[0].Branches[1]; got != (trackedBranch{Branch: "b", Head: "sha-b", Base: "sha-a"}) {
		t.Fatalf("top = %+v", got)
	}
}

func TestApplyTrack_AdoptsInOrder(t *testing.T) {
	st := &stackState{SchemaVersion: 1}
	err := applyTrack(st, "main", "sha-main", []trackedBranch{
		{Branch: "a", Head: "sha-a", Base: "sha-main"},
		{Branch: "b", Head: "sha-b", Base: "sha-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Stacks) != 1 || st.Stacks[0].Trunk.Branch != "main" {
		t.Fatalf("stack = %+v", st.Stacks)
	}
	if got := st.Stacks[0].Branches; len(got) != 2 || got[0].Branch != "a" || got[1].Branch != "b" {
		t.Fatalf("branches = %+v", got)
	}
}
