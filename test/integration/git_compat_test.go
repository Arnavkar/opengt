//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doctorJSON(t *testing.T, f *fixture, extra ...string) (map[string]any, result) {
	t.Helper()
	args := append([]string{"doctor", "--json"}, extra...)
	r := f.run(gtBin, args...)
	var payload map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &payload); err != nil {
		t.Fatalf("doctor --json: %v\nstdout:%s\nstderr:%s", err, r.stdout, r.stderr)
	}
	return payload, r
}

func issueCodes(payload map[string]any) []string {
	raw, _ := payload["issues"].([]any)
	var codes []string
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if c, _ := m["code"].(string); c != "" {
			codes = append(codes, c)
		}
	}
	return codes
}

func hasCode(payload map[string]any, code string) bool {
	for _, c := range issueCodes(payload) {
		if c == code {
			return true
		}
	}
	return false
}

func TestPlainGitCommitOnTop(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.write("layer-two.txt", "two, plus a plain git commit\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "plain git commit")

	if r := f.gt("log"); r.code != 0 {
		t.Fatalf("gt log after plain commit: %s", r.output())
	}
	payload, r := doctorJSON(t, f)
	if r.code != 0 {
		t.Fatalf("gt doctor exited %d, want 0 after a top-of-stack git commit\n%s\n%v", r.code, r.output(), issueCodes(payload))
	}
	f.gt("sync")
	if got := f.subject("layer-two"); got != "plain git commit" {
		t.Errorf("layer-two subject is %q", got)
	}
}

func TestPlainGitCommitOnMiddleNeedsRestack(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.layer("layer-three", "Add layer three")
	f.git("checkout", "--quiet", "layer-two")
	f.write("layer-two.txt", "two, plus a middle commit\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "plain git commit on middle")

	payload, r := doctorJSON(t, f)
	if r.code == 0 {
		t.Fatalf("gt doctor exited 0, want a restack warning\n%s", r.output())
	}
	if !hasCode(payload, "NEEDS_RESTACK") {
		t.Fatalf("expected NEEDS_RESTACK, got %v\n%s", issueCodes(payload), r.output())
	}
	if got := f.tracked(); len(got) != 3 || got[1] != "layer-two" {
		t.Fatalf("middle commit dropped stack membership: %q", got)
	}

	f.gt("restack", "-u")
	payload, r = doctorJSON(t, f)
	if r.code != 0 {
		t.Fatalf("gt doctor after restack exited %d (%v)\n%s", r.code, issueCodes(payload), r.output())
	}
	if f.git("rev-parse", "layer-two") != f.git("rev-parse", "layer-three^") {
		t.Errorf("layer-three was not restacked onto layer-two")
	}
}

func TestManualRebaseStaleMetadataRepair(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.git("checkout", "--quiet", "layer-one")
	f.write("extra.txt", "extra\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "--amend", "--no-edit")
	f.git("checkout", "--quiet", "layer-two")
	if r := f.run("git", "rebase", "layer-one"); r.code != 0 {
		t.Fatalf("git rebase: %s", r.output())
	}

	payload, r := doctorJSON(t, f)
	if !hasCode(payload, "STALE_BASE_SHA") {
		t.Fatalf("expected STALE_BASE_SHA after manual rebase, got %v\n%s", issueCodes(payload), r.output())
	}
	if hasCode(payload, "INVALID_ANCESTRY") {
		t.Fatalf("valid DAG reported as invalid: %v", issueCodes(payload))
	}

	repair := f.run(gtBin, "doctor", "--repair", "--yes", "--json")
	if repair.code > 1 {
		t.Fatalf("repair exited %d\n%s", repair.code, repair.output())
	}
	payload, r = doctorJSON(t, f)
	if hasCode(payload, "STALE_BASE_SHA") {
		t.Fatalf("STALE_BASE_SHA survived repair: %v\n%s", issueCodes(payload), r.output())
	}
}

func TestBrokenAncestryIsReportedAndNotRewritten(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	before := f.git("rev-parse", "layer-two")
	f.git("checkout", "--quiet", "layer-two")
	f.git("reset", "--hard", "--quiet", "main")

	payload, r := doctorJSON(t, f)
	if r.code == 0 {
		t.Fatalf("broken ancestry exited 0")
	}
	if !hasCode(payload, "INVALID_ANCESTRY") && !hasCode(payload, "NEEDS_RESTACK") {
		t.Fatalf("expected ancestry error, got %v\n%s", issueCodes(payload), r.output())
	}
	if !strings.Contains(r.stdout+r.stderr, "does not contain parent") {
		t.Errorf("did not say the child lacks its parent:\n%s", r.output())
	}

	f.run(gtBin, "doctor", "--repair", "--yes")
	if got := f.git("rev-parse", "layer-two"); got != f.git("rev-parse", "main") {
		t.Errorf("repair rewrote layer-two from reset main to %s (was %s)", got, before)
	}
}

func TestWorktreeStackDiscoveryAndConflicts(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.gt("trunk")

	linked := f.dir + "-worktree"
	f.git("worktree", "add", "--quiet", linked, "layer-two")
	worktree := &fixture{t: t, dir: linked, origin: f.origin}

	payload, r := doctorJSON(t, worktree)
	if r.code > 1 {
		t.Fatalf("doctor from linked worktree exited %d (%v)\n%s", r.code, issueCodes(payload), r.output())
	}

	// A conflicting order in a per-worktree state file must be ignored: gt
	// reads only the repo-root file, so the worktree cannot poison the view.
	conflict := []byte(`{
  "schemaVersion": 1,
  "stacks": [{
    "trunk": {"branch": "main"},
    "branches": [{"branch": "layer-two"}, {"branch": "layer-one"}]
  }]
}
`)
	gitDir := worktree.git("rev-parse", "--path-format=absolute", "--git-dir")
	if err := os.WriteFile(filepath.Join(gitDir, "gh-stack"), conflict, 0o644); err != nil {
		t.Fatal(err)
	}
	payload, r = doctorJSON(t, worktree)
	if r.code > 1 {
		t.Fatalf("doctor treated a per-worktree state file as authoritative: exited %d (%v)\n%s",
			r.code, issueCodes(payload), r.output())
	}
	if hasCode(payload, "CONFLICTING_STACK") || hasCode(payload, "AMBIGUOUS_MEMBERSHIP") {
		t.Fatalf("per-worktree state leaked into the reconciled view: %v", issueCodes(payload))
	}
	if got := worktree.tracked(); len(got) != 2 || got[0] != "layer-one" || got[1] != "layer-two" {
		t.Errorf("stack order from the worktree = %v, want the repo-root order [layer-one layer-two]", got)
	}

	f.git("checkout", "--quiet", "-b", "unrelated-old")
	f.write("unrelated.txt", "nope\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "unrelated")
	f.git("push", "--quiet", "-u", "origin", "unrelated-old")
	f.git("checkout", "--quiet", "main")
	f.git("push", "origin", "--delete", "unrelated-old")
	f.gt("sync", "-d")
	if f.git("branch", "--list", "unrelated-old") == "" {
		t.Fatal("sync -d deleted an untracked branch")
	}
}

func TestUnrelatedBranchUntouched(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.git("checkout", "--quiet", "main")
	f.git("checkout", "--quiet", "-b", "sidecar")
	f.write("sidecar.txt", "sidecar\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "sidecar")
	f.git("rebase", "main")

	payload, r := doctorJSON(t, f)
	if r.code > 1 {
		t.Fatalf("doctor exited %d (%v)\n%s", r.code, issueCodes(payload), r.output())
	}
	f.gt("sync")
	f.gt("sync", "-d")
	if f.git("branch", "--list", "sidecar") == "" {
		t.Fatal("sidecar was deleted")
	}
	if got := f.subject("sidecar"); got != "sidecar" {
		t.Errorf("sidecar commit rewritten to %q", got)
	}
}
