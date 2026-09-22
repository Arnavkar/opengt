package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// run executes a command with the terminal attached, so interactive prompts
// and TUIs from gh stack and git work unchanged. Every command is announced
// first: the point of gt is to be a stepping stone to gh stack, so the native
// command has to be visible.
func run(name string, args ...string) error {
	if name == "gh" && len(args) > 0 && args[0] == "stack" && (len(args) == 1 || args[1] != "--version") {
		if err := requireGhStack(); err != nil {
			return err
		}
	}
	err := exec1(name, args)
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s is not installed, or not on PATH", name)
	}
	return err
}

func runRecording(name string, args ...string) (string, error) {
	if name == "gh" && len(args) > 0 && args[0] == "stack" && (len(args) == 1 || args[1] != "--version") {
		if err := requireGhStack(); err != nil {
			return "", err
		}
	}
	stderr, err := execRecording(name, args)
	if err == nil {
		return stderr, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return stderr, fmt.Errorf("%s is not installed, or not on PATH", name)
	}
	return stderr, err
}

func execRecording(name string, args []string) (string, error) {
	announce(name, args)
	var buf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	err := cmd.Run()
	return buf.String(), err
}

// runTee is runRecording but also copies stdout, so a child that prints
// errors on stdout still leaves something to inspect.
func runTee(name string, args ...string) (string, error) {
	if name == "gh" && len(args) > 0 && args[0] == "stack" && (len(args) == 1 || args[1] != "--version") {
		if err := requireGhStack(); err != nil {
			return "", err
		}
	}
	announce(name, args)
	var buf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	err := cmd.Run()
	if err == nil {
		return buf.String(), nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return buf.String(), fmt.Errorf("%s is not installed, or not on PATH", name)
	}
	return buf.String(), err
}

func exec1(name string, args []string) error {
	announce(name, args)
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// extensionChecked keeps the check to once per process: gt runs one command,
// and a failed install should not be retried within it.
var extensionChecked bool

// ensureExtension makes `gh stack` resolvable, offering to install
// github/gh-stack when it is not. gt is a shim for that one extension, so there
// is nothing to fall back to. It reports whether an install actually happened,
// so a caller holding a failed command knows to retry it.
func ensureExtension() (installed bool, err error) {
	if extensionChecked {
		return false, nil
	}
	extensionChecked = true
	if stackExtensionAvailable() {
		return false, nil
	}
	if !confirmInstall() {
		return false, fmt.Errorf("the gh stack extension is required.\n" +
			"    Install it with: gh extension install github/gh-stack")
	}
	if err := exec1("gh", []string{"extension", "install", "github/gh-stack"}); err != nil {
		return false, fmt.Errorf("could not install the gh stack extension: %w", err)
	}
	return true, nil
}

// confirmInstall asks before pulling a binary off the network. With no terminal
// there is nobody to ask, so it declines rather than installing unattended.
func confirmInstall() bool {
	return confirm("gt: the gh stack extension is not installed. Install it now?", true)
}

// confirm asks a yes/no question on stderr. With no terminal it returns false
// rather than taking a default into the void.
func confirm(prompt string, defaultYes bool) bool {
	if !isTerminal(os.Stdin) {
		return false
	}
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	fmt.Fprintf(os.Stderr, "%s %s ", prompt, hint)
	answer, ok := readLine(os.Stdin)
	if !ok {
		return false
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	case "":
		return defaultYes
	}
	return false
}

// readLine reads one line a byte at a time; a buffered reader would swallow
// input meant for the interactive child processes that run after this. The
// second result is false at end of input, so a closed stdin is not read as the
// empty "just pressed enter" answer.
func readLine(f *os.File) (string, bool) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := f.Read(buf)
		if n == 0 || err != nil {
			return strings.TrimSpace(string(line)), len(line) > 0
		}
		if buf[0] == '\n' {
			return strings.TrimSpace(string(line)), true
		}
		line = append(line, buf[0])
	}
}

// stackExtensionAvailable reports whether `gh stack` resolves. It costs a
// process, so callers only reach for it once something has already gone wrong
// or the local stack state looks absent.
func stackExtensionAvailable() bool {
	_, err := capture("gh", "stack", "--version")
	return err == nil
}

// announce writes the command to stderr, copy-pasteable, so stdout stays clean
// for pipes. Commands are echoed one at a time, immediately before they run,
// because some steps are conditional and a plan printed up front could lie.
func announce(name string, args []string) {
	line := name
	for _, a := range args {
		line += " " + shellQuote(a)
	}
	if useColor() {
		fmt.Fprintf(os.Stderr, "\x1b[2m$\x1b[0m \x1b[36m%s\x1b[0m\n", line)
		return
	}
	fmt.Fprintf(os.Stderr, "$ %s\n", line)
}

var colorOnce struct {
	done bool
	on   bool
}

func useColor() bool {
	if !colorOnce.done {
		colorOnce.done = true
		colorOnce.on = os.Getenv("NO_COLOR") == "" && isTerminal(os.Stderr)
	}
	return colorOnce.on
}

// isTerminal needs a real tty check, not a character-device check: /dev/null is
// a character device, and treating it as interactive would make `gt ... </dev/null`
// prompt into the void and take the default.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

const shellSafe = "-_./=:@,+"

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && !strings.ContainsRune(shellSafe, r) {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}

// capture runs a command and returns its trimmed stdout.
func capture(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// passthrough forwards a command straight to `gh stack <sub>`.
func passthrough(sub string) func([]string) error {
	return func(args []string) error {
		return run("gh", append([]string{"stack", sub}, args...)...)
	}
}

// gitPass forwards a Graphite git-passthrough command to `git <sub>`.
func gitPass(sub string) func([]string) error {
	return func(args []string) error {
		return run("git", append([]string{sub}, args...)...)
	}
}

func currentBranch() (string, error) {
	b, err := capture("git", "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	if b == "" {
		return "", fmt.Errorf("HEAD is detached; check out a branch first")
	}
	return b, nil
}

// fastForwardBranch fast-forwards a local branch to match origin/<branch>
// when origin is ahead and the move is a true fast-forward. It does not
// fetch; the caller ensures origin/<branch> is current (e.g. via
// fetchStackOrigin). If the branch is checked out in another worktree the
// fast-forward runs there as `git -C <wt> merge --ff-only`; otherwise the
// ref is moved with `git branch --force`. Non-fast-forward situations are
// left untouched.
func fastForwardBranch(branch string) error {
	remote := "origin/" + branch
	if run2("git", "rev-parse", "--verify", "--quiet", remote) != nil {
		return nil
	}
	local, err := capture("git", "rev-parse", branch)
	if err != nil {
		return nil
	}
	want, err := capture("git", "rev-parse", remote)
	if err != nil {
		return err
	}
	if local == want {
		return nil
	}
	// Confirm the move is a true fast-forward before touching the ref.
	if !isAncestor(local, remote) {
		return nil
	}
	wt, err := worktreePathForBranch(branch)
	if err != nil {
		return err
	}
	if wt == "" {
		return run("git", "branch", "--force", "--", branch, remote)
	}
	return run("git", "-C", wt, "merge", "--ff-only", remote)
}

// fastForwardTrunk is a thin wrapper that fetches origin (targeted, per
// decision #2) and then fast-forwards the repo's trunk. Existing callers
// keep working; new code that has already fetched should call
// fastForwardBranch directly.
func fastForwardTrunk() error {
	if err := fetchStackOrigin(); err != nil {
		return err
	}
	return fastForwardBranch(fallbackTrunk(trunkNames()))
}

// fetchStackOrigin fetches only trunk and stacked branches, then drops
// remote-tracking refs for stacked branches that no longer exist on origin.
// A full `git fetch` / `git remote prune origin` would walk every remote
// branch, which is far more than sync needs.
func fetchStackOrigin() error {
	names := stackFetchNames()
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"ls-remote", "--heads", "origin"}, names...)
	out, err := capture("git", args...)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	var fetch []string
	for _, name := range parseLsRemoteHeads(out) {
		if live[name] {
			continue
		}
		live[name] = true
		fetch = append(fetch, name)
	}
	if len(fetch) > 0 {
		if err := run("git", append([]string{"fetch", "origin"}, fetch...)...); err != nil {
			return err
		}
	}
	for _, name := range names {
		if live[name] {
			continue
		}
		_ = run2("git", "update-ref", "-d", "refs/remotes/origin/"+name)
	}
	return nil
}

func stackFetchNames() []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(fallbackTrunk(trunkNames()))
	if st, err := loadForestState(); err == nil {
		for _, s := range st.Stacks {
			add(s.Trunk.Branch)
			for _, b := range s.Branches {
				add(b.Branch)
			}
		}
	}
	return names
}

func parseLsRemoteHeads(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		_, ref, ok := strings.Cut(line, "\t")
		if !ok {
			_, ref, ok = strings.Cut(line, " ")
		}
		if !ok {
			continue
		}
		name, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/")
		if ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

func worktreePathForBranch(name string) (string, error) {
	out, err := capture("git", "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	return parseWorktreeBranchPaths(out)[name], nil
}

func parseWorktreeBranchPaths(out string) map[string]string {
	m := map[string]string{}
	var path, branch string
	flush := func() {
		if path != "" && branch != "" {
			m[branch] = path
		}
		path, branch = "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return m
}

// gitStackDir is the directory gt reads and writes the `gh-stack` state file
// from: the shared repository directory (`--git-common-dir`), i.e. the repo
// root. One file, shared by every worktree.
//
// gh stack itself resolves the state path per git-dir, so a command run in a
// linked worktree can create `.git/worktrees/<name>/gh-stack`. gt ignores
// those per-worktree copies on purpose: unioning them is what produced
// cross-worktree surprises (duplicated stacks, conflicting definitions, state
// copied between worktrees). The repo-root file is the single source of
// truth.
func gitStackDir() (string, error) {
	return capture("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
}

type stackLocation struct {
	GitDir       string
	WorktreePath string
	StackFile    string
}

func gitStackFiles() ([]string, error) {
	locs, err := listStackLocations()
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(locs))
	for _, loc := range locs {
		files = append(files, loc.StackFile)
	}
	return files, nil
}

// listStackLocations is every git-dir gt reads gh-stack state from: only the
// shared repository directory. Per-worktree state files (which gh stack may
// write when a command runs in a linked worktree) are deliberately not
// listed; see gitStackDir.
func listStackLocations() ([]stackLocation, error) {
	common, err := capture("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	return []stackLocation{{
		GitDir:       common,
		WorktreePath: worktreePathByGitDir()[common],
		StackFile:    filepath.Join(common, ghStackCompat.StateFileName),
	}}, nil
}

func worktreePathByGitDir() map[string]string {
	out, err := capture("git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for _, path := range parseWorktreePaths(out) {
		dir, err := worktreeGitDir(path)
		if err != nil {
			continue
		}
		m[dir] = path
	}
	return m
}

func worktreeGitDir(path string) (string, error) {
	dotGit := filepath.Join(path, ".git")
	info, err := os.Stat(dotGit)
	if err == nil && info.IsDir() {
		return filepath.Abs(dotGit)
	}
	if err == nil {
		data, readErr := os.ReadFile(dotGit)
		if readErr == nil {
			line := strings.TrimSpace(string(data))
			if dir, ok := strings.CutPrefix(line, "gitdir:"); ok {
				dir = strings.TrimSpace(dir)
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(path, dir)
				}
				return filepath.Abs(filepath.Clean(dir))
			}
		}
	}
	return capture("git", "-C", path, "rev-parse", "--path-format=absolute", "--git-dir")
}

func parseWorktreePaths(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths
}

func isLocalBranch(name string) bool {
	return run2("git", "show-ref", "--verify", "--quiet", "refs/heads/"+name) == nil
}

func isAncestor(parent, child string) bool {
	if parent == "" || child == "" {
		return false
	}
	return run2("git", "merge-base", "--is-ancestor", parent, child) == nil
}

// isAncestorQuiet is isAncestor without git's stderr passthrough, for
// probing SHAs that may not exist locally (remote snapshot SHAs).
func isAncestorQuiet(parent, child string) bool {
	if parent == "" || child == "" {
		return false
	}
	return exec.Command("git", "merge-base", "--is-ancestor", parent, child).Run() == nil
}

func branchHead(name string) (string, error) {
	return capture("git", "rev-parse", "--verify", "--quiet", name)
}

// hasStagedChanges reports whether the index differs from HEAD.
func hasStagedChanges() bool {
	err := run2("git", "diff", "--cached", "--quiet")
	return err != nil
}

// run2 is run without inheriting stdout, for commands used as predicates.
func run2(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// commitsOn counts commits on HEAD that are not reachable from parent.
func commitsOn(parent string) (int, error) {
	out, err := capture("git", "rev-list", "--count", parent+"..HEAD")
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(out, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// stage applies the Graphite staging flags before a commit.
func stage(all, update, patch bool) error {
	switch {
	case patch:
		return run("git", "add", "-p")
	case all:
		return run("git", "add", "-A")
	case update:
		return run("git", "add", "-u")
	}
	return nil
}

// joinMessage joins repeated -m values the way git does.
func joinMessage(parts []string) string {
	return strings.Join(parts, "\n\n")
}

// branchNameFrom builds a branch name from a commit message, matching the
// MM-DD-slug shape that `gh stack add -m` generates.
func branchNameFrom(msg, date string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(msg) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		slug = "branch"
	}
	return date + "-" + slug
}
