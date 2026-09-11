package main

import (
	"encoding/json"
	"testing"
)

// stateFrom builds a stackState the way loadState does, from the JSON that
// gh stack writes. The nested types are anonymous, so a literal is not
// practical, and going through the decoder also covers the field tags.
func stateFrom(t *testing.T, jsonText string) *stackState {
	t.Helper()
	var st stackState
	if err := json.Unmarshal([]byte(jsonText), &st); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return &st
}

const linearStack = `{
  "schemaVersion": 1,
  "stacks": [
    {"trunk": {"branch": "main"},
     "branches": [{"branch": "a"}, {"branch": "b"}, {"branch": "c"}]}
  ]
}`

func TestLocateLinear(t *testing.T) {
	st := stateFrom(t, linearStack)
	tests := []struct {
		branch string
		want   position
	}{
		{"a", position{inStack: true, parent: "main"}},
		{"b", position{inStack: true, parent: "a"}},
		{"c", position{inStack: true, atTop: true, parent: "b"}},
		// The trunk is not a member of the stack it anchors.
		{"main", position{}},
		{"never-heard-of-it", position{}},
	}
	for _, tt := range tests {
		if got := locate(st, tt.branch); got != tt.want {
			t.Errorf("locate(%q) = %+v, want %+v", tt.branch, got, tt.want)
		}
	}
}

func TestLocateSingleBranchStackIsTop(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "solo"}]}]
    }`)
	want := position{inStack: true, atTop: true, parent: "main"}
	if got := locate(st, "solo"); got != want {
		t.Errorf("locate(\"solo\") = %+v, want %+v", got, want)
	}
}

// A branch in two stacks at once is the fork case gt refuses to act on.
func TestLocateForkedByTwoStacks(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [
        {"trunk": {"branch": "main"}, "branches": [{"branch": "a"}, {"branch": "b"}]},
        {"trunk": {"branch": "main"}, "branches": [{"branch": "b"}, {"branch": "d"}]}
      ]
    }`)
	if got := locate(st, "b"); !got.forked {
		t.Errorf("locate(\"b\").forked = false, want true; got %+v", got)
	}
	if got := locate(st, "d"); got.forked {
		t.Errorf("locate(\"d\").forked = true, want false; got %+v", got)
	}
}

// A branch that anchors a second stack while sitting inside a first one is
// also a fork, even though it appears in Branches only once.
func TestLocateForkedAsTrunkOfAnotherStack(t *testing.T) {
	st := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [
        {"trunk": {"branch": "main"}, "branches": [{"branch": "a"}, {"branch": "b"}]},
        {"trunk": {"branch": "b"}, "branches": [{"branch": "c"}]}
      ]
    }`)
	if got := locate(st, "b"); !got.forked {
		t.Errorf("locate(\"b\").forked = false, want true; got %+v", got)
	}
	want := position{inStack: true, atTop: true, parent: "b"}
	if got := locate(st, "c"); got != want {
		t.Errorf("locate(\"c\") = %+v, want %+v", got, want)
	}
}

func TestDescendantsAfter(t *testing.T) {
	st := stateFrom(t, linearStack)
	s := st.Stacks[0]
	parent, rest, ok := descendantsAfter(s, "b")
	if !ok || parent != "a" || len(rest) != 1 || rest[0].Branch != "c" {
		t.Fatalf("delete b: parent=%q rest=%v ok=%v", parent, rest, ok)
	}
	parent, rest, ok = descendantsAfter(s, "a")
	if !ok || parent != "main" || len(rest) != 2 || rest[0].Branch != "b" || rest[1].Branch != "c" {
		t.Fatalf("delete a: parent=%q rest=%v ok=%v", parent, rest, ok)
	}
	parent, rest, ok = descendantsAfter(s, "c")
	if !ok || parent != "b" || len(rest) != 0 {
		t.Fatalf("delete tip: parent=%q rest=%v ok=%v", parent, rest, ok)
	}
	if _, _, ok := descendantsAfter(s, "missing"); ok {
		t.Fatal("missing branch should not be found")
	}
}

func TestStackContaining(t *testing.T) {
	st := stateFrom(t, linearStack)
	s, ok := stackContaining(st, "b")
	if !ok || len(s.Branches) != 3 {
		t.Fatalf("stackContaining b = %#v ok=%v", s, ok)
	}
	if _, ok := stackContaining(st, "main"); ok {
		t.Fatal("trunk is not a stack member")
	}
}

func TestLocateNoStacks(t *testing.T) {
	st := stateFrom(t, `{"schemaVersion": 1}`)
	if got := locate(st, "main"); got != (position{}) {
		t.Errorf("locate on empty state = %+v, want zero", got)
	}
}

func TestMergeStacksUnionsDistinct(t *testing.T) {
	dst := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	src := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "b"}]}]
    }`)
	mergeStacks(dst, src)
	if len(dst.Stacks) != 2 {
		t.Fatalf("got %d stacks, want 2", len(dst.Stacks))
	}
	got := stackForest(dst, "b")
	if len(got) != 3 {
		t.Fatalf("forest has %d rows, want a, b, and main", len(got))
	}
}

func TestMergeStacksDedupesIdentical(t *testing.T) {
	dst := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	src := stateFrom(t, `{
      "schemaVersion": 1,
      "stacks": [{"trunk": {"branch": "main"}, "branches": [{"branch": "a"}]}]
    }`)
	mergeStacks(dst, src)
	if len(dst.Stacks) != 1 {
		t.Fatalf("got %d stacks, want 1", len(dst.Stacks))
	}
}
