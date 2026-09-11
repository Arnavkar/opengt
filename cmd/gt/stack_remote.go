package main

import (
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

// This file implements the stack_remote.go contract from contracts.go: a
// StackRemoteClient backed by the GitHub-native stack object.
// (RemoteStackSnapshot: id + ordered PR numbers).
//
// The native stack object mirrors gh-stack v0.1.0 schema v1 (see
// cmd/gt/stack.go: a stack has an id and an ordered list of branches, each
// carrying a pullRequest{number}). The GraphQL shape below is the
// GitHub-side projection of that schema: a Stack node with an ordered
// `entries` connection whose nodes expose `pullRequest { number }`. The
// exact field names track the native stacks API as of schema v1; if the
// upstream API renames fields, only the query/decode pairs here change.
//
// Idempotency (performance acceptance: "Stack update: zero mutations when
// order unchanged") is enforced two ways:
//   - UpdateStack reads the existing order and skips the mutation when it
//     already equals the desired order.
//   - syncStackOrder is the orchestration callers use: it reads, compares
//     via the pure stackUpdateNeeded helper, and only invokes UpdateStack
//     when the order differs, returning whether a mutation was made.
//
// The real client performs GraphQL via go-gh's api client (reusing `gh`
// auth/config, so GHES works). The GraphQL surface is hidden behind the
// unexported stackAPI interface so the network layer is swappable; tests
// do not exercise the real client, they drive a fake StackRemoteClient
// through syncStackOrder and unit-test the pure helpers directly.

// stackAPI is the minimal GraphQL surface stack_remote.go needs. The real
// production implementation is ghGraphQLClient wrapping go-gh's
// *api.GraphQLClient; tests never instantiate it (they use a fake
// StackRemoteClient instead).
type stackAPI interface {
	Do(query string, vars map[string]any) (map[string]any, error)
}

// ghGraphQLClient adapts *api.GraphQLClient to stackAPI. Do returns the
// decoded top-level JSON object so callers can pull out the fields they
// need without re-decoding.
type ghGraphQLClient struct {
	inner *api.GraphQLClient
}

func (c *ghGraphQLClient) Do(query string, vars map[string]any) (map[string]any, error) {
	var raw map[string]any
	if err := c.inner.Do(query, vars, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ghStackRemoteClient implements StackRemoteClient against the
// GitHub-native stack object. repo is "OWNER/REPO" (resolved once via
// NewGitHubClient so GH_REPO / GHES config is honored identically to the
// PR path).
type ghStackRemoteClient struct {
	repo  string
	graph stackAPI
}

// NewStackRemoteClient builds a StackRemoteClient by resolving the current
// repository through NewGitHubClient (reusing `gh` auth/config) and a go-gh
// GraphQL client. Both errors surface so callers can tell auth failure from
// repo-resolution failure.
func NewStackRemoteClient() (StackRemoteClient, error) {
	ghc, err := NewGitHubClient()
	if err != nil {
		return nil, fmt.Errorf("stack remote: %w", err)
	}
	g, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("stack remote: building graphql client: %w", err)
	}
	return &ghStackRemoteClient{repo: ghc.repo, graph: &ghGraphQLClient{inner: g}}, nil
}

// GetStack reads the native stack object for stackID and returns its id
// plus the ordered PR numbers it contains. A non-existent stack surfaces
// as a nil snapshot with a nil error so callers can distinguish "absent"
// from "failed".
//
// Query mirrors gh-stack v0.1.0 schema v1: a Stack node with an ordered
// `entries` connection, each entry exposing its pullRequest number.
func (c *ghStackRemoteClient) GetStack(stackID string) (*RemoteStackSnapshot, error) {
	owner, name, ok := strings.Cut(c.repo, "/")
	if !ok {
		return nil, fmt.Errorf("stack remote: invalid repository spec %q (want OWNER/REPO)", c.repo)
	}
	if stackID == "" {
		return nil, nil
	}

	const query = `query GetStack($owner: String!, $name: String!, $id: ID!) {
  repository(owner: $owner, name: $name) {
    stack(id: $id) {
      id
      entries(first: 100) {
        nodes {
          pullRequest {
            number
          }
        }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": name, "id": stackID}

	raw, err := c.graph.Do(query, vars)
	if err != nil {
		return nil, fmt.Errorf("stack remote: get stack %q: %w", stackID, err)
	}
	return decodeStackSnapshot(raw, stackID)
}

// CreateStack creates a new native stack containing the given PR numbers in
// order and returns the new stack id. The mutation is a single GraphQL
// call; the response carries the allocated id.
func (c *ghStackRemoteClient) CreateStack(prNumbers []int) (string, error) {
	owner, name, ok := strings.Cut(c.repo, "/")
	if !ok {
		return "", fmt.Errorf("stack remote: invalid repository spec %q (want OWNER/REPO)", c.repo)
	}

	const query = `mutation CreateStack($owner: String!, $name: String!, $numbers: [Int!]!) {
  createStack(input: {repository: {owner: $owner, name: $name}, pullRequestNumbers: $numbers}) {
    stack {
      id
    }
  }
}`
	vars := map[string]any{
		"owner":   owner,
		"name":    name,
		"numbers": prNumbers,
	}

	raw, err := c.graph.Do(query, vars)
	if err != nil {
		return "", fmt.Errorf("stack remote: create stack: %w", err)
	}
	return decodeCreatedStackID(raw)
}

// UpdateStack makes the native stack object reflect prNumbers in order. It
// is IDEMPOTENT: the existing order is read first and the mutation is
// skipped entirely when it already equals prNumbers (performance
// acceptance: zero mutations when order unchanged). The mutation runs only
// when stackUpdateNeeded reports a difference.
func (c *ghStackRemoteClient) UpdateStack(stackID string, prNumbers []int) error {
	if stackID == "" {
		return fmt.Errorf("stack remote: update requires a non-empty stack id")
	}

	existing, err := c.GetStack(stackID)
	if err != nil {
		return fmt.Errorf("stack remote: read before update: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("stack remote: stack %q not found", stackID)
	}
	if !stackUpdateNeeded(existing.Numbers, prNumbers) {
		return nil
	}

	const query = `mutation UpdateStack($id: ID!, $numbers: [Int!]!) {
  updateStack(input: {id: $id, pullRequestNumbers: $numbers}) {
    stack {
      id
    }
  }
}`
	vars := map[string]any{"id": stackID, "numbers": prNumbers}

	if _, err := c.graph.Do(query, vars); err != nil {
		return fmt.Errorf("stack remote: update stack %q: %w", stackID, err)
	}
	return nil
}

// --------------------------------------------------------------------------------
// Pure helpers (no I/O) — unit-tested directly.
// --------------------------------------------------------------------------------

// orderEqual reports whether two PR-number slices are identical in length
// and element-by-element order. nil and empty slices compare equal.
func orderEqual(a, b []int) bool {
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

// stackUpdateNeeded reports whether the existing ordered PR numbers differ
// from the desired order. It returns true when any element differs or the
// lengths differ; false only when the two slices are identical in order.
// This is the pure idempotency decision: UpdateStack mutates only when this
// returns true.
func stackUpdateNeeded(existing, desired []int) bool {
	return !orderEqual(existing, desired)
}

// syncStackOrder is the orchestration callers use to (re)set a stack's PR
// order idempotently. It reads the current order, compares via
// stackUpdateNeeded, and invokes UpdateStack only when the order differs.
// The returned bool is true when a mutation was made, false when the stack
// was already in the desired order (or when GetStack reports the stack
// absent and there is nothing to mutate).
//
// This is the seam tests drive through a fake StackRemoteClient: it
// exercises the read->compare->maybe-mutate flow without touching the
// network. UpdateStack re-checks idempotency internally too, so a
// concurrent reorder between the read here and the mutation is still safe
// (the worst case is a redundant read, never a redundant mutation).
func syncStackOrder(c StackRemoteClient, stackID string, numbers []int) (bool, error) {
	existing, err := c.GetStack(stackID)
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, nil
	}
	if !stackUpdateNeeded(existing.Numbers, numbers) {
		return false, nil
	}
	if err := c.UpdateStack(stackID, numbers); err != nil {
		return false, err
	}
	return true, nil
}

// --------------------------------------------------------------------------------
// Response decoders (pure: raw map -> typed snapshot).
// --------------------------------------------------------------------------------

// decodeStackSnapshot pulls the Stack node out of a GetStack response and
// returns its id plus the ordered PR numbers. A null/missing stack node
// yields a nil snapshot (the stack does not exist) without an error.
func decodeStackSnapshot(raw map[string]any, stackID string) (*RemoteStackSnapshot, error) {
	repo, _ := raw["repository"].(map[string]any)
	if repo == nil {
		return nil, nil
	}
	stack, _ := repo["stack"].(map[string]any)
	if stack == nil {
		return nil, nil
	}
	id := toString(stack["id"])
	if id == "" {
		id = stackID
	}
	entries, _ := stack["entries"].(map[string]any)
	nodes, _ := entries["nodes"].([]any)

	numbers := make([]int, 0, len(nodes))
	for _, n := range nodes {
		entry, _ := n.(map[string]any)
		if entry == nil {
			continue
		}
		pr, _ := entry["pullRequest"].(map[string]any)
		if pr == nil {
			continue
		}
		numbers = append(numbers, toInt(pr["number"]))
	}
	return &RemoteStackSnapshot{ID: id, Numbers: numbers}, nil
}

// decodeCreatedStackID pulls the new stack id out of a CreateStack response.
func decodeCreatedStackID(raw map[string]any) (string, error) {
	create, _ := raw["createStack"].(map[string]any)
	if create == nil {
		return "", fmt.Errorf("stack remote: create response missing createStack")
	}
	stack, _ := create["stack"].(map[string]any)
	if stack == nil {
		return "", fmt.Errorf("stack remote: create response missing stack")
	}
	id := toString(stack["id"])
	if id == "" {
		return "", fmt.Errorf("stack remote: create response missing stack id")
	}
	return id, nil
}
