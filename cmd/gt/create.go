package main

import "fmt"

func stacksFromState(st *stackState) []resolvedStack {
	if st == nil {
		return nil
	}
	out := make([]resolvedStack, 0, len(st.Stacks))
	for _, s := range st.Stacks {
		out = append(out, resolvedStack{trackedStack: s})
	}
	return out
}

func applyCreateToRepo(repo *repoStackState, parent, name string, extend bool) error {
	if repo == nil {
		return fmt.Errorf("missing stack state")
	}
	parentSHA, err := branchHead(parent)
	if err != nil {
		return err
	}
	childSHA, err := branchHead(name)
	if err != nil {
		return err
	}
	st := repo.asState()
	if err := applyCreate(st, parent, name, parentSHA, childSHA, extend); err != nil {
		return err
	}
	repo.Stacks = stacksFromState(st)
	return nil
}

func applyTrackToRepo(repo *repoStackState, trunk string, names []string) error {
	if repo == nil {
		return fmt.Errorf("missing stack state")
	}
	trunkSHA, err := branchHead(trunk)
	if err != nil {
		return fmt.Errorf("trunk %q: %w", trunk, err)
	}
	var branches []trackedBranch
	prev := trunkSHA
	for _, name := range names {
		sha, err := branchHead(name)
		if err != nil {
			return fmt.Errorf("branch %q: %w", name, err)
		}
		branches = append(branches, trackedBranch{Branch: name, Head: sha, Base: prev})
		prev = sha
	}
	st := repo.asState()
	if err := applyTrack(st, trunk, trunkSHA, branches); err != nil {
		return err
	}
	repo.Stacks = stacksFromState(st)
	return nil
}

// applyCreate records a new stacked branch in schema v1. id and number stay
// empty: gh-stack 0.2.1 schema.json does not require them. extend appends to
// the stack that already contains parent. Otherwise a new stack is started
// with parent as trunk.
func applyCreate(st *stackState, parent, name, parentSHA, childSHA string, extend bool) error {
	if st == nil {
		return fmt.Errorf("missing stack state")
	}
	if parent == "" || name == "" {
		return fmt.Errorf("create needs a parent and a branch name")
	}
	child := trackedBranch{Branch: name, Head: childSHA, Base: parentSHA}
	if !extend {
		st.Stacks = append(st.Stacks, trackedStack{
			Trunk:    trackedBranch{Branch: parent, Head: parentSHA},
			Branches: []trackedBranch{child},
		})
		return nil
	}
	for i := range st.Stacks {
		s := &st.Stacks[i]
		for _, b := range s.Branches {
			if b.Branch != parent {
				continue
			}
			s.Branches = append(s.Branches, child)
			return nil
		}
	}
	return fmt.Errorf("branch %q is not in a stack", parent)
}

// applyTrack adopts existing branches as one new stack, bottom to top.
func applyTrack(st *stackState, trunk string, trunkSHA string, branches []trackedBranch) error {
	if st == nil {
		return fmt.Errorf("missing stack state")
	}
	if trunk == "" || len(branches) == 0 {
		return fmt.Errorf("track needs a trunk and at least one branch")
	}
	st.Stacks = append(st.Stacks, trackedStack{
		Trunk:    trackedBranch{Branch: trunk, Head: trunkSHA},
		Branches: append([]trackedBranch{}, branches...),
	})
	return nil
}
