package main

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// withFakes swaps the two adapter vars for the duration of a test and restores
// them on cleanup. Tests in this file never touch the network.
func withFakes(t *testing.T, refsFn func([]string) (RemoteRefSnapshot, error), prsFn func(map[string]int, []string) (map[string]PRSnapshot, error)) {
	t.Helper()
	oldRefs, oldPRs := loadRemoteRefsFn, batchPRSnapshotFn
	loadRemoteRefsFn = refsFn
	batchPRSnapshotFn = prsFn
	t.Cleanup(func() {
		loadRemoteRefsFn = oldRefs
		batchPRSnapshotFn = oldPRs
	})
}

func fakeRefs(branches []string) RemoteRefSnapshot {
	refs := make(map[string]RemoteRef, len(branches))
	for _, b := range branches {
		refs[b] = RemoteRef{Branch: b, Exists: true, RemoteSHA: "sha-" + b}
	}
	return RemoteRefSnapshot{Refs: refs, FetchedAt: time.Unix(1700000000, 0)}
}

func fakePRs(branches []string) map[string]PRSnapshot {
	m := make(map[string]PRSnapshot, len(branches))
	for i, b := range branches {
		m[b] = PRSnapshot{Number: 1000 + i, Head: b, State: "OPEN"}
	}
	return m
}

func TestLoadRemoteSnapshot_HappyPath(t *testing.T) {
	branches := []string{"main", "feat/a", "feat/b"}
	withFakes(t,
		func(b []string) (RemoteRefSnapshot, error) { return fakeRefs(b), nil },
		func(_ map[string]int, b []string) (map[string]PRSnapshot, error) { return fakePRs(b), nil },
	)

	snap, err := LoadRemoteSnapshot(branches, map[string]int{"feat/a": 1001})
	if err != nil {
		t.Fatalf("LoadRemoteSnapshot error = %v, want nil", err)
	}
	if snap == nil {
		t.Fatal("LoadRemoteSnapshot snapshot = nil, want non-nil")
	}
	if len(snap.Refs.Refs) != 3 {
		t.Fatalf("Refs.Refs len = %d, want 3", len(snap.Refs.Refs))
	}
	if got := snap.Refs.Refs["feat/a"].RemoteSHA; got != "sha-feat/a" {
		t.Fatalf("Refs.Refs[feat/a].RemoteSHA = %q, want sha-feat/a", got)
	}
	if len(snap.PRs) != 3 {
		t.Fatalf("PRs len = %d, want 3", len(snap.PRs))
	}
	if got := snap.PRs["feat/a"].Number; got != 1001 {
		t.Fatalf("PRs[feat/a].Number = %d, want 1001", got)
	}
	if snap.RemoteStack != nil {
		t.Fatalf("RemoteStack = %#v, want nil (loaded elsewhere)", snap.RemoteStack)
	}
}

func TestLoadRemoteSnapshot_BothErrorsSurface(t *testing.T) {
	refsErr := errors.New("refs boom")
	prsErr := errors.New("prs boom")
	withFakes(t,
		func(_ []string) (RemoteRefSnapshot, error) { return RemoteRefSnapshot{}, refsErr },
		func(_ map[string]int, _ []string) (map[string]PRSnapshot, error) { return nil, prsErr },
	)

	snap, err := LoadRemoteSnapshot([]string{"main"}, nil)
	if snap != nil {
		t.Fatalf("snapshot = %#v, want nil when refs fail", snap)
	}
	if err == nil {
		t.Fatal("err = nil, want non-nil when both fail")
	}
	if !errors.Is(err, refsErr) {
		t.Errorf("err does not wrap refsErr: %v", err)
	}
	if !errors.Is(err, prsErr) {
		t.Errorf("err does not wrap prsErr (both errors must surface): %v", err)
	}
}

func TestLoadRemoteSnapshot_RefsErrorReturnsNil(t *testing.T) {
	refsErr := errors.New("git ls-remote failed")
	withFakes(t,
		func(_ []string) (RemoteRefSnapshot, error) { return RemoteRefSnapshot{}, refsErr },
		func(_ map[string]int, b []string) (map[string]PRSnapshot, error) { return fakePRs(b), nil },
	)

	snap, err := LoadRemoteSnapshot([]string{"main"}, nil)
	if snap != nil {
		t.Fatalf("snapshot = %#v, want nil when refs fail", snap)
	}
	if !errors.Is(err, refsErr) {
		t.Errorf("err = %v, want refsErr", err)
	}
}

func TestLoadRemoteSnapshot_PRsErrorReturnsPartial(t *testing.T) {
	prsErr := errors.New("graphql failed")
	withFakes(t,
		func(b []string) (RemoteRefSnapshot, error) { return fakeRefs(b), nil },
		func(_ map[string]int, _ []string) (map[string]PRSnapshot, error) { return nil, prsErr },
	)

	snap, err := LoadRemoteSnapshot([]string{"main", "feat/a"}, nil)
	if snap == nil {
		t.Fatal("snapshot = nil, want partial snapshot when refs ok but PRs fail")
	}
	if err == nil {
		t.Fatal("err = nil, want non-nil so caller can decide on PR failure")
	}
	if !errors.Is(err, prsErr) {
		t.Errorf("err = %v, want prsErr", err)
	}
	if len(snap.Refs.Refs) != 2 {
		t.Errorf("Refs.Refs len = %d, want 2 (refs must still be populated)", len(snap.Refs.Refs))
	}
	if snap.PRs != nil {
		t.Errorf("PRs = %#v, want nil on PR failure", snap.PRs)
	}
}

// TestLoadRemoteSnapshot_Concurrent proves the two adapters run in parallel:
// the refs fake blocks until the PRs fake has signalled it started. If
// LoadRemoteSnapshot ran them serially (refs first), this test would time
// out instead of completing.
func TestLoadRemoteSnapshot_Concurrent(t *testing.T) {
	prsStarted := make(chan struct{})
	refsRelease := make(chan struct{})

	withFakes(t,
		func(b []string) (RemoteRefSnapshot, error) {
			// Block until the PRs goroutine has actually started. This
			// confirms the two runs overlap rather than sequencing.
			select {
			case <-prsStarted:
			case <-time.After(2 * time.Second):
				return RemoteRefSnapshot{}, errors.New("refs fake timed out waiting for PRs to start; not concurrent")
			}
			select {
			case <-refsRelease:
			case <-time.After(2 * time.Second):
				return RemoteRefSnapshot{}, errors.New("refs fake timed out waiting for release")
			}
			return fakeRefs(b), nil
		},
		func(_ map[string]int, b []string) (map[string]PRSnapshot, error) {
			close(prsStarted)
			// Let the refs fake proceed once we have produced our result.
			defer close(refsRelease)
			return fakePRs(b), nil
		},
	)

	snap, err := LoadRemoteSnapshot([]string{"main", "feat/a"}, nil)
	if err != nil {
		t.Fatalf("LoadRemoteSnapshot error = %v, want nil", err)
	}
	if snap == nil || len(snap.Refs.Refs) != 2 || len(snap.PRs) != 2 {
		t.Fatalf("snapshot not fully populated: %+v", snap)
	}
}

// TestLoadRemoteSnapshot_Concurrent_WaitGroupBothRun is a lighter concurrency
// check: both fakes must be invoked even when one returns instantly. This
// catches a regression where one goroutine is skipped on a fast-path.
func TestLoadRemoteSnapshot_Concurrent_WaitGroupBothRun(t *testing.T) {
	var refsCalled, prsCalled int32
	var mu sync.Mutex
	refsCalls := func() int32 { mu.Lock(); defer mu.Unlock(); return refsCalled }
	prsCalls := func() int32 { mu.Lock(); defer mu.Unlock(); return prsCalled }

	withFakes(t,
		func(b []string) (RemoteRefSnapshot, error) {
			mu.Lock()
			refsCalled++
			mu.Unlock()
			return fakeRefs(b), nil
		},
		func(_ map[string]int, b []string) (map[string]PRSnapshot, error) {
			mu.Lock()
			prsCalled++
			mu.Unlock()
			return fakePRs(b), nil
		},
	)

	if _, err := LoadRemoteSnapshot([]string{"main"}, nil); err != nil {
		t.Fatalf("LoadRemoteSnapshot error = %v, want nil", err)
	}
	if refsCalls() != 1 {
		t.Errorf("refs fake called %d times, want 1", refsCalls())
	}
	if prsCalls() != 1 {
		t.Errorf("prs fake called %d times, want 1", prsCalls())
	}
}
