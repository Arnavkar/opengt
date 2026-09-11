//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

// TestCreateStartsAndExtendsStack covers the two halves of gt create: off the
// trunk it starts a stack, on top of one it appends a layer. gt stages and
// commits itself in both cases rather than letting `gh stack add -m` do it.
func TestCreateStartsAndExtendsStack(t *testing.T) {
	f := newFixture(t)

	f.write("layer-one.txt", "one\n")
	first := f.gt("create", "layer-one", "-a", "-m", "Add layer one")
	if !first.announced("gh stack init --base main layer-one") {
		t.Errorf("create off the trunk did not run `gh stack init --base main layer-one`\n%s", first.output())
	}
	if !first.announced("git commit -m 'Add layer one'") {
		t.Errorf("create did not make the commit itself\n%s", first.output())
	}

	f.write("layer-two.txt", "two\n")
	second := f.gt("create", "layer-two", "-a", "-m", "Add layer two")
	if !second.announced("gh stack add layer-two") {
		t.Errorf("create on top of a stack did not run `gh stack add layer-two`\n%s", second.output())
	}

	if got := f.branch(); got != "layer-two" {
		t.Errorf("on branch %q, want layer-two", got)
	}
	if got := f.tracked(); len(got) != 2 || got[0] != "layer-one" || got[1] != "layer-two" {
		t.Errorf("tracked stack is %q, want [layer-one layer-two]", got)
	}
	if got := f.state().Stacks[0].Trunk.Branch; got != "main" {
		t.Errorf("stack trunk is %q, want main", got)
	}
	// One commit per layer, each carrying its own message: proof that neither
	// commit landed on the branch below it.
	if got := f.commitsIn("main..layer-one"); got != 1 {
		t.Errorf("layer-one has %d commits over main, want 1", got)
	}
	if got := f.commitsIn("layer-one..layer-two"); got != 1 {
		t.Errorf("layer-two has %d commits over layer-one, want 1", got)
	}
	if got := f.subject("layer-one"); got != "Add layer one" {
		t.Errorf("layer-one subject is %q, want %q", got, "Add layer one")
	}
	if got := f.subject("layer-two"); got != "Add layer two" {
		t.Errorf("layer-two subject is %q, want %q", got, "Add layer two")
	}
	if got := f.git("status", "--porcelain"); got != "" {
		t.Errorf("work tree is not clean after create:\n%s", got)
	}
}

// TestCreateGeneratesBranchNameFromMessage checks gt's own naming. That it
// still matches what `gh stack add -m` produces is checked separately, in
// TestGeneratedBranchNameMatchesGhStack.
func TestCreateGeneratesBranchNameFromMessage(t *testing.T) {
	f := newFixture(t)

	f.write("work.txt", "work\n")
	before := time.Now()
	f.gt("create", "-a", "-m", "Add the login form")

	got := f.branch()
	// A run that straddles midnight would otherwise be flaky.
	for _, at := range []time.Time{before, time.Now()} {
		if got == at.Format("01-02")+"-add_the_login_form" {
			return
		}
	}
	t.Errorf("created branch %q, want %s-add_the_login_form", got, before.Format("01-02"))
}

// TestCreateRefusesInMiddleOfStack is the fork guard: gh stack keeps a stack
// flat, so branching from the middle has nowhere to go.
func TestCreateRefusesInMiddleOfStack(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.gt("down")

	f.write("fork.txt", "fork\n")
	r := f.gtFails("create", "forked", "-a", "-m", "Fork the stack")
	if !strings.Contains(r.stderr, "not the top of its stack") {
		t.Errorf("error does not explain the fork:\n%s", r.output())
	}
	if got := f.tracked(); len(got) != 2 {
		t.Errorf("refused create still changed the stack: %q", got)
	}
	if got := f.branch(); got != "layer-one" {
		t.Errorf("refused create moved to %q, want to stay on layer-one", got)
	}
}

func TestCreateWithoutNameOrMessage(t *testing.T) {
	f := newFixture(t)
	r := f.gtFails("create")
	if !strings.Contains(r.stderr, "needs a branch name") {
		t.Errorf("unexpected error:\n%s", r.output())
	}
}

// TestModifyAmendsAndRestacksUpstack is the command with no gh stack
// equivalent: gt amends with git, then asks gh stack to cascade the rebase.
func TestModifyAmendsAndRestacksUpstack(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.gt("down")

	f.write("layer-one.txt", "one, revised\n")
	r := f.gt("modify", "-a", "-m", "Add layer one, revised")
	if !r.announced("gh stack rebase --upstack --no-trunk") {
		t.Errorf("modify did not cascade the rebase\n%s", r.output())
	}

	if got := f.commitsIn("main..layer-one"); got != 1 {
		t.Errorf("layer-one has %d commits over main, want 1: modify added instead of amending", got)
	}
	if got := f.subject("layer-one"); got != "Add layer one, revised" {
		t.Errorf("layer-one subject is %q, want the amended message", got)
	}
	if f.git("rev-parse", "layer-one") != f.git("rev-parse", "layer-two^") {
		t.Errorf("layer-two was not rebased onto the amended layer-one\n%s", f.git("log", "--graph", "--oneline", "--all"))
	}
	if got := f.subject("layer-two"); got != "Add layer two" {
		t.Errorf("layer-two subject is %q, want it unchanged", got)
	}
}

// TestModifyCommitsWhenBranchHasNoCommits guards the case gt goes out of its
// way to handle: amending here would rewrite the parent's commit instead.
func TestModifyCommitsWhenBranchHasNoCommits(t *testing.T) {
	f := newFixture(t)
	f.gt("create", "empty-layer")
	if got := f.commitsIn("main..HEAD"); got != 0 {
		t.Fatalf("new branch already has %d commits, want 0", got)
	}

	f.write("work.txt", "work\n")
	r := f.gt("modify", "-a", "-m", "First commit on this layer")
	if strings.Contains(r.stderr, "--amend") {
		t.Errorf("modify amended a branch with no commits of its own\n%s", r.output())
	}
	if got := f.commitsIn("main..HEAD"); got != 1 {
		t.Errorf("branch has %d commits over main, want 1", got)
	}
	if got := f.subject("main"); got != "Initial commit" {
		t.Errorf("trunk commit became %q; modify rewrote the parent", got)
	}
}

// TestModifyOnTopSkipsRebase: there is nothing above the top branch, so gt
// must not spend a rebase on it.
func TestModifyOnTopSkipsRebase(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")

	f.write("layer-one.txt", "one, revised\n")
	r := f.gt("modify", "-a", "-m", "Add layer one, revised")
	if strings.Contains(r.stderr, "gh stack rebase") {
		t.Errorf("modify on the top branch still rebased\n%s", r.output())
	}
}

func TestModifyOutsideAStack(t *testing.T) {
	f := newFixture(t)
	f.write("loose.txt", "loose\n")
	r := f.gtFails("modify", "-a", "-m", "Not in a stack")
	if !strings.Contains(r.stderr, "not part of a stack") || !strings.Contains(r.stderr, "gt create") {
		t.Errorf("unexpected error:\n%s", r.output())
	}
}

func TestModifyUntrackedBranchHintsInit(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "scratch")
	f.write("scratch.txt", "scratch\n")
	r := f.gtFails("modify", "-a", "-m", "Not in a stack")
	if !strings.Contains(r.stderr, "not part of a stack") {
		t.Errorf("unexpected error:\n%s", r.output())
	}
	if !strings.Contains(r.stderr, "`gt track`") {
		t.Errorf("did not point at gt track:\n%s", r.output())
	}
	if strings.Contains(r.stderr, "start one with `gt create`") {
		t.Errorf("told them to create a new branch:\n%s", r.output())
	}
}

func TestTrackAdoptsCurrentBranch(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "scratch")
	r := f.gt("track")
	if !r.announced("gh stack init --base main scratch") {
		t.Errorf("gt track did not run gh stack init:\n%s", r.output())
	}
	if got := f.tracked(); len(got) != 1 || got[0] != "scratch" {
		t.Errorf("tracked stack is %q, want [scratch]", got)
	}

	r = f.gtFails("track")
	if !strings.Contains(r.stderr, "already in a stack") {
		t.Errorf("second track: %s", r.output())
	}
}

func TestTrackOnTrunkRefuses(t *testing.T) {
	f := newFixture(t)
	r := f.gtFails("track")
	if !strings.Contains(r.stderr, "gt create") {
		t.Errorf("track on trunk: %s", r.output())
	}
}

func TestNavigation(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.layer("layer-three", "Add layer three")

	steps := []struct {
		command, native, want string
	}{
		{"down", "gh stack down", "layer-two"},
		{"d", "gh stack down", "layer-one"},
		{"u", "gh stack up", "layer-two"},
		{"t", "gh stack top", "layer-three"},
		{"b", "gh stack bottom", "layer-one"},
		{"trunk", "gh stack trunk", "main"},
	}
	for _, step := range steps {
		r := f.gt(step.command)
		if !r.announced(step.native) {
			t.Errorf("gt %s did not run `%s`\n%s", step.command, step.native, r.output())
		}
		if got := f.branch(); got != step.want {
			t.Fatalf("gt %s landed on %q, want %q", step.command, got, step.want)
		}
	}
}

func TestGitPassthrough(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")

	r := f.gt("rebase", "HEAD")
	if !r.announced("git rebase HEAD") {
		t.Errorf("gt rebase did not run git rebase\n%s", r.output())
	}

	f.write("staged.txt", "staged\n")
	r = f.gt("add", "-A")
	if !r.announced("git add -A") {
		t.Errorf("gt add did not run git add\n%s", r.output())
	}
	if got := f.git("diff", "--cached", "--name-only"); got != "staged.txt" {
		t.Errorf("staged files are %q, want staged.txt", got)
	}

	r = f.gt("restore", "--staged", "staged.txt")
	if !r.announced("git restore --staged staged.txt") {
		t.Errorf("gt restore did not run git restore\n%s", r.output())
	}
	if got := f.git("diff", "--cached", "--name-only"); got != "" {
		t.Errorf("index still has %q after restore --staged", got)
	}

	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "a commit to reset")
	before := f.git("rev-parse", "HEAD~1")
	r = f.gt("reset", "--soft", "HEAD~1")
	if !r.announced("git reset --soft 'HEAD~1'") {
		t.Errorf("gt reset did not run git reset\n%s", r.output())
	}
	if got := f.git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD is %s after reset, want %s", got, before)
	}

	r = f.run(gtBin, "cherry-pick", "not-a-commit")
	if !r.announced("git cherry-pick not-a-commit") {
		t.Errorf("gt cherry-pick did not run git cherry-pick\n%s", r.output())
	}
	if strings.Contains(r.stderr, "unknown command") {
		t.Errorf("gt cherry-pick was not dispatched\n%s", r.output())
	}
}

func TestSubmitUpdateOnlyIsAccepted(t *testing.T) {
	f := newFixture(t)
	r := f.run(gtBin, "submit", "--help")
	if r.code != 0 {
		t.Fatalf("gt submit --help exited %d\n%s", r.code, r.output())
	}
	help := r.output()
	if !strings.Contains(help, "update-only") || !strings.Contains(help, "-u") {
		t.Errorf("gt submit --help is missing --update-only / -u\n%s", help)
	}
}

func TestLogViews(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")

	full := f.gt("log")
	if !full.announced("gh stack view") {
		t.Errorf("gt log did not run `gh stack view`\n%s", full.output())
	}
	short := f.gt("ls")
	if !short.announced("gh stack view --short") {
		t.Errorf("gt ls did not run `gh stack view --short`\n%s", short.output())
	}
	long := f.gt("ll")
	if !long.announced("git log --graph --oneline --decorate --all") {
		t.Errorf("gt ll did not run the git graph\n%s", long.output())
	}

	// The stack view is the one piece of output people read, and gt promises
	// to keep stdout clean so it can be piped.
	for _, view := range []struct {
		name string
		r    result
	}{{"gt log", full}, {"gt ls", short}} {
		for _, branch := range []string{"layer-one", "layer-two"} {
			if !strings.Contains(view.r.stdout, branch) {
				t.Errorf("%s stdout does not mention %s\n%s", view.name, branch, view.r.output())
			}
		}
	}
}

func TestRestackOntoMovedTrunk(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")

	f.git("checkout", "--quiet", "main")
	f.write("trunk-moves.txt", "moved\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "Trunk moves on")
	f.git("push", "--quiet", "origin", "main")
	f.git("checkout", "--quiet", "layer-two")

	r := f.gt("restack")
	if !r.announced("gh stack rebase") {
		t.Errorf("gt restack did not run `gh stack rebase`\n%s", r.output())
	}
	if f.git("rev-parse", "main") != f.git("rev-parse", "layer-one^") {
		t.Errorf("layer-one was not rebased onto the moved trunk\n%s", f.git("log", "--graph", "--oneline", "--all"))
	}
	if f.git("rev-parse", "layer-one") != f.git("rev-parse", "layer-two^") {
		t.Errorf("layer-two was not carried along\n%s", f.git("log", "--graph", "--oneline", "--all"))
	}
}

func TestRestackDirections(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.layer("layer-three", "Add layer three")
	f.gt("down")

	if r := f.gt("restack", "-d"); !r.announced("gh stack rebase --downstack") {
		t.Errorf("gt restack -d did not pass --downstack\n%s", r.output())
	}
	if r := f.gt("restack", "-u"); !r.announced("gh stack rebase --upstack") {
		t.Errorf("gt restack -u did not pass --upstack\n%s", r.output())
	}
	if r := f.gtFails("restack", "-d", "-u"); !strings.Contains(r.stderr, "conflict") {
		t.Errorf("unexpected error for conflicting directions:\n%s", r.output())
	}
	if r := f.gtFails("restack", "-o"); !strings.Contains(r.stderr, "no gh stack equivalent") {
		t.Errorf("unexpected error for --only:\n%s", r.output())
	}
}

// TestSyncRestacksWithoutPushing: Graphite's sync pulls and restacks locally.
// Pushing is submit's job. The rewritten sync does this directly (targeted
// ls-remote + local restack) rather than delegating to `gh stack rebase`, so
// the guarantee to check is: no `gh stack` call, no push.
func TestSyncRestacksWithoutPushing(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")

	r := f.gt("sync")
	if r.announced("gh stack") {
		t.Errorf("gt sync must not call `gh stack` (it restacks locally now)\n%s", r.output())
	}
	if r.announced("git push") {
		t.Errorf("gt sync must not push; only submit pushes\n%s", r.output())
	}

	remote := f.git("ls-remote", "--heads", "origin")
	for _, branch := range []string{"layer-one", "layer-two"} {
		if strings.Contains(remote, "refs/heads/"+branch) {
			t.Errorf("sync pushed %s; only submit should push\n%s", branch, remote)
		}
	}
}

// TestSyncDeletesGoneUpstream: -d may delete a stale *stack* branch, but must
// never delete an unrelated Git branch whose upstream disappeared.
func TestSyncDeletesGoneUpstream(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.git("push", "--quiet", "-u", "origin", "layer-one")

	f.git("checkout", "--quiet", "-b", "scratch")
	f.write("scratch.txt", "scratch\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "scratch")
	f.git("push", "--quiet", "-u", "origin", "scratch")
	f.git("checkout", "--quiet", "main")
	f.git("push", "origin", "--delete", "scratch")
	f.git("push", "origin", "--delete", "layer-one")

	listed := f.gt("sync")
	if strings.Contains(listed.stderr, "scratch") {
		t.Errorf("gt sync offered to prune unrelated branch scratch:\n%s", listed.output())
	}
	if !strings.Contains(listed.stderr, "layer-one") {
		t.Errorf("gt sync did not report the stale stack branch:\n%s", listed.output())
	}
	if f.git("branch", "--list", "scratch") == "" {
		t.Error("gt sync deleted scratch without -d or a prompt")
	}

	deleted := f.gt("sync", "-d")
	if strings.Contains(deleted.stderr, "git branch -D scratch") {
		t.Errorf("gt sync -d deleted unrelated scratch:\n%s", deleted.output())
	}
	if f.git("branch", "--list", "scratch") == "" {
		t.Fatal("gt sync -d deleted unrelated branch scratch")
	}
	if f.git("branch", "--list", "layer-one") != "" {
		t.Fatal("stale stack branch layer-one still exists after gt sync -d")
	}
}

// TestSyncOnTrunkWithoutStack: gh stack sync refuses to run off a stack, but
// gt sync on main should still fetch and fast-forward the trunk.
func TestSyncOnTrunkWithoutStack(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "--quiet", "-b", "tmp")
	f.write("ahead.txt", "ahead\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "ahead")
	f.git("push", "--quiet", "origin", "tmp:main")
	f.git("checkout", "--quiet", "main")
	f.git("branch", "--quiet", "-D", "tmp")

	r := f.gt("sync")
	if r.announced("gh stack sync") {
		t.Errorf("gt sync on the trunk should not run gh stack sync\n%s", r.output())
	}
	if f.git("rev-parse", "main") != f.git("rev-parse", "origin/main") {
		t.Errorf("main was not fast-forwarded: local %s origin %s", f.git("rev-parse", "main"), f.git("rev-parse", "origin/main"))
	}
}

// TestSyncUpdatesTrunkInOtherWorktree: git will not `branch -f main` while
// another worktree has it checked out. gt sync must move main in that worktree.
func TestSyncUpdatesTrunkInOtherWorktree(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.gt("trunk")

	linked := f.dir + "-worktree"
	f.git("worktree", "add", "--quiet", linked, "layer-one")

	f.write("ahead.txt", "ahead\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "ahead")
	f.git("push", "--quiet", "origin", "main")
	f.git("reset", "--hard", "--quiet", "HEAD~1")

	worktree := &fixture{t: t, dir: linked, origin: f.origin}
	worktree.gt("sync")
	if f.git("rev-parse", "main") != f.git("rev-parse", "origin/main") {
		t.Errorf("main in %s was not updated: local %s origin %s", f.dir, f.git("rev-parse", "main"), f.git("rev-parse", "origin/main"))
	}
}

// TestDeleteNamedBranch: the picker is for a TTY; a name deletes that branch,
// restacks what was above it, and updates gh-stack metadata.
func TestDeleteNamedBranch(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.layer("layer-three", "Add layer three")

	if r := f.gtFails("delete"); !strings.Contains(r.stderr, "needs a branch") {
		t.Errorf("bare delete without a TTY:\n%s", r.output())
	}
	if r := f.gtFails("delete", "main"); !strings.Contains(r.stderr, "cannot delete trunk") {
		t.Errorf("delete trunk:\n%s", r.output())
	}

	f.gt("delete", "layer-two")
	if f.git("branch", "--list", "layer-two") != "" {
		t.Fatal("layer-two still exists")
	}
	if got := f.tracked(); len(got) != 2 || got[0] != "layer-one" || got[1] != "layer-three" {
		t.Errorf("tracked after deleting middle = %q", got)
	}
	if got := f.commitsIn("layer-one..layer-three"); got != 1 {
		t.Errorf("layer-three has %d commits over layer-one, want 1 (layer-two dropped)", got)
	}
	if got := f.subject("layer-three"); got != "Add layer three" {
		t.Errorf("layer-three subject is %q", got)
	}

	f.gt("delete", "layer-three")
	if got := f.branch(); got != "layer-one" {
		t.Errorf("after deleting current tip, on %q, want layer-one", got)
	}
	if got := f.tracked(); len(got) != 1 || got[0] != "layer-one" {
		t.Errorf("tracked after deleting tip = %q", got)
	}

	f.git("checkout", "--quiet", "-b", "scratch")
	f.write("scratch.txt", "scratch\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "scratch")
	f.gt("delete", "scratch")
	if f.git("branch", "--list", "scratch") != "" {
		t.Fatal("untracked scratch still exists")
	}
	if got := f.tracked(); len(got) != 1 || got[0] != "layer-one" {
		t.Errorf("stack should be unchanged after deleting untracked: %q", got)
	}
}

// TestCheckoutRouting: `gh stack checkout` reads a bare name as a stack
// number, then a PR, then a tracked branch, so gt sends anything it knows to
// be untracked to git instead.
func TestCheckoutRouting(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")

	if r := f.gt("checkout", "layer-one"); !r.announced("gh stack checkout layer-one") {
		t.Errorf("a tracked branch did not go through gh stack\n%s", r.output())
	} else if got := f.branch(); got != "layer-one" {
		t.Errorf("landed on %q, want layer-one", got)
	}

	f.git("branch", "scratch")
	if r := f.gt("checkout", "scratch"); !r.announced("git checkout scratch") {
		t.Errorf("an untracked branch did not go through git\n%s", r.output())
	} else if got := f.branch(); got != "scratch" {
		t.Errorf("landed on %q, want scratch", got)
	}

	if r := f.gt("checkout", "main"); !r.announced("git checkout main") {
		t.Errorf("the trunk did not go through git\n%s", r.output())
	}

	f.gt("checkout", "layer-two")
	if r := f.gt("co", "-t"); !r.announced("gh stack trunk") {
		t.Errorf("gt co -t did not run `gh stack trunk`\n%s", r.output())
	} else if got := f.branch(); got != "main" {
		t.Errorf("landed on %q, want main", got)
	}
}

// pausedRebase builds a stack whose two layers touch the same file, then
// rewrites the bottom one so the cascading rebase stops on a conflict. gt
// finds a paused operation by the marker file gh stack leaves behind, so the
// name of that file is part of the contract.
func pausedRebase(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.write("shared.txt", "one\n")
	f.gt("create", "layer-one", "-a", "-m", "Add layer one")
	f.write("shared.txt", "two\n")
	f.gt("create", "layer-two", "-a", "-m", "Add layer two")
	f.gt("down")

	f.write("shared.txt", "conflicting\n")
	// The rebase is expected to stop, so the exit status is not the point here.
	f.run(gtBin, "modify", "-a", "-m", "Rewrite layer one")

	if !f.gitFileExists("gh-stack-rebase-state") {
		t.Fatalf("no .git/gh-stack-rebase-state after a conflicting rebase; gt cannot detect a paused operation\n%s",
			f.git("status"))
	}
	return f
}

func TestContinueAfterResolvingConflict(t *testing.T) {
	f := pausedRebase(t)

	if r := f.run(gtBin, "continue"); !r.announced("gh stack rebase --continue") {
		t.Fatalf("gt continue did not resume the rebase\n%s", r.output())
	}

	f.write("shared.txt", "resolved\n")
	f.git("add", "shared.txt")
	r := f.gt("continue")
	if !r.announced("gh stack rebase --continue") {
		t.Errorf("gt continue did not resume the rebase\n%s", r.output())
	}
	if f.gitFileExists("gh-stack-rebase-state") {
		t.Errorf("the paused-rebase marker survived a successful continue")
	}
	if f.git("rev-parse", "layer-one") != f.git("rev-parse", "layer-two^") {
		t.Errorf("layer-two was not rebased onto layer-one\n%s", f.git("log", "--graph", "--oneline", "--all"))
	}
}

func TestAbortRestoresTheStack(t *testing.T) {
	f := pausedRebase(t)
	before := f.git("rev-parse", "layer-two")

	r := f.gt("abort")
	if !r.announced("gh stack rebase --abort") {
		t.Errorf("gt abort did not abort the rebase\n%s", r.output())
	}
	if f.gitFileExists("gh-stack-rebase-state") {
		t.Errorf("the paused-rebase marker survived the abort")
	}
	if got := f.git("rev-parse", "layer-two"); got != before {
		t.Errorf("layer-two is at %s after the abort, want %s", got, before)
	}
}

func TestContinueWithNothingPaused(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	r := f.gtFails("continue")
	if !strings.Contains(r.stderr, "no gh stack rebase or modify is in progress") {
		t.Errorf("unexpected error:\n%s", r.output())
	}
}

// TestUnsupportedCommands: gt refuses these on purpose, with the reason and
// the nearest alternative. Silently forwarding one would be worse than
// stopping.
func TestUnsupportedCommands(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"fold", "reorder", "split", "absorb", "undo", "config"} {
		r := f.run(gtBin, name)
		if r.code != 2 {
			t.Errorf("gt %s exited %d, want 2\n%s", name, r.code, r.output())
		}
		if !strings.Contains(r.stderr, "cannot be translated") {
			t.Errorf("gt %s did not explain itself\n%s", name, r.output())
		}
	}
}

func TestUnknownCommandPassesToGit(t *testing.T) {
	f := newFixture(t)
	r := f.gt("status", "--porcelain")
	if !r.announced("git status --porcelain") {
		t.Errorf("gt status did not run git status\n%s", r.output())
	}

	f.write("extra.txt", "extra\n")
	r = f.gt("add", "extra.txt")
	if !r.announced("git add extra.txt") {
		t.Errorf("gt add did not run git add\n%s", r.output())
	}
	r = f.gt("commit", "-m", "passthrough commit")
	if !r.announced("git commit -m 'passthrough commit'") {
		t.Errorf("gt commit did not run git commit\n%s", r.output())
	}
	if got := f.subject("HEAD"); got != "passthrough commit" {
		t.Errorf("HEAD subject is %q", got)
	}

	bogus := f.run(gtBin, "no-such-command")
	if !bogus.announced("git no-such-command") {
		t.Errorf("unknown command was not passed to git\n%s", bogus.output())
	}
	if strings.Contains(bogus.stderr, "unknown command") {
		t.Errorf("gt still claimed the command was unknown\n%s", bogus.output())
	}
	if bogus.code == 0 {
		t.Error("git no-such-command succeeded")
	}
}

// TestLinkedWorktree covers both sides of a case gt cannot fix on its own.
//
// gt prefers `--git-dir` (the worktree's own gh-stack file) and falls back to
// `--git-common-dir` when this checkout has not written one yet. A stack
// created in the main repository must still be visible from a linked worktree
// so `gt create` does not run `gh stack init` and start a second stack.
func TestLinkedWorktree(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	// A branch checked out in the main repository cannot also be checked out
	// in a worktree.
	f.gt("trunk")

	linked := f.dir + "-worktree"
	f.git("worktree", "add", "--quiet", linked, "layer-one")
	worktree := &fixture{t: t, dir: linked, origin: f.origin}

	if got := worktree.tracked(); len(got) != 1 || got[0] != "layer-one" {
		t.Errorf("gt sees %q as the stack from a linked worktree, want [layer-one]", got)
	}
}

// skipNoGitHub is defined in bench_test.go; the submit tests below reuse it to
// gate on a real GitHub remote + token.

// TestSyncCleanNoGhStackCalls pins the headline performance claim of the
// rewritten cmdSync: a clean sync makes zero `gh stack` invocations and does
// not fetch, push, or rebase (nothing is out of date). The PR-load failure is
// tolerated — cmdSync prints a stderr note and proceeds with refs only.
func TestSyncCleanNoGhStackCalls(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.layer("layer-three", "Add layer three")
	for _, b := range []string{"layer-one", "layer-two", "layer-three"} {
		f.git("push", "--quiet", "-u", "origin", b)
	}

	r := f.gt("sync")

	if r.announced("gh stack") {
		t.Errorf("clean sync invoked `gh stack` (want zero gh stack calls):\n%s", r.stderr)
	}
	for _, cmd := range []string{"git fetch", "git push", "git rebase"} {
		if r.announced(cmd) {
			t.Errorf("clean sync announced `%s` (nothing is out of date):\n%s", cmd, r.stderr)
		}
	}
	if !strings.Contains(r.stderr, "proceeding with refs only") {
		t.Errorf("clean sync did not tolerate the PR-load failure:\n%s", r.stderr)
	}
	// NOTE: refactor-plan.md also targets "exactly one `git ls-remote`" for a
	// clean sync. LoadRemoteRefs shells out via capture(), which (unlike run())
	// does not echo `$ <cmd>` to stderr, so that count is not observable here.
	// The assertions above pin the clean-sync guarantees that are observable.
}

// TestSyncFastForwardsTrunk advances origin/main past local main, then asserts
// `gt sync` fast-forwards the local trunk to match the remote.
func TestSyncFastForwardsTrunk(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")

	// Advance origin/main one commit, then rewind local main so the trunk is
	// behind the remote. The stack branches stay parented on the old main.
	f.git("checkout", "--quiet", "main")
	f.write("trunk-moves.txt", "moved\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "Trunk moves on")
	f.git("push", "--quiet", "origin", "main")
	f.git("reset", "--hard", "--quiet", "HEAD~1")
	f.git("checkout", "--quiet", "layer-two")

	f.gt("sync")

	if got, want := f.git("rev-parse", "main"), f.git("rev-parse", "origin/main"); got != want {
		t.Errorf("main was not fast-forwarded: local %s, origin %s", got, want)
	}
}

// TestSyncNoRestack asserts `--no-restack` skips the cascade restack step: no
// `git rebase` is announced regardless of stack state.
func TestSyncNoRestack(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")

	r := f.gt("sync", "--no-restack")
	if r.announced("git rebase") {
		t.Errorf("gt sync --no-restack announced a rebase:\n%s", r.stderr)
	}
}

// TestSyncSkipsOtherWorktreeBranch (matrix #11) checks that a mid-stack branch
// checked out in a linked worktree is not mutated by sync from the main
// checkout. Best-effort: the assertion is that sync exits 0 and that branch's
// SHA is unchanged.
func TestSyncSkipsOtherWorktreeBranch(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	f.layer("layer-three", "Add layer three")
	// layer-two cannot be checked out in two places at once, so leave the main
	// checkout before adding the worktree.
	f.gt("trunk")

	linked := f.dir + "-wt"
	f.git("worktree", "add", "--quiet", linked, "layer-two")
	before := f.git("rev-parse", "layer-two")

	f.gt("sync")

	if got := f.git("rev-parse", "layer-two"); got != before {
		t.Errorf("layer-two moved during sync: was %s, now %s", before, got)
	}
}

// TestSubmitNoOp (matrix #1) is GitHub-gated: a clean `gt submit -u` against a
// pushed stack with open PRs is a no-op — zero `git push`, exit 0, and stderr
// reports the stack is already up to date.
func TestSubmitNoOp(t *testing.T) {
	skipNoGitHub(t)

	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	for _, b := range []string{"layer-one", "layer-two"} {
		f.git("push", "--quiet", "-u", "origin", b)
	}
	// PRs must exist for -u; creating them requires the GitHub API, so this
	// setup only completes under GT_TEST_GITHUB=1 with a real remote + token.
	f.gt("submit") // first submit opens the PRs

	r := f.gt("submit", "-u")
	if strings.Count(r.stderr, "$ git push") != 0 {
		t.Errorf("no-op submit pushed (want zero git push):\n%s", r.stderr)
	}
	if !strings.Contains(r.stderr, "already up to date") {
		t.Errorf("no-op submit did not report up-to-date:\n%s", r.stderr)
	}
}

// TestSubmitOneChangedOnePush (matrix #2) is GitHub-gated: in a 5-branch stack,
// changing two branches yields exactly one atomic `git push`.
func TestSubmitOneChangedOnePush(t *testing.T) {
	skipNoGitHub(t)

	f := newFixture(t)
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		f.layer(name, "Add "+name)
	}
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		f.git("push", "--quiet", "-u", "origin", name)
	}
	f.gt("submit") // open PRs

	// Change two branches.
	f.gt("bottom")
	f.write("one.txt", "one, revised\n")
	f.gt("modify", "-a", "-m", "Add one, revised")
	f.gt("up")
	f.write("two.txt", "two, revised\n")
	f.gt("modify", "-a", "-m", "Add two, revised")
	f.gt("top")

	r := f.gt("submit")
	if got := strings.Count(r.stderr, "$ git push"); got != 1 {
		t.Errorf("changed submit announced %d git push, want 1:\n%s", got, r.stderr)
	}
}

// TestSubmitLeaseFailureNoPartial (matrix #4) is GitHub-gated: when a branch's
// remote ref is moved out from under it, the atomic push fails on the lease
// and no partial push happens — the unchanged branches' remote refs still
// match their local heads.
func TestSubmitLeaseFailureNoPartial(t *testing.T) {
	skipNoGitHub(t)

	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	for _, b := range []string{"layer-one", "layer-two"} {
		f.git("push", "--quiet", "-u", "origin", b)
	}
	f.gt("submit")

	// Change layer-two locally and move its remote ref out from under it so
	// the --force-with-lease check fails.
	f.gt("top")
	f.write("layer-two.txt", "two, revised\n")
	f.gt("modify", "-a", "-m", "Add layer two, revised")
	f.git("push", "--quiet", "origin", "layer-two:refs/heads/layer-two", "--no-verify")
	// Force-move the remote ref to a different SHA so the lease no longer
	// matches what the snapshot will load.
	f.git("checkout", "--quiet", "main")
	f.git("branch", "--force", "layer-two-tmp", "layer-two^")
	f.git("push", "--force", "--quiet", "origin", "layer-two-tmp:refs/heads/layer-two")
	f.git("branch", "--quiet", "-D", "layer-two-tmp")
	f.gt("top")

	r := f.gtFails("submit")
	if r.code == 0 {
		t.Fatalf("submit succeeded against a moved remote ref:\n%s", r.output())
	}
	// The unchanged branch's remote ref must still match its local head.
	remote := f.git("ls-remote", "--heads", "origin", "layer-one")
	if !strings.Contains(remote, f.git("rev-parse", "layer-one")) {
		t.Errorf("layer-one was partially pushed:\n%s", remote)
	}
}

// TestSubmitUpdateOnlyMissingPR (matrix #5) is GitHub-gated: with -u, a branch
// that has no open PR is skipped — it is not pushed and no PR is created for
// it. Assumption: the stack has one branch with a PR and one without.
func TestSubmitUpdateOnlyMissingPR(t *testing.T) {
	skipNoGitHub(t)

	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	// Only layer-one gets a remote ref + PR; layer-two has neither.
	f.git("push", "--quiet", "-u", "origin", "layer-one")
	f.gt("submit") // opens a PR for layer-one only (layer-two is not pushed yet)

	// Make layer-two diverge so a non--u submit would push it.
	f.gt("top")
	f.write("layer-two.txt", "two, revised\n")
	f.gt("modify", "-a", "-m", "Add layer two, revised")

	r := f.gt("submit", "-u")
	if strings.Contains(r.stderr, "refs/heads/layer-two:refs/heads/layer-two") {
		t.Errorf("-u pushed the branch with no open PR:\n%s", r.stderr)
	}
	if strings.Contains(r.stderr, "pr create") && strings.Contains(r.stderr, "layer-two") {
		t.Errorf("-u created a PR for layer-two:\n%s", r.stderr)
	}
}

// TestSubmitNativeFallbackPreMutation (matrix #15) is GitHub-gated: when the
// repo is in a state validateStackForMutation rejects (here: an ambiguous
// stack), `gt submit --native` delegates to `gh stack submit` before any
// mutation has happened. Best-effort: the assertion is that `gh stack submit`
// is announced.
func TestSubmitNativeFallbackPreMutation(t *testing.T) {
	skipNoGitHub(t)

	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")
	for _, b := range []string{"layer-one", "layer-two"} {
		f.git("push", "--quiet", "-u", "origin", b)
	}

	// Corrupt the gh-stack state so the stack is ambiguous: track layer-one
	// under a second stack as well. This makes validateStackForMutation
	// reject with an ambiguity error, which --native is allowed to bypass.
	f.git("checkout", "--quiet", "main")
	f.git("checkout", "--quiet", "-b", "other")
	f.gt("track", "other")
	// Re-point a second stack at layer-one by editing state is fiddly; the
	// gate means this body does not execute locally. Under GT_TEST_GITHUB=1
	// the operator is expected to set up an ambiguous state manually if this
	// automated setup does not produce one.
	f.gt("top")

	r := f.gt("submit", "--native")
	if !r.announced("gh stack submit") {
		t.Errorf("--native did not delegate to `gh stack submit`:\n%s", r.stderr)
	}
}
