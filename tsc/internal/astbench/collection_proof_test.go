package astbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectionRequiresWorkerBinding(t *testing.T) {
	dir, _ := savedCampaign(t)
	path := attemptPathForVariant(t, dir, "after")
	var metadata AttemptMetadata
	if err := readJSON(filepath.Join(path, "metadata.json"), &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Binary = "before"
	if err := writeJSON(filepath.Join(path, "metadata.json"), metadata); err != nil {
		t.Fatal(err)
	}
	c, err := Collect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Valid || !strings.Contains(c.Reason, "binding differs") {
		t.Fatalf("attempt with a mislabeled worker was accepted: %+v", c)
	}
}

func TestCollectionRequiresRecordedWorkerArgv(t *testing.T) {
	dir, _ := savedCampaign(t)
	path := attemptPathForVariant(t, dir, "after")
	var metadata AttemptMetadata
	if err := readJSON(filepath.Join(path, "metadata.json"), &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Argv[0] = "/untrusted/worker"
	if err := writeJSON(filepath.Join(path, "metadata.json"), metadata); err != nil {
		t.Fatal(err)
	}
	c, err := Collect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Valid || !strings.Contains(c.Reason, "binding differs") {
		t.Fatalf("attempt with an unbound argv was accepted: %+v", c)
	}
}

func TestCollectionRequiresExactTraceProof(t *testing.T) {
	dir, p := savedCampaign(t)
	path := filepath.Join(dir, "control", "verification-"+p.Cells[0].ID+".json")
	var proof VerificationRecord
	if err := readJSON(path, &proof); err != nil {
		t.Fatal(err)
	}
	proof.After.Store = proof.After.Store[:len(proof.After.Store)-1]
	if err := writeJSON(path, proof); err != nil {
		t.Fatal(err)
	}
	c, err := Collect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Valid || !strings.Contains(c.Reason, "trace verification") {
		t.Fatalf("tampered trace proof was accepted: %+v", c)
	}
}

func attemptPathForVariant(t *testing.T, dir, variant string) string {
	t.Helper()
	entries, err := readAttemptDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		var metadata AttemptMetadata
		if err := readJSON(filepath.Join(path, "metadata.json"), &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Variant == variant {
			return path
		}
	}
	t.Fatalf("no %s attempt", variant)
	return ""
}

func readAttemptDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "attempts"))
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			paths = append(paths, filepath.Join(dir, "attempts", entry.Name()))
		}
	}
	return paths, nil
}
