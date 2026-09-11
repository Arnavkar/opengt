package main

import (
	"reflect"
	"testing"
	"time"
)

func TestParseLsRemoteHeadsWithSHA(t *testing.T) {
	out := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\trefs/heads/main\n" +
		"cafecafecafecafecafecafecafecafecafecafe\trefs/heads/chore/x\n"
	got := parseLsRemoteHeadsWithSHA(out)
	want := map[string]string{
		"main":    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"chore/x": "cafecafecafecafecafecafecafecafecafecafe",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLsRemoteHeadsWithSHA = %#v, want %#v", got, want)
	}
}

func TestParseLsRemoteHeadsWithSHA_SpaceSeparator(t *testing.T) {
	// Some servers emit a space instead of a tab; the parser should still cope,
	// matching parseLsRemoteHeads's fallback.
	got := parseLsRemoteHeadsWithSHA("abc123 refs/heads/main")
	if got["main"] != "abc123" {
		t.Fatalf("space-separated line not parsed: %#v", got)
	}
}

func TestParseLsRemoteHeadsWithSHA_BlankAndNonHeads(t *testing.T) {
	out := "\n" +
		"111 refs/tags/v1.0\n" +
		"222\trefs/heads/main\n"
	got := parseLsRemoteHeadsWithSHA(out)
	if len(got) != 1 || got["main"] != "222" {
		t.Fatalf("non-heads/blank lines not filtered: %#v", got)
	}
}

func TestBuildRemoteRefSnapshot_MissingAndPresent(t *testing.T) {
	out := "aaa\trefs/heads/main\n" +
		"bbb\trefs/heads/feature\n"
	requested := []string{"main", "missing", "feature"}
	at := time.Unix(1700000000, 0)

	snap := buildRemoteRefSnapshot(out, requested, at)
	if !snap.FetchedAt.Equal(at) {
		t.Fatalf("FetchedAt = %v, want %v", snap.FetchedAt, at)
	}
	main := snap.Refs["main"]
	if !main.Exists || main.RemoteSHA != "aaa" {
		t.Fatalf("main = %#v, want Exists=true RemoteSHA=aaa", main)
	}
	feat := snap.Refs["feature"]
	if !feat.Exists || feat.RemoteSHA != "bbb" {
		t.Fatalf("feature = %#v, want Exists=true RemoteSHA=bbb", feat)
	}
	miss := snap.Refs["missing"]
	if miss.Exists || miss.RemoteSHA != "" {
		t.Fatalf("missing = %#v, want Exists=false RemoteSHA=\"\"", miss)
	}
}

func TestBuildRemoteRefSnapshot_RequestedOrderPreserved(t *testing.T) {
	// Even when ls-remote lists branches in a different order, the snapshot
	// keys are the requested branches.
	out := "111\trefs/heads/zeta\n" +
		"222\trefs/heads/alpha\n"
	requested := []string{"alpha", "zeta"}
	snap := buildRemoteRefSnapshot(out, requested, time.Now())
	if snap.Refs["alpha"].RemoteSHA != "222" {
		t.Fatalf("alpha SHA = %q, want 222", snap.Refs["alpha"].RemoteSHA)
	}
	if snap.Refs["zeta"].RemoteSHA != "111" {
		t.Fatalf("zeta SHA = %q, want 111", snap.Refs["zeta"].RemoteSHA)
	}
}

func TestDedupBranches(t *testing.T) {
	got := dedupBranches([]string{"main", "feature", "main", "chore/x", "", "feature"})
	want := []string{"main", "feature", "chore/x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupBranches = %#v, want %#v", got, want)
	}
}
