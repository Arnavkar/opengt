package main

import (
	"fmt"
	"os"
)

// executeSubmit owns submit after flag parsing: validate, load the snapshot
// (including the native stack), plan, then mutate. --native stays available
// only before any mutation.
func executeSubmit(opts SubmitOpts, native, edit, publish bool, extra []string) error {
	repo, err := loadRepoStacks()
	if err != nil {
		return err
	}
	current, err := currentBranch()
	if err != nil {
		return err
	}

	if verr := validateStackForMutation(repo, current); verr != nil {
		if native {
			return nativeSubmit(edit, publish, extra)
		}
		fmt.Fprintln(os.Stderr, verr)
		fmt.Fprintln(os.Stderr, "gt: hint — re-run with --native to delegate to gh stack submit")
		return verr
	}
	if native {
		return nativeSubmit(edit, publish, extra)
	}

	if !opts.Stack {
		if stack, ok := findStackForBranch(repo, current); ok {
			if above := upstackOf(stack.trackedStack, current); len(above) > 0 {
				if confirmSubmitUpstack(above) {
					opts.Stack = true
				}
			}
		}
	}

	branches, knownPRs := submitStackBranches(repo, current)
	stackID := ""
	if stack, ok := findStackForBranch(repo, current); ok {
		stackID = stack.ID
	}
	snap, snapErr := LoadSubmitSnapshot(branches, knownPRs, stackID)
	if snap == nil {
		return snapErr
	}
	if snapErr != nil {
		fmt.Fprintf(os.Stderr, "gt: could not load remote state (%v); proceeding with refs only\n", snapErr)
	}

	refreshStackSHAs(repo)
	plan, err := BuildSubmitPlan(repo, current, snap, opts)
	if err != nil {
		return err
	}

	if plan.IsNoOp() && !opts.Always {
		fmt.Fprintln(os.Stderr, "Stack already up to date")
		return nil
	}

	if opts.DryRun {
		printSubmitPlan(os.Stderr, plan)
		return nil
	}

	mut := MutationState{}

	if opts.Restack {
		moved, rerr := restackForSubmit(repo, current, opts.Force)
		if rerr != nil {
			return rerr
		}
		if moved {
			mut.GitMutated = true
			for i := range plan.Pushes {
				sha, err := branchHead(plan.Pushes[i].Branch)
				if err != nil {
					return err
				}
				plan.Pushes[i].LocalSHA = sha
			}
		}
	}

	if gerr := gateRemoteReplace(plan.Pushes, opts.Force); gerr != nil {
		return gerr
	}

	if len(plan.Pushes) > 0 {
		if perr := AtomicPush(plan.Pushes, PushOpts{
			Force:    opts.Force,
			NoVerify: opts.NoVerify,
			DryRun:   opts.DryRun,
		}); perr != nil {
			explainSwallowedPush("")
			return perr
		}
		mut.GitMutated = true
	}

	if cerr := createPRs(plan.Creates, opts.Draft); cerr != nil {
		return cerr
	}
	if len(plan.Creates) > 0 {
		mut.RemoteMutated = true
	}
	if werr := updatePRBases(plan.BaseUpdates); werr != nil {
		fmt.Fprintf(os.Stderr, "gt: warning: %v\n", werr)
	} else if len(plan.BaseUpdates) > 0 {
		mut.RemoteMutated = true
	}
	if werr := publishPRs(plan.PublishUpdates); werr != nil {
		fmt.Fprintf(os.Stderr, "gt: warning: %v\n", werr)
	} else if len(plan.PublishUpdates) > 0 {
		mut.RemoteMutated = true
	}
	if werr := disableAutoMerges(plan.AutoMergeDisables); werr != nil {
		fmt.Fprintf(os.Stderr, "gt: warning: %v\n", werr)
	} else if len(plan.AutoMergeDisables) > 0 {
		mut.RemoteMutated = true
	}

	if plan.StackUpdate != nil {
		client, cerr := NewStackRemoteClient()
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "gt: warning: stack remote: %v\n", cerr)
		} else {
			changed, serr := syncStackOrder(client, plan.StackUpdate.StackID, plan.StackUpdate.Numbers)
			if serr != nil {
				fmt.Fprintf(os.Stderr, "gt: warning: stack update: %v\n", serr)
			} else if changed {
				mut.RemoteMutated = true
			}
		}
	}

	if mut.GitMutated {
		refreshStackSHAs(repo)
		if perr := persistSyncState(repo); perr != nil {
			return perr
		}
	}
	return nil
}
