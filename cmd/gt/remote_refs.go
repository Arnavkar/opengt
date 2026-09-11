package main

import (
	"strings"
	"time"
)

// LoadRemoteRefs runs a targeted `git ls-remote --heads origin <branches...>`
// and returns one RemoteRef per requested branch. Missing branches are
// reported with Exists:false rather than dropped. This mirrors
// fetchStackOrigin's stack-only ls-remote strategy (git.go:299) and does not
// perform a full `git fetch`.
func LoadRemoteRefs(branches []string) (RemoteRefSnapshot, error) {
	requested := dedupBranches(branches)
	if len(requested) == 0 {
		return RemoteRefSnapshot{Refs: map[string]RemoteRef{}, FetchedAt: time.Now()}, nil
	}
	args := append([]string{"ls-remote", "--heads", "origin"}, requested...)
	out, err := capture("git", args...)
	if err != nil {
		return RemoteRefSnapshot{}, err
	}
	return buildRemoteRefSnapshot(out, requested, time.Now()), nil
}

// buildRemoteRefSnapshot is the pure, network-free core of LoadRemoteRefs: it
// maps ls-remote output plus the requested branch list into a snapshot. A
// requested branch absent from the output gets Exists:false; a present one
// gets Exists:true with the SHA from the output.
func buildRemoteRefSnapshot(out string, requested []string, fetchedAt time.Time) RemoteRefSnapshot {
	shas := parseLsRemoteHeadsWithSHA(out)
	refs := make(map[string]RemoteRef, len(requested))
	for _, b := range requested {
		sha, ok := shas[b]
		refs[b] = RemoteRef{Branch: b, Exists: ok, RemoteSHA: sha}
	}
	return RemoteRefSnapshot{Refs: refs, FetchedAt: fetchedAt}
}

// parseLsRemoteHeadsWithSHA parses `git ls-remote --heads` output into a
// name→sha map. It mirrors parseLsRemoteHeads (git.go:354): each line is
// `<sha>\trefs/heads/<name>` (a space separator is also tolerated), the
// refs/heads/ prefix is stripped, and blank lines are skipped.
func parseLsRemoteHeadsWithSHA(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			sha, ref, ok = strings.Cut(line, " ")
		}
		if !ok {
			continue
		}
		name, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/")
		if !ok || name == "" {
			continue
		}
		m[name] = strings.TrimSpace(sha)
	}
	return m
}

// dedupBranches returns the de-duplicated list of branches, preserving the
// first-seen order. Empty names are dropped.
func dedupBranches(branches []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range branches {
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true
		out = append(out, b)
	}
	return out
}
