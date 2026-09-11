// Command gt is a Graphite-compatible front end for the `gh stack` extension.
//
// It accepts the Graphite CLI commands and flags you use day to day and
// translates them into `gh stack` (and plain git) invocations. Only linear
// stacks are supported: when a branch belongs to more than one stack, gt
// stops with an error instead of guessing.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

// version is replaced at release time by -X main.version. It must stay a var:
// the linker silently ignores -X on a constant.
var version = "0.1.0"

type command struct {
	names []string
	// maps describes the translation, shown in `gt help`.
	maps string
	run  func(args []string) error
}

var commands = []command{
	{[]string{"create", "c"}, "gh stack init | gh stack add", cmdCreate},
	{[]string{"modify", "m"}, "git commit + gh stack rebase --upstack --no-trunk", cmdModify},
	{[]string{"submit", "s"}, "push through current (asks before upstack)", cmdSubmit},
	{[]string{"ss"}, "atomic push + PR update (whole stack)", func(a []string) error {
		return cmdSubmit(append([]string{"--stack"}, a...))
	}},
	{[]string{"sync"}, "fetch + restack (no push)", cmdSync},
	{[]string{"doctor"}, "diagnose local stacks vs Git and GitHub", cmdDoctor},
	{[]string{"restack"}, "gh stack rebase", cmdRestack},
	{[]string{"continue"}, "gh stack rebase|modify --continue", cmdContinue},
	{[]string{"abort"}, "gh stack rebase|modify --abort", cmdAbort},
	{[]string{"checkout", "co"}, "tree picker | gh stack checkout", cmdCheckout},
	{[]string{"delete"}, "tree picker + restack", cmdDelete},
	{[]string{"get"}, "gh stack checkout", cmdGet},
	{[]string{"log"}, "gh stack view", cmdLog},
	{[]string{"ls"}, "gh stack view --short", func(a []string) error {
		return cmdLog(append([]string{"short"}, a...))
	}},
	{[]string{"ll"}, "git log --graph", func(a []string) error {
		return cmdLog(append([]string{"long"}, a...))
	}},
	{[]string{"up", "u"}, "gh stack up", passthrough("up")},
	{[]string{"down", "d"}, "gh stack down", passthrough("down")},
	{[]string{"top", "t"}, "gh stack top", passthrough("top")},
	{[]string{"bottom", "b"}, "gh stack bottom", passthrough("bottom")},
	{[]string{"trunk"}, "gh stack trunk", passthrough("trunk")},
	{[]string{"merge"}, "gh stack merge", passthrough("merge")},
	{[]string{"switch"}, "gh stack switch", passthrough("switch")},
	{[]string{"pr"}, "gh pr view --web", cmdPR},
	{[]string{"add"}, "git add", gitPass("add")},
	{[]string{"cherry-pick"}, "git cherry-pick", gitPass("cherry-pick")},
	{[]string{"rebase"}, "git rebase", gitPass("rebase")},
	{[]string{"reset"}, "git reset", gitPass("reset")},
	{[]string{"restore"}, "git restore", gitPass("restore")},
	{[]string{"track", "tr"}, "gh stack init", cmdTrack},
	{[]string{"init"}, "(explains the gh stack model)", cmdInit},
}

// unsupported maps a Graphite command to the reason it cannot be translated
// and the closest thing to run instead.
var unsupported = map[string]string{
	"absorb":    "gh stack has no absorb. Amend by hand: git add -p, then gt modify on the right branch.",
	"fold":      "gh stack exposes fold only inside the `gh stack modify` TUI.",
	"move":      "gh stack exposes reordering only inside the `gh stack modify` TUI.",
	"reorder":   "gh stack exposes reordering only inside the `gh stack modify` TUI.",
	"rename":    "gh stack exposes rename only inside the `gh stack modify` TUI.",
	"split":     "gh stack has no split. Use git rebase -i, then `gh stack modify` to re-track.",
	"squash":    "gh stack has no squash. Use git reset --soft <parent> && git commit, then gt restack.",
	"pop":       "gh stack has no pop. Use git reset --soft HEAD~1.",
	"revert":    "gh stack has no revert. Use git revert, then gt restack.",
	"undo":      "gh stack keeps no undo journal. Use git reflog.",
	"freeze":    "gh stack has no freeze.",
	"unfreeze":  "gh stack has no freeze.",
	"untrack":   "gh stack has no per-branch untrack. `gh stack unstack --local` drops the whole stack from local tracking.",
	"unlink":    "gh stack has no per-branch unlink. `gh stack unstack --local` drops the whole stack from local tracking.",
	"info":      "Use `gh stack view --json`.",
	"children":  "Use `gh stack view --json`.",
	"parent":    "Use `gh stack view --json`.",
	"dash":      "Open the stack on GitHub instead.",
	"auth":      "Use `gh auth login`.",
	"config":    "gt has no config of its own; configure gh instead.",
	"aliases":   "Use `gh stack alias`, or shell aliases.",
	"demo":      "No equivalent.",
	"guide":     "No equivalent.",
	"docs":      "See https://gh.io/stacks.",
	"feedback":  "Use `gh stack feedback`.",
	"changelog": "No equivalent.",
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printHelp()
		return
	}
	switch args[0] {
	case "--version":
		// Named to be told apart from Graphite's `gt` and from `gh stack`.
		fmt.Printf("gt %s (opengt)\n", version)
		return
	case "help", "--help", "-h":
		printHelp()
		return
	}

	name := args[0]
	if hint, ok := unsupported[name]; ok {
		fmt.Fprintf(os.Stderr, "gt: `gt %s` cannot be translated to gh stack.\n    %s\n", name, hint)
		os.Exit(2)
	}
	for _, c := range commands {
		if slices.Contains(c.names, name) {
			exit(c.run(args[1:]))
			return
		}
	}
	exit(run("git", args...))
}

// exit propagates a child process exit code, or reports our own error.
func exit(err error) {
	if err == nil {
		return
	}
	var xe *exitError
	if errors.As(err, &xe) {
		if xe.Error() != "" {
			fmt.Fprintln(os.Stderr, "gt: "+xe.Error())
		}
		os.Exit(xe.code)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		// A bare *exec.ExitError is a passthrough command (git/gh spoke
		// for itself); exit silently with its code. A gt error that
		// *wraps* an exec.ExitError is one of our own steps failing (a
		// restack conflict, a push lease failure) — print the gt
		// message so the user sees what happened, not just the child's
		// raw output.
		if _, bare := err.(*exec.ExitError); !bare {
			fmt.Fprintln(os.Stderr, "gt: "+err.Error())
		}
		os.Exit(ee.ExitCode())
	}
	fmt.Fprintln(os.Stderr, "gt: "+err.Error())
	os.Exit(1)
}

func printHelp() {
	fmt.Printf("gt %s — opengt\n\n", version)
	fmt.Println("COMMANDS")
	for _, c := range commands {
		label := c.names[0]
		if len(c.names) > 1 {
			label = fmt.Sprintf("%s (%s)", c.names[0], joinNames(c.names[1:]))
		}
		fmt.Printf("  %-22s → %s\n", label, c.maps)
	}
	fmt.Println("\nPass --help to any command for its flags.")
	fmt.Println("Anything else is passed through to git.")
	fmt.Println("Only linear stacks are supported; gt stops with an error at a fork.")
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
