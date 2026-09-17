package astbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Collect validates saved samples without starting workers or changing artifacts.
func Collect(runDir string) (Collection, error) {
	out := Collection{SchemaVersion: SchemaVersion, RunDir: runDir, Complete: true, Valid: true}
	var p Plan
	var id Identity
	if err := readJSON(filepath.Join(runDir, "plan.json"), &p); err != nil {
		return out, err
	}
	if err := readJSON(filepath.Join(runDir, "identity.json"), &id); err != nil {
		return out, err
	}
	if err := validatePlan(p); err != nil {
		return out, err
	}
	if len(p.Order) == 0 {
		return out, errors.New("plan has no saved execution order")
	}
	if err := validateOrder(p); err != nil {
		return out, err
	}
	if id.SchemaVersion != SchemaVersion || p.SchemaVersion != SchemaVersion {
		return out, errors.New("unsupported artifact schema")
	}
	if p.RepoRoot != id.RepoRoot || p.Identity.BeforeRevision != id.BeforeRevision || p.Identity.AfterRevision != id.AfterRevision || p.Identity.SourceHash != id.SourceHash || p.Identity.FixtureHash != id.FixtureHash {
		return out, errors.New("plan/identity mismatch")
	}
	for _, label := range []string{"before", "after"} {
		b, ok := id.Binary[label]
		if !ok || b.Label != label || b.Snapshot == "" || b.SHA256 == "" {
			return out, fmt.Errorf("missing or invalid required binary %q", label)
		}
		path := b.Snapshot
		h, err := hashFile(path)
		if err != nil {
			return out, fmt.Errorf("binary %s: %w", label, err)
		}
		if h != b.SHA256 {
			return out, fmt.Errorf("binary %s hash mismatch", label)
		}
	}
	expected := make(map[string]Slot, len(p.Order))
	for _, s := range p.Order {
		expected[s.ID] = s
	}
	seen := map[string]Sample{}
	reasons := []string{}
	reject := func(reason string) { out.Valid = false; reasons = append(reasons, reason) }
	proofs := validateVerificationRecords(runDir, p, id, reject)
	entries, err := os.ReadDir(filepath.Join(runDir, "attempts"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var m AttemptMetadata
		var r AttemptResult
		dir := filepath.Join(runDir, "attempts", e.Name())
		if err := readJSON(filepath.Join(dir, "metadata.json"), &m); err != nil {
			out.Attempts = append(out.Attempts, Collected{Metadata: AttemptMetadata{AttemptID: e.Name()}, Result: AttemptResult{Error: err.Error()}})
			continue
		}
		if err := readJSON(filepath.Join(dir, "metrics.json"), &r); err != nil {
			r.Error = err.Error()
		}
		out.Attempts = append(out.Attempts, Collected{Metadata: m, Result: r})
		slot, ok := expected[m.SlotID]
		if !ok {
			reject("unknown slot " + m.SlotID)
			continue
		}
		if m.SchemaVersion != SchemaVersion || m.AttemptID != e.Name() || m.CellID != slot.CellID || m.Lane != slot.Lane || m.Pair != slot.Pair || m.Block != slot.Block || m.Label != slot.Label || m.Variant != slot.Variant {
			reject("attempt metadata mismatch: " + e.Name())
			continue
		}
		cell, _ := findCell(p.Cells, slot.CellID)
		if err := validateAttemptBinding(m, slot, cell, id); err != nil {
			reject("attempt binding differs from plan: " + e.Name() + ": " + err.Error())
			continue
		}
		if !r.Complete || !r.Sample.Valid || m.FinishedAt.IsZero() || m.Timeout || m.Cancelled || m.ExitCode != 0 || r.Error != "" {
			continue
		}
		if r.SchemaVersion != SchemaVersion {
			reject("attempt schema mismatch: " + e.Name())
			continue
		}
		if len(r.Counters.Start) != 0 || len(r.Counters.End) != 0 || len(r.Counters.Delta) != 0 || r.Counters.Reason != "" {
			if err := validateCounters(r.Counters); err != nil {
				reject("invalid counters " + e.Name() + ": " + err.Error())
				continue
			}
		}
		if err := validateSample(r.Sample); err != nil {
			reject("invalid successful sample " + e.Name() + ": " + err.Error())
			continue
		}
		if r.Sample.Allocations != 0 || r.Sample.AllocatedBytes != 0 || r.Sample.GCCycles != 0 || r.Sample.BytesPerOp != 0 || r.Sample.AllocsPerOp != 0 {
			reject("synthetic allocation/GC contract violation: " + e.Name())
			continue
		}
		if err := validateMeasuredWork(r.Sample, cell, proofs[cell.ID]); err != nil {
			reject("sample work differs from trace proof: " + e.Name() + ": " + err.Error())
			continue
		}
		if _, ok := seen[m.SlotID]; ok {
			reject("duplicate successful attempt for slot " + m.SlotID)
			continue
		}
		seen[m.SlotID] = r.Sample
	}
	reference := map[string]Sample{}
	for _, s := range p.Order {
		sample, ok := seen[s.ID]
		if !ok {
			out.Complete = false
			reasons = append(reasons, "missing or invalid slot "+s.ID)
			continue
		}
		if old, ok := reference[s.CellID]; ok {
			if sample.Visits != old.Visits || sample.Checksum != old.Checksum || sample.LogicalNodes != old.LogicalNodes || sample.EdgeReads != old.EdgeReads || sample.AttributeReads != old.AttributeReads || sample.Revisits != old.Revisits {
				reject("work/checksum mismatch in cell " + s.CellID)
			}
		} else {
			reference[s.CellID] = sample
		}
	}
	if !out.Complete {
		out.Valid = false
	}
	out.Reason = strings.Join(reasons, "; ")
	return out, nil
}

func validateAttemptBinding(m AttemptMetadata, slot Slot, cell Cell, id Identity) error {
	config := configFromCell(cell, cell.Batch)
	if slot.Variant == "after" && cell.AfterRepresentation != "" {
		config.Representation = cell.AfterRepresentation
	}
	if m.Config != config {
		return errors.New("config")
	}
	binary, ok := id.Binary[slot.Variant]
	if !ok || binary.Label != slot.Variant || binary.Snapshot == "" || binary.SHA256 == "" || m.Binary != slot.Variant {
		return errors.New("binary")
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if len(m.Argv) != 4 || m.Argv[0] != binary.Snapshot || m.Argv[1] != "sample" || m.Argv[2] != "--config" || m.Argv[3] != string(configJSON) {
		return errors.New("argv")
	}
	if !reflect.DeepEqual(m.Env, []string{"GOMAXPROCS=1", "GOGC=off", "GOMEMLIMIT=off"}) {
		return errors.New("environment")
	}
	return nil
}

func validateVerificationRecords(runDir string, p Plan, id Identity, reject func(string)) map[string]VerificationRecord {
	proofs := make(map[string]VerificationRecord, len(p.Cells))
	for _, cell := range p.Cells {
		var record VerificationRecord
		path := filepath.Join(runDir, "control", "verification-"+cell.ID+".json")
		if err := readJSON(path, &record); err != nil {
			reject("missing or invalid trace verification for cell " + cell.ID)
			continue
		}
		if record.CellID != cell.ID || record.Status != "verified" || record.Config != configFromCell(cell, 1) ||
			record.BeforeSHA256 != id.Binary["before"].SHA256 || record.AfterSHA256 != id.Binary["after"].SHA256 {
			reject("trace verification metadata differs from plan for cell " + cell.ID)
			continue
		}
		if !validTraceVerification(record.Before, cell.Shape == "fixture") || !validTraceVerification(record.After, cell.Shape == "fixture") {
			reject("invalid trace verification for cell " + cell.ID)
			continue
		}
		if !reflect.DeepEqual(record.Before.Store, record.After.Store) {
			reject("cross-binary store trace mismatch for cell " + cell.ID)
			continue
		}
		if cell.Shape != "fixture" && !reflect.DeepEqual(record.Before.Pointer, record.After.Pointer) {
			reject("cross-binary pointer trace mismatch for cell " + cell.ID)
			continue
		}
		proofs[cell.ID] = record
	}
	return proofs
}

func validTraceVerification(v TraceVerification, fixture bool) bool {
	if !v.Valid || v.Entries == 0 || v.Entries != len(v.Store) || len(v.Store) == 0 {
		return false
	}
	if fixture {
		return len(v.Pointer) == 0
	}
	return len(v.Pointer) == len(v.Store) && reflect.DeepEqual(v.Store, v.Pointer)
}

func validateMeasuredWork(sample Sample, cell Cell, proof VerificationRecord) error {
	want := traceWork(proof.Before.Store)
	if sample.Visits != want.Visits || sample.Checksum != want.Checksum || sample.EdgeReads != want.EdgeReads || sample.AttributeReads != want.AttributeReads {
		return errors.New("visits, checksum, edge reads, or attribute reads")
	}
	if cell.Shape == "fixture" {
		if sample.LogicalNodes < sample.Visits {
			return errors.New("fixture logical node count is below visits")
		}
	} else if sample.LogicalNodes != uint64(cell.Nodes) {
		return errors.New("synthetic logical node count")
	}
	return nil
}

func validateCounters(c Counters) error {
	if !c.Valid || c.Reason != "" || len(c.Start) == 0 || len(c.Start) != len(c.End) || len(c.Start) != len(c.Delta) {
		return errors.New("counter read failed or shape mismatch")
	}
	for i, start := range c.Start {
		if c.End[i] < start || c.Delta[i] != c.End[i]-start || c.Delta[i] == 0 {
			return fmt.Errorf("counter %d is zero, decreasing, or inconsistent", i)
		}
	}
	return nil
}

func Report(runDir string) (string, error) {
	c, err := Collect(runDir)
	if err != nil {
		return "", err
	}
	var id Identity
	if err := readJSON(filepath.Join(runDir, "identity.json"), &id); err != nil {
		return "", err
	}
	var p Plan
	if err := readJSON(filepath.Join(runDir, "plan.json"), &p); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# AST traversal measurement\n\nSelected repo: `%s`\n\nArtifact set: `%s`\n\nBefore revision: `%s`; after revision: `%s`; TSGolint: null.\n\n", id.RepoRoot, runDir, id.BeforeRevision, id.AfterRevision)
	fmt.Fprintf(&b, "Completeness: %t; validity: %t. %s\n\n", c.Complete, c.Valid, c.Reason)
	b.WriteString("Identity status requires `inspect --repo` against the selected checkout; saved artifacts alone do not establish currentness.\n\nCandidate clones: cursor-ast-store-tests, binder-rewrite, profile, flownode, lock-design-inv, lock-profile, store-pr-*, store-redesign, store-schema-foreach-child, main checkout.\n\n")
	b.WriteString("| Attempt | Cell | Label | ns/op | B/op | allocs/op | visits/op | status |\n| --- | --- | --- | ---: | ---: | ---: | ---: | --- |\n")
	for _, a := range c.Attempts {
		s := a.Result.Sample
		status := "invalid"
		if a.Result.Complete && validateSample(s) == nil {
			status = "valid"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %.3f | %.3f | %.3f | %d | %s |\n", a.Metadata.AttemptID, a.Metadata.CellID, a.Metadata.Label, s.NSPerOp, s.BytesPerOp, s.AllocsPerOp, s.Visits, status)
	}
	b.WriteString("\nOne op is one root traversal; ns/node uses actual visits, not logical node count. One process is one statistical sample regardless of batch size.\n\n")
	if !c.Valid || !c.Complete {
		b.WriteString("Diagnosis: invalid/incomplete; comparison is not available.\n\n")
	} else {
		text, err := os.ReadFile(filepath.Join(runDir, "analysis", "benchstat.txt"))
		wantHash, hashErr := os.ReadFile(filepath.Join(runDir, "analysis", "input.sha256"))
		gotHash, inputErr := analysisInputHash(runDir)
		if hashErr != nil || inputErr != nil || strings.TrimSpace(string(wantHash)) != gotHash {
			err = errors.New("analysis inputs changed or digest missing")
		}
		if err != nil {
			b.WriteString("Comparison: missing. Run collect with an explicit analysis output before interpreting differences.\n\n")
		} else {
			b.WriteString("## benchstat\n\n```text\n")
			b.Write(text)
			b.WriteString("\n```\n\n")
		}
		b.WriteString("Diagnosis: daily direction check only. Wall-time uncertainty and A/A control must be considered; no optimization adoption is established.\n\n")
	}
	b.WriteString("Hot paths: current check-phase top ten is missing. Old profile candidates are Handle.Parent, Expression, Name, Text and Store.listOwner; these are stale evidence. Broader checker/map/link work may dominate end-to-end execution; AST edges are the narrower actionable target.\n\nAllocation drivers: AST construction, list materialization and checker type/inference are outside the synthetic interval; their allocation cost is missing. Synthetic interval allocation and GC are required to be zero. Working-set size and LLC residency are missing.\n\nTelemetry: thermal state, pressure, swap, frequency and core placement are missing; absence of contamination is not established. KPC calibration and decision lane are unsupported.\n\nNext action: select representative accesses from a current check profile, then compare a specific layout change and validate representative end-to-end workloads.\n")
	return b.String(), nil
}
