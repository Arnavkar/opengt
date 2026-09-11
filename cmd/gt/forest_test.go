package main

import (
	"strings"
	"testing"
)

func TestStackForestSingleStack(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"},
                  "branches": [{"branch": "a"}, {"branch": "b"}]}]
    }`)
	got := stackForest(st, "b")
	want := []pickRow{
		{branch: "b", current: true, text: "◉  b"},
		{branch: "a", text: "◯  a"},
		{branch: "main", trunk: true, text: "◯  main"},
	}
	assertRows(t, got, want)
}

func TestStackForestSiblingStacksShareTrunk(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [
        {"trunk": {"branch": "main"}, "branches": [{"branch": "a"}, {"branch": "b"}]},
        {"trunk": {"branch": "main"}, "branches": [{"branch": "c"}]}
      ]
    }`)
	got := stackForest(st, "c")
	want := []pickRow{
		{branch: "b", text: "◯    b"},
		{branch: "a", text: "◯    a"},
		{branch: "c", current: true, text: "│ ◉  c"},
		{branch: "main", trunk: true, text: "◯─┘  main"},
	}
	assertRows(t, got, want)
}

func TestStackForestTwoSingleBranchStacks(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [
        {"trunk": {"branch": "main"}, "branches": [{"branch": "chore/annotation-ui-polish"}]},
        {"trunk": {"branch": "main"}, "branches": [{"branch": "feat/set-up-hatchet-worker"}]}
      ]
    }`)
	got := stackForest(st, "feat/set-up-hatchet-worker")
	want := []pickRow{
		{branch: "chore/annotation-ui-polish", text: "◯    chore/annotation-ui-polish"},
		{branch: "feat/set-up-hatchet-worker", current: true, text: "│ ◉  feat/set-up-hatchet-worker"},
		{branch: "main", trunk: true, text: "◯─┘  main"},
	}
	assertRows(t, got, want)
}

func TestStackForestCurrentOnTrunk(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	got := stackForest(st, "main")
	want := []pickRow{
		{branch: "a", text: "◯  a"},
		{branch: "main", current: true, trunk: true, text: "◉  main"},
	}
	assertRows(t, got, want)
}

func TestPickRowRenderColor(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [
        {"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]},
        {"trunk": {"branch": "main"}, "branches": [{"branch": "b"}]}
      ]
    }`)
	rows := stackForest(st, "b")
	plain := rows[1].render(false, false)
	if plain != rows[1].text {
		t.Fatalf("uncolored render = %q, want %q", plain, rows[1].text)
	}
	colored := rows[1].render(true, true)
	if colored == plain {
		t.Fatal("colored render produced no ANSI")
	}
	if !containsANSI(colored) || !containsANSI(rows[0].render(true, false)) {
		t.Fatalf("expected ANSI in graph: %q / %q", colored, rows[0].render(true, false))
	}
	if githubStacksRow().render(true, false) == githubStacksRow().text {
		t.Fatal("github row should dim when color is on")
	}
}

func containsANSI(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			return true
		}
	}
	return false
}

func TestStackForestEmpty(t *testing.T) {
	st := stateFrom(t, `{"schemaVersion": 1}`)
	if got := stackForest(st, "main"); len(got) != 0 {
		t.Errorf("empty state produced %d rows, want 0", len(got))
	}
}

func TestEnsureCurrentAddsMissing(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	got := ensureCurrent(stackForest(st, "scratch"), "scratch")
	if got[0].branch != "scratch" || !got[0].current || !got[0].untracked || got[0].detail != untrackedDetail {
		t.Fatalf("untracked current was not prepended: %+v", got[0])
	}
}

func TestMarkUntrackedRowsLabelsWorktreeBranches(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	st = withWorktreeStacks(st, []string{"a", "main", "other"}, "main")
	rows := stackForest(st, "a")
	got := markUntrackedRows(rows, stackedBranchNames(stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)), "main")
	found := false
	for _, r := range got {
		if r.branch != "other" {
			continue
		}
		found = true
		if !r.untracked || r.detail != untrackedDetail {
			t.Fatalf("untracked row = %+v", r)
		}
	}
	if !found {
		t.Fatalf("missing untracked row: %+v", got)
	}
	for _, r := range got {
		if r.branch == "a" && (r.untracked || r.detail != "") {
			t.Fatalf("tracked branch marked untracked: %+v", r)
		}
		if r.trunk && r.untracked {
			t.Fatalf("trunk marked untracked: %+v", r)
		}
	}
}

func TestPickRowRenderDimsUntracked(t *testing.T) {
	r := pickRow{branch: "scratch", untracked: true, detail: untrackedDetail, graph: []graphSpan{{s: "◯", col: -2}}}
	colored := r.render(true, false)
	if !containsANSI(colored) {
		t.Fatalf("untracked row should dim: %q", colored)
	}
	if !strings.Contains(colored, untrackedDetail) {
		t.Fatalf("missing init hint: %q", colored)
	}
}

func TestFilterDeleteRowsOmitsTrunkAndGithub(t *testing.T) {
	rows := []pickRow{
		{branch: "b", text: "◯  b"},
		{branch: "a", text: "◯  a"},
		{branch: "main", trunk: true, text: "◯  main"},
		githubStacksRow(),
	}
	got := filterDeleteRows(rows)
	if len(got) != 2 || got[0].branch != "b" || got[1].branch != "a" {
		t.Fatalf("filterDeleteRows = %#v", got)
	}
}

func TestKeepExistingRowsDropsGhosts(t *testing.T) {
	rows := []pickRow{
		{branch: "ghost"},
		{branch: "real"},
		{branch: "main", trunk: true},
	}
	got := keepExistingRows(rows, map[string]bool{"real": true})
	if len(got) != 2 || got[0].branch != "real" || !got[1].trunk {
		t.Fatalf("keepExistingRows = %#v", got)
	}
}

func TestWithWorktreeStacksAddsMissing(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	got := withWorktreeStacks(st, []string{"a", "main", "other"}, "main")
	if len(got.Stacks) != 2 || got.Stacks[1].Branches[0].Branch != "other" {
		t.Fatalf("withWorktreeStacks = %#v", got.Stacks)
	}
}

func TestEnsureCurrentLeavesPresent(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	forest := stackForest(st, "a")
	got := ensureCurrent(forest, "a")
	if len(got) != len(forest) {
		t.Fatalf("duplicated current: got %d rows, want %d", len(got), len(forest))
	}
}

func assertRows(t *testing.T, got, want []pickRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d\n got %#v\nwant %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i].branch != want[i].branch || got[i].text != want[i].text || got[i].current != want[i].current || got[i].openGithub != want[i].openGithub || got[i].trunk != want[i].trunk {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
