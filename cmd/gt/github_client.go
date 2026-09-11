package main

import (
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

// This file implements the github_client.go contract from contracts.go: a
// go-gh backed GitHubClient that reuses `gh` auth/config (and therefore
// supports GHES) and fetches PR state in a single GraphQL round-trip.
//
// The query-builder and response-decoder are split out as pure functions
// (buildBatchPRQuery, decodeBatchPRResponse) so they can be unit-tested
// without touching the network. Only NewGitHubClient and BatchPRSnapshot
// perform I/O.
//
// One new dependency only (design decision #1): github.com/cli/go-gh/v2.
// No other third-party imports are added by this file.

// prSelectionSet is the GraphQL field set requested for every PR, both via
// the pullRequests connection (head lookup) and the pullRequest object
// (number lookup). Keeping one shared selection set guarantees the two
// shapes decode identically.
const prSelectionSet = "{\n" +
	"  number\n" +
	"  id\n" +
	"  state\n" +
	"  baseRefName\n" +
	"  headRefName\n" +
	"  isDraft\n" +
	"  merged\n" +
	"  url\n" +
	"  autoMergeRequest { mergeMethod }\n" +
	"}"

// NewGitHubClient resolves the current repository from git remotes (or
// GH_REPO) via go-gh's repository.Current, which is the same resolution `gh`
// itself uses. The returned client reuses gh's auth/config, so GHES hosts
// configured via `gh auth login --hostname ...` work without extra flags.
//
// The resolved owner/name is stored in c.repo as "OWNER/REPO"; the host is
// not stored because api.DefaultGraphQLClient resolves the correct GraphQL
// endpoint from gh's config on its own.
func NewGitHubClient() (*GitHubClient, error) {
	repo, err := repository.Current()
	if err != nil {
		return nil, fmt.Errorf("resolving current repository: %w", err)
	}
	return &GitHubClient{repo: repo.Owner + "/" + repo.Name}, nil
}

// BatchPRSnapshot fetches the GitHub-side view of every PR whose head is in
// heads, plus any PR numbers in known whose head is not already in heads, in
// a single GraphQL request. The result is keyed by head branch name.
//
// heads are queried via aliased `pullRequests(headRefName: $hN, first: 1)`
// connections; known numbers not covered by heads are queried via aliased
// `pullRequest(number: $nN)` objects. All aliases live inside one
// `repository(owner: $owner, name: $name)` selection so the whole batch is
// one HTTP call regardless of stack depth.
func (c *GitHubClient) BatchPRSnapshot(known map[string]int, heads []string) (map[string]PRSnapshot, error) {
	query, aliasToHead := buildBatchPRQuery(known, heads)
	if query == "" {
		return map[string]PRSnapshot{}, nil
	}

	owner, name, ok := strings.Cut(c.repo, "/")
	if !ok {
		return nil, fmt.Errorf("invalid repository spec %q (want OWNER/REPO)", c.repo)
	}

	variables := buildBatchPRVariables(owner, name, known, heads, aliasToHead)

	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("building graphql client: %w", err)
	}

	var raw map[string]any
	if err := client.Do(query, variables, &raw); err != nil {
		return nil, fmt.Errorf("batch PR query: %w", err)
	}

	inner, _ := raw["repository"].(map[string]any)
	if inner == nil {
		return nil, fmt.Errorf("batch PR query: no repository in response")
	}
	return decodeBatchPRResponse(inner, aliasToHead)
}

// buildBatchPRQuery constructs the GraphQL query string for BatchPRSnapshot.
// It is pure: no I/O, no allocation of variables. Heads receive aliases
// h0, h1, ... in input order; known PR numbers whose head is not in heads
// receive aliases n0, n1, ... in a deterministic (sorted) order so the query
// is stable across runs.
//
// It returns the query string and a map from alias to head branch name. Only
// head aliases appear in the returned map: number aliases resolve their head
// from the response's headRefName field at decode time. An empty query string
// means there is nothing to fetch.
func buildBatchPRQuery(known map[string]int, heads []string) (string, map[string]string) {
	headSet := map[string]bool{}
	for _, h := range heads {
		headSet[h] = true
	}

	// Numbers to query by number: those in known whose head is not in heads.
	// Skip a number whose head is already being fetched by name (it would
	// just duplicate the head alias). Sort for determinism so two calls with
	// the same inputs produce the same query string.
	var nums []int
	seen := map[int]bool{}
	for head, n := range known {
		if headSet[head] {
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		nums = append(nums, n)
	}
	// Insertion-sort: the number of distinct numbers is small (stack depth).
	for i := 1; i < len(nums); i++ {
		for j := i; j > 0 && nums[j-1] > nums[j]; j-- {
			nums[j-1], nums[j] = nums[j], nums[j-1]
		}
	}

	// Nothing to fetch: emit no query so BatchPRSnapshot skips the network.
	if len(heads) == 0 && len(nums) == 0 {
		return "", map[string]string{}
	}

	aliasToHead := map[string]string{}
	var b strings.Builder
	b.WriteString("query BatchPR($owner: String!, $name: String!")
	for i, h := range heads {
		alias := fmt.Sprintf("h%d", i)
		aliasToHead[alias] = h
		fmt.Fprintf(&b, ", $%s: String!", alias)
	}
	for i := range nums {
		fmt.Fprintf(&b, ", $n%d: Int!", i)
	}
	b.WriteString(") {\n  repository(owner: $owner, name: $name) {\n")

	for i := range heads {
		alias := fmt.Sprintf("h%d", i)
		fmt.Fprintf(&b, "    %s: pullRequests(headRefName: $%s, first: 1, "+
			"orderBy: {field: CREATED_AT, direction: DESC}) {\n", alias, alias)
		b.WriteString("      nodes ")
		b.WriteString(prSelectionSet)
		b.WriteString("\n    }\n")
	}
	for i := range nums {
		fmt.Fprintf(&b, "    n%d: pullRequest(number: $n%d) ", i, i)
		b.WriteString(prSelectionSet)
		b.WriteString("\n")
	}
	b.WriteString("  }\n}\n")

	return b.String(), aliasToHead
}

// buildBatchPRVariables builds the variables map that pairs with the query
// produced by buildBatchPRQuery. It is separate from the builder so the
// builder stays pure and unit-testable without variables.
func buildBatchPRVariables(owner, name string, known map[string]int, heads []string, aliasToHead map[string]string) map[string]any {
	vars := map[string]any{
		"owner": owner,
		"name":  name,
	}
	for alias, head := range aliasToHead {
		vars[alias] = head
	}
	// Rebuild number variables in the same sorted order the builder used,
	// skipping numbers whose head is already fetched by name.
	headSet := map[string]bool{}
	for _, h := range heads {
		headSet[h] = true
	}
	var nums []int
	seen := map[int]bool{}
	for head, n := range known {
		if headSet[head] {
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		nums = append(nums, n)
	}
	for i := 1; i < len(nums); i++ {
		for j := i; j > 0 && nums[j-1] > nums[j]; j-- {
			nums[j-1], nums[j] = nums[j], nums[j-1]
		}
	}
	for i, n := range nums {
		vars[fmt.Sprintf("n%d", i)] = n
	}
	return vars
}

// decodeBatchPRResponse maps the inner `repository { ... }` object of a
// GraphQL response into PRSnapshots keyed by head branch name. It is pure:
// given the raw decoded JSON object and the alias→head map from the query
// builder, it produces the snapshot map without any I/O.
//
// Head aliases (h0, h1, ...) are pullRequests connections: their value is
// { "nodes": [ { ... } ] }; the first node is the PR (none if the branch has
// no PR). Number aliases (n0, n1, ...) are pullRequest objects: their value
// is the PR object directly, or null if no PR with that number exists. The
// head for a number alias is read from the response's headRefName.
func decodeBatchPRResponse(raw map[string]any, aliasToHead map[string]string) (map[string]PRSnapshot, error) {
	out := make(map[string]PRSnapshot, len(raw))
	for alias, head := range aliasToHead {
		conn, ok := raw[alias].(map[string]any)
		if !ok {
			continue
		}
		nodes, _ := conn["nodes"].([]any)
		if len(nodes) == 0 {
			continue
		}
		node, _ := nodes[0].(map[string]any)
		if node == nil {
			continue
		}
		snap, err := decodePRNode(node)
		if err != nil {
			return nil, fmt.Errorf("alias %s: %w", alias, err)
		}
		snap.Head = head
		out[head] = snap
	}
	// Number aliases: any key in raw not in aliasToHead is a number alias.
	for alias, val := range raw {
		if _, isHead := aliasToHead[alias]; isHead {
			continue
		}
		if len(alias) == 0 || alias[0] != 'n' {
			continue
		}
		node, _ := val.(map[string]any)
		if node == nil {
			continue // PR not found / null
		}
		snap, err := decodePRNode(node)
		if err != nil {
			return nil, fmt.Errorf("alias %s: %w", alias, err)
		}
		if snap.Head == "" {
			continue
		}
		out[snap.Head] = snap
	}
	return out, nil
}

// decodePRNode maps one GraphQL PR object (number/id/state/baseRefName/
// headRefName/isDraft/merged/url/autoMergeRequest{mergeMethod}) into a PRSnapshot.
// State is uppercased to the contract's "OPEN"/"MERGED"/"CLOSED" form.
func decodePRNode(node map[string]any) (PRSnapshot, error) {
	snap := PRSnapshot{
		Number:           toInt(node["number"]),
		NodeID:           toString(node["id"]),
		State:            strings.ToUpper(toString(node["state"])),
		Base:             toString(node["baseRefName"]),
		Head:             toString(node["headRefName"]),
		Draft:            toBool(node["isDraft"]),
		Merged:           toBool(node["merged"]),
		URL:              toString(node["url"]),
		AutoMergeEnabled: autoMergeEnabled(node["autoMergeRequest"]),
	}
	return snap, nil
}

// autoMergeEnabled reports whether auto-merge is enabled for a PR. The
// GraphQL AutoMergeRequest object is present (non-null) iff auto-merge is
// enabled, so presence — not an inner field — is the signal. (GitHub's
// AutoMergeRequest type does not expose an `enabled` field.)
func autoMergeEnabled(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func toBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	default:
		return 0
	}
}
