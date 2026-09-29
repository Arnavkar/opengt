package main

import (
	"slices"
	"testing"
)

func TestSubmitArgs(t *testing.T) {
	tests := []struct {
		name          string
		edit, publish bool
		want          []string
	}{
		// The bare `gt submit` must not open the editor: Graphite's does not,
		// and matching that is the point of the command.
		{"default", false, false, []string{"stack", "submit", "--auto"}},
		{"publish", false, true, []string{"stack", "submit", "--auto", "--open"}},
		{"edit", true, false, []string{"stack", "submit"}},
		{"edit and publish", true, true, []string{"stack", "submit", "--open"}},
	}
	for _, tt := range tests {
		if got := submitArgs(tt.edit, tt.publish); !slices.Equal(got, tt.want) {
			t.Errorf("%s: submitArgs(%v, %v) = %q, want %q", tt.name, tt.edit, tt.publish, got, tt.want)
		}
	}
}

func TestFailedPushBranch(t *testing.T) {
	stderr := "✗ failed to push chore/extract-shared-utilities: failed to run git: error: failed to push some refs to 'github.com:holly-revamp/holly.git'\n"
	if got := failedPushBranch(stderr); got != "chore/extract-shared-utilities" {
		t.Errorf("failedPushBranch = %q", got)
	}
	if !swallowedPushError(stderr) {
		t.Error("expected swallowedPushError")
	}
	if got := failedPushBranch("error: failed to push some refs to 'github.com:org/repo.git'\n"); got != "" {
		t.Errorf("generic git line should not parse a branch, got %q", got)
	}
	if swallowedPushError("current branch is not part of a stack") {
		t.Error("unrelated submit error must not look swallowed")
	}
}

func TestIgnorableStackSyncError(t *testing.T) {
	if !ignorableStackSyncError(`fatal: cannot force update the branch 'main' used by worktree at '/repo'`) {
		t.Error("worktree force-update should be ignorable")
	}
	if !ignorableStackSyncError(`current branch "main" is not part of a stack`) {
		t.Error("not on a stack should be ignorable")
	}
	if ignorableStackSyncError("rebase conflict") {
		t.Error("a real sync failure must not be ignored")
	}
}

func TestSlug(t *testing.T) {
	tests := []struct{ in, want string }{
		{"mybranch", "mybranch"},
		{"fix the login bug", "fix_the_login_bug"},
		{"  trim   me  ", "trim_me"},
		{"", "branch"},
		{"!!!", "branch"},
	}
	for _, tt := range tests {
		if got := slug(tt.in); got != tt.want {
			t.Errorf("slug(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
