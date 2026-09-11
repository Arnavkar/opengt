package main

import (
	"fmt"
)

// validateStackForMutation is the cheap pre-mutation check (not a full
// `gt doctor`): schema support, ambiguity, branch existence, worktree state,
// paused gh-stack op, and ancestry. All local — zero network. Returns nil
// when the branch's stack is safe to mutate. See contracts.go and
// docs/refactor-plan.md decision #10.
func validateStackForMutation(repo *repoStackState, branch string) error {
	if err := validateSchema(repo); err != nil {
		return err
	}
	if err := validateAmbiguity(repo, branch); err != nil {
		return err
	}
	stack, ok := findStackForBranch(repo, branch)
	if !ok {
		return nil
	}
	if err := validateBranchExistence(stack, isLocalBranch); err != nil {
		return err
	}
	if err := validateWorktreeForBranch(branch); err != nil {
		return err
	}
	if err := validatePaused(); err != nil {
		return err
	}
	heads, _ := localBranchHeads()
	if bad := validateAncestry(stack.trackedStack, heads, isAncestor); len(bad) > 0 {
		first := bad[0]
		return fmt.Errorf(
			"%s does not contain parent %s\n"+
				"Run:\n  gt restack, or restore the missing commits\n"+
				"No changes were made.", first.branch, first.parent)
	}
	return nil
}

// ancestryIssue names one member whose parent is not its ancestor and whose
// cached base is also not its ancestor.
type ancestryIssue struct {
	branch string
	parent string
}

// validateAncestry walks the stack chain and reports members with broken
// ancestry. ancestorFn is injected so tests can substitute a fake without a
// git repo. A member is invalid only when both the parent and the cached base
// fail the ancestor test (mirrors doctor.go checkStackGitWithHeads).
func validateAncestry(stack trackedStack, heads map[string]string, ancestorFn func(string, string) bool) []ancestryIssue {
	var issues []ancestryIssue
	prev := stack.Trunk.Branch
	for _, b := range stack.Branches {
		if b.Branch == "" {
			continue
		}
		if _, ok := heads[b.Branch]; ok {
			if _, pok := heads[prev]; prev != "" && pok {
				parentOK := ancestorFn(prev, b.Branch)
				cacheOK := b.Base != "" && ancestorFn(b.Base, b.Branch)
				if !parentOK && !cacheOK {
					issues = append(issues, ancestryIssue{branch: b.Branch, parent: prev})
				}
			}
		}
		prev = b.Branch
	}
	return issues
}

// validateSchema returns an error when any source has an unsupported schema.
// reconcileSources already records UNSUPPORTED_SCHEMA issues; reuse them.
func validateSchema(repo *repoStackState) error {
	for _, iss := range repo.Issues {
		if iss.Code == "UNSUPPORTED_SCHEMA" {
			return fmt.Errorf("%s\nRun:\n  gt doctor\nNo changes were made.", iss.Message)
		}
	}
	return nil
}

// validateAmbiguity returns an error when the branch resolves to more than
// one stack, or when conflicting stack definitions are present anywhere in
// the repo state. Reuses errAmbiguous for the branch-specific case.
func validateAmbiguity(repo *repoStackState, branch string) error {
	if hits := repo.ambiguous(branch); len(hits) > 0 {
		return errAmbiguous(branch, hits)
	}
	if repo.hasConflicts() {
		for _, iss := range repo.Issues {
			switch iss.Code {
			case "CONFLICTING_STACK", "AMBIGUOUS_PREFIX", "AMBIGUOUS_MEMBERSHIP":
				return fmt.Errorf("%s\nRun:\n  gt doctor\nNo changes were made.", iss.Message)
			}
		}
	}
	return nil
}

// findStackForBranch returns the non-stale resolved stack that contains branch
// as a member.
func findStackForBranch(repo *repoStackState, branch string) (resolvedStack, bool) {
	for _, s := range repo.Stacks {
		if s.Stale {
			continue
		}
		for _, b := range s.Branches {
			if b.Branch == branch {
				return s, true
			}
		}
	}
	return resolvedStack{}, false
}

// validateBranchExistence errors when the stack trunk or any member is not a
// local branch. isLocal is injected so the pure decision is testable without
// git.
func validateBranchExistence(stack resolvedStack, isLocal func(string) bool) error {
	if stack.Trunk.Branch != "" && !isLocal(stack.Trunk.Branch) {
		return fmt.Errorf("trunk %q is not a local branch\nRun:\n  gt doctor\nNo changes were made.", stack.Trunk.Branch)
	}
	for _, b := range stack.Branches {
		if b.Branch == "" {
			continue
		}
		if !isLocal(b.Branch) {
			return fmt.Errorf("stack metadata references missing branch %q\nRun:\n  gt sync -d after the PR is merged, or gt doctor --repair\nNo changes were made.", b.Branch)
		}
	}
	return nil
}

// validateWorktreeForBranch is the git-backed worktree check. The branch is
// unsafe to mutate when it is checked out in a worktree that is not this one.
func validateWorktreeForBranch(branch string) error {
	current, err := currentBranch()
	if err != nil {
		current = ""
	}
	wt, err := worktreePathForBranch(branch)
	if err != nil {
		return nil
	}
	return validateWorktreeState(branch, current, wt)
}

// validateWorktreeState is the pure decision: a non-empty worktree path for a
// branch that is not the current branch means the branch is checked out
// elsewhere.
func validateWorktreeState(branch, current, wtPath string) error {
	if wtPath == "" {
		return nil
	}
	if branch == current {
		return nil
	}
	return fmt.Errorf("branch %q is checked out in another worktree (%s); cannot mutate it\nNo changes were made.", branch, wtPath)
}

// validatePaused is the git-backed paused-operation check.
func validatePaused() error {
	op, err := pausedOperation()
	if err != nil {
		return err
	}
	return validatePausedOp(op)
}

// validatePausedOp is the pure decision: a non-empty op means a rebase or
// modify is halted waiting for conflict resolution.
func validatePausedOp(op string) error {
	if op == "" {
		return nil
	}
	return fmt.Errorf("a gh stack %s is in progress; resolve it before mutating the stack\nRun:\n  gt doctor\nNo changes were made.", op)
}
