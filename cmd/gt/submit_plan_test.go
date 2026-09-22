package main

import (
	"testing"
	"time"
)

func repoFromStack(t *testing.T, jsonText string) *repoStackState {
	t.Helper()
	st := stateFrom(t, jsonText)
	repo := reconcileSources([]stackStateSource{{State: st}})
	return &repo
}

func makeSnap(refs map[string]RemoteRef, prs map[string]PRSnapshot, stack *RemoteStackSnapshot) *RemoteSnapshot {
	return &RemoteSnapshot{
		Refs:        RemoteRefSnapshot{Refs: refs, FetchedAt: time.Now()},
		PRs:         prs,
		RemoteStack: stack,
	}
}

func rref(sha string) RemoteRef           { return RemoteRef{Exists: true, RemoteSHA: sha} }
func rpr(num int, base string) PRSnapshot { return PRSnapshot{Number: num, State: "OPEN", Base: base} }

const fiveStack = `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [
       {"branch": "a", "head": "la"},
       {"branch": "b", "head": "lb"},
       {"branch": "c", "head": "lc"},
       {"branch": "d", "head": "ld"},
       {"branch": "e", "head": "le"}
     ]}
  ]
}`

// allUpToDate: every branch pushed, every PR on its parent, stack object lists
// all five PR numbers in order.
func allUpToDate() *RemoteSnapshot {
	refs := map[string]RemoteRef{
		"a": rref("la"), "b": rref("lb"), "c": rref("lc"),
		"d": rref("ld"), "e": rref("le"),
	}
	prs := map[string]PRSnapshot{
		"a": rpr(1, "main"), "b": rpr(2, "a"), "c": rpr(3, "b"),
		"d": rpr(4, "c"), "e": rpr(5, "d"),
	}
	return makeSnap(refs, prs, &RemoteStackSnapshot{ID: "st1", Numbers: []int{1, 2, 3, 4, 5}})
}

func TestSubmitPlan_NoOp(t *testing.T) {
	repo := repoFromStack(t, fiveStack)
	plan, err := BuildSubmitPlan(repo, "e", allUpToDate(), SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if !plan.IsNoOp() {
		t.Fatalf("expected no-op, got %+v", plan)
	}
	if plan.StackUpdate != nil {
		t.Fatalf("no-op should not mutate stack object, got %+v", plan.StackUpdate)
	}
}

func TestSubmitPlan_OneChangedBranch(t *testing.T) {
	repo := repoFromStack(t, `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [
       {"branch": "a", "head": "la"},
       {"branch": "b", "head": "lb"},
       {"branch": "c", "head": "lc-new"},
       {"branch": "d", "head": "ld"},
       {"branch": "e", "head": "le"}
     ]}
  ]
}`)
	snap := allUpToDate()
	plan, err := BuildSubmitPlan(repo, "e", snap, SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if plan.IsNoOp() {
		t.Fatalf("expected non-no-op")
	}
	if len(plan.Pushes) != 1 {
		t.Fatalf("Pushes len = %d, want 1 (%+v)", len(plan.Pushes), plan.Pushes)
	}
	p := plan.Pushes[0]
	if p.Branch != "c" {
		t.Fatalf("push branch = %q, want c", p.Branch)
	}
	if p.IsNew {
		t.Fatalf("IsNew = true, want false")
	}
	if p.ExpectedOld != "lc" {
		t.Fatalf("ExpectedOld = %q, want lc", p.ExpectedOld)
	}
	if p.LocalSHA != "lc-new" {
		t.Fatalf("LocalSHA = %q, want lc-new", p.LocalSHA)
	}
	if plan.StackUpdate != nil {
		t.Fatalf("order unchanged, want nil StackUpdate, got %+v", plan.StackUpdate)
	}
}

func TestSubmitPlan_ThreeChangedBranches(t *testing.T) {
	repo := repoFromStack(t, `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [
       {"branch": "a", "head": "la-new"},
       {"branch": "b", "head": "lb"},
       {"branch": "c", "head": "lc-new"},
       {"branch": "d", "head": "ld"},
       {"branch": "e", "head": "le-new"}
     ]}
  ]
}`)
	snap := allUpToDate()
	plan, err := BuildSubmitPlan(repo, "e", snap, SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if len(plan.Pushes) != 3 {
		t.Fatalf("Pushes len = %d, want 3 (%+v)", len(plan.Pushes), plan.Pushes)
	}
	want := map[string]bool{"a": true, "c": true, "e": true}
	for _, p := range plan.Pushes {
		if !want[p.Branch] {
			t.Fatalf("unexpected push for %q", p.Branch)
		}
	}
}

func TestSubmitPlan_UpdateOnly_MissingPR(t *testing.T) {
	repo := repoFromStack(t, `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [
       {"branch": "a", "head": "la"},
       {"branch": "b", "head": "lb"},
       {"branch": "c", "head": "lc"},
       {"branch": "d", "head": "ld-new"},
       {"branch": "e", "head": "le"}
     ]}
  ]
}`)
	refs := map[string]RemoteRef{
		"a": rref("la"), "b": rref("lb"), "c": rref("lc"),
		"d": rref("ld"), "e": rref("le"),
	}
	prs := map[string]PRSnapshot{
		"a": rpr(1, "main"), "b": rpr(2, "a"), "c": rpr(3, "b"), "e": rpr(5, "d"),
	}
	snap := makeSnap(refs, prs, &RemoteStackSnapshot{ID: "st1", Numbers: []int{1, 2, 3, 5}})
	plan, err := BuildSubmitPlan(repo, "e", snap, SubmitOpts{UpdateOnly: true})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	for _, p := range plan.Pushes {
		if p.Branch == "d" {
			t.Fatalf("d should be excluded from Pushes under -u, got %+v", p)
		}
	}
	for _, c := range plan.Creates {
		if c.Branch == "d" {
			t.Fatalf("d should not get a PRCreate under -u, got %+v", c)
		}
	}
}

func TestSubmitPlan_RegularSubmit_MissingPR(t *testing.T) {
	repo := repoFromStack(t, fiveStack)
	refs := map[string]RemoteRef{
		"a": rref("la"), "b": rref("lb"), "c": rref("lc"), "d": rref("ld"),
	}
	prs := map[string]PRSnapshot{
		"a": rpr(1, "main"), "b": rpr(2, "a"), "c": rpr(3, "b"), "d": rpr(4, "c"),
	}
	snap := makeSnap(refs, prs, nil)
	plan, err := BuildSubmitPlan(repo, "e", snap, SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	var ePush *PushRef
	for i := range plan.Pushes {
		if plan.Pushes[i].Branch == "e" {
			ePush = &plan.Pushes[i]
		}
	}
	if ePush == nil {
		t.Fatalf("e should be in Pushes, got %+v", plan.Pushes)
	}
	if !ePush.IsNew {
		t.Fatalf("e push IsNew = false, want true")
	}
	var eCreate *PRCreate
	for i := range plan.Creates {
		if plan.Creates[i].Branch == "e" {
			eCreate = &plan.Creates[i]
		}
	}
	if eCreate == nil {
		t.Fatalf("expected PRCreate for e, got %+v", plan.Creates)
	}
	if plan.StackUpdate == nil {
		t.Fatalf("expected StackUpdate present when creating a PR")
	}
}

func TestSubmitPlan_WrongPRBase(t *testing.T) {
	repo := repoFromStack(t, fiveStack)
	refs := map[string]RemoteRef{
		"a": rref("la"), "b": rref("lb"), "c": rref("lc"),
		"d": rref("ld"), "e": rref("le"),
	}
	prs := map[string]PRSnapshot{
		"a": rpr(1, "main"), "b": rpr(2, "a"), "c": rpr(3, "main"),
		"d": rpr(4, "c"), "e": rpr(5, "d"),
	}
	snap := makeSnap(refs, prs, &RemoteStackSnapshot{ID: "st1", Numbers: []int{1, 2, 3, 4, 5}})
	plan, err := BuildSubmitPlan(repo, "e", snap, SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if len(plan.BaseUpdates) != 1 {
		t.Fatalf("BaseUpdates len = %d, want 1 (%+v)", len(plan.BaseUpdates), plan.BaseUpdates)
	}
	bu := plan.BaseUpdates[0]
	if bu.PRNumber != 3 || bu.NewBase != "b" {
		t.Fatalf("BaseUpdate = %+v, want PR 3 -> b", bu)
	}
	for _, p := range plan.Pushes {
		if p.Branch == "c" {
			t.Fatalf("c should not be pushed for a base-only update, got %+v", p)
		}
	}
}

func TestSubmitPlan_NoOpStackObject(t *testing.T) {
	repo := repoFromStack(t, fiveStack)
	snap := allUpToDate()
	plan, err := BuildSubmitPlan(repo, "e", snap, SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if plan.StackUpdate != nil {
		t.Fatalf("order unchanged, want nil StackUpdate, got %+v", plan.StackUpdate)
	}
}

func TestSubmitPlan_StackObjectOrderChanged(t *testing.T) {
	repo := repoFromStack(t, fiveStack)
	snap := allUpToDate()
	snap.RemoteStack.Numbers = []int{1, 2, 4, 3, 5}
	plan, err := BuildSubmitPlan(repo, "e", snap, SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if plan.StackUpdate == nil {
		t.Fatalf("order changed, want StackUpdate present")
	}
	if plan.StackUpdate.StackID != "st1" {
		t.Fatalf("StackID = %q, want st1", plan.StackUpdate.StackID)
	}
	if !intSliceEq(plan.StackUpdate.Numbers, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("Numbers = %+v, want [1 2 3 4 5]", plan.StackUpdate.Numbers)
	}
}

// TestSubmitPlan_EmptyHeadMissingRemotePushes: gh stack init / add leave the
// cached head empty, so an in-scope branch with no remote ref is still an
// IsNew push, and the PR is created after that push.
func TestSubmitPlan_EmptyHeadMissingRemotePushes(t *testing.T) {
	repo := repoFromStack(t, `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [{"branch": "a", "head": ""}]}
  ]
}`)
	snap := makeSnap(map[string]RemoteRef{}, map[string]PRSnapshot{}, nil)
	plan, err := BuildSubmitPlan(repo, "a", snap, SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if plan.IsNoOp() {
		t.Fatalf("expected non-no-op (%+v)", plan)
	}
	if len(plan.Pushes) != 1 {
		t.Fatalf("Pushes len = %d, want 1 (%+v)", len(plan.Pushes), plan.Pushes)
	}
	p := plan.Pushes[0]
	if p.Branch != "a" || !p.IsNew {
		t.Fatalf("push = %+v, want IsNew push for a", p)
	}
	if len(plan.Creates) != 1 || plan.Creates[0].Branch != "a" {
		t.Fatalf("Creates = %+v, want PRCreate for a", plan.Creates)
	}
}

// TestAllowRemoteReplace: the submit-safety gate. A remote commit this branch
// never contained (not an ancestor, not in the reflog) is refused; every
// otherwise-safe case is allowed.
func TestAllowRemoteReplace(t *testing.T) {
	cases := []struct {
		name     string
		local    string
		remote   string
		ancestor bool
		inReflog bool
		want     bool
	}{
		{"first push, missing remote", "aaa", "", false, false, true},
		{"local equals remote, no push", "aaa", "aaa", false, false, true},
		{"fast-forward over ancestor", "aaa", "bbb", true, false, true},
		{"own amend or rebase, reflog hit", "aaa", "bbb", false, true, true},
		{"unknown remote commit", "aaa", "bbb", false, false, false},
		{"reflog hit wins over unknown", "aaa", "bbb", false, true, true},
	}
	for _, c := range cases {
		if got := allowRemoteReplace(c.local, c.remote, c.ancestor, c.inReflog); got != c.want {
			t.Errorf("%s: allowRemoteReplace(%q,%q,%v,%v) = %v, want %v",
				c.name, c.local, c.remote, c.ancestor, c.inReflog, got, c.want)
		}
	}
}

func TestResolveSubmitScope_DefaultTrunkToCurrent(t *testing.T) {
	st := stateFrom(t, fiveStack)
	stack := st.Stacks[0]
	got := ResolveSubmitScope(stack, "c", false, false, nil)
	want := []string{"a", "b", "c"}
	if !strSliceEq(got, want) {
		t.Fatalf("default scope = %+v, want %+v", got, want)
	}
}

func TestUpstackOf(t *testing.T) {
	st := stateFrom(t, fiveStack)
	stack := st.Stacks[0]
	if got := upstackOf(stack, "c"); !strSliceEq(got, []string{"d", "e"}) {
		t.Fatalf("upstack of c = %+v", got)
	}
	if got := upstackOf(stack, "e"); len(got) != 0 {
		t.Fatalf("tip should have no upstack, got %+v", got)
	}
	if got := upstackOf(stack, "missing"); got != nil {
		t.Fatalf("missing current = %+v, want nil", got)
	}
}

func TestSubmitPlan_FromMiddleOmitsUpstack(t *testing.T) {
	repo := repoFromStack(t, `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [
       {"branch": "a", "head": "la"},
       {"branch": "b", "head": "lb-new"},
       {"branch": "c", "head": "lc"},
       {"branch": "d", "head": "ld-new"},
       {"branch": "e", "head": "le"}
     ]}
  ]
}`)
	plan, err := BuildSubmitPlan(repo, "c", allUpToDate(), SubmitOpts{})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if len(plan.Pushes) != 1 || plan.Pushes[0].Branch != "b" {
		t.Fatalf("from c, Pushes = %+v, want only b", plan.Pushes)
	}
	for _, p := range plan.Pushes {
		if p.Branch == "d" {
			t.Fatalf("upstack d should not be pushed without --stack")
		}
	}
	if plan.StackUpdate != nil {
		t.Fatalf("must not rewrite stack object to drop upstack PRs, got %+v", plan.StackUpdate)
	}
}

func TestSubmitPlan_FromMiddleWithStackFlagIncludesUpstack(t *testing.T) {
	repo := repoFromStack(t, `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [
       {"branch": "a", "head": "la"},
       {"branch": "b", "head": "lb"},
       {"branch": "c", "head": "lc"},
       {"branch": "d", "head": "ld-new"},
       {"branch": "e", "head": "le"}
     ]}
  ]
}`)
	plan, err := BuildSubmitPlan(repo, "c", allUpToDate(), SubmitOpts{Stack: true})
	if err != nil {
		t.Fatalf("BuildSubmitPlan: %v", err)
	}
	if len(plan.Pushes) != 1 || plan.Pushes[0].Branch != "d" {
		t.Fatalf("--stack from c, Pushes = %+v, want d", plan.Pushes)
	}
}

func TestResolveSubmitScope_StackFlagWholeStack(t *testing.T) {
	st := stateFrom(t, fiveStack)
	stack := st.Stacks[0]
	got := ResolveSubmitScope(stack, "c", true, false, nil)
	want := []string{"a", "b", "c", "d", "e"}
	if !strSliceEq(got, want) {
		t.Fatalf("--stack scope = %+v, want %+v", got, want)
	}
}

func TestResolveSubmitScope_UpdateOnlyFiltersMissingPR(t *testing.T) {
	st := stateFrom(t, fiveStack)
	stack := st.Stacks[0]
	prs := map[string]PRSnapshot{
		"a": rpr(1, "main"), "b": rpr(2, "a"), "c": rpr(3, "b"), "e": rpr(5, "d"),
	}
	got := ResolveSubmitScope(stack, "e", false, true, prs)
	want := []string{"a", "b", "c", "e"}
	if !strSliceEq(got, want) {
		t.Fatalf("-u scope = %+v, want %+v", got, want)
	}
}

func TestResolveSubmitScope_UpdateOnlyFiltersClosedPR(t *testing.T) {
	st := stateFrom(t, fiveStack)
	stack := st.Stacks[0]
	prs := map[string]PRSnapshot{
		"a": rpr(1, "main"),
		"b": {Number: 2, State: "CLOSED", Base: "a"},
		"c": rpr(3, "b"),
	}
	got := ResolveSubmitScope(stack, "c", false, true, prs)
	want := []string{"a", "c"}
	if !strSliceEq(got, want) {
		t.Fatalf("-u scope with closed PR = %+v, want %+v", got, want)
	}
}

func TestSubmitPlan_IsNoOpAcrossCases(t *testing.T) {
	repo := repoFromStack(t, fiveStack)
	if p, _ := BuildSubmitPlan(repo, "e", allUpToDate(), SubmitOpts{}); !p.IsNoOp() {
		t.Fatalf("up-to-date should be no-op")
	}
	changed := allUpToDate()
	changed.Refs.Refs["c"] = rref("lc-old")
	repo2 := repoFromStack(t, `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [
       {"branch": "a", "head": "la"},
       {"branch": "b", "head": "lb"},
       {"branch": "c", "head": "lc"},
       {"branch": "d", "head": "ld"},
       {"branch": "e", "head": "le"}
     ]}
  ]
}`)
	if p, _ := BuildSubmitPlan(repo2, "e", changed, SubmitOpts{}); p.IsNoOp() {
		t.Fatalf("changed branch should not be no-op")
	}
}

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intSliceEq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
