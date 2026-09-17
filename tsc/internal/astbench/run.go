package astbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type RunOptions struct {
	RunDir string
	Lane   string
	Self   string
}

func Run(ctx context.Context, opts RunOptions) error {
	if opts.RunDir == "" {
		return errors.New("run requires --run")
	}
	var plan Plan
	if err := readJSON(filepath.Join(opts.RunDir, "plan.json"), &plan); err != nil {
		return err
	}
	var identity Identity
	if err := readJSON(filepath.Join(opts.RunDir, "identity.json"), &identity); err != nil {
		return err
	}
	if plan.RepoRoot != identity.RepoRoot || plan.Identity.BeforeRevision != identity.BeforeRevision || plan.Identity.AfterRevision != identity.AfterRevision || plan.Identity.SourceHash != identity.SourceHash || plan.Identity.FixtureHash != identity.FixtureHash {
		return errors.New("plan/identity mismatch")
	}
	for name, want := range map[string]string{"before": identity.FixtureHash, "after": identity.SourceHash} {
		if want == "" {
			continue
		}
		got, err := hashTree(filepath.Join(opts.RunDir, "snapshots", name))
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", name, err)
		}
		if got != want {
			return fmt.Errorf("snapshot %s hash mismatch: want %s got %s", name, want, got)
		}
	}
	if err := EnsureOrder(&plan); err != nil {
		return err
	}
	verifiedCells := map[string]bool{}
	if opts.Lane != "" && !laneAllowed(plan, opts.Lane) {
		return fmt.Errorf("unknown lane %q", opts.Lane)
	}
	if opts.Lane != "" {
		lane := plan.Lanes[opts.Lane]
		if lane.Kind == "decision" || lane.Backend == "kpc" {
			return fmt.Errorf("unsupported: decision/KPC lane is not calibrated")
		}
	}
	lockCtx := ctx
	if plan.Timeout > 0 {
		var cancel context.CancelFunc
		lockCtx, cancel = context.WithTimeout(ctx, plan.Timeout)
		defer cancel()
	}
	lock, err := acquireLock(lockCtx, filepath.Join(os.TempDir(), "astbench-campaign.lock"))
	if err != nil {
		return err
	}
	defer lock.release()
	if err := writeJSON(filepath.Join(opts.RunDir, "plan.json"), plan); err != nil {
		return err
	}
	completed, err := completedSlots(opts.RunDir)
	if err != nil {
		return err
	}
	for _, slot := range plan.Order {
		if err := ctx.Err(); err != nil {
			return err
		}
		if opts.Lane != "" && slot.Lane != opts.Lane {
			continue
		}
		laneCfg := plan.Lanes[slot.Lane]
		if laneCfg.Kind == "decision" || laneCfg.Backend == "kpc" {
			return fmt.Errorf("unsupported: decision/KPC lane is not calibrated")
		}
		if !verifiedCells[slot.CellID] {
			cell, ok := findCell(plan.Cells, slot.CellID)
			if !ok {
				return fmt.Errorf("slot %q references cell %q", slot.ID, slot.CellID)
			}
			if err := verifyCellTrace(ctx, opts.RunDir, identity, cell); err != nil {
				return fmt.Errorf("trace gate for cell %s: %w", cell.ID, err)
			}
			verifiedCells[slot.CellID] = true
		}
		if completed[slot.ID] {
			continue
		}
		if err := runSlot(ctx, opts.RunDir, plan, identity, slot); err != nil {
			return err
		}
	}
	if opts.Lane == "" {
		if err := WriteAnalysis(opts.RunDir); err != nil {
			return err
		}
	} else if done, err := completedSlots(opts.RunDir); err != nil {
		return err
	} else if len(done) == len(plan.Order) {
		if err := WriteAnalysis(opts.RunDir); err != nil {
			return err
		}
	}
	return nil
}

func completedSlots(runDir string) (map[string]bool, error) {
	result := map[string]bool{}
	var plan Plan
	var identity Identity
	if err := readJSON(filepath.Join(runDir, "plan.json"), &plan); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(runDir, "identity.json"), &identity); err != nil {
		return nil, err
	}
	slots := map[string]Slot{}
	for _, slot := range plan.Order {
		slots[slot.ID] = slot
	}
	entries, err := os.ReadDir(filepath.Join(runDir, "attempts"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var m AttemptMetadata
		var r AttemptResult
		if readJSON(filepath.Join(runDir, "attempts", entry.Name(), "metadata.json"), &m) != nil || readJSON(filepath.Join(runDir, "attempts", entry.Name(), "metrics.json"), &r) != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(runDir, "attempts", entry.Name(), "bench.txt")); err != nil {
			continue
		}
		slot, ok := slots[m.SlotID]
		if !ok {
			continue
		}
		cell, ok := findCell(plan.Cells, slot.CellID)
		if !ok {
			continue
		}
		if validateAttemptBinding(m, slot, cell, identity) != nil {
			continue
		}
		if r.Complete && validateSample(r.Sample) == nil && m.SlotID != "" && !m.FinishedAt.IsZero() && !m.Timeout && !m.Cancelled && m.ExitCode == 0 && r.Error == "" && r.Sample.GCCycles == 0 && r.Sample.Allocations == 0 && r.Sample.AllocatedBytes == 0 {
			result[m.SlotID] = true
		}
	}
	return result, nil
}

func runSlot(ctx context.Context, runDir string, plan Plan, identity Identity, slot Slot) error {
	cell, ok := findCell(plan.Cells, slot.CellID)
	if !ok {
		return fmt.Errorf("slot %q references cell %q", slot.ID, slot.CellID)
	}
	batch := cell.Batch
	if batch < 1 {
		batch = plan.Lanes[slot.Lane].Batches
		if batch < 1 {
			batch = 1
		}
	}
	config := configFromCell(cell, batch)
	if slot.Variant == "after" && cell.AfterRepresentation != "" {
		config.Representation = cell.AfterRepresentation
	}
	// A/A runs deliberately use the same worker and config. A/B representation
	// differences are carried by the cell plan and binary snapshot metadata.
	if slot.Label == "A" && config.Representation == "" {
		config.Representation = "store"
	}
	if slot.Label == "before" && config.Representation == "" {
		config.Representation = "store"
	}
	if slot.Label == "after" && config.Representation == "" {
		config.Representation = "store"
	}
	configBytes, err := json.Marshal(config)
	if err != nil {
		return err
	}
	attemptID := nextAttemptID(runDir)
	dir := filepath.Join(runDir, "attempts", fmt.Sprintf("%06d", attemptID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	now := time.Now().UTC()
	variant := slot.Variant
	if variant == "" {
		variant = slot.Label
	}
	if variant == "A" {
		if _, ok := identity.Binary["A"]; !ok {
			variant = "before"
		}
	}
	meta := AttemptMetadata{SchemaVersion: SchemaVersion, AttemptID: fmt.Sprintf("%06d", attemptID), SlotID: slot.ID, CellID: slot.CellID, Lane: slot.Lane, Pair: slot.Pair, Block: slot.Block, Label: slot.Label, Variant: slot.Variant, Binary: variant, StartedAt: now}
	meta.Config = config
	worker, err := binaryPath(identity, variant)
	if err != nil {
		return err
	}
	meta.Argv = []string{worker, "sample", "--config", string(configBytes)}
	meta.Env = []string{"GOMAXPROCS=1", "GOGC=off", "GOMEMLIMIT=off"}
	if err := writeJSON(filepath.Join(dir, "metadata.json"), meta); err != nil {
		return err
	}
	stdoutFile, err := os.OpenFile(filepath.Join(dir, "stdout.txt"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer stdoutFile.Close()
	stderrFile, err := os.OpenFile(filepath.Join(dir, "stderr.txt"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer stderrFile.Close()
	deadline := plan.Timeout
	if deadline <= 0 {
		deadline = 5 * time.Minute
	}
	slotCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	cmd := exec.CommandContext(slotCtx, worker, "sample", "--config", string(configBytes))
	cmd.Stdout, cmd.Stderr = stdoutFile, stderrFile
	cmd.Dir = runDir
	cmd.Env = append(os.Environ(), "GOMAXPROCS=1", "GOGC=off", "GOMEMLIMIT=off")
	err = cmd.Run()
	_ = stdoutFile.Sync()
	_ = stderrFile.Sync()
	stdout, readErr := os.ReadFile(filepath.Join(dir, "stdout.txt"))
	if readErr != nil {
		return readErr
	}
	var sample Sample
	var sampleParseErr error
	if len(bytes.TrimSpace(stdout)) != 0 {
		sampleParseErr = json.Unmarshal(stdout, &sample)
	} else {
		sampleParseErr = errors.New("worker produced no JSON sample")
	}
	if slotCtx.Err() == context.DeadlineExceeded {
		meta.Timeout = true
	}
	if ctx.Err() != nil {
		meta.Cancelled = true
	}
	if sampleParseErr != nil {
		sample.Reason = sampleParseErr.Error()
	}
	if err != nil && sample.Reason == "" {
		sample.Reason = err.Error()
	}
	if err == nil && !sample.Valid && sample.Reason == "" {
		sample.Reason = "worker returned invalid sample"
	}
	if sampleParseErr == nil && err == nil {
		if validationErr := validateSample(sample); validationErr != nil {
			sample.Valid = false
			sample.Reason = validationErr.Error()
		}
	}
	if sample.Valid && sampleParseErr == nil && err == nil {
		var proof VerificationRecord
		validationErr := readJSON(filepath.Join(runDir, "control", "verification-"+cell.ID+".json"), &proof)
		if validationErr == nil {
			validationErr = validateMeasuredWork(sample, cell, proof)
		}
		if validationErr != nil {
			sample.Valid = false
			sample.Reason = validationErr.Error()
		}
	}
	result := AttemptResult{SchemaVersion: SchemaVersion, Sample: sample, Complete: err == nil && sampleParseErr == nil && sample.Valid}
	if err != nil {
		result.Error = err.Error()
	}
	meta.FinishedAt = time.Now().UTC()
	if exitErr, ok := err.(*exec.ExitError); ok {
		meta.ExitCode = exitErr.ExitCode()
	}
	if writeErr := writeJSON(filepath.Join(dir, "metrics.json"), result); writeErr != nil {
		return writeErr
	}
	if !result.Complete {
		if writeErr := writeJSON(filepath.Join(dir, "metadata.json"), meta); writeErr != nil {
			return writeErr
		}
		if result.Error != "" {
			return fmt.Errorf("slot %s failed: %s", slot.ID, result.Error)
		}
		return fmt.Errorf("slot %s produced invalid sample: %s", slot.ID, sample.Reason)
	}
	bench := fmt.Sprintf("BenchmarkTraversal/%s\t%d\t%.9f ns/op\t%.9f B/op\t%.9f allocs/op\t%.9f ns/visit\n", cell.ID, batch, sample.NSPerOp, sample.BytesPerOp, sample.AllocsPerOp, sample.NSPerOp/float64(sample.Visits))
	if err := os.WriteFile(filepath.Join(dir, "bench.txt"), []byte(bench), 0o644); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "metadata.json"), meta); err != nil {
		return err
	}
	return nil
}

func nextAttemptID(runDir string) int {
	entries, _ := os.ReadDir(filepath.Join(runDir, "attempts"))
	max := 0
	for _, e := range entries {
		n, err := strconv.Atoi(e.Name())
		if err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

func findCell(cells []Cell, id string) (Cell, bool) {
	for _, c := range cells {
		if c.ID == id {
			return c, true
		}
	}
	return Cell{}, false
}

func binaryPath(identity Identity, variant string) (string, error) {
	if variant == "A" {
		variant = "before"
	}
	b, ok := identity.Binary[variant]
	if !ok || b.Snapshot == "" {
		return "", fmt.Errorf("missing required binary snapshot for variant %q", variant)
	}
	h, err := hashFile(b.Snapshot)
	if err != nil {
		return "", err
	}
	if b.SHA256 == "" || h != b.SHA256 {
		return "", fmt.Errorf("binary %q hash changed: want %s got %s", variant, b.SHA256, h)
	}
	return b.Snapshot, nil
}

func verifyCellTrace(parent context.Context, runDir string, identity Identity, c Cell) error {
	started := time.Now()
	configBytes, err := json.Marshal(configFromCell(c, 1))
	if err != nil {
		return err
	}
	workers := []string{"before", "after"}
	verified := map[string]TraceVerification{}
	for _, variant := range workers {
		worker, err := binaryPath(identity, variant)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
		cmd := exec.CommandContext(ctx, worker, "verify", "--config", string(configBytes))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, commandErr := cmd.Output()
		cancel()
		if commandErr != nil {
			return fmt.Errorf("verify %s: %w: %s", variant, commandErr, stderr.String())
		}
		var result TraceVerification
		if err := json.Unmarshal(output, &result); err != nil {
			return fmt.Errorf("verify %s output: %w", variant, err)
		}
		if !result.Valid {
			return fmt.Errorf("verify %s: %s", variant, result.Reason)
		}
		verified[variant] = result
	}
	a, _ := json.Marshal(verified["before"].Store)
	b, _ := json.Marshal(verified["after"].Store)
	if !bytes.Equal(a, b) {
		return errors.New("cross-binary store trace mismatch")
	}
	return writeJSON(filepath.Join(runDir, "control", "verification-"+c.ID+".json"), VerificationRecord{ElapsedNS: time.Since(started).Nanoseconds(), CellID: c.ID, Status: "verified", Config: configFromCell(c, 1), BeforeSHA256: identity.Binary["before"].SHA256, AfterSHA256: identity.Binary["after"].SHA256, Before: verified["before"], After: verified["after"]})
}
