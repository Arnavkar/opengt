package main

import (
	"strings"
	"testing"
)

func testViewStack() trackedStack {
	return trackedStack{
		Trunk: trackedBranch{Branch: "main", Head: "sha-main"},
		Branches: []trackedBranch{
			{Branch: "layer-one", Head: "sha-one", Base: "sha-main"},
			{Branch: "layer-two", Head: "sha-two", Base: "sha-one"},
		},
	}
}

func testViewCommits() map[string]branchCommit {
	return map[string]branchCommit{
		"layer-one": {short: "df78169", when: "1 second ago", subject: "Add one"},
		"layer-two": {short: "3fcfa7e", when: "1 second ago", subject: "Add two"},
	}
}

func testViewHeads() map[string]string {
	return map[string]string{
		"main":      "sha-main",
		"layer-one": "sha-one",
		"layer-two": "sha-two",
	}
}

func TestRenderStackView_FullMatchesGhStack(t *testing.T) {
	got := renderStackView(testViewStack(), "layer-two", testViewHeads(), testViewCommits(), false)
	want := "" +
		"● layer-two (current) \n" +
		"│ 3fcfa7e · 1 second ago\n" +
		"│ Add two\n" +
		"│\n" +
		"○ layer-one \n" +
		"│ df78169 · 1 second ago\n" +
		"│ Add one\n" +
		"│\n" +
		"└ main\n"
	if got != want {
		t.Fatalf("full view:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderStackView_ShortMatchesGhStack(t *testing.T) {
	got := renderStackView(testViewStack(), "layer-two", testViewHeads(), testViewCommits(), true)
	want := "" +
		"» layer-two (current)\n" +
		"├ layer-one\n" +
		"└ main\n"
	if got != want {
		t.Fatalf("short view:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderStackView_WarnsWhenBaseDisagrees(t *testing.T) {
	stack := testViewStack()
	stack.Branches[0].Base = "stale"
	got := renderStackView(stack, "layer-two", testViewHeads(), testViewCommits(), false)
	if got[:len("● layer-two")] != "● layer-two" {
		t.Fatalf("current should stay ●:\n%s", got)
	}
	if !strings.Contains(got, "⚠ layer-one ") {
		t.Fatalf("expected warning icon on layer-one:\n%s", got)
	}
}

func TestRenderStackJSON_BottomToTop(t *testing.T) {
	got := renderStackJSON(testViewStack(), "layer-two", testViewHeads())
	want := "{\n" +
		"  \"trunk\": \"main\",\n" +
		"  \"currentBranch\": \"layer-two\",\n" +
		"  \"branches\": [\n" +
		"    {\n" +
		"      \"name\": \"layer-one\",\n" +
		"      \"base\": \"sha-main\",\n" +
		"      \"isCurrent\": false,\n" +
		"      \"isMerged\": false,\n" +
		"      \"isQueued\": false,\n" +
		"      \"needsRebase\": false\n" +
		"    },\n" +
		"    {\n" +
		"      \"name\": \"layer-two\",\n" +
		"      \"base\": \"sha-one\",\n" +
		"      \"isCurrent\": true,\n" +
		"      \"isMerged\": false,\n" +
		"      \"isQueued\": false,\n" +
		"      \"needsRebase\": false\n" +
		"    }\n" +
		"  ]\n" +
		"}\n"
	if got != want {
		t.Fatalf("json:\n%s\nwant:\n%s", got, want)
	}
}

func TestPlanNav_ClampsAndSkipsMerged(t *testing.T) {
	stack := trackedStack{
		Trunk: trackedBranch{Branch: "main"},
		Branches: []trackedBranch{
			{Branch: "a"},
			{Branch: "b", PullRequest: &pullRequestRef{Merged: true}},
			{Branch: "c"},
		},
	}
	cases := []struct {
		current, cmd string
		n            int
		want         string
		kind         navKind
		steps        int
	}{
		{"c", "up", 1, "c", navStayTop, 0},
		{"a", "down", 1, "a", navStayBottom, 0},
		{"a", "up", 1, "c", navUp, 1},
		{"a", "up", 9, "c", navUp, 1},
		{"c", "down", 2, "a", navDown, 1},
		{"c", "top", 1, "c", navStayTop, 0},
		{"a", "top", 1, "c", navSwitch, 0},
		{"a", "bottom", 1, "a", navStayBottom, 0},
		{"c", "bottom", 1, "a", navSwitch, 0},
		{"c", "trunk", 1, "main", navSwitch, 0},
		{"main", "trunk", 1, "main", navAlreadyTrunk, 0},
		{"main", "up", 1, "a", navUp, 1},
		{"main", "up", 9, "c", navUp, 2},
		{"main", "down", 1, "main", navStayBottom, 0},
		{"b", "up", 1, "c", navUp, 1},
		{"b", "down", 1, "a", navDown, 1},
		{"b", "top", 1, "c", navSwitch, 0},
		{"b", "bottom", 1, "a", navSwitch, 0},
	}
	for _, tc := range cases {
		got, kind, steps := planNav(stack, tc.current, tc.cmd, tc.n)
		if got != tc.want || kind != tc.kind || steps != tc.steps {
			t.Errorf("%s from %s n=%d = %s %v %d, want %s %v %d",
				tc.cmd, tc.current, tc.n, got, kind, steps, tc.want, tc.kind, tc.steps)
		}
	}
}

func TestFormatNavMessage(t *testing.T) {
	if got := formatNavMessage(navStayTop, "c", 0); got != "Already at the top of the stack" {
		t.Fatalf("stay top = %q", got)
	}
	if got := formatNavMessage(navStayBottom, "a", 0); got != "Already at the bottom of the stack" {
		t.Fatalf("stay bottom = %q", got)
	}
	if got := formatNavMessage(navUp, "layer-two", 1); got != "✓ Checked out layer-two, 1 branch up" {
		t.Fatalf("up = %q", got)
	}
	if got := formatNavMessage(navDown, "layer-one", 1); got != "✓ Checked out layer-one, 1 branch down" {
		t.Fatalf("down = %q", got)
	}
	if got := formatNavMessage(navUp, "c", 2); got != "✓ Checked out c, 2 branches up" {
		t.Fatalf("plural = %q", got)
	}
	if got := formatNavMessage(navSwitch, "layer-two", 0); got != "✓ Switched to layer-two" {
		t.Fatalf("switch = %q", got)
	}
	if got := formatNavMessage(navSwitch, "main", 0); got != "✓ Switched to main" {
		t.Fatalf("trunk = %q", got)
	}
	if got := formatNavMessage(navAlreadyTrunk, "main", 0); got != "Already on trunk branch main" {
		t.Fatalf("already trunk = %q", got)
	}
}

func TestRenderStackView_MergedUsesCheck(t *testing.T) {
	stack := testViewStack()
	stack.Branches[0].PullRequest = &pullRequestRef{Merged: true}
	got := renderStackView(stack, "layer-two", testViewHeads(), testViewCommits(), false)
	want := "" +
		"● layer-two (current) \n" +
		"│ 3fcfa7e · 1 second ago\n" +
		"│ Add two\n" +
		"│\n" +
		"✓ layer-one \n" +
		"│ df78169 · 1 second ago\n" +
		"│ Add one\n" +
		"│\n" +
		"└ main\n"
	if got != want {
		t.Fatalf("merged full view:\n%s\nwant:\n%s", got, want)
	}
	short := renderStackView(stack, "layer-two", testViewHeads(), testViewCommits(), true)
	wantShort := "" +
		"» layer-two (current)\n" +
		"✓ layer-one\n" +
		"└ main\n"
	if short != wantShort {
		t.Fatalf("merged short view:\n%s\nwant:\n%s", short, wantShort)
	}
}

func TestBranchNeedsRebase_BaseVersusParentTip(t *testing.T) {
	if branchNeedsRebase(trackedBranch{Base: ""}, "sha-main") {
		t.Fatal("empty base does not need rebase")
	}
	if branchNeedsRebase(trackedBranch{Base: "sha-main"}, "sha-main") {
		t.Fatal("matching parent tip does not need rebase")
	}
	if !branchNeedsRebase(trackedBranch{Base: "stale"}, "sha-main") {
		t.Fatal("stale base needs rebase")
	}
	if !branchNeedsRebase(trackedBranch{Base: "stale"}, "") {
		t.Fatal("missing parent tip differs from a saved base")
	}
}
