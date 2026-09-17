package astbench

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type pairEstimate struct {
	Cell             string  `json:"cell"`
	Pairs            int     `json:"pairs"`
	AfterBeforeRatio float64 `json:"after_before_ratio"`
	Low              float64 `json:"bootstrap_low"`
	High             float64 `json:"bootstrap_high"`
	Status           string  `json:"status"`
}

// WriteAnalysis materializes derived files only after validation. Collect and
// Report remain read-only; neither can run a measurement or silently repair it.
func WriteAnalysis(runDir string) error {
	return WriteAnalysisTo(runDir, filepath.Join(runDir, "analysis"))
}

func WriteAnalysisTo(runDir, outDir string) error {
	absRun, err := filepath.Abs(runDir)
	if err != nil {
		return err
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRun, absOut)
	if err != nil {
		return err
	}
	if rel != "analysis" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("analysis output must be the analysis directory or outside the run")
	}
	c, err := Collect(runDir)
	if err != nil {
		return err
	}
	if !c.Complete || !c.Valid {
		return fmt.Errorf("comparison unavailable: %s", c.Reason)
	}
	inputHash, err := analysisInputHash(runDir)
	if err != nil {
		return err
	}
	var p Plan
	if err := readJSON(filepath.Join(runDir, "plan.json"), &p); err != nil {
		return err
	}
	bySlot := map[string]Collected{}
	for _, a := range c.Attempts {
		if a.Result.Complete && a.Result.Sample.Valid {
			bySlot[a.Metadata.SlotID] = a
		}
	}
	raw := map[string]*bytes.Buffer{"before": {}, "after": {}, "control-before": {}, "control-after": {}}
	type pair struct{ before, after float64 }
	pairs := map[string]map[string]*pair{}
	for _, slot := range p.Order {
		a := bySlot[slot.ID]
		cell, ok := findCell(p.Cells, slot.CellID)
		if !ok {
			return errors.New("unknown cell")
		}
		label := slot.Label
		if label == "A" || label == "A1" {
			label = "control-before"
		}
		if label == "A2" {
			label = "control-after"
		}
		if _, ok := raw[label]; !ok {
			return fmt.Errorf("unsupported analysis label %q", slot.Label)
		}
		s := a.Result.Sample
		if cell.Batch < 1 {
			return errors.New("analysis requires explicit fixed batch")
		}
		fmt.Fprintf(raw[label], "BenchmarkTraversal/%s\t%d\t%.9f ns/op\t%.9f B/op\t%.9f allocs/op\t%.9f ns/node\n", cell.ID, cell.Batch, s.NSPerOp, s.BytesPerOp, s.AllocsPerOp, s.NSPerOp/float64(s.Visits))
		if label == "before" || label == "after" {
			if pairs[cell.ID] == nil {
				pairs[cell.ID] = map[string]*pair{}
			}
			key := fmt.Sprintf("%s/%d/%d", slot.Lane, slot.Block, slot.Pair)
			if pairs[cell.ID][key] == nil {
				pairs[cell.ID][key] = &pair{}
			}
			q := pairs[cell.ID][key]
			if label == "before" {
				q.before = s.NSPerOp
			} else {
				q.after = s.NSPerOp
			}
		}
	}
	for _, label := range []string{"before", "after", "control-before", "control-after"} {
		if raw[label].Len() == 0 {
			return fmt.Errorf("missing %s samples", label)
		}
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(outDir, "benchstat.txt")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "input.sha256"), []byte(inputHash+"\n"), 0644); err != nil {
		return err
	}
	for label, data := range raw {
		if err := os.WriteFile(filepath.Join(outDir, label+".txt"), data.Bytes(), 0644); err != nil {
			return err
		}
	}
	var estimates []pairEstimate
	for cell, blocks := range pairs {
		var ratios []float64
		keys := make([]string, 0, len(blocks))
		for k := range blocks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			v := blocks[key]
			if v.before <= 0 || v.after <= 0 {
				return fmt.Errorf("missing pair arm: %s", key)
			}
			ratios = append(ratios, math.Log(v.after/v.before))
		}
		point, lo, hi := bootstrapLogRatios(ratios, p.Seed)
		estimates = append(estimates, pairEstimate{Cell: cell, Pairs: len(ratios), AfterBeforeRatio: point, Low: lo, High: hi, Status: "unresolved: daily only; A/A tolerances not calibrated"})
	}
	sort.Slice(estimates, func(i, j int) bool { return estimates[i].Cell < estimates[j].Cell })
	if err := writeJSON(filepath.Join(outDir, "paired.json"), estimates); err != nil {
		return err
	}
	tool, err := exec.LookPath("benchstat")
	if err != nil {
		return os.WriteFile(filepath.Join(outDir, "benchstat.txt"), []byte("missing: benchstat unavailable; raw files preserved; comparison not performed\n"), 0644)
	}
	hash, err := hashFile(tool)
	if err != nil {
		return err
	}
	info, _ := exec.Command("go", "version", "-m", tool).CombinedOutput()
	if err := writeJSON(filepath.Join(outDir, "benchstat-tool.json"), struct{ Path, SHA256, BuildInfo string }{tool, hash, string(info)}); err != nil {
		return err
	}
	var output strings.Builder
	for _, labels := range [][2]string{{"control-before", "control-after"}, {"before", "after"}} {
		cmd := exec.Command(tool, filepath.Join(outDir, labels[0]+".txt"), filepath.Join(outDir, labels[1]+".txt"))
		data, err := cmd.CombinedOutput()
		fmt.Fprintf(&output, "%s vs %s\n%s\n", labels[0], labels[1], data)
		if err != nil {
			output.WriteString("benchstat failed: " + err.Error() + "\n")
			_ = os.WriteFile(filepath.Join(outDir, "benchstat.txt"), []byte(output.String()), 0644)
			return err
		}
	}
	finalHash, err := analysisInputHash(runDir)
	if err != nil {
		return err
	}
	if finalHash != inputHash {
		return errors.New("analysis inputs changed during collection")
	}
	return os.WriteFile(filepath.Join(outDir, "benchstat.txt"), []byte(output.String()), 0644)
}

func analysisInputHash(runDir string) (string, error) {
	var b strings.Builder
	for _, name := range []string{"plan.json", "identity.json"} {
		h, err := hashFile(filepath.Join(runDir, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintln(&b, name, h)
	}
	for _, name := range []string{"attempts", "control"} {
		path := filepath.Join(runDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Fprintln(&b, name, "missing")
			continue
		}
		h, err := hashTree(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintln(&b, name, h)
	}
	return hashReader(strings.NewReader(b.String()))
}

func bootstrapLogRatios(values []float64, seed int64) (float64, float64, float64) {
	mean := func(xs []float64) float64 {
		var s float64
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs))
	}
	if len(values) == 0 {
		return 0, 0, 0
	}
	r := rand.New(rand.NewSource(seed))
	samples := make([]float64, 10000)
	for i := range samples {
		var s float64
		for range values {
			s += values[r.Intn(len(values))]
		}
		samples[i] = math.Exp(s / float64(len(values)))
	}
	sort.Float64s(samples)
	return math.Exp(mean(values)), samples[249], samples[9749]
}
