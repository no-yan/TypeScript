package astbench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitUntrackedListsOnlyUntrackedFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := runGit(root, "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(root, "add", "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(root, "-c", "user.name=astbench", "-c", "user.email=astbench@example.invalid", "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gitUntracked(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "untracked.txt\n" {
		t.Fatalf("got untracked list %q", got)
	}
}

func TestCopyHarnessOverlayIncludesFixture(t *testing.T) {
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// This test runs from tsc/internal/astbench; walk back to the module root.
	fixture := filepath.Join(repo, "workload", "testdata", "fixture.ts")
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("fixture unavailable: %v", err)
	}
	dst := t.TempDir()
	moduleRoot := filepath.Clean(filepath.Join(repo, "..", "..", ".."))
	if err := copyHarnessOverlay(moduleRoot, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "tsc", "internal", "astbench", "workload", "testdata", "fixture.ts")); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRejectsSuppliedBinariesBeforeSnapshot(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	p := defaultPlan("reject", root, "", "", "", "")
	p.Binaries = []BinarySpec{{Label: "external", Path: "/tmp/external"}}
	if err := writeJSON(planPath, p); err != nil {
		t.Fatal(err)
	}
	_, err = Prepare(context.Background(), PrepareOptions{Repo: root, Artifacts: t.TempDir(), Before: "HEAD", After: "HEAD", PlanPath: planPath, RunID: "reject"})
	if err == nil || !strings.Contains(err.Error(), "supplied binaries") {
		t.Fatalf("expected supplied binary rejection, got %v", err)
	}
}

func TestValidateRunID(t *testing.T) {
	for _, id := range []string{"", ".", "..", "nested/run", "/absolute", "\\absolute"} {
		if err := validateRunID(id); err == nil {
			t.Fatalf("accepted invalid run id %q", id)
		}
	}
	for _, id := range []string{"run-1", "20260917T120000Z"} {
		if err := validateRunID(id); err != nil {
			t.Fatalf("rejected valid run id %q: %v", id, err)
		}
	}
}

func runGit(dir string, args ...string) ([]byte, error) {
	// Keep this tiny test helper local so production git wrappers remain the
	// only path used by Prepare.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
