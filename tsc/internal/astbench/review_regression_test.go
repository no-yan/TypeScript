package astbench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareRejectsArtifactRootInsideRepository(t *testing.T) {
	for _, alias := range []string{"direct", "artifact-symlink", "repo-symlink"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			repo := filepath.Join(root, "repo")
			if err := os.Mkdir(repo, 0755); err != nil {
				t.Fatal(err)
			}
			selected := repo
			out := filepath.Join(repo, "unignored", "new", "artifacts")
			if alias == "artifact-symlink" {
				link := filepath.Join(root, "alias")
				if err := os.Symlink(repo, link); err != nil {
					t.Fatal(err)
				}
				out = filepath.Join(link, "unignored", "new", "artifacts")
			}
			if alias == "repo-symlink" {
				selected = filepath.Join(root, "selected")
				if err := os.Symlink(repo, selected); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Prepare(context.Background(), PrepareOptions{Repo: selected, Artifacts: out, Before: "HEAD", AfterWorkingTree: true, RunID: "nested"})
			if err == nil || !strings.Contains(err.Error(), "artifact root must be outside") {
				t.Errorf("expected boundary rejection before snapshots, got %v", err)
			}
			entries, err := os.ReadDir(repo)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("prepare wrote inside selected repository before rejecting: %v", entries)
			}
		})
	}
}

func TestRunSlotRawUsesVisitUnit(t *testing.T) {
	dir, p := savedCampaign(t)
	worker := filepath.Join(t.TempDir(), "astbench")
	cmd := exec.Command("go", "build", "-o", worker, "./cmd/astbench")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build worker: %v\n%s", err, out)
	}
	var id Identity
	if err := readJSON(filepath.Join(dir, "identity.json"), &id); err != nil {
		t.Fatal(err)
	}
	hash, err := hashFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	slot := p.Order[0]
	id.Binary[slot.Variant] = Binary{Label: slot.Variant, Path: worker, Snapshot: worker, SHA256: hash}
	if err := os.RemoveAll(filepath.Join(dir, "attempts")); err != nil {
		t.Fatal(err)
	}
	p.Timeout = 10 * time.Second
	if err := runSlot(context.Background(), dir, p, id, slot); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "attempts", "000001", "bench.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ns/node") || !strings.Contains(string(raw), "ns/visit") {
		t.Fatalf("raw worker output uses wrong normalized unit: %s", raw)
	}
}
