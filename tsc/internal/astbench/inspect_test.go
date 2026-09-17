package astbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInspectSeparatesIdentityAndStage(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		return string(b)
	}
	runGit("init", "-q")
	runGit("commit", "--allow-empty", "-qm", "test")
	rev, e := gitRevision(repo, "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	artifacts := t.TempDir()
	id := Identity{RepoRoot: repo, TypescriptGoGitRev: rev, TSGolintGitRev: nil}
	if e := writeJSON(filepath.Join(artifacts, "identity.json"), id); e != nil {
		t.Fatal(e)
	}
	check := func(identity, stage string) {
		t.Helper()
		got, e := Inspect(repo, artifacts, "BenchmarkTraversal")
		if e != nil || len(got) != 1 {
			t.Fatalf("inspect: %v %v", got, e)
		}
		if got[0].IdentityStatus != identity || got[0].StageStatus != stage {
			t.Fatalf("unexpected %+v", got[0])
		}
	}
	check("current", "unsupported")
	if e := os.WriteFile(filepath.Join(artifacts, "bench.txt"), []byte("BenchmarkTraversal/small 10 100 ns/op 0 B/op 0 allocs/op\n"), 0644); e != nil {
		t.Fatal(e)
	}
	check("current", "current")
	id.TypescriptGoGitRev = "other"
	if e := writeJSON(filepath.Join(artifacts, "identity.json"), id); e != nil {
		t.Fatal(e)
	}
	check("stale", "stale")
	if e := os.Remove(filepath.Join(artifacts, "bench.txt")); e != nil {
		t.Fatal(e)
	}
	check("stale", "unsupported")
}
