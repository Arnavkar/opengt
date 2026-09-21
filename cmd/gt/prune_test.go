package main

import (
	"reflect"
	"testing"
)

func TestParseLocalBranches(t *testing.T) {
	got := parseLocalBranches("main\torigin/main\t\nfeat/a\torigin/feat/a\t[gone]\nlocal\t\t\nfeat/b\torigin/feat/b\t[ahead 1]\n")
	want := []localBranch{
		{name: "main", upstream: "origin/main", local: true},
		{name: "feat/a", upstream: "origin/feat/a", gone: true, local: true},
		{name: "local", local: true},
		{name: "feat/b", upstream: "origin/feat/b", local: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseLocalBranches = %#v, want %#v", got, want)
	}
}

func TestStaleLocalsGoneUpstream(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "main", upstream: "origin/main", local: true},
		{name: "feat/a", upstream: "origin/feat/a", gone: true, local: true},
		{name: "local", local: true},
	}, nil, map[string]bool{"main": true})
	if len(got) != 1 || got[0].name != "feat/a" || got[0].reason != "origin/feat/a is gone" {
		t.Errorf("staleLocals = %#v", got)
	}
}

func TestStaleLocalsMergedAndClosedPR(t *testing.T) {
	branches := []localBranch{
		{name: "merged-one", upstream: "origin/merged-one", local: true},
		{name: "closed-one", upstream: "origin/closed-one", local: true},
		{name: "open-one", upstream: "origin/open-one", local: true},
	}
	prs := []pullRequest{
		{Number: 1, State: "MERGED", HeadRefName: "merged-one"},
		{Number: 2, State: "CLOSED", HeadRefName: "closed-one"},
		{Number: 3, State: "OPEN", HeadRefName: "open-one"},
	}
	got := staleLocals(branches, prs, map[string]bool{"main": true})
	if len(got) != 2 {
		t.Fatalf("got %d stale, want 2: %#v", len(got), got)
	}
	if got[0].reason != "PR #1 merged" || got[1].reason != "PR #2 closed" {
		t.Errorf("reasons = %q, %q", got[0].reason, got[1].reason)
	}
}

func TestStaleLocalsReusedBranchWithOpenPR(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "feat/reused", local: true},
	}, []pullRequest{
		{Number: 4, State: "CLOSED", HeadRefName: "feat/reused"},
		{Number: 5, State: "OPEN", HeadRefName: "feat/reused"},
	}, map[string]bool{"main": true})
	if len(got) != 0 {
		t.Fatalf("reused branch was stale: %#v", got)
	}
}

func TestStaleLocalsTrackedMergedButReusedOpen(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "feat/reused", trackedPR: 5, mergedPR: 5, local: true},
	}, []pullRequest{
		{Number: 5, State: "MERGED", HeadRefName: "feat/reused"},
		{Number: 9, State: "OPEN", HeadRefName: "feat/reused"},
	}, map[string]bool{"main": true})
	if len(got) != 0 {
		t.Fatalf("reused branch was stale: %#v", got)
	}
}

func TestStaleLocalsTrackedMergedNoOpen(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "feat/done", trackedPR: 5, mergedPR: 5, local: true},
	}, []pullRequest{
		{Number: 5, State: "MERGED", HeadRefName: "feat/done"},
	}, map[string]bool{"main": true})
	if len(got) != 1 || got[0].reason != "PR #5 merged" {
		t.Fatalf("staleLocals = %#v", got)
	}
}

func TestStaleLocalsTwoClosedPrefersTracked(t *testing.T) {
	prs := []pullRequest{
		{Number: 4, State: "CLOSED", HeadRefName: "feat/reused"},
		{Number: 5, State: "MERGED", HeadRefName: "feat/reused"},
	}
	got := staleLocals([]localBranch{
		{name: "feat/reused", trackedPR: 5, local: true},
	}, prs, map[string]bool{"main": true})
	if len(got) != 1 || got[0].reason != "PR #5 merged" {
		t.Fatalf("tracked staleLocals = %#v", got)
	}
	got = staleLocals([]localBranch{
		{name: "feat/reused", local: true},
	}, prs, map[string]bool{"main": true})
	if len(got) != 1 {
		t.Fatalf("untracked staleLocals = %#v", got)
	}
}

func TestStaleLocalsPRFailureOnlyUsesGoneUpstream(t *testing.T) {
	got := staleLocalsWithPRs([]localBranch{
		{name: "feat/done", trackedPR: 5, mergedPR: 5, local: true},
		{name: "feat/gone", upstream: "origin/feat/gone", gone: true, local: true},
	}, nil, map[string]bool{"main": true}, false)
	if len(got) != 1 || got[0].name != "feat/gone" {
		t.Fatalf("staleLocalsWithPRs = %#v", got)
	}
}

func TestStaleLocalsMatchesUpstreamBranchName(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "local-name", upstream: "origin/feat/api", local: true},
	}, []pullRequest{
		{Number: 9, State: "MERGED", HeadRefName: "feat/api"},
	}, map[string]bool{"main": true})
	if len(got) != 1 || got[0].name != "local-name" || got[0].reason != "PR #9 merged" {
		t.Errorf("staleLocals = %#v", got)
	}
}

func TestStaleLocalsPrefersPRReasonWhenGone(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "feat/a", upstream: "origin/feat/a", gone: true, local: true},
	}, []pullRequest{
		{Number: 4, State: "MERGED", HeadRefName: "feat/a"},
	}, map[string]bool{"main": true})
	if len(got) != 1 || got[0].reason != "PR #4 merged" {
		t.Errorf("staleLocals = %#v", got)
	}
}

func TestStaleLocalsSkipsTrunkEvenIfGone(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "main", upstream: "origin/main", gone: true},
	}, nil, map[string]bool{"main": true})
	if len(got) != 0 {
		t.Errorf("trunk was stale: %#v", got)
	}
}

func TestUpstreamBranch(t *testing.T) {
	if got := upstreamBranch("origin/feat/a"); got != "feat/a" {
		t.Errorf("upstreamBranch(origin/feat/a) = %q", got)
	}
}

func TestStaleLocalsClosedPRWithoutUpstream(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "chore/drop-hybrid-retrieval-flag-types", local: true},
	}, []pullRequest{
		{Number: 1973, State: "CLOSED", HeadRefName: "chore/drop-hybrid-retrieval-flag-types"},
	}, map[string]bool{"main": true})
	if len(got) != 1 || got[0].reason != "PR #1973 closed" {
		t.Errorf("staleLocals = %#v", got)
	}
}

func TestStaleLocalsGhostStackBranch(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "chore/clear-old-retrieval-code", mergedPR: 1972},
	}, nil, map[string]bool{"main": true})
	if len(got) != 1 || got[0].reason != "PR #1972 merged" {
		t.Errorf("staleLocals = %#v", got)
	}
}

func TestStaleLocalsUnpushedLocalKept(t *testing.T) {
	got := staleLocals([]localBranch{
		{name: "wip", local: true},
	}, nil, map[string]bool{"main": true})
	if len(got) != 0 {
		t.Errorf("unpushed local was stale: %#v", got)
	}
}

func TestStripStackBranches(t *testing.T) {
	in := []byte(`{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"}, "branches": [{"branch": "gone"}, {"branch": "keep"}]}
  ]
}`)
	out, changed, err := stripStackBranches(in, map[string]bool{"gone": true})
	if err != nil || !changed {
		t.Fatalf("stripStackBranches: changed=%v err=%v", changed, err)
	}
	st := stateFrom(t, string(out))
	if len(st.Stacks) != 1 || len(st.Stacks[0].Branches) != 1 || st.Stacks[0].Branches[0].Branch != "keep" {
		t.Fatalf("remaining = %#v", st.Stacks)
	}
}

func TestStalePickRows(t *testing.T) {
	got := stalePickRows([]staleBranch{
		{name: "feat/a", reason: "PR #1 merged"},
		{name: "feat/b", reason: "origin/feat/b is gone"},
	})
	if len(got) != 2 || got[0].branch != "feat/a" || got[0].detail != "PR #1 merged" {
		t.Fatalf("stalePickRows = %#v", got)
	}
	if got[1].text != "feat/b  origin/feat/b is gone" {
		t.Errorf("text = %q", got[1].text)
	}
}

// TestOrderForDeletionBottomUp: stale branches delete bottom-up within a
// stack so each parent is removed (and its upstack rebased) before the
// branches above it. The input order is the random order listStaleCandidates
// produces.
func TestOrderForDeletionBottomUp(t *testing.T) {
	depth := map[string]int{"bottom": 0, "mid": 1, "top": 2}
	got := sortByStackDepth([]string{"top", "bottom", "mid"}, depth)
	if len(got) != 3 || got[0] != "bottom" || got[1] != "mid" || got[2] != "top" {
		t.Fatalf("sortByStackDepth = %v, want [bottom mid top]", got)
	}
}
