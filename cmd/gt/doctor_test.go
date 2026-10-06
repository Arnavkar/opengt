package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckStackRemoteSkipsMissingAndContinues(t *testing.T) {
	stack := trackedStack{
		Trunk: trackedBranch{Branch: "main"},
		Branches: []trackedBranch{
			{Branch: "no-pr"},
			{Branch: "has-pr"},
		},
	}
	pr := pullRequest{
		Number: 7, State: "OPEN", HeadRefName: "has-pr", BaseRefName: "wrong-base",
	}
	issues, skipped := checkStackRemoteWithPRs(stack, doctorPullRequests{
		byNumber: map[int]pullRequest{7: pr},
		byHead:   map[string][]pullRequest{"has-pr": {pr}},
	})
	if !skipped {
		t.Fatal("missing PR was not reported as skipped")
	}
	if len(issues) != 1 || issues[0].Code != "PR_BASE_MISMATCH" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestCheckStackRemoteMatchesTrackedNumber(t *testing.T) {
	stack := trackedStack{
		Trunk: trackedBranch{Branch: "main"},
		Branches: []trackedBranch{{
			Branch:      "reused",
			PullRequest: &pullRequestRef{Number: 5},
		}},
	}
	old := pullRequest{
		Number: 5, State: "MERGED", HeadRefName: "reused", BaseRefName: "main",
	}
	current := pullRequest{
		Number: 9, State: "OPEN", HeadRefName: "reused", BaseRefName: "main",
	}
	issues, skipped := checkStackRemoteWithPRs(stack, doctorPullRequests{
		byNumber: map[int]pullRequest{5: old, 9: current},
		byHead:   map[string][]pullRequest{"reused": {old, current}},
	})
	if skipped || len(issues) != 1 || issues[0].Code != "PR_MERGED" {
		t.Fatalf("issues=%#v skipped=%v", issues, skipped)
	}
}

func TestDoctorRemoteFailureExit(t *testing.T) {
	if doctorExitFrom([]stackIssue{{
		Code: "REMOTE_CHECK_FAILED", Severity: "error",
	}}) != exitDoctorDependency {
		t.Fatal("remote failure must use dependency exit")
	}
}

func TestLegacyCatalogPaths(t *testing.T) {
	dir := t.TempDir()
	if got := legacyCatalogPaths(dir); len(got) != 0 {
		t.Fatalf("no worktrees: %v", got)
	}
	wt := filepath.Join(dir, "worktrees", "feature-x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ghStackCompat.StateFileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := legacyCatalogPaths(dir)
	if len(got) != 1 || got[0] != filepath.Join(wt, ghStackCompat.StateFileName) {
		t.Fatalf("want the per-worktree catalog, got %v", got)
	}
}
