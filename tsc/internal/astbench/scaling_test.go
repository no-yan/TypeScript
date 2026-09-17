package astbench

import (
	"encoding/json"
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/astbench/workload"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSweepBoundsAndGeneration(t *testing.T) {
	p, e := NewSweep(SweepOptions{RunID: "test", Visitor: "expression", StartSubtrees: 32, MaxCases: 3, Batch: 2, MemoryBudgetBytes: 16 << 20, Timeout: time.Second, Seed: 7})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Cells) != 3 || len(p.Order) != 48 {
		t.Fatalf("shape %d %d", len(p.Cells), len(p.Order))
	}
	for i, c := range p.Cells {
		if c.Subtrees != 32<<i || c.Generation.ActualNodes != 1+c.Subtrees*7 || c.Generation.RequestedNodes != c.Nodes {
			t.Fatal(c)
		}
	}
	p.Cells[0].Generation.MaxDepth++
	if EnsureOrder(&p) == nil {
		t.Fatal("tampered generation accepted")
	}
}
func TestSweepRequiresTwoBudgetedPoints(t *testing.T) {
	_, e := NewSweep(SweepOptions{Visitor: "expression", StartSubtrees: 32, MaxCases: 10, Batch: 1, MemoryBudgetBytes: 225 * 4096, Timeout: time.Second})
	if e == nil {
		t.Fatal("single point accepted")
	}
}
func TestScalingModeRequiresMetadata(t *testing.T) {
	p := defaultPlan("", "", "", "", "", "")
	p.Mode = "daily-selected"
	if EnsureOrder(&p) == nil {
		t.Fatal("unjustified preset accepted")
	}
}

// savedSweep constructs evidence without running timed workers. The deliberately
// simple memory/time values isolate selection policy from machine performance.
func savedSweep(t *testing.T) (string, Plan) {
	t.Helper()
	dir, old := savedCampaign(t)
	if err := os.RemoveAll(filepath.Join(dir, "attempts")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "control")); err != nil {
		t.Fatal(err)
	}
	p, err := NewSweep(SweepOptions{RunID: "sweep-test", Repo: old.RepoRoot, Visitor: "expression", StartSubtrees: 1, MaxCases: 4, Batch: 100, MemoryBudgetBytes: 1 << 20, Timeout: time.Second, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	p.Identity = old.Identity
	p.Sweep.Host.Cache = map[string]string{"hw.l1dcachesize": "128"}
	if err := writeJSON(filepath.Join(dir, "plan.json"), p); err != nil {
		t.Fatal(err)
	}
	var id Identity
	if err := readJSON(filepath.Join(dir, "identity.json"), &id); err != nil {
		t.Fatal(err)
	}
	samples := map[string]Sample{}
	for i, c := range p.Cells {
		cfg := configFromCell(c, 1)
		raw, _ := json.Marshal(cfg)
		proof, err := VerifyJSONBytes(raw)
		if err != nil {
			t.Fatal(err)
		}
		rec := VerificationRecord{ElapsedNS: 1000000, CellID: c.ID, Status: "verified", Config: cfg, BeforeSHA256: id.Binary["before"].SHA256, AfterSHA256: id.Binary["after"].SHA256, Before: proof, After: proof}
		if err := writeJSON(filepath.Join(dir, "control", "verification-"+c.ID+".json"), rec); err != nil {
			t.Fatal(err)
		}
		s := traceWork(proof.Store)
		s.Generation = *c.Generation
		s.LogicalNodes = uint64(c.Generation.ActualNodes)
		s.Valid = true
		s.NSPerOp = 1000
		s.MeasurementOverheadNS = 10
		s.MeasurementOverheadMethod = "test timer"
		bytes := uint64(100 << i)
		s.Memory.AccessFootprintEstimate = workload.ByteEstimate{Bytes: &bytes, Method: "test lower bound"}
		samples[c.ID] = s
	}
	for i, slot := range p.Order {
		saveAttempt(t, dir, fmt.Sprintf("%06d", i+1), slot, samples[slot.CellID])
	}
	return dir, p
}
func TestSelectDailyEvidence(t *testing.T) {
	for _, name := range []string{"eligible", "unknown-host", "oversized-l1", "overhead", "missing-sample", "missing-footprint", "small-too-large", "large-too-small", "missing-verification-time", "wrong-order", "zero-footprint", "missing-footprint-method", "negative-duration"} {
		t.Run(name, func(t *testing.T) {
			dir, p := savedSweep(t)
			small, large := p.Cells[0].ID, p.Cells[3].ID
			target := uint64(128)
			switch name {
			case "unknown-host":
				p.Sweep.Host.Cache = nil
				if err := writeJSON(filepath.Join(dir, "plan.json"), p); err != nil {
					t.Fatal(err)
				}
			case "oversized-l1":
				target = 256
			case "wrong-order":
				small, large = large, small
			case "negative-duration":
				path := filepath.Join(dir, "attempts", "000001", "metadata.json")
				var m AttemptMetadata
				if err := readJSON(path, &m); err != nil {
					t.Fatal(err)
				}
				m.FinishedAt = m.StartedAt.Add(-time.Second)
				if err := writeJSON(path, m); err != nil {
					t.Fatal(err)
				}
			case "missing-sample":

				if err := os.Remove(filepath.Join(dir, "attempts", "000001", "metrics.json")); err != nil {
					t.Fatal(err)
				}
			case "missing-verification-time":
				path := filepath.Join(dir, "control", "verification-"+small+".json")
				var r VerificationRecord
				if err := readJSON(path, &r); err != nil {
					t.Fatal(err)
				}
				r.ElapsedNS = 0
				if err := writeJSON(path, r); err != nil {
					t.Fatal(err)
				}
			case "overhead", "missing-footprint", "small-too-large", "large-too-small", "zero-footprint", "missing-footprint-method":
				for i, slot := range p.Order {
					if (name == "large-too-small" && slot.CellID != large) || (name != "large-too-small" && slot.CellID != small) {
						continue
					}
					path := filepath.Join(dir, "attempts", fmt.Sprintf("%06d", i+1), "metrics.json")
					var r AttemptResult
					if err := readJSON(path, &r); err != nil {
						t.Fatal(err)
					}
					switch name {
					case "overhead":
						r.Sample.MeasurementOverheadNS = 10000
					case "zero-footprint":
						n := uint64(0)
						r.Sample.Memory.AccessFootprintEstimate.Bytes = &n
					case "missing-footprint-method":
						r.Sample.Memory.AccessFootprintEstimate.Method = ""
					case "missing-footprint":

						r.Sample.Memory.AccessFootprintEstimate.Bytes = nil
					case "small-too-large":
						n := uint64(129)
						r.Sample.Memory.AccessFootprintEstimate.Bytes = &n
					case "large-too-small":
						n := uint64(511)
						r.Sample.Memory.AccessFootprintEstimate.Bytes = &n
					}
					if err := writeJSON(path, r); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			selected, err := SelectDaily(dir, small, large, "test host and time evidence", target, 1)
			if name != "eligible" {
				if err == nil {
					t.Fatal("invalid selection accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if selected.Mode != "daily-selected" || len(selected.Cells) != 2 || len(selected.Order) != 32 || selected.Preset.SourceSweepID != p.RunID || selected.Preset.SourceInputHash == "" {
				t.Fatalf("selection lost provenance: %+v", selected)
			}
			for _, change := range []func(*PresetSelection){func(p *PresetSelection) { p.SourceInputHash = "bad" }, func(p *PresetSelection) { p.L1DTargetBytes = 256 }, func(p *PresetSelection) { p.Host.Cache = nil }, func(p *PresetSelection) { p.EstimatedSeconds = 0 }} {
				forged := selected
				copy := *selected.Preset
				forged.Preset = &copy
				change(forged.Preset)
				if EnsureOrder(&forged) == nil {
					t.Fatal("forged preset evidence accepted")
				}
			}
			if err := validatePresetHost(selected, selected.Preset.Host); err != nil {
				t.Fatal(err)
			}
			changed := selected.Preset.Host
			changed.CPU += "different"
			if validatePresetHost(selected, changed) == nil {
				t.Fatal("different host accepted")
			}
			selected.Preset.LargeCell = "forged"

			if EnsureOrder(&selected) == nil {
				t.Fatal("forged preset binding accepted")
			}
		})
	}
}

func TestReportScalingTerminology(t *testing.T) {
	dir, _ := savedSweep(t)
	report, err := Report(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ns/visit", "divided by visits/op", "Mode: sweep", "real-pointer-vs-real-store", "provisional", "representation_used_bytes", "representation_capacity_bytes", "reachable_heap_bytes_estimate", "process_peak_rss", "access_footprint_estimate", "scaling.tsv", "scaling.svg", "samples.tsv", "memory.tsv", "cells/expression-subtrees-1/benchstat.txt"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if strings.Contains(report, "ns/node") || strings.Contains(report, "Working-set size") {
		t.Fatal("report retains misleading legacy units or combined memory terminology")
	}
}
