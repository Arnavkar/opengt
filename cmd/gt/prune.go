package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type localBranch struct {
	name      string
	upstream  string
	gone      bool
	local     bool
	trackedPR int
	mergedPR  int
}

type pullRequest struct {
	Number      int    `json:"number"`
	State       string `json:"state"`
	BaseRefName string `json:"baseRefName"`
	HeadRefName string `json:"headRefName"`
}

type staleBranch struct {
	name   string
	reason string
}

// pruneStaleBranches deletes a stack only when every branch in it has a pull
// request and every one of those pull requests is merged or closed. A stack
// that still has an open PR, or a branch with no PR, is left intact — merged
// branches stay in the stack so restack and `gh stack view` keep the full
// chain. gh stack --prune deletes local branches for merged PRs but keeps
// that metadata; gt goes one step further and does not delete those branches
// until the whole stack is finished.
//
// PR state comes from snap (loaded once by LoadRemoteSnapshot). A missing
// snapshot does not delete anything: gone remotes alone are not a finished
// stack.
func pruneStaleBranches(snap *RemoteSnapshot, deleteAll bool) (bool, error) {
	groups, err := listTrackedStackGroups()
	if err != nil {
		return false, err
	}
	prs, prsAvailable := prsFromSnapshot(snap)
	if !prsAvailable {
		fmt.Fprintf(os.Stderr, "gt: no pull request snapshot available; leaving stacks in place\n")
	}
	stale := finishedStackBranches(groups, prs, trunkNames(), prsAvailable)
	if len(stale) == 0 {
		return false, nil
	}

	chosen, err := chooseStaleToDelete(stale, deleteAll)
	if err != nil {
		return false, err
	}

	if err := validatePaused(); err != nil {
		fmt.Fprintf(os.Stderr, "gt: skipping stale branch cleanup: %v\n", err)
		return false, nil
	}

	changed := false
	for _, stack := range groupChosenByStack(chosen, groups) {
		if err := deleteFinishedStack(stack); err != nil {
			fmt.Fprintf(os.Stderr, "gt: could not delete stack: %v\n", err)
			continue
		}
		changed = true
	}
	return changed, nil
}

// finishedStackBranches returns every branch of stacks whose pull requests are
// all merged or closed. Stacks are not split: one open or untracked branch
// keeps every merged branch below it.
func finishedStackBranches(groups [][]localBranch, prs []pullRequest, trunks map[string]bool, prsAvailable bool) []staleBranch {
	if !prsAvailable {
		return nil
	}
	var out []staleBranch
	for _, group := range groups {
		var reasons []staleBranch
		done := len(group) > 0
		for _, b := range group {
			one := staleLocalsWithPRs([]localBranch{b}, prs, trunks, true)
			if len(one) != 1 || !strings.HasPrefix(one[0].reason, "PR #") {
				done = false
				break
			}
			reasons = append(reasons, one[0])
		}
		if done {
			out = append(out, reasons...)
		}
	}
	return out
}

// groupChosenByStack buckets a flat deletion list back into the stacks it
// came from, so a finished stack is removed as one unit.
func groupChosenByStack(chosen []staleBranch, groups [][]localBranch) [][]string {
	want := map[string]bool{}
	for _, s := range chosen {
		want[s.name] = true
	}
	var out [][]string
	for _, group := range groups {
		var names []string
		for _, b := range group {
			if want[b.name] {
				names = append(names, b.name)
			}
		}
		if len(names) > 0 {
			out = append(out, names)
		}
	}
	return out
}

// deleteFinishedStack removes every branch of a stack that is fully merged or
// closed. Nothing above those branches survives, so there is no restack onto
// the trunk. The stack entry is dropped with the branches.
func deleteFinishedStack(names []string) error {
	if err := validatePaused(); err != nil {
		return err
	}
	for _, name := range names {
		if isLocalBranch(name) {
			if err := validateWorktreeForBranch(name); err != nil {
				return err
			}
		}
	}
	current, err := currentBranch()
	if err != nil {
		current = ""
	}
	trunk := fallbackTrunk(trunkNames())
	for _, name := range names {
		if name == current {
			if err := run("git", "checkout", trunk); err != nil {
				return err
			}
			break
		}
	}
	set := map[string]bool{}
	for _, name := range names {
		set[name] = true
	}
	if err := dropBranchesFromStackFiles(set); err != nil {
		return err
	}
	for _, name := range names {
		if !isLocalBranch(name) {
			continue
		}
		if err := deleteLocalBranch(name, "", trunk); err != nil {
			return err
		}
	}
	return nil
}

// orderForDeletion sorts branches bottom-up within their stack, so a branch
// is deleted (and its upstack rebased) before the branches above it. Branches
// from different stacks are independent, so only relative order within a
// stack matters; untracked names keep a stable position at the end.
func orderForDeletion(names []string) []string {
	st, err := loadForestState()
	if err != nil {
		return names
	}
	depth := map[string]int{}
	for _, s := range st.Stacks {
		for i, b := range s.Branches {
			depth[b.Branch] = i
		}
	}
	return sortByStackDepth(names, depth)
}

// sortByStackDepth is the pure core of orderForDeletion: a stable sort by
// stack position (bottom first).
func sortByStackDepth(names []string, depth map[string]int) []string {
	out := append([]string(nil), names...)
	sort.SliceStable(out, func(i, j int) bool { return depth[out[i]] < depth[out[j]] })
	return out
}

// prsFromSnapshot converts the PRSnapshots in snap into the local pullRequest
// struct that staleLocalsWithPRs consumes. State is uppercased to match the
// existing convention. Returns (nil, false) when snap or snap.PRs is nil so
// the caller falls back to the deleted-remotes-only path.
func prsFromSnapshot(snap *RemoteSnapshot) ([]pullRequest, bool) {
	if snap == nil || snap.PRs == nil {
		return nil, false
	}
	out := make([]pullRequest, 0, len(snap.PRs))
	for _, p := range snap.PRs {
		out = append(out, pullRequest{
			Number:      p.Number,
			State:       strings.ToUpper(p.State),
			BaseRefName: p.Base,
			HeadRefName: p.Head,
		})
	}
	return out, true
}

func chooseStaleToDelete(stale []staleBranch, deleteAll bool) ([]staleBranch, error) {
	fmt.Fprintf(os.Stderr, "gt: %d stale stack branch(es):\n", len(stale))
	for _, s := range stale {
		fmt.Fprintf(os.Stderr, "    %s  %s\n", s.name, s.reason)
	}
	if deleteAll {
		return stale, nil
	}
	interactive := isTerminal(os.Stdin) && isTerminal(os.Stderr)
	if !interactive {
		fmt.Fprintln(os.Stderr, "gt: rerun `gt sync` in a terminal to keep or delete each branch, or pass -d to delete them all.")
		return nil, nil
	}
	if len(stale) == 1 {
		if !confirm(fmt.Sprintf("gt: delete local branch %q?", stale[0].name), false) {
			return nil, nil
		}
		return stale, nil
	}
	chosen, err := pickMulti(stalePickRows(stale), deletePrompt)
	if err != nil {
		return nil, nil
	}
	byName := map[string]staleBranch{}
	for _, s := range stale {
		byName[s.name] = s
	}
	var out []staleBranch
	for _, r := range chosen {
		if s, ok := byName[r.branch]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func stalePickRows(stale []staleBranch) []pickRow {
	rows := make([]pickRow, 0, len(stale))
	for _, s := range stale {
		rows = append(rows, pickRow{
			branch: s.name,
			detail: s.reason,
			text:   s.name + "  " + s.reason,
		})
	}
	return rows
}

func listLocalBranches() ([]localBranch, error) {
	out, err := capture("git", "for-each-ref",
		"--format=%(refname:short)%09%(upstream:short)%09%(upstream:track)",
		"refs/heads")
	if err != nil {
		return nil, err
	}
	return parseLocalBranches(out), nil
}

func listTrackedStackGroups() ([][]localBranch, error) {
	locals, err := listLocalBranches()
	if err != nil {
		return nil, err
	}
	byName := map[string]localBranch{}
	for _, b := range locals {
		byName[b.name] = b
	}
	st, err := loadForestState()
	if err != nil {
		return nil, err
	}
	var groups [][]localBranch
	for _, s := range st.Stacks {
		var group []localBranch
		for _, br := range s.Branches {
			if br.Branch == "" {
				continue
			}
			cur := byName[br.Branch]
			cur.name = br.Branch
			if br.PullRequest != nil && br.PullRequest.Number != 0 {
				cur.trackedPR = br.PullRequest.Number
				if br.PullRequest.Merged {
					cur.mergedPR = br.PullRequest.Number
				}
			}
			group = append(group, cur)
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func parseLocalBranches(out string) []localBranch {
	var branches []localBranch
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		name, rest, _ := strings.Cut(line, "\t")
		upstream, track, _ := strings.Cut(rest, "\t")
		branches = append(branches, localBranch{
			name:     name,
			upstream: upstream,
			gone:     strings.Contains(track, "gone"),
			local:    true,
		})
	}
	return branches
}

func listPullRequests() ([]pullRequest, error) {
	out, err := capture("gh", "pr", "list", "--state", "all", "--limit", "1000",
		"--json", "number,state,baseRefName,headRefName")
	if err != nil {
		return nil, err
	}
	var prs []pullRequest
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

func staleLocals(branches []localBranch, prs []pullRequest, trunks map[string]bool) []staleBranch {
	return staleLocalsWithPRs(branches, prs, trunks, true)
}

func staleLocalsWithPRs(branches []localBranch, prs []pullRequest, trunks map[string]bool, prsAvailable bool) []staleBranch {
	prByNumber := map[int]pullRequest{}
	prsByHead := map[string][]pullRequest{}
	for _, pr := range prs {
		pr.State = strings.ToUpper(pr.State)
		prByNumber[pr.Number] = pr
		prsByHead[pr.HeadRefName] = append(prsByHead[pr.HeadRefName], pr)
	}
	var out []staleBranch
	for _, b := range branches {
		if trunks[b.name] {
			continue
		}
		reason := ""
		if prsAvailable {
			heads := []string{b.name}
			if b.upstream != "" {
				heads = append(heads, upstreamBranch(b.upstream))
			}
			open := false
			for _, head := range heads {
				for _, pr := range prsByHead[head] {
					if pr.State == "OPEN" {
						open = true
					}
				}
			}
			if open {
				continue
			}
			if b.trackedPR != 0 {
				if pr, ok := prByNumber[b.trackedPR]; ok {
					if pr.State == "OPEN" {
						continue
					}
					if pr.State == "MERGED" || pr.State == "CLOSED" {
						reason = prReason(pr)
					}
				} else if b.mergedPR == b.trackedPR {
					reason = fmt.Sprintf("PR #%d merged", b.mergedPR)
				}
			} else {
				for _, head := range heads {
					for _, pr := range prsByHead[head] {
						if pr.State == "MERGED" || pr.State == "CLOSED" {
							reason = prReason(pr)
							break
						}
					}
					if reason != "" {
						break
					}
				}
				if reason == "" && b.mergedPR != 0 {
					reason = fmt.Sprintf("PR #%d merged", b.mergedPR)
				}
			}
		}
		switch {
		case reason != "":
		case b.gone && b.upstream != "":
			reason = b.upstream + " is gone"
		case !b.local:
			reason = "not a local branch"
		default:
			continue
		}
		out = append(out, staleBranch{name: b.name, reason: reason})
	}
	return out
}

func prReason(pr pullRequest) string {
	return fmt.Sprintf("PR #%d %s", pr.Number, strings.ToLower(pr.State))
}

func upstreamBranch(upstream string) string {
	_, name, ok := strings.Cut(upstream, "/")
	if !ok {
		return upstream
	}
	return name
}

func trunkNames() map[string]bool {
	m := map[string]bool{}
	if st, err := loadForestState(); err == nil {
		for _, s := range st.Stacks {
			if s.Trunk.Branch != "" {
				m[s.Trunk.Branch] = true
			}
		}
	}
	if head, err := capture("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if _, name, ok := strings.Cut(head, "/"); ok {
			m[name] = true
		}
	}
	if len(m) == 0 {
		m["main"] = true
		m["master"] = true
	}
	return m
}

func fallbackTrunk(trunks map[string]bool) string {
	for _, name := range []string{"main", "master"} {
		if trunks[name] {
			return name
		}
	}
	for name := range trunks {
		return name
	}
	return "main"
}

func deleteLocalBranch(name, current, fallback string) error {
	if name == current {
		if fallback == "" || fallback == name {
			return fmt.Errorf("cannot delete the current branch (no trunk to check out)")
		}
		if err := run("git", "checkout", fallback); err != nil {
			return err
		}
	}
	return run("git", "branch", "-D", name)
}

func dropBranchesFromStackFiles(names map[string]bool) error {
	if len(names) == 0 {
		return nil
	}
	files, err := gitStackFiles()
	if err != nil {
		return err
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		updated, changed, err := stripStackBranches(data, names)
		if err != nil || !changed {
			if err != nil {
				return err
			}
			continue
		}
		if err := os.WriteFile(path, updated, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func stripStackBranches(data []byte, names map[string]bool) ([]byte, bool, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, err
	}
	stacks, ok := raw["stacks"].([]any)
	if !ok {
		return data, false, nil
	}
	changed := false
	var kept []any
	for _, s := range stacks {
		sm, ok := s.(map[string]any)
		if !ok {
			kept = append(kept, s)
			continue
		}
		branches, _ := sm["branches"].([]any)
		var nb []any
		for _, b := range branches {
			bm, ok := b.(map[string]any)
			if !ok {
				nb = append(nb, b)
				continue
			}
			name, _ := bm["branch"].(string)
			if names[name] {
				changed = true
				continue
			}
			nb = append(nb, b)
		}
		if len(nb) == 0 {
			changed = true
			continue
		}
		sm["branches"] = nb
		kept = append(kept, sm)
	}
	if !changed {
		return data, false, nil
	}
	raw["stacks"] = kept
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(out, '\n'), true, nil
}
