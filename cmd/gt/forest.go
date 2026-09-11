package main

import (
	"fmt"
	"sort"
	"strings"
)

// pickRow is one line in the checkout tree. openGithub means "run
// `gh stack checkout` with no arguments" instead of checking out a branch.
type pickRow struct {
	branch     string
	text       string
	detail     string // dim suffix, e.g. why a branch is stale
	graph      []graphSpan
	openGithub bool
	current    bool
	trunk      bool
	untracked  bool // local branch not in gh-stack metadata
}

type graphSpan struct {
	s   string
	col int // stack column for color; -1 dim (trunk); -2 uncolored
}

func nodeMark(current bool) string {
	if current {
		return "◉"
	}
	return "◯"
}

func spansText(spans []graphSpan) string {
	n := 0
	for _, sp := range spans {
		n += len(sp.s)
	}
	b := make([]byte, 0, n)
	for _, sp := range spans {
		b = append(b, sp.s...)
	}
	return string(b)
}

func rowText(graph []graphSpan, name string) string {
	return fmt.Sprintf("%s  %s", spansText(graph), name)
}

// stackForest is a Graphite-style tree: tips at the top, trunk at the bottom,
// each stack a column that stays vertically continuous until it merges.
func stackForest(st *stackState, current string) []pickRow {
	var trunks []string
	byTrunk := map[string][][]string{}
	seenTrunk := map[string]bool{}
	for _, s := range st.Stacks {
		trunk := s.Trunk.Branch
		if trunk == "" {
			continue
		}
		if !seenTrunk[trunk] {
			seenTrunk[trunk] = true
			trunks = append(trunks, trunk)
		}
		var branches []string
		for _, b := range s.Branches {
			if b.Branch != "" {
				branches = append(branches, b.Branch)
			}
		}
		if len(branches) == 0 {
			continue
		}
		byTrunk[trunk] = append(byTrunk[trunk], branches)
	}

	var rows []pickRow
	for _, trunk := range trunks {
		rows = append(rows, renderTrunkGroup(trunk, byTrunk[trunk], current)...)
	}
	return rows
}

func renderTrunkGroup(trunk string, stacks [][]string, current string) []pickRow {
	n := len(stacks)
	active := make([]bool, n)
	var rows []pickRow
	for si, branches := range stacks {
		for i := len(branches) - 1; i >= 0; i-- {
			b := branches[i]
			active[si] = true
			on := b == current
			graph := branchGraph(n, si, active, nodeMark(on))
			rows = append(rows, pickRow{
				branch:  b,
				current: on,
				graph:   graph,
				text:    rowText(graph, b),
			})
		}
	}
	on := trunk == current
	graph := trunkGraph(n, nodeMark(on))
	rows = append(rows, pickRow{
		branch:  trunk,
		current: on,
		trunk:   true,
		graph:   graph,
		text:    rowText(graph, trunk),
	})
	return rows
}

// branchGraph is one row of the Graphite column graph. Columns are two cells
// wide (`◯ `, `│ `, or `  `) so a track that has no node on this row still
// occupies its slot and reads as a continuous line down to the trunk.
func branchGraph(nCols, nodeCol int, active []bool, mark string) []graphSpan {
	var spans []graphSpan
	for c := 0; c < nCols; c++ {
		if c > 0 {
			spans = append(spans, graphSpan{s: " ", col: -2})
		}
		switch {
		case c == nodeCol:
			spans = append(spans, graphSpan{s: mark, col: nodeCol})
		case active[c]:
			spans = append(spans, graphSpan{s: "│", col: c})
		default:
			spans = append(spans, graphSpan{s: " ", col: -2})
		}
	}
	return spans
}

func trunkGraph(nCols int, mark string) []graphSpan {
	spans := []graphSpan{{s: mark, col: -1}}
	if nCols <= 1 {
		return spans
	}
	for c := 1; c < nCols; c++ {
		spans = append(spans, graphSpan{s: "─", col: c})
		if c == nCols-1 {
			spans = append(spans, graphSpan{s: "┘", col: c})
		} else {
			spans = append(spans, graphSpan{s: "┴", col: c})
		}
	}
	return spans
}

func githubStacksRow() pickRow {
	return pickRow{openGithub: true, text: "…  All stacks on GitHub"}
}

const untrackedDetail = "not in a stack · gt track"

func checkoutRows(st *stackState, current string) []pickRow {
	tracked := stackedBranchNames(st)
	trunk := fallbackTrunk(trunkNames())
	st = withWorktreeStacks(st, worktreeBranchNames(), trunk)
	rows := keepExistingRows(stackForest(st, current), localBranchSet())
	rows = ensureCurrent(rows, current)
	return markUntrackedRows(rows, tracked, trunk)
}

func deleteRows(st *stackState, current string) []pickRow {
	return filterDeleteRows(checkoutRows(st, current))
}

func filterDeleteRows(rows []pickRow) []pickRow {
	var out []pickRow
	for _, r := range rows {
		if r.trunk || r.openGithub {
			continue
		}
		out = append(out, r)
	}
	return out
}

func stackedBranchNames(st *stackState) map[string]bool {
	m := map[string]bool{}
	if st == nil {
		return m
	}
	for _, s := range st.Stacks {
		if s.Trunk.Branch != "" {
			m[s.Trunk.Branch] = true
		}
		for _, b := range s.Branches {
			if b.Branch != "" {
				m[b.Branch] = true
			}
		}
	}
	return m
}

func markUntrackedRows(rows []pickRow, tracked map[string]bool, trunk string) []pickRow {
	for i := range rows {
		r := &rows[i]
		if r.openGithub || r.trunk || r.branch == "" || r.branch == trunk || tracked[r.branch] {
			continue
		}
		r.untracked = true
		if r.detail == "" {
			r.detail = untrackedDetail
		}
	}
	return rows
}

func localBranchSet() map[string]bool {
	out, err := capture("git", "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil
	}
	branches := map[string]bool{}
	for _, name := range strings.Fields(out) {
		branches[name] = true
	}
	return branches
}

func worktreeBranchNames() []string {
	out, err := capture("git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var names []string
	for name := range parseWorktreeBranchPaths(out) {
		names = append(names, name)
	}
	return names
}

func withWorktreeStacks(st *stackState, names []string, trunk string) *stackState {
	if st == nil {
		st = &stackState{SchemaVersion: ghStackCompat.PrimarySchema()}
	}
	have := map[string]bool{}
	for _, s := range st.Stacks {
		have[s.Trunk.Branch] = true
		for _, b := range s.Branches {
			have[b.Branch] = true
		}
	}
	sorted := append([]string{}, names...)
	sort.Strings(sorted)
	for _, name := range sorted {
		if name == "" || name == trunk || have[name] {
			continue
		}
		have[name] = true
		st.Stacks = append(st.Stacks, trackedStack{
			Trunk:    trackedBranch{Branch: trunk},
			Branches: []trackedBranch{{Branch: name}},
		})
	}
	return st
}

func keepExistingRows(rows []pickRow, exists map[string]bool) []pickRow {
	var out []pickRow
	for _, r := range rows {
		if r.trunk || r.openGithub || r.current || r.branch == "" || exists[r.branch] {
			out = append(out, r)
		}
	}
	return out
}

// ensureCurrent puts the current branch at the top when it is not already in
// the forest (an untracked branch, or a stack recorded in no gh-stack file).
func ensureCurrent(rows []pickRow, current string) []pickRow {
	for _, r := range rows {
		if r.branch == current {
			return rows
		}
	}
	graph := []graphSpan{{s: nodeMark(true), col: -2}}
	row := pickRow{branch: current, current: true, untracked: true, graph: graph, detail: untrackedDetail, text: rowText(graph, current)}
	return append([]pickRow{row}, rows...)
}

// Graphite-ish palette: one hue per stack column, cycling if there are more
// stacks than colors. -1 is dim (trunk node); -2 is uncolored.
var stackPalette = []string{
	"\x1b[35m", // magenta
	"\x1b[33m", // yellow
	"\x1b[36m", // cyan
	"\x1b[32m", // green
	"\x1b[34m", // blue
	"\x1b[91m", // bright red
}

func spanColor(col int) string {
	switch {
	case col == -1:
		return "\x1b[2m"
	case col < 0:
		return ""
	}
	return stackPalette[col%len(stackPalette)]
}

func paint(code, s string) string {
	if code == "" {
		return s
	}
	return code + s + "\x1b[0m"
}

func (r pickRow) render(color, selected bool) string {
	if !color {
		return r.text
	}
	if r.openGithub {
		return paint("\x1b[2m", r.text)
	}
	var b []byte
	for _, sp := range r.graph {
		code := spanColor(sp.col)
		if r.current && (sp.s == "◉" || sp.s == "◯") {
			code = "\x1b[1;36m"
		}
		b = append(b, paint(code, sp.s)...)
	}
	b = append(b, "  "...)
	switch {
	case selected:
		b = append(b, paint("\x1b[1;32m", r.branch)...)
	case r.current:
		b = append(b, paint("\x1b[1;36m", r.branch)...)
	case r.trunk, r.untracked:
		b = append(b, paint("\x1b[2m", r.branch)...)
	default:
		b = append(b, r.branch...)
	}
	if r.detail != "" {
		b = append(b, "  "...)
		b = append(b, paint("\x1b[2m", r.detail)...)
	}
	return string(b)
}
