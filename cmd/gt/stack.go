package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tracked* types match github/gh-stack v0.2.0 schema v1
// (internal/stack/schema.json). gt does not own this format.

type pullRequestRef struct {
	Number int    `json:"number"`
	ID     string `json:"id,omitempty"`
	URL    string `json:"url,omitempty"`
	Merged bool   `json:"merged,omitempty"`
}

type trackedBranch struct {
	Branch      string          `json:"branch"`
	Head        string          `json:"head,omitempty"`
	Base        string          `json:"base,omitempty"`
	PullRequest *pullRequestRef `json:"pullRequest,omitempty"`
}

type trackedStack struct {
	ID       string          `json:"id,omitempty"`
	Number   int             `json:"number,omitempty"`
	Trunk    trackedBranch   `json:"trunk"`
	Branches []trackedBranch `json:"branches"`
}

type stackState struct {
	SchemaVersion int            `json:"schemaVersion"`
	Repository    string         `json:"repository,omitempty"`
	Stacks        []trackedStack `json:"stacks"`
}

type schemaError struct {
	got, want int
	path      string
}

func (e *schemaError) Error() string {
	return fmt.Sprintf(
		"gh stack state is schema v%d but this gt understands v%d; upgrade gt or use gh stack directly",
		e.got, e.want)
}

func readStackFile(path string) (*stackState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st stackState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("reading gh stack state: %w", err)
	}
	if !ghStackCompat.SupportsSchema(st.SchemaVersion) {
		return &st, &schemaError{got: st.SchemaVersion, want: ghStackCompat.PrimarySchema(), path: path}
	}
	return &st, nil
}

func writeStackFile(path string, st *stackState) error {
	if st.Stacks == nil {
		st.Stacks = []trackedStack{}
	}
	st.SchemaVersion = ghStackCompat.PrimarySchema()
	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// gh-stack >= 0.2.0 shares this catalog with gt and assumes its writers
	// are atomic (internal/stack/atomic.go) and hold its interop lock
	// (internal/stack/lock.go). gt honors both: same-dir rename means a
	// half-written file can never be read, and the flock means a gt write
	// cannot clobber a concurrent gh-stack mutation.
	unlock, err := withCatalogLock(path)
	if err != nil {
		return err
	}
	defer unlock()
	return writeFileAtomic(path, append(out, '\n'))
}

// writeFileAtomic writes data to path via a temp file in the same directory
// and a rename, so readers only ever see whole files.
func writeFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gt-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer func() {
		if cerr := tmp.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func mergeStacks(dst, src *stackState) {
	seen := map[string]bool{}
	for _, s := range dst.Stacks {
		seen[stackKey(s)] = true
	}
	for _, s := range src.Stacks {
		k := stackKey(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		dst.Stacks = append(dst.Stacks, s)
	}
}

func stackKey(s trackedStack) string {
	key := s.Trunk.Branch
	for _, b := range s.Branches {
		key += "\x00" + b.Branch
	}
	return key
}

// position describes where a branch sits in the tracked stacks.
type position struct {
	inStack bool
	// forked is true when the branch is a member of one stack and also the
	// trunk of another, or a member of two stacks. gh stack cannot resolve
	// these without an interactive prompt, so gt refuses to act.
	forked bool
	atTop  bool
	// parent is the branch below this one, or the trunk for the bottom branch.
	parent string
}

func locate(st *stackState, branch string) position {
	var p position
	members, trunks := 0, 0
	for _, s := range st.Stacks {
		if s.Trunk.Branch == branch {
			trunks++
		}
		for i, b := range s.Branches {
			if b.Branch != branch {
				continue
			}
			members++
			p.inStack = true
			p.atTop = i == len(s.Branches)-1
			if i == 0 {
				p.parent = s.Trunk.Branch
			} else {
				p.parent = s.Branches[i-1].Branch
			}
		}
	}
	p.forked = members > 1 || (members == 1 && trunks > 0)
	return p
}

func descendantsAfter(s trackedStack, name string) (parent string, descendants []trackedBranch, ok bool) {
	parent = s.Trunk.Branch
	for i, b := range s.Branches {
		if b.Branch == name {
			return parent, append([]trackedBranch{}, s.Branches[i+1:]...), true
		}
		parent = b.Branch
	}
	return "", nil, false
}

func stackContaining(st *stackState, name string) (trackedStack, bool) {
	if st == nil {
		return trackedStack{}, false
	}
	for _, s := range st.Stacks {
		for _, b := range s.Branches {
			if b.Branch == name {
				return s, true
			}
		}
	}
	return trackedStack{}, false
}

func errForked(branch string) error {
	return fmt.Errorf(
		"branch %q belongs to more than one stack; gt only supports linear stacks.\n"+
			"    Run `gt doctor` to see the definitions, or use gh stack directly.", branch)
}

// pausedOperation reports which gh stack operation, if any, is halted waiting
// for conflict resolution. gh stack drops a marker file per operation. Both
// this worktree's git-dir and the shared repository directory are checked:
// since 0.2.0 markers live in the shared directory, and a marker left by an
// older release sits in the worktree's own git-dir.
func pausedOperation() (string, error) {
	dirs, err := pausedMarkerDirs()
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	return scanMarkerDirs(dirs), nil
}

// scanMarkerDirs reports the first paused gh-stack operation with a marker in
// dirs. A marker is the plain name (gh-stack-<op>-state) or its suffixed
// variant: 0.2.0's cross-worktree modify drops gh-stack-modify-state_<wt>
// beside it, and either form means a paused op.
func scanMarkerDirs(dirs []string) string {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, op := range []string{"rebase", "modify"} {
			want := "gh-stack-" + op + "-state"
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				if n := e.Name(); n == want || strings.HasPrefix(n, want+"_") {
					return op
				}
			}
		}
	}
	return ""
}

// pausedMarkerDirs lists the git-dirs a paused-operation marker may live in:
// this worktree's git-dir first, then the shared repository directory.
func pausedMarkerDirs() ([]string, error) {
	var dirs []string
	gitDir, err := capture("git", "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, gitDir)
	if common, err := capture("git", "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && common != gitDir {
		dirs = append(dirs, common)
	}
	return dirs, nil
}
