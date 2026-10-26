package main

import (
	"slices"
	"testing"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "''"},
		{"gt", "gt"},
		{"07-31-add_the_login_form", "07-31-add_the_login_form"},
		{"--no-trunk", "--no-trunk"},
		{"https://github.com/hSATAC/gtstack", "https://github.com/hSATAC/gtstack"},
		{"a b", "'a b'"},
		{"Add the login form", "'Add the login form'"},
		{"it's", `'it'\''s'`},
		{"$(rm -rf /)", `'$(rm -rf /)'`},
		{"a;b", "'a;b'"},
		// Non-ASCII is not in the alnum range, so it takes the quoted path.
		{"修正", "'修正'"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBranchNameFrom(t *testing.T) {
	tests := []struct {
		msg, date, want string
	}{
		{"Add the login form", "07-31", "07-31-add_the_login_form"},
		{"CamelCase", "07-31", "07-31-camelcase"},
		{"v2 API", "07-31", "07-31-v2_api"},
		// Runs of punctuation collapse into a single separator.
		{"Fix   multiple   spaces", "01-02", "01-02-fix_multiple_spaces"},
		{"a---b", "01-02", "01-02-a_b"},
		// Leading separators never open the slug, trailing ones are trimmed.
		{"   leading", "01-02", "01-02-leading"},
		{"trailing!!!", "01-02", "01-02-trailing"},
		{"...both...", "01-02", "01-02-both"},
		// A message with nothing usable still has to yield a valid branch name.
		{"", "01-02", "01-02-branch"},
		{"!!!", "01-02", "01-02-branch"},
		// Non-ASCII drops out, but the ASCII around it survives.
		{"修正 bug", "01-02", "01-02-bug"},
	}
	for _, tt := range tests {
		if got := branchNameFrom(tt.msg, tt.date); got != tt.want {
			t.Errorf("branchNameFrom(%q, %q) = %q, want %q", tt.msg, tt.date, got, tt.want)
		}
	}
}

func TestJoinMessage(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"Subject"}, "Subject"},
		{[]string{"Subject", "Body"}, "Subject\n\nBody"},
		{[]string{"Subject", "Body", "More"}, "Subject\n\nBody\n\nMore"},
	}
	for _, tt := range tests {
		if got := joinMessage(tt.in); got != tt.want {
			t.Errorf("joinMessage(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseWorktreeBranchPaths(t *testing.T) {
	out := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\nworktree /repo-feat\nHEAD def\nbranch refs/heads/feat/a\n\nworktree /repo-detach\nHEAD ghi\ndetached\n"
	got := parseWorktreeBranchPaths(out)
	if got["main"] != "/repo" || got["feat/a"] != "/repo-feat" {
		t.Fatalf("parseWorktreeBranchPaths = %#v", got)
	}
	if _, ok := got[""]; ok {
		t.Fatal("detached HEAD should not map an empty branch")
	}
}

func TestGitFactsFromOutputs(t *testing.T) {
	resetProcessCaches(t)
	refs := "main\t111\torigin/main\t\t111\t1 second ago\tInitial commit\nfeature\t222\torigin/feature\t\t222\t2 seconds ago\tAdd feature\n"
	porcelain := "" +
		"worktree /repo\n" +
		"HEAD abc\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repo-wt\n" +
		"HEAD def\n" +
		"branch refs/heads/feature\n"
	facts := gitFactsFromOutputs(refs, porcelain, "feature")
	if facts.heads["main"] != "111" || facts.heads["feature"] != "222" || len(facts.heads) != 2 {
		t.Fatalf("heads = %#v, want main=111 feature=222", facts.heads)
	}
	if facts.worktrees["main"] != "/repo" || facts.worktrees["feature"] != "/repo-wt" || len(facts.worktrees) != 2 {
		t.Fatalf("worktrees = %#v, want main=/repo feature=/repo-wt", facts.worktrees)
	}
	if facts.current != "feature" {
		t.Fatalf("current = %q, want feature", facts.current)
	}
	if len(facts.locals) != 2 ||
		facts.locals[0].name != "main" || facts.locals[0].upstream != "origin/main" || facts.locals[0].gone ||
		facts.locals[1].name != "feature" || facts.locals[1].upstream != "origin/feature" || facts.locals[1].gone {
		t.Fatalf("locals = %#v", facts.locals)
	}
	if facts.commits["main"] != (branchCommit{short: "111", when: "1 second ago", subject: "Initial commit"}) ||
		facts.commits["feature"] != (branchCommit{short: "222", when: "2 seconds ago", subject: "Add feature"}) {
		t.Fatalf("commits = %#v", facts.commits)
	}
}

func resetProcessCaches(t *testing.T) {
	t.Helper()
	cachedFacts = GitFacts{}
	invalidateCatalogCache()
	t.Cleanup(func() {
		cachedFacts = GitFacts{}
		invalidateCatalogCache()
	})
}

func TestOriginFetchPlan_UsesSnapshotExists(t *testing.T) {
	names := []string{"main", "a", "gone"}
	refs := map[string]RemoteRef{
		"main": {Branch: "main", Exists: true, RemoteSHA: "111"},
		"a":    {Branch: "a", Exists: true, RemoteSHA: "222"},
		"gone": {Branch: "gone", Exists: false},
	}
	fetch, gone := originFetchPlan(names, refs)
	if len(fetch) != 2 || fetch[0] != "main" || fetch[1] != "a" {
		t.Fatalf("fetch = %v, want [main a]", fetch)
	}
	if len(gone) != 1 || gone[0] != "gone" {
		t.Fatalf("gone = %v, want [gone]", gone)
	}
}

func TestParseLsRemoteHeads(t *testing.T) {
	out := "abc123\trefs/heads/main\ndef456\trefs/heads/chore/extract-shared-utilities\n"
	got := parseLsRemoteHeads(out)
	want := []string{"main", "chore/extract-shared-utilities"}
	if !slices.Equal(got, want) {
		t.Fatalf("parseLsRemoteHeads = %#v, want %#v", got, want)
	}
	if got := parseLsRemoteHeads(""); len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}
