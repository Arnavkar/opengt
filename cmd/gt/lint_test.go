package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenSubstrings are import path fragments that planner files must not
// pull in. They enforce the dependency direction in docs/refactor-plan.md:
// commands → planner → domain → adapters. The planners (sync_plan.go,
// submit_plan.go, validate.go) sit between commands and the adapters, so
// they must not import os/exec (an adapter concern) or github.com/cli/go-gh
// (the GitHub adapter).
var forbiddenSubstrings = []string{
	"os/exec",
	"cli/go-gh",
}

// plannerFiles lists the *_plan.go files the lint must keep pure. Asserting
// they exist prevents the lint from silently passing if a planner is
// renamed away from the *_plan.go convention.
var plannerFiles = []string{
	"sync_plan.go",
	"submit_plan.go",
}

// TestPlannersExist guards against silent renames: if a planner is moved to
// a name that no longer ends in _plan.go, the *_plan.go glob would skip it
// and TestPlannersArePure would pass vacuously.
func TestPlannersExist(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read planner dir %q: %v", dir, err)
	}
	present := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			present[e.Name()] = true
		}
	}
	for _, name := range plannerFiles {
		if !present[name] {
			t.Errorf("expected planner file %s to exist in %s", name, dir)
		}
	}
}

// TestPlannersArePure parses every *_plan.go file in the package directory
// and asserts none imports a forbidden path. It fails (not skips) when no
// planner files are found, so a wholesale rename can't hide a regression.
func TestPlannersArePure(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read planner dir %q: %v", dir, err)
	}
	var planners []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, "_plan.go") {
			planners = append(planners, filepath.Join(dir, name))
		}
	}
	if len(planners) == 0 {
		t.Fatalf("no *_plan.go files found in %s; lint cannot enforce purity", dir)
	}
	fset := token.NewFileSet()
	for _, path := range planners {
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbiddenSubstrings {
				if strings.Contains(impPath, bad) {
					t.Errorf("%s imports forbidden path %q (matches %q); planners must stay pure",
						filepath.Base(path), impPath, bad)
				}
			}
		}
	}
}
