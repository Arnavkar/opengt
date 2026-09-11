package main

// pushArgs builds the argument vector for a single `git push --atomic` after
// the leading "push" subcommand. It is pure: no git invocation, so it can be
// unit-tested without a repository.
//
// Refspec rules:
//   - New branch (IsNew): plain refs/heads/<b>:refs/heads/<b>. No force, no
//     lease — a first push needs neither.
//   - Existing branch: refs/heads/<b>:refs/heads/<b> plus
//     --force-with-lease=<b>:<ExpectedOld>, where ExpectedOld is the SHA the
//     snapshot saw on origin.
//
// Force handling (decision #5): --force-with-lease is always applied per
// existing ref. When opts.Force is set, --force is added and the per-ref
// leases are dropped — --force wins and the push becomes unconditional. This
// is an explicit opt-in only; there is never an automatic fallback from a
// failed lease to unconditional --force. On lease failure the atomic push
// fails as a whole and no branch moves.
func pushArgs(refs []PushRef, opts PushOpts) []string {
	if len(refs) == 0 {
		return nil
	}
	args := []string{"--atomic", "origin"}
	for _, r := range refs {
		if !r.IsNew && !opts.Force {
			args = append(args, "--force-with-lease="+r.Branch+":"+r.ExpectedOld)
		}
	}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.NoVerify {
		args = append(args, "--no-verify")
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	for _, r := range refs {
		args = append(args, "refs/heads/"+r.Branch+":refs/heads/"+r.Branch)
	}
	return args
}

// AtomicPush runs one `git push --atomic` built from refs and opts. Empty
// refs is a no-op. On lease failure the entire push fails; no branch moves
// and there is no fallback to unconditional --force.
func AtomicPush(refs []PushRef, opts PushOpts) error {
	args := pushArgs(refs, opts)
	if args == nil {
		return nil
	}
	return run("git", append([]string{"push"}, args...)...)
}
