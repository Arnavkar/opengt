package main

import (
	"reflect"
	"testing"
)

func TestPushArgsNewBranchPlainRefspec(t *testing.T) {
	refs := []PushRef{{Branch: "b", LocalSHA: "abc", IsNew: true}}
	args := pushArgs(refs, PushOpts{})
	if !contains(args, "refs/heads/b:refs/heads/b") {
		t.Fatalf("missing plain refspec for new branch: %v", args)
	}
	for _, a := range args {
		if len(a) > len("--force-with-lease=") && a[:len("--force-with-lease=")] == "--force-with-lease=" {
			t.Fatalf("new branch must not carry a lease, got %q", a)
		}
	}
}

func TestPushArgsExistingBranchHasLeaseAndRefspec(t *testing.T) {
	refs := []PushRef{{Branch: "b", LocalSHA: "abc", ExpectedOld: "deadbeef", IsNew: false}}
	args := pushArgs(refs, PushOpts{})
	if !contains(args, "--force-with-lease=b:deadbeef") {
		t.Fatalf("missing lease for existing branch: %v", args)
	}
	if !contains(args, "refs/heads/b:refs/heads/b") {
		t.Fatalf("missing refspec for existing branch: %v", args)
	}
}

func TestPushArgsLeaseValueFromSnapshot(t *testing.T) {
	refs := []PushRef{{Branch: "feat", ExpectedOld: "cafebabe", IsNew: false}}
	args := pushArgs(refs, PushOpts{})
	if !contains(args, "--force-with-lease=feat:cafebabe") {
		t.Fatalf("lease value not taken from ExpectedOld: %v", args)
	}
}

func TestPushArgsNoVerify(t *testing.T) {
	refs := []PushRef{{Branch: "b", IsNew: true}}
	args := pushArgs(refs, PushOpts{NoVerify: true})
	if !contains(args, "--no-verify") {
		t.Fatalf("missing --no-verify: %v", args)
	}
}

func TestPushArgsDryRun(t *testing.T) {
	refs := []PushRef{{Branch: "b", IsNew: true}}
	args := pushArgs(refs, PushOpts{DryRun: true})
	if !contains(args, "--dry-run") {
		t.Fatalf("missing --dry-run: %v", args)
	}
}

func TestPushArgsAtomicAlwaysPresent(t *testing.T) {
	refs := []PushRef{{Branch: "b", IsNew: true}}
	args := pushArgs(refs, PushOpts{})
	if len(args) == 0 || args[0] != "--atomic" {
		t.Fatalf("--atomic must lead the arg vector when refs non-empty: %v", args)
	}
}

func TestPushArgsEmptyRefsShortCircuits(t *testing.T) {
	if got := pushArgs(nil, PushOpts{}); got != nil {
		t.Fatalf("pushArgs(nil) = %v, want nil", got)
	}
	if got := pushArgs([]PushRef{}, PushOpts{}); got != nil {
		t.Fatalf("pushArgs([]) = %v, want nil", got)
	}
	if err := AtomicPush(nil, PushOpts{}); err != nil {
		t.Fatalf("AtomicPush(nil) = %v, want nil", err)
	}
	if err := AtomicPush([]PushRef{}, PushOpts{}); err != nil {
		t.Fatalf("AtomicPush([]) = %v, want nil", err)
	}
}

func TestPushArgsForceDropsLeases(t *testing.T) {
	refs := []PushRef{
		{Branch: "a", ExpectedOld: "111", IsNew: false},
		{Branch: "b", IsNew: true},
	}
	args := pushArgs(refs, PushOpts{Force: true})
	if !contains(args, "--force") {
		t.Fatalf("missing --force: %v", args)
	}
	for _, a := range args {
		if len(a) > len("--force-with-lease=") && a[:len("--force-with-lease=")] == "--force-with-lease=" {
			t.Fatalf("--force must drop leases, got %q", a)
		}
	}
	if !contains(args, "refs/heads/a:refs/heads/a") || !contains(args, "refs/heads/b:refs/heads/b") {
		t.Fatalf("refspecs missing under --force: %v", args)
	}
}

func TestPushArgsOrderingLeasesBeforeFlagsBeforeRefspecs(t *testing.T) {
	refs := []PushRef{
		{Branch: "a", ExpectedOld: "1", IsNew: false},
		{Branch: "b", IsNew: true},
	}
	args := pushArgs(refs, PushOpts{NoVerify: true, DryRun: true})
	want := []string{
		"--atomic", "origin",
		"--force-with-lease=a:1",
		"--no-verify",
		"--dry-run",
		"refs/heads/a:refs/heads/a",
		"refs/heads/b:refs/heads/b",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("arg vector mismatch:\n got %v\nwant %v", args, want)
	}
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}
