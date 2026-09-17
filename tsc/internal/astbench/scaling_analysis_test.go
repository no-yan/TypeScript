package astbench

import (
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScalingOutputsUseVisitsAndPreserveAttempts(t *testing.T) {
	out := t.TempDir()
	p := Plan{Seed: 42, Cells: []Cell{{ID: "expression-small", Case: "expression", Nodes: 100, Representation: "pointer", AfterRepresentation: "store", Batch: 50}}}
	c := Collection{}
	bySlot := map[string]Collected{}
	for i, label := range []string{"before", "after", "before", "after", "before", "after", "before", "after"} {
		slot := Slot{ID: label + string(rune('a'+i)), CellID: p.Cells[0].ID, Label: label, Variant: label}
		p.Order = append(p.Order, slot)
		a := Collected{Metadata: AttemptMetadata{AttemptID: slot.ID, SlotID: slot.ID, CellID: slot.CellID, Label: label, Variant: label, StartedAt: time.Unix(int64(i), 0), FinishedAt: time.Unix(int64(i+1), 0)}, Result: AttemptResult{Complete: true, Sample: Sample{Valid: true, LogicalNodes: 100, Visits: 25, NSPerOp: 100}}}
		bySlot[slot.ID] = a
		c.Attempts = append(c.Attempts, a)
	}
	failed := c.Attempts[0]
	failed.Metadata.AttemptID = "failed-retry"
	failed.Result.Complete = false
	failed.Result.Error = "timeout"
	c.Attempts = append(c.Attempts, failed)
	estimates := []pairEstimate{{Cell: p.Cells[0].ID, Pairs: 4, AfterBeforeRatio: 1, Low: 1, High: 1, Status: ratioStatus(1, 1)}}
	if err := writeScalingAnalysis(out, p, c, bySlot, estimates); err != nil {
		t.Fatal(err)
	}
	read := func(name string) [][]string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		r := csv.NewReader(bytes.NewReader(data))
		r.Comma = '\t'
		rows, err := r.ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := read("samples.tsv")
	if len(rows) != 10 {
		t.Fatalf("lost failed attempt: %d rows", len(rows))
	}
	valid, invalid := 0, 0
	for _, row := range rows[1:] {
		if row[17] == "true" {
			valid++
			if row[13] != "4" {
				t.Fatalf("expected ns/op / visits, got %q", row[13])
			}
		} else {
			invalid++
			if row[13] != "null" {
				t.Fatal("failed sample normalized")
			}
		}
	}
	if valid != 8 || invalid != 1 {
		t.Fatalf("valid=%d invalid=%d", valid, invalid)
	}
	memory := read("memory.tsv")
	if len(memory) != 46 || memory[1][5] != "null" || memory[1][7] == "" {
		t.Fatalf("missing memory must retain explicit reason: %v", memory[1])
	}
	summary := read("scaling.tsv")
	if summary[1][2] != "100" || summary[1][3] != "25" || summary[1][4] != "4" || summary[1][7] != "4" {
		t.Fatalf("normalization mismatch: %v", summary[1])
	}
	svg, err := os.ReadFile(filepath.Join(out, "scaling.svg"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(svg))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid SVG: %v", err)
		}
	}
	if bytes.Contains(svg, []byte("%!")) || bytes.Contains(svg, []byte("NaN")) || !bytes.Contains(svg, []byte("log2 axis")) {
		t.Fatalf("invalid SVG coordinates or labels: %s", svg)
	}
}

func TestDailyRatioNeverClaimsConfirmedImprovement(t *testing.T) {
	if !strings.HasPrefix(ratioStatus(.99, 1.01), "noisy:") {
		t.Fatal("interval includes no change")
	}
	if !strings.HasPrefix(ratioStatus(.80, .90), "directional:") {
		t.Fatal("daily signal must remain directional")
	}
	a, lo, hi := bootstrapMean([]float64{4, 4, 4, 4}, 42)
	if a != 4 || lo != 4 || hi != 4 {
		t.Fatalf("constant-sample uncertainty: %g %g %g", a, lo, hi)
	}
}

func TestAnalysisRegeneratesWithoutBenchstat(t *testing.T) {
	run, _ := savedCampaign(t)
	t.Setenv("PATH", t.TempDir())
	if err := WriteAnalysis(run); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(run, "analysis")
	for _, name := range []string{"samples.tsv", "memory.tsv", "scaling.tsv", "scaling.svg", "paired.json", "cells/small/before.txt", "cells/small/after.txt", "cells/small/control-before.txt", "cells/small/control-after.txt"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || len(data) == 0 {
			t.Fatalf("missing generated %s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, "cells/small/before.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("ns/visit")) || bytes.Contains(raw, []byte("ns/node")) {
		t.Fatalf("normalization unit: %s", raw)
	}
	stat, err := os.ReadFile(filepath.Join(out, "cells/small/benchstat.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stat, []byte("comparison not performed")) {
		t.Fatal("missing benchstat silently hidden")
	}
	before, err := os.ReadFile(filepath.Join(out, "scaling.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAnalysis(run); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(out, "scaling.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("regeneration must be deterministic")
	}
}
