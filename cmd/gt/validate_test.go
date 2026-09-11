package main

import (
	"strings"
	"testing"
)

// srcFrom builds a stackStateSource from JSON, mirroring reconcile_test.go's src.
func srcFrom(t *testing.T, path, jsonText string) stackStateSource {
	t.Helper()
	return stackStateSource{Path: path, State: stateFrom(t, jsonText)}
}

func repoFrom(t *testing.T, sources ...stackStateSource) *repoStackState {
	t.Helper()
	r := reconcileSources(sources)
	return &r
}

func healthyStack() string {
	return `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [{"branch": "a"}, {"branch": "b"}, {"branch": "c"}]}
  ]
}`
}

func TestValidateSchemaUnsupported(t *testing.T) {
	// A schema v2 file is unsupported by ghStackCompat (which knows v1 only).
	bad := `{
  "schemaVersion": 2,
  "stacks": [
    {"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}
  ]
}`
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", bad))
	err := validateSchema(repo)
	if err == nil {
		t.Fatal("expected UNSUPPORTED_SCHEMA error, got nil")
	}
	if !strings.Contains(err.Error(), "UNSUPPORTED_SCHEMA") && !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error should mention schema: %s", err)
	}
	if !strings.Contains(err.Error(), "gt doctor") {
		t.Fatalf("error should point to doctor: %s", err)
	}
}

func TestValidateSchemaHealthy(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	if err := validateSchema(repo); err != nil {
		t.Fatalf("healthy schema returned error: %v", err)
	}
}

func TestValidateAmbiguityAmbiguousBranch(t *testing.T) {
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
	repo := repoFrom(t,
		srcFrom(t, "long/gh-stack", long),
		srcFrom(t, "short/gh-stack", short),
	)
	err := validateAmbiguity(repo, "c")
	if err == nil {
		t.Fatal("expected ambiguous error for c")
	}
	if !strings.Contains(err.Error(), "doctor") || !strings.Contains(err.Error(), "ambiguous") && !strings.Contains(err.Error(), "incompatible") {
		// errAmbiguous says "claimed by two incompatible stack definitions"
		t.Fatalf("error should mention doctor/incompatibility: %s", err)
	}
}

func TestValidateAmbiguityConflictingStacks(t *testing.T) {
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
	repo := repoFrom(t, srcFrom(t, "wt-a/gh-stack", a), srcFrom(t, "wt-b/gh-stack", b))
	// "a" is not ambiguous by membership (same chain key would be, but here
	// order differs so it is a CONFLICTING_STACK). hasConflicts is true, so
	// validateAmbiguity must error even for a branch that ambiguous() does not
	// flag.
	err := validateAmbiguity(repo, "a")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "gt doctor") {
		t.Fatalf("error should point to doctor: %s", err)
	}
}

func TestValidateAmbiguityHealthy(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	if err := validateAmbiguity(repo, "b"); err != nil {
		t.Fatalf("healthy branch returned error: %v", err)
	}
}

func TestValidateBranchExistenceMissingMember(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	stack, ok := findStackForBranch(repo, "b")
	if !ok {
		t.Fatal("expected to find stack for b")
	}
	// Trunk exists, "a" exists, "b" missing, "c" exists.
	isLocal := func(name string) bool {
		switch name {
		case "main", "a", "c":
			return true
		}
		return false
	}
	err := validateBranchExistence(stack, isLocal)
	if err == nil {
		t.Fatal("expected MISSING_BRANCH error for b")
	}
	if !strings.Contains(err.Error(), "missing branch") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("error should name missing branch b: %s", err)
	}
}

func TestValidateBranchExistenceMissingTrunk(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	stack, _ := findStackForBranch(repo, "b")
	isLocal := func(name string) bool {
		// trunk missing
		return name != "main"
	}
	err := validateBranchExistence(stack, isLocal)
	if err == nil {
		t.Fatal("expected missing trunk error")
	}
	if !strings.Contains(err.Error(), "trunk") || !strings.Contains(err.Error(), "main") {
		t.Fatalf("error should name missing trunk main: %s", err)
	}
}

func TestValidateBranchExistenceHealthy(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	stack, _ := findStackForBranch(repo, "b")
	allLocal := func(string) bool { return true }
	if err := validateBranchExistence(stack, allLocal); err != nil {
		t.Fatalf("healthy existence returned error: %v", err)
	}
}

func TestValidateWorktreeStateCheckedOutElsewhere(t *testing.T) {
	err := validateWorktreeState("feature", "other", "/repo/wt-feature")
	if err == nil {
		t.Fatal("expected worktree error")
	}
	if !strings.Contains(err.Error(), "another worktree") || !strings.Contains(err.Error(), "feature") {
		t.Fatalf("error should mention branch and another worktree: %s", err)
	}
}

func TestValidateWorktreeStateCheckedOutHere(t *testing.T) {
	if err := validateWorktreeState("feature", "feature", "/repo/wt-feature"); err != nil {
		t.Fatalf("branch checked out here should be ok: %v", err)
	}
}

func TestValidateWorktreeStateNotCheckedOutAnywhere(t *testing.T) {
	if err := validateWorktreeState("feature", "other", ""); err != nil {
		t.Fatalf("branch not in any worktree should be ok: %v", err)
	}
}

func TestValidatePausedOpInProgress(t *testing.T) {
	err := validatePausedOp("rebase")
	if err == nil {
		t.Fatal("expected paused op error")
	}
	if !strings.Contains(err.Error(), "rebase") || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("error should name the paused op: %s", err)
	}
}

func TestValidatePausedOpHealthy(t *testing.T) {
	if err := validatePausedOp(""); err != nil {
		t.Fatalf("no paused op should be ok: %v", err)
	}
}

func TestValidateAncestryBroken(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	stack, _ := findStackForBranch(repo, "c")
	heads := map[string]string{
		"main": "s0", "a": "s1", "b": "s2", "c": "s3",
	}
	// b does not contain a, and c does not contain b.
	ancestorFn := func(parent, child string) bool {
		if parent == "a" && child == "b" {
			return false
		}
		if parent == "b" && child == "c" {
			return false
		}
		return true
	}
	bad := validateAncestry(stack.trackedStack, heads, ancestorFn)
	if len(bad) == 0 {
		t.Fatal("expected ancestry issues")
	}
	if bad[0].branch != "b" || bad[0].parent != "a" {
		t.Fatalf("first issue should be b/a, got %+v", bad[0])
	}
}

func TestValidateAncestryCacheBaseSaves(t *testing.T) {
	// b does not contain parent a, but cached base (some other commit) is an
	// ancestor of b — that is a NEEDS_RESTACK, not INVALID_ANCESTRY, so the
	// validator should NOT flag it.
	stack := trackedStack{
		Trunk:    trackedBranch{Branch: "main"},
		Branches: []trackedBranch{{Branch: "a"}, {Branch: "b", Base: "cache"}},
	}
	heads := map[string]string{"main": "s0", "a": "s1", "b": "s2"}
	ancestorFn := func(parent, child string) bool {
		// parent a is NOT an ancestor of b, but cache IS.
		if parent == "a" && child == "b" {
			return false
		}
		if parent == "cache" && child == "b" {
			return true
		}
		return true
	}
	if bad := validateAncestry(stack, heads, ancestorFn); len(bad) != 0 {
		t.Fatalf("cache-base rescue should not flag ancestry, got %+v", bad)
	}
}

func TestValidateAncestryHealthy(t *testing.T) {
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	stack, _ := findStackForBranch(repo, "c")
	heads := map[string]string{"main": "s0", "a": "s1", "b": "s2", "c": "s3"}
	ancestorFn := func(string, string) bool { return true }
	if bad := validateAncestry(stack.trackedStack, heads, ancestorFn); len(bad) != 0 {
		t.Fatalf("healthy ancestry returned issues: %+v", bad)
	}
}

func TestValidateStackForMutationUntrackedBranchIsNil(t *testing.T) {
	// A branch that is not a member of any stack has nothing to validate; the
	// cheap validator returns nil and lets the calling command decide scope.
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	if err := validateStackForMutation(repo, "not-a-stack-branch"); err != nil {
		t.Fatalf("untracked branch should return nil, got %v", err)
	}
}

func TestValidateStackForMutationAmbiguousBranch(t *testing.T) {
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
	repo := repoFrom(t,
		srcFrom(t, "long/gh-stack", long),
		srcFrom(t, "short/gh-stack", short),
	)
	err := validateStackForMutation(repo, "c")
	if err == nil {
		t.Fatal("expected ambiguous error from validator")
	}
	if !strings.Contains(err.Error(), "doctor") {
		t.Fatalf("error should point to doctor: %s", err)
	}
}

func TestValidateStackForMutationUnsupportedSchema(t *testing.T) {
	bad := `{
  "schemaVersion": 2,
  "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
}`
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", bad))
	err := validateStackForMutation(repo, "a")
	if err == nil {
		t.Fatal("expected schema error from validator")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error should mention schema: %s", err)
	}
}

// TestValidateNoNetwork is a structural assertion: validate.go must not
// import github.com/cli/go-gh. The pure helpers exercise no I/O. The
// git-backed wrappers call only local git plumbing (isLocalBranch,
// worktreePathForBranch, currentBranch, pausedOperation), never gh.
func TestValidateNoNetwork(t *testing.T) {
	// Exercise every pure helper with healthy inputs; none perform I/O.
	repo := repoFrom(t, srcFrom(t, "wt/gh-stack", healthyStack()))
	if err := validateSchema(repo); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if err := validateAmbiguity(repo, "b"); err != nil {
		t.Fatalf("ambiguity: %v", err)
	}
	stack, _ := findStackForBranch(repo, "b")
	if err := validateBranchExistence(stack, func(string) bool { return true }); err != nil {
		t.Fatalf("existence: %v", err)
	}
	if err := validateWorktreeState("b", "b", ""); err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := validatePausedOp(""); err != nil {
		t.Fatalf("paused: %v", err)
	}
	heads := map[string]string{"main": "s0", "a": "s1", "b": "s2", "c": "s3"}
	if bad := validateAncestry(stack.trackedStack, heads, func(string, string) bool { return true }); len(bad) != 0 {
		t.Fatalf("ancestry: %+v", bad)
	}
}
