package main

import (
	"errors"
	"sync"
)

// loadRemoteRefsFn is the refs adapter LoadRemoteSnapshot calls. It is a
// package-level var (initialized to the real LoadRemoteRefs from remote_refs.go)
// so tests can swap in a no-network fake. Keeping the real wiring here means
// the production path uses the genuine adapter the moment remote_refs.go is
// linked in; nothing in snapshot.go hardcodes the git shell-out.
var loadRemoteRefsFn = LoadRemoteRefs

// batchPRSnapshotFn is the PR adapter LoadRemoteSnapshot calls. The real
// implementation constructs a GitHubClient and runs one batched GraphQL read;
// tests swap in a fake to avoid the network. heads is the list of branch
// names to query (the same branches refs were requested for).
var batchPRSnapshotFn = func(known map[string]int, heads []string) (map[string]PRSnapshot, error) {
	c, err := NewGitHubClient()
	if err != nil {
		return nil, err
	}
	return c.BatchPRSnapshot(known, heads)
}

// LoadRemoteSnapshot loads git refs and GitHub PRs concurrently and returns a
// single RemoteSnapshot. Per refactor-plan.md decision #6, the two reads run
// in goroutines so the latency floor is max(GitRTT, GitHubRTT) rather than
// their sum.
//
// Error policy (binding — see snapshot_test.go):
//
//   - refs fail: refs are essential to any downstream plan, so return
//     (nil, err). If PRs also failed, both errors are joined with errors.Join
//     so neither source is hidden ("both errors surface").
//   - PRs fail but refs succeed: return (snapshot, err) with snapshot.PRs nil.
//     The caller decides whether to proceed with refs alone (e.g. a sync that
//     only needs ref state) or treat the PR failure as fatal.
//   - both succeed: return (snapshot, nil) with both populated.
//
// RemoteStack is intentionally left nil; the native stack object is loaded
// elsewhere (stack_remote.go / submit) when needed. Keeping this focused on
// refs+PRs preserves the concurrent-read boundary and avoids coupling the
// snapshot to the stack-object adapter.
func LoadRemoteSnapshot(branches []string, knownPRs map[string]int) (*RemoteSnapshot, error) {
	var (
		refs    RemoteRefSnapshot
		prs     map[string]PRSnapshot
		refsErr error
		prsErr  error
		wg      sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		refs, refsErr = loadRemoteRefsFn(branches)
	}()
	go func() {
		defer wg.Done()
		prs, prsErr = batchPRSnapshotFn(knownPRs, branches)
	}()
	wg.Wait()

	if refsErr != nil {
		// Refs are essential: no usable snapshot without them. If PRs also
		// failed, join both so the caller sees every error source.
		if prsErr != nil {
			return nil, errors.Join(refsErr, prsErr)
		}
		return nil, refsErr
	}

	snap := &RemoteSnapshot{
		Refs: refs,
		PRs:  prs,
	}
	if prsErr != nil {
		// Refs ok, PRs failed: hand back a partial snapshot and a non-nil
		// error so the caller can choose to proceed (sync) or bail (submit).
		snap.PRs = nil
		return snap, prsErr
	}
	return snap, nil
}
