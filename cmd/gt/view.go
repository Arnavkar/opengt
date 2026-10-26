package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type branchCommit struct {
	short   string
	when    string
	subject string
}

type stackViewBranch struct {
	Name        string `json:"name"`
	Base        string `json:"base"`
	IsCurrent   bool   `json:"isCurrent"`
	IsMerged    bool   `json:"isMerged"`
	IsQueued    bool   `json:"isQueued"`
	NeedsRebase bool   `json:"needsRebase"`
}

type stackViewJSON struct {
	Trunk         string            `json:"trunk"`
	CurrentBranch string            `json:"currentBranch"`
	Branches      []stackViewBranch `json:"branches"`
}

// branchNeedsRebase is the catalog Base compared with the parent tip in
// GitFacts. An empty Base is not a disagreement. A missing parent tip is.
func branchNeedsRebase(b trackedBranch, parentSHA string) bool {
	return b.Base != "" && b.Base != parentSHA
}

func branchMerged(b trackedBranch) bool {
	return b.PullRequest != nil && b.PullRequest.Merged
}

func renderStackView(stack trackedStack, current string, heads map[string]string, commits map[string]branchCommit, short bool) string {
	var b strings.Builder
	for i := len(stack.Branches) - 1; i >= 0; i-- {
		br := stack.Branches[i]
		parent := stack.Trunk.Branch
		if i > 0 {
			parent = stack.Branches[i-1].Branch
		}
		merged := branchMerged(br)
		isCurrent := br.Branch == current
		warn := !isCurrent && !merged && branchNeedsRebase(br, heads[parent])
		if short {
			mark := "├"
			if isCurrent {
				mark = "»"
			} else if merged {
				mark = "✓"
			} else if warn {
				mark = "⚠"
			}
			if isCurrent {
				fmt.Fprintf(&b, "%s %s (current)\n", mark, br.Branch)
			} else {
				fmt.Fprintf(&b, "%s %s\n", mark, br.Branch)
			}
			continue
		}
		mark := "○"
		if isCurrent {
			mark = "●"
		} else if merged {
			mark = "✓"
		} else if warn {
			mark = "⚠"
		}
		if isCurrent {
			fmt.Fprintf(&b, "%s %s (current) \n", mark, br.Branch)
		} else {
			fmt.Fprintf(&b, "%s %s \n", mark, br.Branch)
		}
		info := commits[br.Branch]
		fmt.Fprintf(&b, "│ %s · %s\n", info.short, info.when)
		fmt.Fprintf(&b, "│ %s\n", info.subject)
		b.WriteString("│\n")
	}
	fmt.Fprintf(&b, "└ %s\n", stack.Trunk.Branch)
	return b.String()
}

func renderStackJSON(stack trackedStack, current string, heads map[string]string) string {
	out := stackViewJSON{
		Trunk:         stack.Trunk.Branch,
		CurrentBranch: current,
		Branches:      make([]stackViewBranch, 0, len(stack.Branches)),
	}
	for i, br := range stack.Branches {
		parent := stack.Trunk.Branch
		if i > 0 {
			parent = stack.Branches[i-1].Branch
		}
		out.Branches = append(out.Branches, stackViewBranch{
			Name:        br.Branch,
			Base:        br.Base,
			IsCurrent:   br.Branch == current,
			IsMerged:    branchMerged(br),
			IsQueued:    false,
			NeedsRebase: branchNeedsRebase(br, heads[parent]),
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "{}\n"
	}
	return string(data) + "\n"
}

type navKind int

const (
	navStayTop navKind = iota
	navStayBottom
	navAlreadyTrunk
	navUp
	navDown
	navSwitch
)

// planNav walks non-merged stacked branches. n counts those members only and
// clamps to the ones that exist. Top is the last of them, bottom the first,
// never the trunk. Trunk is that stack's trunk branch.
func planNav(stack trackedStack, current, cmd string, n int) (string, navKind, int) {
	if n < 1 {
		n = 1
	}
	if cmd == "trunk" {
		if stack.Trunk.Branch == current {
			return current, navAlreadyTrunk, 0
		}
		return stack.Trunk.Branch, navSwitch, 0
	}

	active := make([]int, 0, len(stack.Branches))
	curIdx := -1
	for i, br := range stack.Branches {
		if br.Branch == current {
			curIdx = i
		}
		if !branchMerged(br) {
			active = append(active, i)
		}
	}
	if len(active) == 0 {
		if cmd == "up" || cmd == "top" {
			return current, navStayTop, 0
		}
		return current, navStayBottom, 0
	}

	pos := -1
	if curIdx >= 0 {
		for i, ai := range active {
			if ai == curIdx {
				pos = i
				break
			}
		}
		if pos < 0 {
			insert := 0
			for _, ai := range active {
				if ai < curIdx {
					insert++
				}
			}
			return planNavFromGap(stack, active, current, insert, cmd, n)
		}
	}

	nameAt := func(i int) string { return stack.Branches[active[i]].Branch }
	switch cmd {
	case "top":
		target := nameAt(len(active) - 1)
		if target == current {
			return current, navStayTop, 0
		}
		return target, navSwitch, 0
	case "bottom":
		target := nameAt(0)
		if target == current {
			return current, navStayBottom, 0
		}
		return target, navSwitch, 0
	case "up":
		if pos >= len(active)-1 {
			return current, navStayTop, 0
		}
		next := pos + n
		if next >= len(active) {
			next = len(active) - 1
		}
		return nameAt(next), navUp, next - pos
	case "down":
		if pos <= 0 {
			return current, navStayBottom, 0
		}
		next := pos - n
		if next < 0 {
			next = 0
		}
		return nameAt(next), navDown, pos - next
	default:
		return current, navStayBottom, 0
	}
}

// planNavFromGap moves from a merged branch that sits between active members.
// insert is how many active branches are below it.
func planNavFromGap(stack trackedStack, active []int, current string, insert int, cmd string, n int) (string, navKind, int) {
	nameAt := func(i int) string { return stack.Branches[active[i]].Branch }
	switch cmd {
	case "top":
		target := nameAt(len(active) - 1)
		if target == current {
			return current, navStayTop, 0
		}
		return target, navSwitch, 0
	case "bottom":
		target := nameAt(0)
		if target == current {
			return current, navStayBottom, 0
		}
		return target, navSwitch, 0
	case "up":
		if insert >= len(active) {
			return current, navStayTop, 0
		}
		next := insert + n - 1
		if next >= len(active) {
			next = len(active) - 1
		}
		return nameAt(next), navUp, next - insert + 1
	case "down":
		if insert == 0 {
			return current, navStayBottom, 0
		}
		next := insert - n
		if next < 0 {
			next = 0
		}
		return nameAt(next), navDown, insert - next
	default:
		return current, navStayBottom, 0
	}
}

func formatNavMessage(kind navKind, target string, steps int) string {
	word := "branch"
	if steps != 1 {
		word = "branches"
	}
	switch kind {
	case navStayTop:
		return "Already at the top of the stack"
	case navStayBottom:
		return "Already at the bottom of the stack"
	case navAlreadyTrunk:
		return fmt.Sprintf("Already on trunk branch %s", target)
	case navUp:
		return fmt.Sprintf("✓ Checked out %s, %d %s up", target, steps, word)
	case navDown:
		return fmt.Sprintf("✓ Checked out %s, %d %s down", target, steps, word)
	default:
		return fmt.Sprintf("✓ Switched to %s", target)
	}
}

func loadCurrentMemberStack() (trackedStack, string, error) {
	return resolveCurrentStack(false)
}

func loadNavStack() (trackedStack, string, error) {
	return resolveCurrentStack(true)
}

// resolveCurrentStack reads the schema v1 catalog. Log wants a branch that is
// a member. Navigation also accepts the trunk when exactly one stack uses it.
func resolveCurrentStack(allowTrunk bool) (trackedStack, string, error) {
	repo, err := loadNativeRepo()
	if err != nil {
		return trackedStack{}, "", err
	}
	cur, err := currentBranch()
	if err != nil {
		return trackedStack{}, "", err
	}
	if hits := repo.ambiguous(cur); len(hits) > 0 {
		return trackedStack{}, "", errAmbiguous(cur, hits)
	}
	st := repo.asState()
	if locate(st, cur).forked {
		return trackedStack{}, "", errForked(cur)
	}
	if stack, ok := stackContaining(st, cur); ok {
		return stack, cur, nil
	}
	if allowTrunk {
		var trunks []trackedStack
		for _, s := range st.Stacks {
			if s.Trunk.Branch == cur {
				trunks = append(trunks, s)
			}
		}
		if len(trunks) == 1 {
			return trunks[0], cur, nil
		}
	}
	return trackedStack{}, "", errNotInStack(cur)
}

func cmdLog(args []string) error {
	form := ""
	if len(args) > 0 && (args[0] == "short" || args[0] == "long") {
		form, args = args[0], args[1:]
	}
	// gt ll is git log, including flags this view does not model.
	if form == "long" {
		return run("git", append([]string{"log", "--graph", "--oneline", "--decorate", "--all"}, args...)...)
	}

	native := false
	jsonOut := false
	short := form == "short"
	delegate := false
	for _, a := range args {
		switch a {
		case "--native":
			native = true
		case "--json":
			jsonOut = true
		case "--short", "-s":
			short = true
		default:
			// Unmodeled flags (--print-path and anything else starting with -)
			// and unexpected positionals stay on today's gh stack view.
			delegate = true
		}
	}
	if native || delegate {
		return delegateStackView(form, args)
	}

	stack, cur, err := loadCurrentMemberStack()
	if err != nil {
		return err
	}
	facts, err := gitFacts()
	if err != nil {
		return err
	}
	if jsonOut {
		fmt.Fprint(os.Stdout, renderStackJSON(stack, cur, facts.heads))
		return nil
	}
	fmt.Fprint(os.Stdout, renderStackView(stack, cur, facts.heads, facts.commits, form == "short" || short))
	return nil
}

func delegateStackView(form string, args []string) error {
	gh := []string{"stack", "view"}
	if form == "short" && !hasArg(args, "--short") && !hasArg(args, "-s") {
		gh = append(gh, "--short")
	}
	return run("gh", append(gh, withoutNative(args)...)...)
}

func withoutNative(args []string) []string {
	var out []string
	for _, a := range args {
		if a != "--native" {
			out = append(out, a)
		}
	}
	return out
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func cmdUp(args []string) error     { return cmdNav("up", args) }
func cmdDown(args []string) error   { return cmdNav("down", args) }
func cmdTop(args []string) error    { return cmdNav("top", args) }
func cmdBottom(args []string) error { return cmdNav("bottom", args) }
func cmdTrunk(args []string) error  { return cmdNav("trunk", args) }

func cmdNav(kind string, args []string) error {
	// --native and any flag this path does not model (--print-path, -h, …)
	// keep today's `gh stack <kind>` command. --native itself is not a gh flag.
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return run("gh", append([]string{"stack", kind}, withoutNative(args)...)...)
		}
	}
	rest := args

	n := 1
	if kind == "up" || kind == "down" {
		if len(rest) > 1 {
			return fmt.Errorf("unexpected argument %q", rest[1])
		}
		if len(rest) == 1 {
			var err error
			n, err = strconv.Atoi(rest[0])
			if err != nil || n < 1 {
				return fmt.Errorf("invalid step count %q", rest[0])
			}
		}
	} else if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q", rest[0])
	}

	stack, cur, err := loadNavStack()
	if err != nil {
		return err
	}
	target, nav, steps := planNav(stack, cur, kind, n)
	msg := formatNavMessage(nav, target, steps)
	if nav == navStayTop || nav == navStayBottom || nav == navAlreadyTrunk {
		fmt.Fprintln(os.Stdout, msg)
		return nil
	}
	if target != cur {
		if err := run("git", "checkout", target); err != nil {
			return err
		}
		invalidateGitFacts()
	}
	fmt.Fprintln(os.Stdout, msg)
	return nil
}
