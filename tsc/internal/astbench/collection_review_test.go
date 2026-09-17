package astbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func savedCampaign(t *testing.T) (string, Plan) {
	t.Helper()
	dir := t.TempDir()
	p := defaultPlan("test", "/selected", "rev", "rev", "source-a", "source-b")
	p.SchemaVersion = SchemaVersion
	p.Cells = []Cell{{ID: "small", Case: "full-tree", Shape: "mixed", Nodes: 32, Seed: 1, LayoutSeed: 1, Layout: "construction", Representation: "store", GC: "off", P: 1, Backend: "wall", Batch: 10}}
	p.Lanes = map[string]Lane{"daily": {Name: "daily", Kind: "daily", Backend: "wall", Batches: 1, Warmup: 2, Directions: []string{"AB", "AB", "BA", "BA", "AA"}, Repeat: 1}}
	p.Order = nil
	if err := EnsureOrder(&p); err != nil {
		t.Fatal(err)
	}
	id := Identity{SchemaVersion: SchemaVersion, RepoRoot: p.RepoRoot, BeforeRevision: p.Identity.BeforeRevision, AfterRevision: p.Identity.AfterRevision, SourceHash: p.Identity.SourceHash, FixtureHash: p.Identity.FixtureHash}
	id.Binary = map[string]Binary{}
	for _, label := range []string{"before", "after"} {
		path := filepath.Join(dir, label+"-worker")
		if err := os.WriteFile(path, []byte(label), 0755); err != nil {
			t.Fatal(err)
		}
		h, err := hashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		id.Binary[label] = Binary{Label: label, Path: path, Snapshot: path, SHA256: h}
	}
	if err := writeJSON(filepath.Join(dir, "plan.json"), p); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "identity.json"), id); err != nil {
		t.Fatal(err)
	}
	cfg := configFromCell(p.Cells[0], 1)
	raw, _ := json.Marshal(cfg)
	proof, err := VerifyJSONBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "control", "verification-small.json"), VerificationRecord{CellID: "small", Status: "verified", Config: cfg, BeforeSHA256: id.Binary["before"].SHA256, AfterSHA256: id.Binary["after"].SHA256, Before: proof, After: proof}); err != nil {
		t.Fatal(err)
	}
	for i, s := range p.Order {
		saveAttempt(t, dir, fmt.Sprintf("%06d", i+1), s, validSavedSample(t))
	}
	return dir, p
}
func saveAttempt(t *testing.T, dir, name string, s Slot, sample Sample) {
	t.Helper()
	m := AttemptMetadata{SchemaVersion: SchemaVersion, AttemptID: name, SlotID: s.ID, CellID: s.CellID, Lane: s.Lane, Pair: s.Pair, Block: s.Block, Label: s.Label, Variant: s.Variant, StartedAt: time.Unix(1, 0), FinishedAt: time.Unix(2, 0)}
	var p Plan
	if err := readJSON(filepath.Join(dir, "plan.json"), &p); err != nil {
		t.Fatal(err)
	}
	cell, _ := findCell(p.Cells, s.CellID)
	m.Config = configFromCell(cell, cell.Batch)
	if s.Variant == "after" && cell.AfterRepresentation != "" {
		m.Config.Representation = cell.AfterRepresentation
	}
	var id Identity
	if err := readJSON(filepath.Join(dir, "identity.json"), &id); err != nil {
		t.Fatal(err)
	}
	m.Binary = s.Variant
	raw, _ := json.Marshal(m.Config)
	m.Argv = []string{id.Binary[s.Variant].Snapshot, "sample", "--config", string(raw)}
	m.Env = []string{"GOMAXPROCS=1", "GOGC=off", "GOMEMLIMIT=off"}
	if err := writeJSON(filepath.Join(dir, "attempts", name, "metadata.json"), m); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "attempts", name, "metrics.json"), AttemptResult{SchemaVersion: SchemaVersion, Sample: sample, Complete: true}); err != nil {
		t.Fatal(err)
	}
}
func TestCollectionRejectsMisleadingEvidence(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Sample)
	}{
		{"allocation", func(s *Sample) { s.Allocations = 1 }},
		{"gc", func(s *Sample) { s.GCCycles = 1 }},
		{"checksum", func(s *Sample) { s.Checksum++ }},
		{"visits", func(s *Sample) { s.Visits++ }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, p := savedCampaign(t)
			s := validSavedSample(t)
			tc.change(&s)
			saveAttempt(t, dir, "000001", p.Order[0], s)
			c, err := Collect(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.Valid {
				t.Fatal("misleading evidence accepted")
			}
		})
	}
}
func TestCollectionCompleteMissingDuplicateAndResume(t *testing.T) {
	dir, p := savedCampaign(t)
	c, err := Collect(dir)
	if err != nil || !c.Complete || !c.Valid {
		t.Fatalf("complete campaign: %+v %v", c, err)
	}
	path := filepath.Join(dir, "attempts", "000001", "metrics.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	c, err = Collect(dir)
	if err != nil || c.Complete || c.Valid {
		t.Fatalf("missing arm accepted: %+v %v", c, err)
	}
	sample := validSavedSample(t)
	saveAttempt(t, dir, "000999", p.Order[0], sample)
	c, err = Collect(dir)
	if err != nil || !c.Complete || !c.Valid {
		t.Fatalf("resumed campaign: %+v %v", c, err)
	}
	if len(c.Attempts) != len(p.Order)+1 {
		t.Fatal("partial attempt discarded")
	}
	saveAttempt(t, dir, "001000", p.Order[0], sample)
	c, err = Collect(dir)
	if err != nil || c.Valid || !strings.Contains(c.Reason, "duplicate") {
		t.Fatalf("duplicate accepted: %+v %v", c, err)
	}
}
func TestCollectionRejectsIdentityMismatch(t *testing.T) {
	dir, _ := savedCampaign(t)
	var id Identity
	if err := readJSON(filepath.Join(dir, "identity.json"), &id); err != nil {
		t.Fatal(err)
	}
	id.SourceHash = "changed"
	if err := writeJSON(filepath.Join(dir, "identity.json"), id); err != nil {
		t.Fatal(err)
	}
	if _, err := Collect(dir); err == nil {
		t.Fatal("changed source identity accepted")
	}
}
func TestPairBootstrapUsesIndependentPairs(t *testing.T) {
	a, l, h := bootstrapLogRatios([]float64{0, 0, 0, 0}, 42)
	if a != 1 || l != 1 || h != 1 {
		t.Fatalf("A/A bootstrap: %g %g %g", a, l, h)
	}
}

func TestCounterFailuresNeverBecomeZeroWork(t *testing.T) {
	for _, c := range []Counters{
		{Start: []uint64{10}, End: []uint64{20}, Delta: []uint64{10}, Valid: false, Reason: "read failure"},
		{Start: []uint64{10}, End: []uint64{9}, Delta: []uint64{0}, Valid: true},
		{Start: []uint64{10}, End: []uint64{10}, Delta: []uint64{0}, Valid: true},
		{Start: []uint64{10}, End: []uint64{20}, Delta: []uint64{8}, Valid: true},
	} {
		if err := validateCounters(c); err == nil {
			t.Fatalf("invalid counter accepted: %+v", c)
		}
	}
	if err := validateCounters(Counters{Start: []uint64{10}, End: []uint64{20}, Delta: []uint64{10}, Valid: true}); err != nil {
		t.Fatal(err)
	}
}

func validSavedSample(t *testing.T) Sample {
	t.Helper()
	raw := []byte(`{"case":"full-tree","shape":"mixed","nodes":32,"seed":1,"layout_seed":1,"layout":"construction","representation":"store","batch":1}`)
	v, err := VerifyJSONBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := traceWork(v.Store)
	s.NSPerOp = 100
	s.LogicalNodes = 32
	s.Valid = true
	return s
}
