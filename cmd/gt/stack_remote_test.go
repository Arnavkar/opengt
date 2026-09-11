package main

// These tests cover the network-free pieces of stack_remote.go:
//   - the pure idempotency helpers (orderEqual, stackUpdateNeeded)
//   - the pure response decoders (decodeStackSnapshot, decodeCreatedStackID)
//   - the syncStackOrder orchestration driven through a fake
//     StackRemoteClient (no network, no go-gh).
//
// NewStackRemoteClient and the ghStackRemoteClient methods themselves touch
// the network and are intentionally not exercised here.

import (
	"fmt"
	"reflect"
	"testing"
)

// --------------------------------------------------------------------------------
// Pure helpers.
// --------------------------------------------------------------------------------

func TestOrderEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b []int
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []int{}, []int{}, true},
		{"nil vs empty", nil, []int{}, true},
		{"identical single", []int{1}, []int{1}, true},
		{"identical multi", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"differ by element", []int{1, 2, 3}, []int{1, 2, 4}, false},
		{"differ by order", []int{1, 2, 3}, []int{3, 2, 1}, false},
		{"a shorter", []int{1, 2}, []int{1, 2, 3}, false},
		{"b shorter", []int{1, 2, 3}, []int{1, 2}, false},
		{"a empty b nonempty", []int{}, []int{1}, false},
		{"duplicates same", []int{1, 1, 2}, []int{1, 1, 2}, true},
		{"duplicates differ", []int{1, 1, 2}, []int{1, 2, 2}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := orderEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("orderEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestStackUpdateNeeded(t *testing.T) {
	cases := []struct {
		name              string
		existing, desired []int
		want              bool
	}{
		{"identical -> no update", []int{1, 2, 3}, []int{1, 2, 3}, false},
		{"both empty -> no update", nil, nil, false},
		{"order differs -> update", []int{1, 2, 3}, []int{3, 2, 1}, true},
		{"element differs -> update", []int{1, 2, 3}, []int{1, 2, 4}, true},
		{"length differs (a longer) -> update", []int{1, 2, 3}, []int{1, 2}, true},
		{"length differs (b longer) -> update", []int{1, 2}, []int{1, 2, 3}, true},
		{"existing empty desired nonempty -> update", nil, []int{1}, true},
		{"existing nonempty desired empty -> update", []int{1}, nil, true},
		{"single element identical -> no update", []int{42}, []int{42}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stackUpdateNeeded(tc.existing, tc.desired); got != tc.want {
				t.Errorf("stackUpdateNeeded(%v, %v) = %v, want %v",
					tc.existing, tc.desired, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------------
// Pure decoders.
// --------------------------------------------------------------------------------

func TestDecodeStackSnapshot_Present(t *testing.T) {
	raw := map[string]any{
		"repository": map[string]any{
			"stack": map[string]any{
				"id": "STACK_ID_1",
				"entries": map[string]any{
					"nodes": []any{
						map[string]any{"pullRequest": map[string]any{"number": float64(101)}},
						map[string]any{"pullRequest": map[string]any{"number": float64(102)}},
						map[string]any{"pullRequest": map[string]any{"number": float64(103)}},
					},
				},
			},
		},
	}
	got, err := decodeStackSnapshot(raw, "STACK_ID_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a snapshot, got nil")
	}
	want := &RemoteStackSnapshot{ID: "STACK_ID_1", Numbers: []int{101, 102, 103}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeStackSnapshot = %+v, want %+v", got, want)
	}
}

func TestDecodeStackSnapshot_Absent(t *testing.T) {
	// stack node null -> nil snapshot, no error.
	raw := map[string]any{"repository": map[string]any{"stack": nil}}
	got, err := decodeStackSnapshot(raw, "STACK_ID_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil snapshot for absent stack, got %+v", got)
	}
}

func TestDecodeStackSnapshot_MissingRepository(t *testing.T) {
	got, err := decodeStackSnapshot(map[string]any{}, "STACK_ID_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil snapshot when repository missing, got %+v", got)
	}
}

func TestDecodeStackSnapshot_FallsBackToStackIDArg(t *testing.T) {
	// id field empty -> snapshot.ID falls back to the stackID argument.
	raw := map[string]any{
		"repository": map[string]any{
			"stack": map[string]any{
				"entries": map[string]any{
					"nodes": []any{
						map[string]any{"pullRequest": map[string]any{"number": float64(7)}},
					},
				},
			},
		},
	}
	got, err := decodeStackSnapshot(raw, "FALLBACK_ID")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != "FALLBACK_ID" {
		t.Fatalf("expected ID FALLBACK_ID, got %+v", got)
	}
	if !reflect.DeepEqual(got.Numbers, []int{7}) {
		t.Errorf("Numbers = %v, want [7]", got.Numbers)
	}
}

func TestDecodeCreatedStackID(t *testing.T) {
	raw := map[string]any{
		"createStack": map[string]any{
			"stack": map[string]any{"id": "NEW_STACK_ID"},
		},
	}
	got, err := decodeCreatedStackID(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "NEW_STACK_ID" {
		t.Errorf("decodeCreatedStackID = %q, want NEW_STACK_ID", got)
	}
}

func TestDecodeCreatedStackID_Errors(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"missing createStack", map[string]any{}},
		{"missing stack", map[string]any{"createStack": map[string]any{}}},
		{"missing id", map[string]any{"createStack": map[string]any{"stack": map[string]any{}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeCreatedStackID(tc.raw); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

// --------------------------------------------------------------------------------
// syncStackOrder orchestration via a fake StackRemoteClient (no network).
// --------------------------------------------------------------------------------

// testFakeStackRemoteClient is an in-memory StackRemoteClient. It holds the
// current state of one stack (by id) and records every call so tests can
// assert which methods ran and in what order. updateCalls counts only
// UpdateStack invocations; the orchestration under test must skip it when
// the order is unchanged.
type testFakeStackRemoteClient struct {
	// state maps stack id -> current ordered numbers.
	state map[string][]int
	// errors to return, keyed by method. nil means succeed.
	getErr    error
	createErr error
	updateErr error
	// call counters.
	getCalls    int
	createCalls int
	updateCalls int
	// last numbers passed to UpdateStack, if any.
	lastUpdateNumbers []int
}

func newFakeStackClient(initial map[string][]int) *testFakeStackRemoteClient {
	if initial == nil {
		initial = map[string][]int{}
	}
	return &testFakeStackRemoteClient{state: initial}
}

func (f *testFakeStackRemoteClient) GetStack(stackID string) (*RemoteStackSnapshot, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	nums, ok := f.state[stackID]
	if !ok {
		return nil, nil
	}
	// Return a copy so callers can't mutate our state.
	cp := append([]int(nil), nums...)
	return &RemoteStackSnapshot{ID: stackID, Numbers: cp}, nil
}

func (f *testFakeStackRemoteClient) CreateStack(prNumbers []int) (string, error) {
	f.createCalls++
	if f.createErr != nil {
		return "", f.createErr
	}
	id := fmt.Sprintf("STACK_%d", f.createCalls)
	f.state[id] = append([]int(nil), prNumbers...)
	return id, nil
}

func (f *testFakeStackRemoteClient) UpdateStack(stackID string, prNumbers []int) error {
	f.updateCalls++
	f.lastUpdateNumbers = append([]int(nil), prNumbers...)
	if f.updateErr != nil {
		return f.updateErr
	}
	f.state[stackID] = append([]int(nil), prNumbers...)
	return nil
}

func TestSyncStackOrder_SkipsWhenOrderUnchanged(t *testing.T) {
	fake := newFakeStackClient(map[string][]int{
		"S1": {101, 102, 103},
	})

	mutated, err := syncStackOrder(fake, "S1", []int{101, 102, 103})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mutated {
		t.Errorf("mutated = true, want false (order unchanged)")
	}
	if fake.updateCalls != 0 {
		t.Errorf("UpdateStack called %d times, want 0 (zero mutations when order unchanged)",
			fake.updateCalls)
	}
	if fake.getCalls != 1 {
		t.Errorf("GetStack called %d times, want 1", fake.getCalls)
	}
}

func TestSyncStackOrder_MutatesWhenOrderDiffers(t *testing.T) {
	fake := newFakeStackClient(map[string][]int{
		"S1": {101, 102, 103},
	})

	mutated, err := syncStackOrder(fake, "S1", []int{103, 102, 101})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mutated {
		t.Errorf("mutated = false, want true (order differs)")
	}
	if fake.updateCalls != 1 {
		t.Errorf("UpdateStack called %d times, want 1", fake.updateCalls)
	}
	if !reflect.DeepEqual(fake.lastUpdateNumbers, []int{103, 102, 101}) {
		t.Errorf("UpdateStack called with %v, want [103 102 101]", fake.lastUpdateNumbers)
	}
	if got := fake.state["S1"]; !reflect.DeepEqual(got, []int{103, 102, 101}) {
		t.Errorf("state[S1] = %v, want [103 102 101]", got)
	}
}

func TestSyncStackOrder_MutatesWhenLengthDiffers(t *testing.T) {
	fake := newFakeStackClient(map[string][]int{
		"S1": {101, 102},
	})

	mutated, err := syncStackOrder(fake, "S1", []int{101, 102, 103})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mutated {
		t.Errorf("mutated = false, want true (length differs)")
	}
	if fake.updateCalls != 1 {
		t.Errorf("UpdateStack called %d times, want 1", fake.updateCalls)
	}
}

func TestSyncStackOrder_AbsentStackNoMutation(t *testing.T) {
	fake := newFakeStackClient(map[string][]int{})

	mutated, err := syncStackOrder(fake, "MISSING", []int{1, 2, 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mutated {
		t.Errorf("mutated = true, want false (stack absent)")
	}
	if fake.updateCalls != 0 {
		t.Errorf("UpdateStack called %d times, want 0 for absent stack", fake.updateCalls)
	}
}

func TestSyncStackOrder_PropagatesGetStackError(t *testing.T) {
	fake := newFakeStackClient(map[string][]int{"S1": {1}})
	fake.getErr = errFake("simulated network failure")

	if _, err := syncStackOrder(fake, "S1", []int{1, 2}); err == nil {
		t.Fatal("expected error from GetStack, got nil")
	}
	if fake.updateCalls != 0 {
		t.Errorf("UpdateStack called %d times, want 0 after GetStack error", fake.updateCalls)
	}
}

func TestSyncStackOrder_PropagatesUpdateStackError(t *testing.T) {
	fake := newFakeStackClient(map[string][]int{"S1": {1, 2, 3}})
	fake.updateErr = errFake("simulated mutation failure")

	if _, err := syncStackOrder(fake, "S1", []int{3, 2, 1}); err == nil {
		t.Fatal("expected error from UpdateStack, got nil")
	}
	// UpdateStack was attempted (and failed) exactly once.
	if fake.updateCalls != 1 {
		t.Errorf("UpdateStack called %d times, want 1", fake.updateCalls)
	}
}

// errFake is a trivial error type for the fake.
type errFake string

func (e errFake) Error() string { return string(e) }
