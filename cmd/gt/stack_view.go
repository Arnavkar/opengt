package main

import (
	"encoding/json"
	"fmt"
)

// ghStackView is the subset of `gh stack view --json` gt reads. gh resolves
// the stack from the current branch (or its trunk), so a view only proves
// membership for the branch it was run on, never for another branch.
type ghStackView struct {
	Trunk         string              `json:"trunk"`
	CurrentBranch string              `json:"currentBranch"`
	Branches      []ghStackViewBranch `json:"branches"`
}

type ghStackViewBranch struct {
	Name string `json:"name"`
	Base string `json:"base"`
}

// ghStackViewFn runs `gh stack view --json`. Tests swap it. The command fails
// when the current branch is in no stack, which is the "no" answer gt needs.
var ghStackViewFn = func() (*ghStackView, error) {
	out, err := capture("gh", "stack", "view", "--json")
	if err != nil {
		return nil, err
	}
	var v ghStackView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, fmt.Errorf("reading gh stack view: %w", err)
	}
	return &v, nil
}

// ghStackForBranch asks gh whether branch is a member of a stack. Local
// gh-stack state is written per git-dir, so a linked worktree can hold a
// stack the repo-root file gt reads does not; gh resolves that path itself.
// A view only answers for its current branch, so anything else is a miss.
func ghStackForBranch(branch string) (trackedStack, bool) {
	if branch == "" {
		return trackedStack{}, false
	}
	v, err := ghStackViewFn()
	if err != nil || v == nil || v.Trunk == "" {
		return trackedStack{}, false
	}
	member := false
	for _, b := range v.Branches {
		if b.Name == branch {
			member = true
			break
		}
	}
	if !member {
		return trackedStack{}, false
	}
	ts := trackedStack{Trunk: trackedBranch{Branch: v.Trunk}}
	for i, b := range v.Branches {
		if b.Name == "" {
			continue
		}
		if i == 0 {
			// branches[0].base is the trunk tip this stack was built on.
			ts.Trunk.Head = b.Base
		}
		ts.Branches = append(ts.Branches, trackedBranch{Branch: b.Name, Base: b.Base})
	}
	return ts, true
}

// adoptGhStack consults `gh stack view` when local state does not place the
// current branch in a stack. It only runs for a branch absent from every local
// stack, so a local definition is never overridden. The adopted stack is
// appended to repo.Stacks and later persisted, healing the repo-root file.
func adoptGhStack(repo *repoStackState, current string) {
	if repo == nil {
		return
	}
	if _, ok := findStackForBranch(repo, current); ok {
		return
	}
	ts, ok := ghStackForBranch(current)
	if !ok {
		return
	}
	repo.Stacks = append(repo.Stacks, resolvedStack{
		trackedStack: ts,
		Sources:      []string{"gh stack view"},
	})
}
