//go:build integration

package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// buildStackN builds an N-branch linear stack in a fresh fixture and pushes
// every branch to the bare remote, so `gt sync` has remote refs to read. The
// harness has no GitHub API, so PRs are not created here; the sync count tests
// only care about git/gh subprocess invocations, which the announcement lines
// in gt's stderr expose exactly.
func buildStackN(t *testing.T, n int) *fixture {
	t.Helper()
	f := newFixture(t)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("layer-%d", i)
		f.layer(name, "Add "+name)
		f.git("push", "--quiet", "-u", "origin", name)
	}
	return f
}

// stackSizes is the matrix the refactor plan's performance acceptance calls
// out: prove 5 vs 10 branches keeps network RTT count approximately constant.
var stackSizes = []int{1, 3, 5, 10}

// countAnnouncements returns how many times gt announced a command matching
// prefix to stderr. gt prints every subprocess it shells out to (via run /
// runTee / exec1) as `$ <cmd> <args...>`, so this is the exact invocation count
// for that command. Note: gt's quiet read helper `capture` (used for ls-remote,
// rev-parse, for-each-ref, branch --show-current) deliberately does NOT
// announce — those are reads, not the operations the plan counts as network
// RTTs. The observable network-writing shells (gh stack, gh pr, git fetch, git
// push) all go through `run` and so are countable here.
func countAnnouncements(r result, prefix string) int {
	return strings.Count(r.stderr, "$ "+prefix)
}

// TestSyncInvocationCountMatrix asserts the structural performance guarantees
// of a clean `gt sync` across stack depths 1/3/5/10. These are COUNT-assertion
// tests, not testing.B benchmarks: wall time is flaky, but the number of
// subprocess invocations gt announces to stderr is exact and deterministic.
//
// For every depth a clean sync must announce:
//   - zero `gh stack` lines (clean sync never shells out to gh stack)
//   - zero `gh pr` lines (prune consumes the snapshot, not `gh pr list`)
//   - zero `git fetch` lines (the targeted ls-remote is the only network read;
//     a clean sync has nothing to fetch)
//   - zero `git push` lines (sync never pushes; pushing is submit)
//   - exit 0
//
// The refactor plan's "one git ls-remote regardless of stack depth" guarantee
// is implemented by LoadRemoteRefs, which runs `git ls-remote --heads origin
// <branches...>` once via `capture`. `capture` is gt's quiet-read helper and
// deliberately does not announce (it is also used for `git rev-parse`,
// `git branch --show-current`, etc.), so the single ls-remote is not visible in
// stderr. The observable, deterministic proof of the targeted strategy is the
// cross-depth assertion below: the announced-subprocess profile is identical
// (all zero) from 1 to 10 branches — "5 vs 10 branches: network RTT count
// approximately constant", made exact.
//
// The PR GraphQL load fails in this harness (no GitHub) and gt prints a stderr
// note about it; that is expected and not asserted on.
func TestSyncInvocationCountMatrix(t *testing.T) {
	type counts struct {
		ghStack, ghPR, gitFetch, gitPush, total int
	}
	got := make([]counts, len(stackSizes))

	for i, n := range stackSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			f := buildStackN(t, n)
			r := f.gt("sync")

			if r.code != 0 {
				t.Fatalf("gt sync exited %d, want 0\n%s", r.code, r.output())
			}
			ghStack := countAnnouncements(r, "gh stack")
			ghPR := countAnnouncements(r, "gh pr")
			gitFetch := countAnnouncements(r, "git fetch")
			gitPush := countAnnouncements(r, "git push")

			if ghStack != 0 {
				t.Errorf("n=%d: clean sync announced %d `gh stack` invocations, want 0\n%s", n, ghStack, r.output())
			}
			if ghPR != 0 {
				t.Errorf("n=%d: clean sync announced %d `gh pr` invocations, want 0\n%s", n, ghPR, r.output())
			}
			if gitFetch != 0 {
				t.Errorf("n=%d: clean sync announced %d `git fetch` invocations, want 0\n%s", n, gitFetch, r.output())
			}
			if gitPush != 0 {
				t.Errorf("n=%d: clean sync announced %d `git push` invocations, want 0\n%s", n, gitPush, r.output())
			}

			got[i] = counts{
				ghStack: ghStack, ghPR: ghPR, gitFetch: gitFetch, gitPush: gitPush,
				total: strings.Count(r.stderr, "$ "),
			}
		})
	}

	// Cross-depth: the announced-subprocess profile must be the same for a
	// 1-branch stack and a 10-branch stack. This is the deterministic form of
	// "5 vs 10 branches: network RTT count approximately constant" — the count
	// does not grow with the stack.
	for i := 1; i < len(got); i++ {
		if got[i] != got[0] {
			t.Errorf("announced-subprocess profile differs by depth: n=%d -> %+v, n=%d -> %+v (want identical)",
				stackSizes[0], got[0], stackSizes[i], got[i])
		}
	}
}

// TestSyncNoGhStackAcrossDepths makes the "prune no longer shells out to
// `gh pr list`" guarantee explicit across depths. A clean sync must announce
// zero `gh stack` and zero `gh pr` lines — neither the legacy stack rebase nor
// the old `gh pr list` prune path may run on the common path.
func TestSyncNoGhStackAcrossDepths(t *testing.T) {
	for _, n := range []int{1, 5, 10} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			f := buildStackN(t, n)
			r := f.gt("sync")

			if r.code != 0 {
				t.Fatalf("gt sync exited %d, want 0\n%s", r.code, r.output())
			}
			if got := countAnnouncements(r, "gh stack"); got != 0 {
				t.Errorf("n=%d: sync announced %d `gh stack` lines, want 0\n%s", n, got, r.output())
			}
			if got := countAnnouncements(r, "gh pr"); got != 0 {
				t.Errorf("n=%d: sync announced %d `gh pr` lines, want 0 (prune must not shell out to gh pr)\n%s", n, got, r.output())
			}
		})
	}
}

// skipNoGitHub skips the calling test unless GT_TEST_GITHUB is set. The submit
// path needs a real GitHub: it loads a PR snapshot and runs `gh pr create`.
// The harness deliberately has no GitHub, so these tests skip locally and run
// only in an environment that opted in.
//
// This lives in bench_test.go and is reused by the GitHub-gated submit tests in
// workflow_test.go (same package), so the gate has one definition.
func skipNoGitHub(t *testing.T) {
	t.Helper()
	if os.Getenv("GT_TEST_GITHUB") == "" {
		t.Skip("set GT_TEST_GITHUB=1 to exercise the submit push-count bench (needs GitHub)")
	}
}

// TestSubmitPushCountGitHubGated proves "changed submit = exactly one git push
// regardless of stack depth" (refactor plan, decision #5). For each depth in
// {1,3,5,10} it builds a stack with PRs, changes the top branch, runs
// `gt submit`, and asserts exactly one `git push` announcement — the single
// atomic push (AtomicPush in push.go shells out via `run`, so it is visible) —
// no matter how deep the stack is.
//
// This test is GitHub-gated and skips locally. It still compiles and runs far
// enough to hit the skip, which is what verifies the gate works.
func TestSubmitPushCountGitHubGated(t *testing.T) {
	skipNoGitHub(t)

	for _, n := range stackSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			f := buildStackN(t, n)

			// In a GitHub-enabled run the branches would have PRs from a prior
			// submit. Change the top branch so the next submit has work to do.
			top := fmt.Sprintf("layer-%d", n-1)
			f.git("checkout", "--quiet", top)
			f.write(fmt.Sprintf("%s-extra.txt", top), "change\n")
			f.git("add", "-A")
			f.git("commit", "--quiet", "-m", "Change "+top)

			r := f.gt("submit")

			if r.code != 0 {
				t.Fatalf("gt submit exited %d, want 0\n%s", r.code, r.output())
			}
			if got := countAnnouncements(r, "git push"); got != 1 {
				t.Errorf("n=%d: submit announced %d `git push` invocations, want exactly 1 (atomic push regardless of depth)\n%s",
					n, got, r.output())
			}
		})
	}
}
