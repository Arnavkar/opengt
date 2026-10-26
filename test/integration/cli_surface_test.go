//go:build integration

package integration

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersionNamesOpengt(t *testing.T) {
	out, err := exec.Command(gtBin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("gt --version: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "(opengt)") {
		t.Errorf("gt --version = %q, want (opengt)", got)
	}
}

func TestHelpListsCommands(t *testing.T) {
	out, err := exec.Command(gtBin, "help").CombinedOutput()
	if err != nil {
		t.Fatalf("gt help: %v\n%s", err, out)
	}
	got := string(out)
	for _, name := range []string{"create", "submit", "sync", "doctor"} {
		if !strings.Contains(got, name) {
			t.Errorf("gt help missing %q:\n%s", name, got)
		}
	}
	if !strings.Contains(got, "passed through to git") {
		t.Errorf("gt help does not mention git passthrough:\n%s", got)
	}
}

func TestInitExplainsGhStackModel(t *testing.T) {
	f := newFixture(t)
	r := f.gtFails("init")
	if !strings.Contains(r.stderr, "no repository-level init") {
		t.Errorf("gt init:\n%s", r.output())
	}
	if !strings.Contains(r.stderr, "gt create") || !strings.Contains(r.stderr, "gt track") {
		t.Errorf("gt init did not point at create/track:\n%s", r.output())
	}
}

func TestGetRequiresTarget(t *testing.T) {
	f := newFixture(t)
	r := f.gtFails("get")
	if !strings.Contains(r.stderr, "needs a branch, PR number, or PR URL") {
		t.Errorf("gt get:\n%s", r.output())
	}
}

func TestSubmitDryRunDoesNotPush(t *testing.T) {
	f := newFixture(t)
	f.layer("layer-one", "Add layer one")
	f.layer("layer-two", "Add layer two")

	r := f.run(gtBin, "submit", "--dry-run")
	remote := f.git("ls-remote", "--heads", "origin")
	if strings.Contains(remote, "refs/heads/layer-one") || strings.Contains(remote, "refs/heads/layer-two") {
		t.Errorf("dry-run pushed stack branches:\nremote=%s\n%s", remote, r.output())
	}
}
