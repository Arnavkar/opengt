package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Compatibility with github/gh-stack is a formal dependency of this shim.
// CI pins the tested release in .github/workflows/ci.yml; the daily
// gh-stack-compat workflow runs the same tests against `latest` as an
// early warning and must not be treated as the version developers run.
var ghStackCompat = GhStackCompatibility{
	MinVersion:     Version{0, 1, 0},
	TestedVersion:  Version{0, 1, 1},
	SchemaVersions: []int{1},
	StateFileName:  "gh-stack",
}

type GhStackCompatibility struct {
	MinVersion     Version
	TestedVersion  Version
	SchemaVersions []int
	StateFileName  string
}

type Version struct{ Major, Minor, Patch int }

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v Version) below(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

func (v Version) above(o Version) bool { return o.below(v) }

func (c GhStackCompatibility) SupportsSchema(n int) bool {
	for _, s := range c.SchemaVersions {
		if s == n {
			return true
		}
	}
	return false
}

func (c GhStackCompatibility) PrimarySchema() int { return c.SchemaVersions[0] }

var versionDigits = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func parseGhStackVersion(out string) (Version, bool) {
	m := versionDigits.FindStringSubmatch(out)
	if m == nil {
		return Version{}, false
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	return Version{maj, min, pat}, true
}

type compatStatus struct {
	Installed bool
	Raw       string
	Version   Version
	Parsed    bool
	Warning   string
}

func inspectGhStack() (compatStatus, error) {
	var st compatStatus
	out, err := capture("gh", "stack", "--version")
	if err != nil {
		return st, err
	}
	st.Installed = true
	st.Raw = strings.TrimSpace(out)
	st.Version, st.Parsed = parseGhStackVersion(st.Raw)
	if !st.Parsed {
		st.Warning = "could not parse gh-stack version from " + strconv.Quote(st.Raw) + "; continuing"
		return st, nil
	}
	if st.Version.below(ghStackCompat.MinVersion) {
		return st, fmt.Errorf(
			"gh-stack %s is older than the minimum supported %s.\n"+
				"    Upgrade with: gh extension install github/gh-stack --pin v%s --force",
			st.Version, ghStackCompat.MinVersion, ghStackCompat.MinVersion)
	}
	if st.Version.above(ghStackCompat.TestedVersion) {
		st.Warning = fmt.Sprintf(
			"gh-stack %s is newer than the tested %s; schema v%d is still required",
			st.Version, ghStackCompat.TestedVersion, ghStackCompat.PrimarySchema())
	}
	return st, nil
}

func warnCompat(st compatStatus) {
	if st.Warning == "" || compatWarned {
		return
	}
	compatWarned = true
	fmt.Fprintf(os.Stderr, "gt: warning: %s\n", st.Warning)
}

var compatWarned bool

var (
	ghStackChecked bool
	ghStackErr     error
)

func requireGhStack() error {
	if ghStackChecked {
		return ghStackErr
	}
	ghStackChecked = true
	if _, err := ensureExtension(); err != nil {
		ghStackErr = err
		return err
	}
	st, err := inspectGhStack()
	if err != nil {
		ghStackErr = err
		return err
	}
	warnCompat(st)
	return nil
}
