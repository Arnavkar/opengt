package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// gt doctor exit codes (script-friendly, deterministic):
//
//	0 healthy
//	1 warnings only
//	2 repairable errors (or errors a restack/metadata rewrite can fix)
//	3 ambiguous/unsafe state — do not guess
//	4 dependency/API/repository failure
const (
	exitDoctorHealthy    = 0
	exitDoctorWarning    = 1
	exitDoctorRepairable = 2
	exitDoctorAmbiguous  = 3
	exitDoctorDependency = 4
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func cmdDoctor(args []string) error {
	fs := newFlags("doctor")
	repair := fs.Bool("repair", false, "apply safe metadata repairs")
	yes := fs.Bool("yes", false, "apply safe repairs without prompting")
	asJSON := fs.Bool("json", false, "machine-readable diagnostics")
	if err := parse(fs, args); err != nil {
		return err
	}
	rep := runDoctor(*repair, *yes, *asJSON)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		fmt.Print(formatDoctor(rep))
	}
	return &exitError{code: doctorExit(rep)}
}

type doctorReport struct {
	Status       string           `json:"status"`
	Repository   map[string]any   `json:"repository"`
	Dependencies map[string]any   `json:"dependencies"`
	Stacks       []map[string]any `json:"stacks"`
	Worktrees    []map[string]any `json:"worktrees"`
	Issues       []stackIssue     `json:"issues"`
	Repairs      []string         `json:"repairs,omitempty"`
}

func runDoctor(repair, yes, asJSON bool) doctorReport {
	rep := doctorReport{
		Repository:   map[string]any{},
		Dependencies: map[string]any{},
		Stacks:       []map[string]any{},
		Worktrees:    []map[string]any{},
		Issues:       []stackIssue{},
	}
	if _, err := capture("git", "rev-parse", "--is-inside-work-tree"); err != nil {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "NOT_A_REPOSITORY", Severity: "error",
			Message: "not a git repository",
		})
		rep.Status = "error"
		return rep
	}
	rep.Repository["git"] = true

	branch, berr := currentBranch()
	if berr != nil {
		if strings.Contains(berr.Error(), "detached") {
			rep.Issues = append(rep.Issues, stackIssue{
				Code: "DETACHED_HEAD", Severity: "warning",
				Message: berr.Error(),
			})
			rep.Repository["detachedHead"] = true
		} else {
			rep.Repository["currentBranch"] = ""
		}
	} else {
		rep.Repository["currentBranch"] = branch
	}

	trunk := fallbackTrunk(trunkNames())
	rep.Repository["trunk"] = trunk
	if run2("git", "rev-parse", "--verify", "--quiet", "origin/"+trunk) != nil {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "MISSING_TRUNK", Severity: "warning",
			Message: "origin/" + trunk + " is not reachable",
			Branch:  trunk,
		})
		rep.Repository["originTrunk"] = false
	} else {
		rep.Repository["originTrunk"] = true
	}
	if dirty, _ := capture("git", "status", "--porcelain"); dirty != "" {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "DIRTY_WORKTREE", Severity: "warning",
			Message: "worktree has uncommitted changes",
		})
		rep.Repository["dirty"] = true
	} else {
		rep.Repository["dirty"] = false
	}

	if _, err := capture("gh", "--version"); err != nil {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "GH_MISSING", Severity: "error",
			Message: "gh is not installed, or not on PATH",
		})
	} else {
		rep.Dependencies["gh"] = true
	}

	compat, cerr := inspectGhStack()
	if cerr != nil && !compat.Installed {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "GH_STACK_MISSING", Severity: "error",
			Message: "github/gh-stack is not installed",
			Hint:    "gh extension install github/gh-stack --pin v" + ghStackCompat.TestedVersion.String(),
		})
	} else if cerr != nil {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "GH_STACK_INCOMPATIBLE", Severity: "error",
			Message: cerr.Error(),
		})
	} else {
		rep.Dependencies["ghStack"] = compat.Raw
		rep.Dependencies["schema"] = ghStackCompat.PrimarySchema()
		if compat.Warning != "" {
			rep.Issues = append(rep.Issues, stackIssue{
				Code: "GH_STACK_UNTESTED", Severity: "warning",
				Message: compat.Warning,
			})
		}
	}

	sources, err := discoverStackSources()
	if err != nil {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "NOT_A_REPOSITORY", Severity: "error", Message: err.Error(),
		})
		rep.Status = statusOf(rep.Issues)
		return rep
	}
	repo := reconcileSources(sources)
	rep.Issues = append(rep.Issues, repo.Issues...)

	wtRows := worktreeRows(sources)
	rep.Worktrees = wtRows
	rep.Repository["worktrees"] = len(wtRows)

	localHeads, localErr := localBranchHeads()
	if localErr != nil {
		rep.Issues = append(rep.Issues, stackIssue{
			Code: "GIT_CHECK_FAILED", Severity: "error",
			Message: "could not list local branches: " + localErr.Error(),
		})
	}
	untracked := countUntrackedWithHeads(repo, localHeads)
	rep.Repository["untrackedBranches"] = untracked
	remotePRs := doctorPullRequests{
		byNumber: map[int]pullRequest{},
		byHead:   map[string][]pullRequest{},
	}
	var remoteErr error
	if hasNetworkOrigin() {
		remotePRs, remoteErr = listDoctorPullRequests()
		if remoteErr != nil {
			rep.Issues = append(rep.Issues, stackIssue{
				Code: "REMOTE_CHECK_FAILED", Severity: "error",
				Message: "could not list GitHub pull requests: " + remoteErr.Error(),
			})
		}
	}
	for _, s := range repo.Stacks {
		if s.Stale {
			continue
		}
		entry := map[string]any{"chain": s.Display(), "sources": s.Sources}
		localIssues := checkStackGitWithHeads(s.trackedStack, localHeads)
		var remoteIssues []stackIssue
		skipped := remoteErr != nil
		if remoteErr == nil {
			remoteIssues, skipped = checkStackRemoteWithPRs(s.trackedStack, remotePRs)
		}
		if skipped {
			entry["remote"] = "skipped"
		}
		all := append(localIssues, remoteIssues...)
		rep.Issues = append(rep.Issues, all...)
		entry["ok"] = !hasError(all)
		rep.Stacks = append(rep.Stacks, entry)
	}

	if repair {
		applied, skipped := repairMetadata(repo, yes, asJSON)
		rep.Repairs = applied
		if len(skipped) > 0 && !yes && !isTerminal(os.Stdin) {
			rep.Issues = append(rep.Issues, stackIssue{
				Code: "REPAIR_SKIPPED", Severity: "warning",
				Message:    "non-interactive repair needs --yes; skipped: " + strings.Join(skipped, "; "),
				Repairable: true,
			})
		}
	}

	rep.Status = statusOf(rep.Issues)
	return rep
}

func hasNetworkOrigin() bool {
	remote, err := capture("git", "remote", "get-url", "origin")
	if err != nil || remote == "" || strings.HasPrefix(remote, "file://") {
		return false
	}
	if strings.Contains(remote, "://") {
		return true
	}
	colon := strings.IndexByte(remote, ':')
	slash := strings.IndexByte(remote, '/')
	return colon >= 0 && (slash < 0 || colon < slash)
}

func countUntracked(repo repoStackState) int {
	heads, _ := localBranchHeads()
	return countUntrackedWithHeads(repo, heads)
}

func countUntrackedWithHeads(repo repoStackState, localHeads map[string]string) int {
	known := repo.knownBranches()
	for _, s := range repo.Stacks {
		if s.Trunk.Branch != "" {
			known[s.Trunk.Branch] = true
		}
	}
	n := 0
	for b := range localHeads {
		if !known[b] {
			n++
		}
	}
	return n
}

func localBranchHeads() (map[string]string, error) {
	out, err := capture("git", "for-each-ref",
		"--format=%(refname:short)%09%(objectname)", "refs/heads")
	if err != nil {
		return nil, err
	}
	heads := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		name, head, ok := strings.Cut(line, "\t")
		if ok && name != "" {
			heads[name] = head
		}
	}
	return heads, nil
}

func worktreeRows(sources []stackStateSource) []map[string]any {
	out, err := capture("git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	byDir := map[string]stackStateSource{}
	for _, s := range sources {
		byDir[s.GitDir] = s
	}
	var rows []map[string]any
	var path, branch string
	flush := func() {
		if path == "" {
			return
		}
		row := map[string]any{"path": path, "branch": branch}
		dir, _ := worktreeGitDir(path)
		if src, ok := byDir[dir]; ok {
			row["stateFile"] = src.Path
			if src.State != nil {
				row["stacks"] = len(src.State.Stacks)
			}
		}
		rows = append(rows, row)
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
	return rows
}

func checkStackGit(s trackedStack) []stackIssue {
	heads, _ := localBranchHeads()
	return checkStackGitWithHeads(s, heads)
}

func checkStackGitWithHeads(s trackedStack, localHeads map[string]string) []stackIssue {
	var issues []stackIssue
	chain := s.Display()
	if s.Trunk.Branch == "" {
		issues = append(issues, stackIssue{
			Code: "MISSING_TRUNK", Severity: "error",
			Message: "stack has no trunk", Stack: chain,
		})
	} else if _, ok := localHeads[s.Trunk.Branch]; !ok {
		issues = append(issues, stackIssue{
			Code: "MISSING_TRUNK", Severity: "error",
			Message: fmt.Sprintf("trunk %q is not a local branch", s.Trunk.Branch),
			Branch:  s.Trunk.Branch, Stack: chain,
		})
	} else if s.Trunk.Head != "" {
		if h := localHeads[s.Trunk.Branch]; h != s.Trunk.Head {
			issues = append(issues, stackIssue{
				Code: "STALE_HEAD_SHA", Severity: "warning", Repairable: true,
				Message: fmt.Sprintf("cached HEAD for trunk %s is stale", s.Trunk.Branch),
				Branch:  s.Trunk.Branch, Stack: chain,
			})
		}
	}
	prev := s.Trunk.Branch
	for _, b := range s.Branches {
		if b.Branch == "" {
			continue
		}
		if _, ok := localHeads[b.Branch]; !ok {
			issues = append(issues, stackIssue{
				Code: "MISSING_BRANCH", Severity: "error", Repairable: true,
				Message: fmt.Sprintf("stack metadata references missing branch %q", b.Branch),
				Branch:  b.Branch, Stack: chain,
				Hint: "gt sync -d once every PR in the stack is merged or closed, or gt doctor --repair",
			})
			prev = b.Branch
			continue
		}
		if _, ok := localHeads[prev]; prev != "" && ok {
			parentOK := isAncestor(prev, b.Branch)
			cacheOK := b.Base != "" && isAncestor(b.Base, b.Branch)
			parentHead := localHeads[prev]
			if !parentOK {
				if cacheOK {
					issues = append(issues, stackIssue{
						Code: "NEEDS_RESTACK", Severity: "warning",
						Message: fmt.Sprintf("%s does not contain parent %s; restack descendants", b.Branch, prev),
						Branch:  b.Branch, Stack: chain,
						Hint: "gt restack -u",
					})
				} else {
					issues = append(issues, stackIssue{
						Code: "INVALID_ANCESTRY", Severity: "error",
						Message: fmt.Sprintf("%s does not contain parent %s", b.Branch, prev),
						Branch:  b.Branch, Stack: chain,
						Hint: "gt restack, or restore the missing commits; doctor --repair will not rewrite history",
					})
				}
			} else if b.Base != "" && parentHead != "" && b.Base != parentHead {
				issues = append(issues, stackIssue{
					Code: "STALE_BASE_SHA", Severity: "warning", Repairable: true,
					Message: fmt.Sprintf("cached base SHA for %s is stale (DAG is valid)", b.Branch),
					Branch:  b.Branch, Stack: chain,
				})
			}
		}
		prev = b.Branch
	}
	return issues
}

type doctorPullRequests struct {
	byNumber map[int]pullRequest
	byHead   map[string][]pullRequest
}

func listDoctorPullRequests() (doctorPullRequests, error) {
	prs, err := listPullRequests()
	if err != nil {
		return doctorPullRequests{}, err
	}
	index := doctorPullRequests{
		byNumber: map[int]pullRequest{},
		byHead:   map[string][]pullRequest{},
	}
	for _, pr := range prs {
		pr.State = strings.ToUpper(pr.State)
		index.byNumber[pr.Number] = pr
		index.byHead[pr.HeadRefName] = append(index.byHead[pr.HeadRefName], pr)
	}
	return index, nil
}

func checkStackRemote(s trackedStack) ([]stackIssue, bool) {
	prs, err := listDoctorPullRequests()
	if err != nil {
		return nil, true
	}
	return checkStackRemoteWithPRs(s, prs)
}

func checkStackRemoteWithPRs(s trackedStack, prs doctorPullRequests) ([]stackIssue, bool) {
	var issues []stackIssue
	skipped := false
	for i, b := range s.Branches {
		if b.Branch == "" {
			continue
		}
		parent := s.Trunk.Branch
		if i > 0 {
			parent = s.Branches[i-1].Branch
		}
		var pr pullRequest
		found := false
		if b.PullRequest != nil && b.PullRequest.Number != 0 {
			pr, found = prs.byNumber[b.PullRequest.Number]
		} else if matches := prs.byHead[b.Branch]; len(matches) > 0 {
			pr, found = matches[0], true
			for _, candidate := range matches {
				if candidate.State == "OPEN" {
					pr = candidate
					break
				}
			}
		}
		if !found {
			skipped = true
			continue
		}
		state := pr.State
		switch state {
		case "MERGED":
			issues = append(issues, stackIssue{
				Code: "PR_MERGED", Severity: "warning", Repairable: true,
				Message: fmt.Sprintf("PR #%d for %s is merged", pr.Number, b.Branch),
				Branch:  b.Branch, Stack: s.Display(),
			})
		case "CLOSED":
			issues = append(issues, stackIssue{
				Code: "PR_CLOSED", Severity: "warning", Repairable: true,
				Message: fmt.Sprintf("PR #%d for %s is closed", pr.Number, b.Branch),
				Branch:  b.Branch, Stack: s.Display(),
			})
		}
		if pr.BaseRefName != "" && pr.BaseRefName != parent {
			issues = append(issues, stackIssue{
				Code: "PR_BASE_MISMATCH", Severity: "error",
				Message: fmt.Sprintf("PR #%d for %s has base %s, expected %s", pr.Number, b.Branch, pr.BaseRefName, parent),
				Branch:  b.Branch, Stack: s.Display(),
				Hint: "gh stack submit --auto",
			})
		}
	}
	return issues, skipped
}

func repairMetadata(repo repoStackState, yes, quiet bool) (applied, skipped []string) {
	if repo.hasConflicts() {
		skipped = append(skipped, "conflicting stack definitions (will not guess)")
		return nil, skipped
	}
	var plans []repairPlan
	for _, src := range repo.Sources {
		if src.ReadErr != nil || src.State == nil {
			continue
		}
		st := cloneState(src.State)
		changed, notes := applySafeRepairs(st, repo, src)
		if changed {
			plans = append(plans, repairPlan{path: src.Path, state: st, notes: notes})
		}
	}
	if len(plans) == 0 {
		return nil, nil
	}
	for _, p := range plans {
		skipped = append(skipped, strings.Join(p.notes, ", ")+" -> "+p.path)
	}
	if !yes {
		if quiet || !isTerminal(os.Stdin) {
			return nil, skipped
		}
		if !confirm(fmt.Sprintf("gt: apply %d safe metadata repair(s)?", len(plans)), true) {
			return nil, skipped
		}
	}
	skipped = nil
	for _, p := range plans {
		if err := writeStackFile(p.path, p.state); err != nil {
			skipped = append(skipped, p.path+": "+err.Error())
			continue
		}
		applied = append(applied, strings.Join(p.notes, ", ")+" -> "+p.path)
	}
	return applied, skipped
}

type repairPlan struct {
	path  string
	state *stackState
	notes []string
}

func cloneState(st *stackState) *stackState {
	data, _ := json.Marshal(st)
	var out stackState
	_ = json.Unmarshal(data, &out)
	return &out
}

func applySafeRepairs(st *stackState, repo repoStackState, src stackStateSource) (bool, []string) {
	changed := false
	var notes []string
	// Drop duplicate topologies in this file.
	seen := map[string]bool{}
	var kept []trackedStack
	for _, s := range st.Stacks {
		k := stackKey(s)
		if seen[k] {
			changed = true
			notes = append(notes, "remove duplicate "+s.Display())
			continue
		}
		seen[k] = true
		kept = append(kept, s)
	}
	st.Stacks = kept

	// Refresh cached SHAs from Git. A HEAD change is not corruption.
	for i := range st.Stacks {
		s := &st.Stacks[i]
		if s.Trunk.Branch != "" && isLocalBranch(s.Trunk.Branch) {
			if h, err := branchHead(s.Trunk.Branch); err == nil && s.Trunk.Head != h {
				s.Trunk.Head = h
				changed = true
				notes = append(notes, "refresh trunk HEAD "+s.Trunk.Branch)
			}
		}
		prev := s.Trunk.Branch
		for j := range s.Branches {
			b := &s.Branches[j]
			if prev != "" && isLocalBranch(prev) && isLocalBranch(b.Branch) && isAncestor(prev, b.Branch) {
				if h, err := branchHead(prev); err == nil && b.Base != h {
					b.Base = h
					changed = true
					notes = append(notes, "refresh base SHA "+b.Branch)
				}
			}
			prev = b.Branch
		}
	}
	return changed, uniqueStrings(notes)
}

func hasError(issues []stackIssue) bool {
	for _, i := range issues {
		if i.Severity == "error" {
			return true
		}
	}
	return false
}

func statusOf(issues []stackIssue) string {
	code := doctorExitFrom(issues)
	switch code {
	case exitDoctorHealthy:
		return "ok"
	case exitDoctorWarning:
		return "warning"
	case exitDoctorAmbiguous:
		return "ambiguous"
	case exitDoctorDependency:
		return "error"
	default:
		return "error"
	}
}

func doctorExit(rep doctorReport) int { return doctorExitFrom(rep.Issues) }

func doctorExitFrom(issues []stackIssue) int {
	warn, repairable, ambiguous, dep := false, false, false, false
	for _, i := range issues {
		switch i.Code {
		case "NOT_A_REPOSITORY", "GH_MISSING", "GH_STACK_MISSING", "GH_STACK_INCOMPATIBLE", "GH_UNAVAILABLE", "REMOTE_CHECK_FAILED", "GIT_CHECK_FAILED", "UNSUPPORTED_SCHEMA", "UNPARSEABLE_STATE":
			dep = true
		case "CONFLICTING_STACK", "AMBIGUOUS_PREFIX", "AMBIGUOUS_MEMBERSHIP":
			ambiguous = true
		}
		if i.Severity == "error" {
			if i.Repairable {
				repairable = true
			} else if i.Code == "INVALID_ANCESTRY" || i.Code == "PR_BASE_MISMATCH" || i.Code == "MISSING_TRUNK" || i.Code == "DUPLICATE_BRANCH_NAME" {
				repairable = true
			} else if !dep && !ambiguous {
				repairable = true
			}
		} else {
			warn = true
		}
	}
	switch {
	case dep:
		return exitDoctorDependency
	case ambiguous:
		return exitDoctorAmbiguous
	case repairable:
		return exitDoctorRepairable
	case warn:
		return exitDoctorWarning
	default:
		return exitDoctorHealthy
	}
}

func formatDoctor(rep doctorReport) string {
	var b strings.Builder
	mark := func(ok bool, warn bool) string {
		if !ok {
			return "✗"
		}
		if warn {
			return "⚠"
		}
		return "✓"
	}
	fmt.Fprintf(&b, "Repository\n")
	fmt.Fprintf(&b, "  %s Git repository detected\n", mark(rep.Repository["git"] == true, false))
	if v, ok := rep.Dependencies["ghStack"].(string); ok && v != "" {
		fmt.Fprintf(&b, "  ✓ gh-stack installed: %s\n", v)
	}
	if v, ok := rep.Dependencies["schema"]; ok {
		fmt.Fprintf(&b, "  ✓ gh-stack state schema: v%v\n", v)
	}
	if t, ok := rep.Repository["trunk"].(string); ok && t != "" {
		fmt.Fprintf(&b, "  ✓ trunk: %s\n", t)
	}
	if rep.Repository["originTrunk"] == true {
		fmt.Fprintf(&b, "  ✓ origin/%s reachable\n", rep.Repository["trunk"])
	}

	for _, s := range rep.Stacks {
		chain, _ := s["chain"].(string)
		fmt.Fprintf(&b, "\nStack: %s\n", chain)
		ok := s["ok"] == true
		fmt.Fprintf(&b, "  %s local checks\n", mark(ok, false))
		if s["remote"] == "skipped" {
			fmt.Fprintf(&b, "  ⚠ remote GitHub checks skipped\n")
		}
	}

	fmt.Fprintf(&b, "\nWorktrees\n")
	if len(rep.Worktrees) == 0 {
		fmt.Fprintf(&b, "  ✓ 1 worktree\n")
	} else {
		fmt.Fprintf(&b, "  ✓ %d worktree(s)\n", len(rep.Worktrees))
	}

	issuesBySev := map[string]int{}
	for _, i := range rep.Issues {
		issuesBySev[i.Severity]++
		if i.Severity == "error" {
			fmt.Fprintf(&b, "  ✗ %s\n", oneLine(i.Message))
		} else if i.Code != "REMOTE_SKIPPED" && i.Code != "DIRTY_WORKTREE" {
			fmt.Fprintf(&b, "  ⚠ %s\n", oneLine(i.Message))
		}
	}
	if n, ok := rep.Repository["untrackedBranches"].(int); ok {
		fmt.Fprintf(&b, "\nOther branches\n  ✓ %d untracked branch(es) ignored\n", n)
	}
	fmt.Fprintf(&b, "\nSummary\n  %d warning(s), %d error(s)\n", issuesBySev["warning"], issuesBySev["error"])
	if len(rep.Repairs) > 0 {
		fmt.Fprintf(&b, "\nRepairs\n")
		for _, r := range rep.Repairs {
			fmt.Fprintf(&b, "  ✓ %s\n", r)
		}
	}
	return b.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " / ")
	if len(s) > 200 {
		return s[:197] + "..."
	}
	return s
}
