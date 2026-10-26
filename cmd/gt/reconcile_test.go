package main

import (
	"strings"
	"testing"
)

func src(t *testing.T, path, jsonText string) stackStateSource {
	t.Helper()
	return stackStateSource{Path: path, State: stateFrom(t, jsonText)}
}

func TestCatalogIsSchemaV1_EmptyIsWritable(t *testing.T) {
	if !catalogIsSchemaV1(nil) {
		t.Fatal("empty catalog must be writable schema v1")
	}
	if !catalogIsSchemaV1([]stackStateSource{}) {
		t.Fatal("no sources must be writable schema v1")
	}
}

func TestReconcileDuplicateIdenticalIsNotConflict(t *testing.T) {
	repo := reconcileSources([]stackStateSource{
		src(t, "a/gh-stack", linearStack),
		src(t, "b/gh-stack", linearStack),
	})
	if repo.hasConflicts() {
		t.Fatalf("identical copies conflicted: %+v", repo.Issues)
	}
	n := 0
	for _, s := range repo.Stacks {
		if !s.Stale {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("got %d live stacks, want 1", n)
	}
	dup := false
	for _, i := range repo.Issues {
		if i.Code == "DUPLICATE_STACK" {
			dup = true
		}
	}
	if !dup {
		t.Fatal("expected DUPLICATE_STACK warning")
	}
}

func TestReconcileAmbiguousPrefix(t *testing.T) {
	long := `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"},
                  "branches": [{"branch": "a"}, {"branch": "b"}, {"branch": "c"}]}]
    }`
	short := `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"},
                  "branches": [{"branch": "a"}, {"branch": "b"}]}]
    }`
	repo := reconcileSources([]stackStateSource{
		src(t, "long/gh-stack", long),
		src(t, "short/gh-stack", short),
	})
	if !repo.hasConflicts() {
		t.Fatalf("prefix was not treated as conflict: %+v", repo.Issues)
	}
	found := false
	for _, i := range repo.Issues {
		if i.Code == "AMBIGUOUS_PREFIX" && !i.Repairable {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected AMBIGUOUS_PREFIX: %+v", repo.Issues)
	}
	st := repo.asState()
	if len(st.Stacks) != 2 {
		t.Fatalf("asState kept %+v", st.Stacks)
	}
	if hits := repo.ambiguous("c"); len(hits) != 2 {
		t.Fatalf("long-only tip did not remain ambiguous: %+v", hits)
	}
	applied, skipped := repairMetadata(repo, true, true)
	if len(applied) != 0 || len(skipped) == 0 {
		t.Fatalf("prefix repair applied=%v skipped=%v", applied, skipped)
	}
}

func TestReconcileOrderConflict(t *testing.T) {
	a := `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"},
                  "branches": [{"branch": "a"}, {"branch": "b"}, {"branch": "c"}]}]
    }`
	b := `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"},
                  "branches": [{"branch": "a"}, {"branch": "c"}, {"branch": "b"}]}]
    }`
	repo := reconcileSources([]stackStateSource{
		src(t, "wt-a/gh-stack", a),
		src(t, "wt-b/gh-stack", b),
	})
	if !repo.hasConflicts() {
		t.Fatal("expected conflict")
	}
	hits := repo.ambiguous("a")
	if len(hits) < 2 {
		t.Fatalf("ambiguous(a) = %d", len(hits))
	}
	err := errAmbiguous("a", hits)
	if !strings.Contains(err.Error(), "gt doctor") || !strings.Contains(err.Error(), "No changes were made") {
		t.Fatalf("error not agent-friendly:\n%s", err)
	}
}

func TestReconcileSiblingStacksShareTrunkOnly(t *testing.T) {
	a := `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`
	b := `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "c"}]}]
    }`
	repo := reconcileSources([]stackStateSource{
		src(t, "a/gh-stack", a),
		src(t, "b/gh-stack", b),
	})
	if repo.hasConflicts() {
		t.Fatalf("siblings conflicted: %+v", repo.Issues)
	}
	if len(repo.asState().Stacks) != 2 {
		t.Fatalf("want 2 stacks, got %+v", repo.asState().Stacks)
	}
}

func TestDoctorExitCodes(t *testing.T) {
	if doctorExitFrom(nil) != 0 {
		t.Fatal("healthy")
	}
	if doctorExitFrom([]stackIssue{{Code: "NEEDS_RESTACK", Severity: "warning"}}) != exitDoctorWarning {
		t.Fatal("warning")
	}
	if doctorExitFrom([]stackIssue{{Code: "MISSING_BRANCH", Severity: "error", Repairable: true}}) != exitDoctorRepairable {
		t.Fatal("repairable")
	}
	if doctorExitFrom([]stackIssue{{Code: "CONFLICTING_STACK", Severity: "error"}}) != exitDoctorAmbiguous {
		t.Fatal("ambiguous")
	}
	if doctorExitFrom([]stackIssue{{Code: "AMBIGUOUS_PREFIX", Severity: "error"}}) != exitDoctorAmbiguous {
		t.Fatal("ambiguous prefix")
	}
	if doctorExitFrom([]stackIssue{{Code: "UNSUPPORTED_SCHEMA", Severity: "error"}}) != exitDoctorDependency {
		t.Fatal("dependency")
	}
}
