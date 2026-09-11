//go:build integration

// Package integration checks gt against a real `gh stack` installation.
//
// gt is a thin translation layer, and `gh stack` is early software: a renamed
// flag or a changed default breaks gt without changing a line of gt's own
// code. The unit tests in cmd/gt cannot see that. These tests can, because
// they drive the real extension.
//
// They come in two halves. workflow_test.go runs gt end to end and checks what
// actually happened to the repository. contract_test.go pins the parts of the
// `gh stack` interface gt depends on, including the commands that only work
// against GitHub and so cannot be run here.
//
// Every test works in a throwaway repository whose only remote is a local bare
// repository, so no test reaches the GitHub API or needs authentication.
//
//	go test -tags=integration ./test/integration/
package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gtBin is the gt binary under test, built once for the whole run.
var gtBin string

func TestMain(m *testing.M) { os.Exit(runTests(m)) }

// runTests is separate from TestMain so that the deferred cleanup still runs:
// os.Exit would skip it.
func runTests(m *testing.M) int {
	// A stray global git config -- commit signing, hooks, a different default
	// branch, an alias shadowing a plumbing command -- would change what the
	// fixtures do, so the tests run without one.
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	os.Setenv("GIT_TERMINAL_PROMPT", "0")
	os.Setenv("NO_COLOR", "1")
	os.Setenv("GH_NO_UPDATE_NOTIFIER", "1")
	// Nothing here may reach GitHub. Every operation under test is meant to
	// work locally, so a command that quietly started calling the API would be
	// a change worth failing on rather than one to paper over.
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("GH_ENTERPRISE_TOKEN")

	out, err := exec.Command("gh", "stack", "--version").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: `gh stack --version` failed: %v\n%s"+
			"install it with: gh extension install github/gh-stack\n", err, out)
		return 1
	}
	// The version is the first thing to look at when this suite goes red, so
	// print it whether or not anything fails.
	fmt.Fprintf(os.Stderr, "integration: testing against %s\n", strings.TrimSpace(string(out)))

	dir, err := os.MkdirTemp("", "opengt-integration")
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration:", err)
		return 1
	}
	defer os.RemoveAll(dir)

	gtBin = filepath.Join(dir, "gt")
	build := exec.Command("go", "build", "-o", gtBin, "./cmd/gt")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "integration: building gt: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// result is the outcome of one command run in a fixture.
type result struct {
	stdout, stderr string
	code           int
}

func (r result) output() string { return r.stdout + r.stderr }

// announced reports whether gt echoed cmd to stderr before running it. gt
// prints every native command it runs, so this is how a test checks the
// translation itself rather than only its effect.
func (r result) announced(cmd string) bool {
	return strings.Contains(r.stderr, "$ "+cmd+"\n")
}

// fixture is a throwaway repository with a bare remote on disk standing in for
// GitHub. gh stack fetches, rebases and pushes against it happily; only its
// API calls are out of reach.
type fixture struct {
	t      *testing.T
	dir    string
	origin string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		t:      t,
		dir:    filepath.Join(root, "repo"),
		origin: filepath.Join(root, "origin.git"),
	}
	runIn(t, root, "git", "init", "--quiet", "--bare", "--initial-branch=main", f.origin)
	runIn(t, root, "git", "init", "--quiet", "--initial-branch=main", f.dir)
	f.git("config", "user.name", "opengt tests")
	f.git("config", "user.email", "tests@opengt.invalid")
	f.git("remote", "add", "origin", f.origin)
	f.write("trunk.txt", "trunk\n")
	f.git("add", "-A")
	f.git("commit", "--quiet", "-m", "Initial commit")
	f.git("push", "--quiet", "-u", "origin", "main")
	return f
}

// runIn runs a command outside the fixture repository, for the steps that
// create it.
func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

// run executes a command in the fixture with stdin closed, so nothing can
// block on a prompt or open a TUI.
func (f *fixture) run(name string, args ...string) result {
	f.t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = f.dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := result{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.code = exitErr.ExitCode()
	default:
		f.t.Fatalf("running %s %s: %v", name, strings.Join(args, " "), err)
	}
	return res
}

// gt runs gt and fails the test unless it succeeds.
func (f *fixture) gt(args ...string) result {
	f.t.Helper()
	r := f.run(gtBin, args...)
	if r.code != 0 {
		f.t.Fatalf("gt %s exited %d, want 0\n%s", strings.Join(args, " "), r.code, r.output())
	}
	return r
}

// gtFails runs gt and fails the test unless it reports an error.
func (f *fixture) gtFails(args ...string) result {
	f.t.Helper()
	r := f.run(gtBin, args...)
	if r.code == 0 {
		f.t.Fatalf("gt %s succeeded, want an error\n%s", strings.Join(args, " "), r.output())
	}
	return r
}

// git runs git and returns its trimmed stdout.
func (f *fixture) git(args ...string) string {
	f.t.Helper()
	r := f.run("git", args...)
	if r.code != 0 {
		f.t.Fatalf("git %s exited %d\n%s", strings.Join(args, " "), r.code, r.output())
	}
	return strings.TrimSpace(r.stdout)
}

func (f *fixture) write(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// layer adds one stack layer carrying a file of its own, the shape almost
// every test starts from.
func (f *fixture) layer(name, message string) {
	f.t.Helper()
	f.write(name+".txt", name+"\n")
	f.gt("create", name, "-a", "-m", message)
}

func (f *fixture) branch() string { return f.git("branch", "--show-current") }

func (f *fixture) subject(rev string) string { return f.git("log", "-1", "--format=%s", rev) }

// commitsIn counts the commits in a revision range such as "main..HEAD".
func (f *fixture) commitsIn(revRange string) int {
	f.t.Helper()
	var n int
	if _, err := fmt.Sscanf(f.git("rev-list", "--count", revRange), "%d", &n); err != nil {
		f.t.Fatalf("counting %s: %v", revRange, err)
	}
	return n
}

// gitFileExists reports whether a path exists next to this checkout's
// gh-stack state (worktree git dir, else the shared common dir).
func (f *fixture) gitFileExists(name string) bool {
	f.t.Helper()
	_, err := os.Stat(filepath.Join(f.stackDir(), name))
	return err == nil
}

func (f *fixture) stackDir() string {
	f.t.Helper()
	gitDir := f.git("rev-parse", "--path-format=absolute", "--git-dir")
	if _, err := os.Stat(filepath.Join(gitDir, "gh-stack")); err == nil {
		return gitDir
	}
	return f.git("rev-parse", "--path-format=absolute", "--git-common-dir")
}

// stackState is the part of `.git/gh-stack` that gt reads. It is a deliberate
// copy of the type in cmd/gt rather than an import: these tests check the
// on-disk format, so they must not follow a change made to the parser.
type stackState struct {
	SchemaVersion int `json:"schemaVersion"`
	Stacks        []struct {
		Trunk struct {
			Branch string `json:"branch"`
		} `json:"trunk"`
		Branches []struct {
			Branch string `json:"branch"`
		} `json:"branches"`
	} `json:"stacks"`
}

func (f *fixture) state() stackState {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.stackDir(), "gh-stack"))
	if err != nil {
		f.t.Fatalf("reading gh stack state: %v", err)
	}
	var st stackState
	if err := json.Unmarshal(data, &st); err != nil {
		f.t.Fatalf("decoding gh stack state: %v\n%s", err, data)
	}
	return st
}

// tracked returns the branches of the one tracked stack, bottom to top. Every
// fixture builds a single linear stack, so more than one means the test set up
// something it did not mean to.
func (f *fixture) tracked() []string {
	f.t.Helper()
	st := f.state()
	if len(st.Stacks) != 1 {
		f.t.Fatalf("got %d tracked stacks, want 1: %+v", len(st.Stacks), st)
	}
	var names []string
	for _, b := range st.Stacks[0].Branches {
		names = append(names, b.Branch)
	}
	return names
}
