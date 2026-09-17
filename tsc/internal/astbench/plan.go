package astbench

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/astbench/workload"
)

var safeCellID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func defaultPlan(runID, repo, before, after, beforeHash, afterHash string) Plan {
	p := Plan{SchemaVersion: SchemaVersion, RunID: runID, Seed: 1, RepoRoot: repo, Identity: IdentityRef{BeforeRevision: before, AfterRevision: after, SourceHash: afterHash, FixtureHash: beforeHash}, Lanes: map[string]Lane{"daily": {Name: "daily", Kind: "daily", Backend: "wall", Batches: 1, Warmup: 2, Directions: []string{"AB", "AB", "BA", "BA", "AA", "AA", "AA", "AA"}, Repeat: 1}}, Metrics: []string{"ns/op", "B/op", "allocs/op", "ns/visit", "visits", "checksum"}, Timeout: time.Minute, CreatedAt: time.Now().UTC()}
	for _, visitor := range []string{"full-tree", "expression"} {
		for _, size := range []struct {
			name  string
			nodes int
		}{{"small", 256}, {"large", 16384}} {
			p.Cells = append(p.Cells, Cell{ID: visitor + "-" + size.name, Case: visitor, Shape: "mixed", Nodes: size.nodes, Seed: 1, LayoutSeed: 1, Layout: "construction", Representation: "store", GC: "off", P: 1, Backend: "wall", Batch: 256})
		}
	}
	return p
}

func validatePlan(p Plan) error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported plan schema %d", p.SchemaVersion)
	}
	if len(p.Cells) == 0 {
		return errors.New("plan has no cells")
	}
	if len(p.Lanes) != 1 {
		return errors.New("only one daily lane is supported")
	}
	lane, ok := p.Lanes["daily"]
	if !ok || lane.Kind != "daily" || lane.Name != "daily" || lane.Backend != "wall" {
		return errors.New("unsupported: only daily wall lane is calibrated for direction checks")
	}
	if lane.Batches != 1 || lane.Repeat != 1 || lane.Warmup != 2 {
		return errors.New("daily requires batches=1, repeat=1 and warmup=2; traversal count belongs to cell.batch")
	}
	counts := map[string]int{}
	for _, d := range lane.Directions {
		counts[d]++
	}
	if counts["AB"] != 2 || counts["BA"] != 2 || counts["AA"] < 1 || len(counts) != 3 {
		return errors.New("daily requires exactly two AB and two BA pairs plus A/A controls")
	}
	if p.Timeout <= 0 {
		return errors.New("plan requires positive timeout_ns")
	}
	if len(p.Binaries) != 0 || len(p.Exclude) != 0 {
		return errors.New("supplied binaries and ad hoc exclusion rules are unsupported")
	}
	seen := map[string]bool{}
	for _, c := range p.Cells {
		if !safeCellID.MatchString(c.ID) || seen[c.ID] {
			return fmt.Errorf("invalid or duplicate cell ID %q", c.ID)
		}
		seen[c.ID] = true
		if c.Shape == workload.ShapeRepeated {
			g, err := workload.Describe(configFromCell(c, c.Batch).workload())
			if err != nil {
				return err
			}
			if c.Generation == nil || !reflect.DeepEqual(*c.Generation, g) {
				return fmt.Errorf("cell %s: generation metadata mismatch", c.ID)
			}
			if c.Comparison != comparisonKind(c) || c.VisitorStatus == "" {
				return fmt.Errorf("cell %s: comparison and visitor status required", c.ID)
			}
		}
		if c.GC != "off" || c.P != 1 || c.Backend != "wall" {
			return fmt.Errorf("cell %s: synthetic requires GC off, P=1, wall backend", c.ID)
		}
		if c.Batch < 1 || c.Batch > 10000000 {
			return fmt.Errorf("cell %s: batch must be in [1,10000000]", c.ID)
		}
		if len(c.Metadata) > 0 {
			return fmt.Errorf("cell %s: arbitrary metadata is unsupported", c.ID)
		}
		if err := workload.Validate(workload.Config{Subtrees: c.Subtrees, Case: c.Case, Shape: c.Shape, Nodes: c.Nodes, Seed: c.Seed, LayoutSeed: c.LayoutSeed, Layout: c.Layout, Representation: c.Representation}); err != nil {
			return fmt.Errorf("cell %s: %w", c.ID, err)
		}
		if c.AfterRepresentation != "" {
			if err := workload.Validate(workload.Config{Subtrees: c.Subtrees, Case: c.Case, Shape: c.Shape, Nodes: c.Nodes, Seed: c.Seed, LayoutSeed: c.LayoutSeed, Layout: c.Layout, Representation: c.AfterRepresentation}); err != nil {
				return fmt.Errorf("cell %s after: %w", c.ID, err)
			}
		}
	}
	return validateScalingPlan(p)
}

func generatedOrder(p Plan) []Slot {
	rng := rand.New(rand.NewSource(p.Seed))
	cells := append([]Cell(nil), p.Cells...)
	rng.Shuffle(len(cells), func(i, j int) { cells[i], cells[j] = cells[j], cells[i] })
	var slots []Slot
	// Each block contains every cell; AB/BA are balanced within each cell.
	schedules := map[string][]string{}
	for _, c := range cells {
		ds := append([]string(nil), p.Lanes["daily"].Directions...)
		rng.Shuffle(len(ds), func(i, j int) { ds[i], ds[j] = ds[j], ds[i] })
		schedules[c.ID] = ds
	}
	for pair := range p.Lanes["daily"].Directions {
		order := append([]Cell(nil), cells...)
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		for _, c := range order {
			labels := []string{"before", "after"}
			variants := []string{"before", "after"}
			switch schedules[c.ID][pair] {
			case "BA":
				labels = []string{"after", "before"}
				variants = []string{"after", "before"}
			case "AA":
				labels = []string{"control-before", "control-after"}
				variants = []string{"before", "before"}
			}
			for arm := range 2 {
				slots = append(slots, Slot{ID: fmt.Sprintf("daily-%s-%d-%d", c.ID, pair, arm), CellID: c.ID, Lane: "daily", Block: pair, Pair: pair, Label: labels[arm], Variant: variants[arm], Seed: c.Seed})
			}
		}
	}
	return slots
}
func EnsureOrder(p *Plan) error {
	if err := validatePlan(*p); err != nil {
		return err
	}
	if len(p.Order) == 0 {
		p.Order = generatedOrder(*p)
	}
	return validateOrder(*p)
}
func validateOrder(p Plan) error {
	if err := validatePlan(p); err != nil {
		return err
	}
	expected := generatedOrder(p)
	if len(p.Order) != len(expected) {
		return errors.New("saved schedule is incomplete or has extra slots")
	}
	for i, s := range expected {
		if p.Order[i] != s {
			return fmt.Errorf("saved schedule differs from frozen seed at slot %d", i)
		}
	}
	return nil
}
func sortedLaneNames(m map[string]Lane) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
func laneAllowed(p Plan, name string) bool { return name == "" || name == "daily" }
func slotSummary(s Slot) string {
	return strings.Join([]string{s.Lane, s.CellID, s.Label, fmt.Sprint(s.Pair)}, "/")
}
