package main

import (
	"fmt"
	"os"
	"strings"
)

// Repository stack state is an in-memory view. It never becomes a database:
// gh-stack still owns `.git/gh-stack` (per git-dir). Reconciliation rules:
//
//  1. Identical topology (same trunk + same ordered members) in two files is
//     a duplicate, not a conflict. Keep one copy; note the other sources.
//  2. A shorter chain that is a prefix of a longer one (same trunk) is
//     ambiguous: the shorter chain may represent an intentional pop.
//  3. Two chains that share a member (not merely the trunk) and are neither
//     equal nor a prefix of each other are a conflict. Do not guess.
//  4. A branch resolves to a stack only when every claiming definition has
//     the same topology.

type stackStateSource struct {
	Path         string
	WorktreePath string
	GitDir       string
	State        *stackState
	ReadErr      error
}

type repoStackState struct {
	Sources []stackStateSource
	Stacks  []resolvedStack
	Issues  []stackIssue
}

type resolvedStack struct {
	trackedStack
	Sources []string
	Stale   bool
}

type stackIssue struct {
	Code       string   `json:"code"`
	Severity   string   `json:"severity"`
	Repairable bool     `json:"repairable"`
	Message    string   `json:"message"`
	Paths      []string `json:"paths,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Stack      string   `json:"stack,omitempty"`
	Hint       string   `json:"hint,omitempty"`
}

func stackTopology(s trackedStack) string {
	parts := []string{s.Trunk.Branch}
	for _, b := range s.Branches {
		if b.Branch != "" {
			parts = append(parts, b.Branch)
		}
	}
	return strings.Join(parts, " -> ")
}

func stackMembers(s trackedStack) []string {
	var names []string
	for _, b := range s.Branches {
		if b.Branch != "" {
			names = append(names, b.Branch)
		}
	}
	return names
}

func stackChain(s trackedStack) []string {
	names := []string{s.Trunk.Branch}
	return append(names, stackMembers(s)...)
}

func isChainPrefix(short, long []string) bool {
	if len(short) >= len(long) || len(short) < 2 {
		return false
	}
	for i, n := range short {
		if n != long[i] {
			return false
		}
	}
	return true
}

func memberOverlap(a, b trackedStack) bool {
	seen := map[string]bool{}
	for _, n := range stackMembers(a) {
		seen[n] = true
	}
	for _, n := range stackMembers(b) {
		if seen[n] {
			return true
		}
	}
	if a.Trunk.Branch != "" {
		for _, n := range stackMembers(b) {
			if n == a.Trunk.Branch {
				return true
			}
		}
	}
	if b.Trunk.Branch != "" {
		for _, n := range stackMembers(a) {
			if n == b.Trunk.Branch {
				return true
			}
		}
	}
	return false
}

func reconcileSources(sources []stackStateSource) repoStackState {
	repo := repoStackState{Sources: sources}
	type occ struct {
		src   stackStateSource
		stack trackedStack
	}
	var occs []occ
	for _, src := range sources {
		if src.ReadErr != nil {
			repo.Issues = append(repo.Issues, issueFromReadErr(src))
			continue
		}
		if src.State == nil {
			continue
		}
		if !ghStackCompat.SupportsSchema(src.State.SchemaVersion) && src.State.SchemaVersion != 0 {
			repo.Issues = append(repo.Issues, stackIssue{
				Code:     "UNSUPPORTED_SCHEMA",
				Severity: "error",
				Message: fmt.Sprintf("%s is schema v%d; this gt understands v%d",
					src.Path, src.State.SchemaVersion, ghStackCompat.PrimarySchema()),
				Paths: []string{src.Path},
			})
			continue
		}
		seenInFile := map[string]bool{}
		for _, s := range src.State.Stacks {
			topo := stackTopology(s)
			if seenInFile[topo] {
				repo.Issues = append(repo.Issues, stackIssue{
					Code:       "DUPLICATE_STACK",
					Severity:   "warning",
					Repairable: true,
					Message:    fmt.Sprintf("%s records %s more than once", src.Path, topo),
					Paths:      []string{src.Path},
					Stack:      topo,
				})
				continue
			}
			seenInFile[topo] = true
			if dups := dupNames(stackChain(s)); len(dups) > 0 {
				repo.Issues = append(repo.Issues, stackIssue{
					Code:     "DUPLICATE_BRANCH_NAME",
					Severity: "error",
					Message:  fmt.Sprintf("stack %s contains duplicate branch %q", topo, dups[0]),
					Paths:    []string{src.Path},
					Stack:    topo,
					Branch:   dups[0],
				})
			}
			occs = append(occs, occ{src: src, stack: s})
		}
	}

	// Group by topology key.
	byKey := map[string][]occ{}
	order := []string{}
	for _, o := range occs {
		k := stackKey(o.stack)
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], o)
	}

	type group struct {
		stack   trackedStack
		sources []string
		key     string
	}
	var groups []group
	for _, k := range order {
		list := byKey[k]
		var paths []string
		for _, o := range list {
			paths = append(paths, o.src.Path)
		}
		paths = uniqueStrings(paths)
		if len(list) > 1 {
			repo.Issues = append(repo.Issues, stackIssue{
				Code:       "DUPLICATE_STACK",
				Severity:   "warning",
				Repairable: true,
				Message:    fmt.Sprintf("stack %s is duplicated in %s", stackTopology(list[0].stack), strings.Join(paths, ", ")),
				Paths:      paths,
				Stack:      stackTopology(list[0].stack),
			})
		}
		groups = append(groups, group{stack: list[0].stack, sources: paths, key: k})
	}

	for i := 0; i < len(groups); i++ {
		for j := i + 1; j < len(groups); j++ {
			a, b := groups[i], groups[j]
			if !memberOverlap(a.stack, b.stack) {
				continue
			}
			ca, cb := stackChain(a.stack), stackChain(b.stack)
			switch {
			case isChainPrefix(ca, cb):
				repo.Issues = append(repo.Issues, stackIssue{
					Code:     "AMBIGUOUS_PREFIX",
					Severity: "error",
					Message:  fmt.Sprintf("stack definitions disagree at a prefix:\n  %s\n    %s\n  %s\n    %s", strings.Join(a.sources, ", "), a.stack.Display(), strings.Join(b.sources, ", "), b.stack.Display()),
					Paths:    append(append([]string{}, a.sources...), b.sources...),
					Stack:    a.stack.Display(),
					Hint:     "Run gt doctor, then use gh stack modify to choose the intended stack.",
				})
			case isChainPrefix(cb, ca):
				repo.Issues = append(repo.Issues, stackIssue{
					Code:     "AMBIGUOUS_PREFIX",
					Severity: "error",
					Message:  fmt.Sprintf("stack definitions disagree at a prefix:\n  %s\n    %s\n  %s\n    %s", strings.Join(b.sources, ", "), b.stack.Display(), strings.Join(a.sources, ", "), a.stack.Display()),
					Paths:    append(append([]string{}, b.sources...), a.sources...),
					Stack:    b.stack.Display(),
					Hint:     "Run gt doctor, then use gh stack modify to choose the intended stack.",
				})
			default:
				repo.Issues = append(repo.Issues, stackIssue{
					Code:     "CONFLICTING_STACK",
					Severity: "error",
					Message: fmt.Sprintf("incompatible stack definitions:\n  %s\n    %s\n  %s\n    %s\nRun:\n  gt doctor\nNo changes were made.",
						strings.Join(a.sources, ", "), a.stack.Display(),
						strings.Join(b.sources, ", "), b.stack.Display()),
					Paths: append(append([]string{}, a.sources...), b.sources...),
					Stack: a.stack.Display(),
				})
			}
		}
	}

	claim := map[string][]int{}
	for i, g := range groups {
		repo.Stacks = append(repo.Stacks, resolvedStack{
			trackedStack: g.stack,
			Sources:      g.sources,
			Stale:        false,
		})
		for _, n := range stackMembers(g.stack) {
			claim[n] = append(claim[n], i)
		}
	}
	for branch, idxs := range claim {
		uniq := map[string]bool{}
		var paths []string
		var display []string
		for _, i := range idxs {
			uniq[groups[i].key] = true
			paths = append(paths, groups[i].sources...)
			display = append(display, groups[i].stack.Display())
		}
		if len(uniq) > 1 {
			repo.Issues = append(repo.Issues, stackIssue{
				Code:     "AMBIGUOUS_MEMBERSHIP",
				Severity: "error",
				Message:  fmt.Sprintf("branch %q is claimed by incompatible stacks: %s", branch, strings.Join(display, " | ")),
				Paths:    uniqueStrings(paths),
				Branch:   branch,
				Hint:     "gt doctor",
			})
		}
	}
	return repo
}

func (s trackedStack) Display() string { return stackTopology(s) }

func dupNames(names []string) []string {
	seen := map[string]int{}
	var dups []string
	for _, n := range names {
		if n == "" {
			continue
		}
		seen[n]++
		if seen[n] == 2 {
			dups = append(dups, n)
		}
	}
	return dups
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func issueFromReadErr(src stackStateSource) stackIssue {
	msg := src.ReadErr.Error()
	code := "UNPARSEABLE_STATE"
	if _, ok := src.ReadErr.(*schemaError); ok {
		code = "UNSUPPORTED_SCHEMA"
	}
	return stackIssue{
		Code:     code,
		Severity: "error",
		Message:  src.Path + ": " + msg,
		Paths:    []string{src.Path},
	}
}

func (r repoStackState) asState() *stackState {
	st := &stackState{SchemaVersion: ghStackCompat.PrimarySchema()}
	for _, s := range r.Stacks {
		if s.Stale {
			continue
		}
		st.Stacks = append(st.Stacks, s.trackedStack)
	}
	return st
}

func (r repoStackState) knownBranches() map[string]bool {
	m := map[string]bool{}
	for _, s := range r.Stacks {
		for _, b := range s.Branches {
			if b.Branch != "" {
				m[b.Branch] = true
			}
		}
	}
	return m
}

func (r repoStackState) ambiguous(branch string) []resolvedStack {
	var hits []resolvedStack
	for _, s := range r.Stacks {
		if s.Stale {
			continue
		}
		for _, b := range s.Branches {
			if b.Branch == branch {
				hits = append(hits, s)
				break
			}
		}
	}
	if len(hits) <= 1 {
		for i := 0; i < len(r.Stacks); i++ {
			for j := i + 1; j < len(r.Stacks); j++ {
				a, b := r.Stacks[i], r.Stacks[j]
				if !isChainPrefix(stackChain(a.trackedStack), stackChain(b.trackedStack)) &&
					!isChainPrefix(stackChain(b.trackedStack), stackChain(a.trackedStack)) {
					continue
				}
				for _, name := range append(stackMembers(a.trackedStack), stackMembers(b.trackedStack)...) {
					if name == branch {
						return []resolvedStack{a, b}
					}
				}
			}
		}
		return nil
	}
	key := stackKey(hits[0].trackedStack)
	for _, h := range hits[1:] {
		if stackKey(h.trackedStack) != key {
			return hits
		}
	}
	return nil
}

func (r repoStackState) hasConflicts() bool {
	for _, i := range r.Issues {
		if i.Code == "CONFLICTING_STACK" || i.Code == "AMBIGUOUS_PREFIX" || i.Code == "AMBIGUOUS_MEMBERSHIP" || i.Code == "UNSUPPORTED_SCHEMA" {
			return true
		}
	}
	return false
}

func discoverStackSources() ([]stackStateSource, error) {
	infos, err := listStackLocations()
	if err != nil {
		return nil, err
	}
	var sources []stackStateSource
	for _, loc := range infos {
		src := stackStateSource{
			Path:         loc.StackFile,
			WorktreePath: loc.WorktreePath,
			GitDir:       loc.GitDir,
		}
		st, err := readStackFile(loc.StackFile)
		if os.IsNotExist(err) {
			continue
		}
		src.State, src.ReadErr = st, err
		sources = append(sources, src)
	}
	return sources, nil
}

func loadRepoStacks() (*repoStackState, error) {
	sources, err := discoverStackSources()
	if err != nil {
		return nil, fmt.Errorf("not a git repository")
	}
	if err := requireGhStack(); err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		repo := reconcileSources(nil)
		return &repo, nil
	}
	for _, src := range sources {
		if se, ok := src.ReadErr.(*schemaError); ok {
			return nil, se
		}
		if src.ReadErr != nil && src.State == nil {
			// parse errors on a file still surface; doctor can read them.
			// Mutating commands must not guess from a corrupt file.
			return nil, src.ReadErr
		}
	}
	repo := reconcileSources(sources)
	return &repo, nil
}

func loadState() (*stackState, error) {
	repo, err := loadRepoStacks()
	if err != nil {
		return nil, err
	}
	return repo.asState(), nil
}

func loadForestState() (*stackState, error) { return loadState() }

func errAmbiguous(branch string, hits []resolvedStack) error {
	var b strings.Builder
	fmt.Fprintf(&b, "branch %q is claimed by two incompatible stack definitions\n", branch)
	for _, h := range hits {
		wt := strings.Join(h.Sources, ", ")
		fmt.Fprintf(&b, "\n  %s:\n    %s", wt, h.Display())
	}
	fmt.Fprintf(&b, "\n\nRun:\n  gt doctor\n\nNo changes were made.")
	return fmt.Errorf("%s", b.String())
}

func requireStackPosition(branch string) (position, *stackState, error) {
	repo, err := loadRepoStacks()
	if err != nil {
		return position{}, nil, err
	}
	if hits := repo.ambiguous(branch); len(hits) > 0 {
		return position{}, nil, errAmbiguous(branch, hits)
	}
	st := repo.asState()
	pos := locate(st, branch)
	if pos.forked {
		return pos, st, errForked(branch)
	}
	return pos, st, nil
}
