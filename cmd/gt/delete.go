package main

import (
	"fmt"
	"os"
)

func cmdDelete(args []string) error {
	fs := newFlags("delete [branch]")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("gt delete takes at most one branch")
	}
	if fs.NArg() == 0 {
		return deleteInteractive()
	}
	return deleteBranch(fs.Arg(0))
}

func deleteInteractive() error {
	if !(isTerminal(os.Stdin) && isTerminal(os.Stderr)) {
		return fmt.Errorf("gt delete needs a branch; run in a terminal to pick one")
	}
	st, err := loadForestState()
	if err != nil {
		return err
	}
	current, err := currentBranch()
	if err != nil {
		return err
	}
	rows := deleteRows(st, current)
	if len(rows) == 0 {
		return fmt.Errorf("no branches to delete")
	}
	chosen, err := pickOne(rows, deleteBranchPrompt, "delete cancelled")
	if err != nil {
		return err
	}
	return deleteBranch(chosen.branch)
}

func deleteBranch(name string) error {
	if name == "" {
		return fmt.Errorf("gt delete needs a branch")
	}
	if trunkNames()[name] {
		return fmt.Errorf("cannot delete trunk %q", name)
	}
	if !isLocalBranch(name) {
		return fmt.Errorf("no local branch %q", name)
	}
	if err := validatePaused(); err != nil {
		return err
	}
	if err := validateWorktreeForBranch(name); err != nil {
		return err
	}

	pos, st, err := requireStackPosition(name)
	if err != nil {
		return err
	}

	var descendants []trackedBranch
	parent := pos.parent
	if s, ok := stackContaining(st, name); ok {
		p, rest, found := descendantsAfter(s, name)
		if found {
			parent, descendants = p, rest
		}
	}
	for _, d := range descendants {
		if err := validateWorktreeForBranch(d.Branch); err != nil {
			return err
		}
	}

	if len(descendants) > 0 {
		oldSHA, err := branchHead(name)
		if err != nil {
			return err
		}
		if err := rebaseDescendantsOnto(descendants, parent, oldSHA); err != nil {
			return err
		}
	}

	current, err := currentBranch()
	if err != nil {
		current = ""
	}
	fallback := parent
	if fallback == "" {
		fallback = fallbackTrunk(trunkNames())
	}
	if err := deleteLocalBranch(name, current, fallback); err != nil {
		return err
	}
	if !pos.inStack {
		return nil
	}
	return dropBranchesFromStackFiles(map[string]bool{name: true})
}
