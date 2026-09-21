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

// pruneStaleBranches offers to delete local branches whose upstream is gone
// or whose pull request has been merged or closed. Sync has already fetched
// stacked remotes and dropped missing origin refs, so "gone" is visible. PR
// state is consumed from snap (loaded once by LoadRemoteSnapshot) rather than
// a fresh `gh pr list` shell-out.
//
// Each confirmed deletion rebases the branches above the deleted one onto its
// parent (the trunk for the bottom branch, dropping the deleted branch's
// commits) before removing it — the same machinery as `gt delete`. It returns
// true when anything was deleted or cleaned from the stack state.
func pruneStaleBranches(snap *RemoteSnapshot, deleteAll bool) (bool, error) {
	branches, err := listStaleCandidates()
	if err != nil {
		return false, err
	}
	prs, prsAvailable := prsFromSnapshot(snap)
	if !prsAvailable {
		fmt.Fprintf(os.Stderr, "gt: no pull request snapshot available; checking deleted remotes only\n")
	}
	stale := staleLocalsWithPRs(branches, prs, trunkNames(), prsAvailable)
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
	names := make([]string, len(chosen))
	for i, s := range chosen {
		names[i] = s.name
	}
	for _, name := range orderForDeletion(names) {
		if !isLocalBranch(name) {
			// A state entry with no local branch: clean the state only.
			if err := dropBranchesFromStackFiles(map[string]bool{name: true}); err == nil {
				changed = true
			}
			continue
		}
		if err := deleteBranch(name); err != nil {
			fmt.Fprintf(os.Stderr, "gt: could not delete %s: %v\n", name, err)
			continue
		}
		changed = true
	}
	return changed, nil
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

func listStaleCandidates() ([]localBranch, error) {
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
	known := map[string]bool{}
	for _, s := range st.Stacks {
		for _, br := range s.Branches {
			if br.Branch == "" {
				continue
			}
			known[br.Branch] = true
			cur := byName[br.Branch]
			cur.name = br.Branch
			if br.PullRequest != nil && br.PullRequest.Number != 0 {
				cur.trackedPR = br.PullRequest.Number
				if br.PullRequest.Merged {
					cur.mergedPR = br.PullRequest.Number
				}
			}
			byName[br.Branch] = cur
		}
	}
	out := make([]localBranch, 0, len(known))
	for name := range known {
		out = append(out, byName[name])
	}
	return out, nil
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
