package main

import (
	"strings"
	"testing"
)

// These tests cover the pure, network-free pieces of github_client.go:
// buildBatchPRQuery (query string + alias map) and decodeBatchPRResponse
// (fake GraphQL response -> PRSnapshots). NewGitHubClient / BatchPRSnapshot
// themselves touch the network and are not exercised here.

func TestBuildBatchPRQuery_AliasesPerHeadAndNumber(t *testing.T) {
	known := map[string]int{"feat-a": 101, "feat-b": 202, "feat-c": 303}
	heads := []string{"feat-a", "feat-d"}

	q, aliasToHead := buildBatchPRQuery(known, heads)

	if q == "" {
		t.Fatal("expected a non-empty query")
	}

	// One head alias per head, named h0.. in input order.
	if len(aliasToHead) != 2 {
		t.Fatalf("aliasToHead = %v, want 2 entries", aliasToHead)
	}
	if aliasToHead["h0"] != "feat-a" {
		t.Errorf("h0 = %q, want feat-a", aliasToHead["h0"])
	}
	if aliasToHead["h1"] != "feat-d" {
		t.Errorf("h1 = %q, want feat-d", aliasToHead["h1"])
	}

	// feat-a is in heads, so it must NOT also be queried by number. feat-b
	// and feat-c are not in heads, so they get number aliases n0/n1.
	for _, alias := range []string{"h0", "h1"} {
		if !strings.Contains(q, alias+": pullRequests(") {
			t.Errorf("query missing head alias %q: %s", alias, q)
		}
	}
	if !strings.Contains(q, "n0: pullRequest(number: $n0)") {
		t.Errorf("query missing n0 number alias: %s", q)
	}
	if !strings.Contains(q, "n1: pullRequest(number: $n1)") {
		t.Errorf("query missing n1 number alias: %s", q)
	}
	// Exactly two number aliases (feat-b=202, feat-c=303); no n2.
	if strings.Contains(q, "n2:") {
		t.Errorf("query should not contain n2: %s", q)
	}
}

func TestBuildBatchPRQuery_SelectionSet(t *testing.T) {
	// "x" is fetched by head; "y"'s number (2) is fetched by number alias
	// because "y" is not in heads. Both paths share the selection set.
	known := map[string]int{"x": 1, "y": 2}
	heads := []string{"x"}
	q, _ := buildBatchPRQuery(known, heads)

	for _, field := range []string{
		"number",
		"id",
		"state",
		"baseRefName",
		"headRefName",
		"isDraft",
		"merged",
		"url",
		"autoMergeRequest { mergeMethod }",
	} {
		if !strings.Contains(q, field) {
			t.Errorf("query missing selection %q:\n%s", field, q)
		}
	}
	// Both the pullRequests connection nodes and the pullRequest object
	// share the selection set, so autoMergeRequest appears in both.
	if strings.Count(q, "autoMergeRequest { mergeMethod }") < 2 {
		t.Errorf("expected autoMergeRequest in both head and number selections:\n%s", q)
	}
}

func TestBuildBatchPRQuery_StableAndUnique(t *testing.T) {
	known := map[string]int{"b": 2, "a": 1, "c": 3}
	heads := []string{"h2", "h0", "h1"}

	q1, a1 := buildBatchPRQuery(known, heads)
	q2, a2 := buildBatchPRQuery(known, heads)

	if q1 != q2 {
		t.Fatal("buildBatchPRQuery is not deterministic for identical input")
	}
	if !mapsEqual(a1, a2) {
		t.Fatal("aliasToHead is not deterministic for identical input")
	}

	// Head aliases preserve input order regardless of head name sorting.
	if a1["h0"] != "h2" || a1["h1"] != "h0" || a1["h2"] != "h1" {
		t.Errorf("head alias order wrong: %v", a1)
	}

	// All aliases are unique.
	seen := map[string]bool{}
	for alias := range a1 {
		if seen[alias] {
			t.Errorf("duplicate alias %q", alias)
		}
		seen[alias] = true
	}
	// Number aliases appear in sorted-number order: a=1 -> n0, b=2 -> n1,
	// c=3 -> n2. Assert the query references them in that order.
	idx0 := strings.Index(q1, "n0: pullRequest")
	idx1 := strings.Index(q1, "n1: pullRequest")
	idx2 := strings.Index(q1, "n2: pullRequest")
	if !(idx0 < idx1 && idx1 < idx2) {
		t.Errorf("number aliases not in sorted order: %d %d %d", idx0, idx1, idx2)
	}
}

func TestBuildBatchPRQuery_Empty(t *testing.T) {
	q, aliasToHead := buildBatchPRQuery(nil, nil)
	if q != "" {
		t.Errorf("expected empty query, got %s", q)
	}
	if len(aliasToHead) != 0 {
		t.Errorf("expected empty alias map, got %v", aliasToHead)
	}
}

func TestDecodeBatchPRResponse_HeadAndNumberAliases(t *testing.T) {
	known := map[string]int{"feat-a": 101, "feat-b": 202}
	heads := []string{"feat-a", "feat-d"}
	_, aliasToHead := buildBatchPRQuery(known, heads)

	// Fake the inner repository object. h0 is a head alias (pullRequests
	// connection with nodes); n0 is a number alias (pullRequest object).
	raw := map[string]any{
		"h0": map[string]any{
			"nodes": []any{
				map[string]any{
					"number":      float64(101),
					"id":          "PR_nodeid_a",
					"state":       "open",
					"baseRefName": "main",
					"headRefName": "feat-a",
					"isDraft":     false,
					"merged":      false,
					"url":         "https://example.com/101",
					"autoMergeRequest": map[string]any{
						"enabled": true,
					},
				},
			},
		},
		"h1": map[string]any{
			"nodes": []any{}, // feat-d has no PR
		},
		"n0": map[string]any{ // feat-b queried by number
			"number":      float64(202),
			"id":          "PR_nodeid_b",
			"state":       "merged",
			"baseRefName": "feat-a",
			"headRefName": "feat-b",
			"isDraft":     false,
			"merged":      true,
			"url":         "https://example.com/202",
			// autoMergeRequest absent -> AutoMergeEnabled false
		},
	}

	out, err := decodeBatchPRResponse(raw, aliasToHead)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	a, ok := out["feat-a"]
	if !ok {
		t.Fatal("missing feat-a")
	}
	if a.Number != 101 || a.NodeID != "PR_nodeid_a" {
		t.Errorf("feat-a number/nodeid = %d/%q", a.Number, a.NodeID)
	}
	if a.State != "OPEN" {
		t.Errorf("feat-a state = %q, want OPEN", a.State)
	}
	if a.Base != "main" || a.Head != "feat-a" {
		t.Errorf("feat-a base/head = %q/%q", a.Base, a.Head)
	}
	if a.Draft || a.Merged {
		t.Errorf("feat-a draft/merged = %v/%v, want false/false", a.Draft, a.Merged)
	}
	if !a.AutoMergeEnabled {
		t.Error("feat-a autoMerge should be enabled")
	}
	if a.URL != "https://example.com/101" {
		t.Errorf("feat-a url = %q", a.URL)
	}

	if _, ok := out["feat-d"]; ok {
		t.Error("feat-d has no PR; should not appear in output")
	}

	b, ok := out["feat-b"]
	if !ok {
		t.Fatal("missing feat-b (decoded from number alias via headRefName)")
	}
	if b.Number != 202 || b.State != "MERGED" {
		t.Errorf("feat-b number/state = %d/%q, want 202/MERGED", b.Number, b.State)
	}
	if !b.Merged {
		t.Error("feat-b merged should be true")
	}
	if b.AutoMergeEnabled {
		t.Error("feat-b autoMerge absent -> should be false")
	}
	if b.Head != "feat-b" {
		t.Errorf("feat-b head = %q, want feat-b (from headRefName)", b.Head)
	}
}

func TestDecodeBatchPRResponse_NullNumberAlias(t *testing.T) {
	known := map[string]int{"gone": 999}
	_, aliasToHead := buildBatchPRQuery(known, nil)

	// A number alias whose PR does not exist comes back as null.
	raw := map[string]any{"n0": nil}
	out, err := decodeBatchPRResponse(raw, aliasToHead)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output for null PR, got %v", out)
	}
}

func TestDecodeBatchPRResponse_Empty(t *testing.T) {
	out, err := decodeBatchPRResponse(map[string]any{}, map[string]string{})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output, got %v", out)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
