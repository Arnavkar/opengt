package main

import "testing"

func TestParseGhStackVersion(t *testing.T) {
	v, ok := parseGhStackVersion("gh stack version 0.2.1")
	if !ok || v.String() != "0.2.1" {
		t.Fatalf("got %v ok=%v", v, ok)
	}
	if v.below(ghStackCompat.MinVersion) {
		t.Fatal("0.2.1 should not be below min")
	}
	if v.above(ghStackCompat.TestedVersion) {
		t.Fatal("0.2.1 should not be above tested")
	}
	floor, _ := parseGhStackVersion("0.2.0")
	if floor.below(ghStackCompat.MinVersion) {
		t.Fatal("0.2.0 should not be below min")
	}
	old, _ := parseGhStackVersion("0.1.1")
	if !old.below(ghStackCompat.MinVersion) {
		t.Fatal("0.1.1 should be below min")
	}
	newer, _ := parseGhStackVersion("1.2.3")
	if !newer.above(ghStackCompat.TestedVersion) {
		t.Fatal("1.2.3 should be above tested")
	}
}

func TestSupportsSchema(t *testing.T) {
	if !ghStackCompat.SupportsSchema(1) || ghStackCompat.SupportsSchema(2) {
		t.Fatal("schema support")
	}
	if ghStackCompat.PrimarySchema() != 1 {
		t.Fatal("primary schema")
	}
}
