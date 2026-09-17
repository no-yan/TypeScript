package astbench

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/astbench/workload"
)

type HostInfo struct {
	OS          string            `json:"os"`
	Arch        string            `json:"arch"`
	CPU         string            `json:"cpu"`
	Cache       map[string]string `json:"cache_observations"`
	WorkerCore  *string           `json:"worker_core"`
	Limitations string            `json:"limitations"`
}
type SweepSpec struct {
	StartSubtrees        int      `json:"start_subtrees"`
	MaxCases             int      `json:"max_cases"`
	MemoryBudgetBytes    uint64   `json:"memory_budget_bytes"`
	PlanningBytesPerNode uint64   `json:"planning_bytes_per_node"`
	BudgetMethod         string   `json:"budget_method"`
	StopReason           string   `json:"stop_reason"`
	Host                 HostInfo `json:"host"`
}
type PresetSelection struct {
	Status            string   `json:"status"`
	RuntimeMethod     string   `json:"runtime_method"`
	Version           int      `json:"version"`
	SourceSweepID     string   `json:"source_sweep_id"`
	SourceInputHash   string   `json:"source_input_hash"`
	SmallCell         string   `json:"small_cell"`
	LargeCell         string   `json:"large_cell"`
	Reason            string   `json:"reason"`
	L1DTargetBytes    uint64   `json:"l1d_target_bytes"`
	LargeL1Multiplier float64  `json:"large_l1_multiplier"`
	EstimatedSeconds  float64  `json:"estimated_seconds"`
	Host              HostInfo `json:"host"`
	Limitations       string   `json:"limitations"`
}
type SweepOptions struct {
	RunID, Repo, Visitor           string
	StartSubtrees, MaxCases, Batch int
	MemoryBudgetBytes              uint64
	Timeout                        time.Duration
	Seed                           int64
}

func ReadHostInfo() HostInfo {
	h := HostInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Cache: map[string]string{}, Limitations: "worker core and cache residency unknown; LLC unavailable unless explicitly reported; warm-repeat does not imply residency"}
	if runtime.GOOS == "darwin" {
		for _, key := range []string{"hw.model", "machdep.cpu.brand_string", "hw.memsize", "hw.cachelinesize", "hw.l1dcachesize", "hw.l2cachesize", "hw.l3cachesize", "hw.perflevel0.name", "hw.perflevel0.l1dcachesize", "hw.perflevel0.l2cachesize", "hw.perflevel1.name", "hw.perflevel1.l1dcachesize", "hw.perflevel1.l2cachesize"} {
			b, e := exec.Command("sysctl", "-n", key).Output()
			if e == nil {
				h.Cache[key] = strings.TrimSpace(string(b))
			} else {
				h.Cache[key] = "unavailable: " + e.Error()
			}
		}
		h.CPU = h.Cache["machdep.cpu.brand_string"]
	} else {
		h.CPU = "unavailable: CPU topology collection not implemented for this OS"
	}
	return h
}
func NewSweep(o SweepOptions) (Plan, error) {
	if o.StartSubtrees < 1 || o.MaxCases < 2 || o.MaxCases > 10 || o.Batch < 1 || o.MemoryBudgetBytes == 0 || o.Timeout <= 0 {
		return Plan{}, errors.New("sweep requires positive start, batch, memory budget and timeout; max-cases in [2,10]")
	}
	if o.Visitor != "expression" && o.Visitor != "full-tree" {
		return Plan{}, errors.New("sweep visitor must be expression or full-tree")
	}
	p := defaultPlan(o.RunID, o.Repo, "", "", "", "")
	p.Mode = "sweep"
	p.Seed = o.Seed
	p.Timeout = o.Timeout
	p.Cells = nil
	p.Sweep = &SweepSpec{StartSubtrees: o.StartSubtrees, MaxCases: o.MaxCases, MemoryBudgetBytes: o.MemoryBudgetBytes, PlanningBytesPerNode: 4096, BudgetMethod: "4096 bytes/logical node admission estimate includes representation, temporary graph and trace verification copies; conservative estimate, not a hard RSS cap", StopReason: "maximum cases reached; upper cache region not established", Host: ReadHostInfo()}
	n := o.StartSubtrees
	for i := 0; i < o.MaxCases; i++ {
		if n > 1000000/workload.RepeatedSubtreeNodes {
			p.Sweep.StopReason = "generator node limit reached; upper cache region not established"
			break
		}
		nodes := 1 + n*workload.RepeatedSubtreeNodes
		if uint64(nodes) > o.MemoryBudgetBytes/p.Sweep.PlanningBytesPerNode {
			p.Sweep.StopReason = "planning memory budget reached; upper cache region not established"
			break
		}
		c := Cell{ID: fmt.Sprintf("%s-subtrees-%d", o.Visitor, n), Case: o.Visitor, Shape: workload.ShapeRepeated, Nodes: nodes, Subtrees: n, Seed: o.Seed, LayoutSeed: 1, Layout: "construction", Representation: "pointer", AfterRepresentation: "store", GC: "off", P: 1, Backend: "wall", Batch: o.Batch, Comparison: "real-pointer-vs-real-store", VisitorStatus: "provisional: current real-workload top 10 not established"}
		g, e := workload.Describe(configFromCell(c, c.Batch).workload())
		if e != nil {
			return Plan{}, e
		}
		c.Generation = &g
		p.Cells = append(p.Cells, c)
		n *= 2
	}
	if len(p.Cells) < 2 {
		return Plan{}, errors.New("memory budget permits fewer than two sweep points")
	}
	err := EnsureOrder(&p)
	return p, err
}
func expectedNodes(c Cell) int {
	if c.Generation != nil {
		return c.Generation.ActualNodes
	}
	return c.Nodes
}
func validateScalingPlan(p Plan) error {
	switch p.Mode {
	case "":
		if p.Sweep != nil || p.Preset != nil {
			return errors.New("scaling metadata requires mode")
		}
		return nil
	case "sweep":
		if p.Sweep == nil || p.Sweep.StartSubtrees < 1 || p.Preset != nil || len(p.Cells) < 2 || len(p.Cells) > p.Sweep.MaxCases || p.Sweep.MaxCases > 10 || p.Sweep.MemoryBudgetBytes == 0 || p.Sweep.PlanningBytesPerNode != 4096 {
			return errors.New("invalid sweep bounds")
		}
		for i, c := range p.Cells {
			if c.Shape != workload.ShapeRepeated || c.Subtrees != p.Sweep.StartSubtrees<<i || uint64(expectedNodes(c)) > p.Sweep.MemoryBudgetBytes/p.Sweep.PlanningBytesPerNode {
				return errors.New("sweep violates fixed doubling or memory bounds")
			}
			if i > 0 {
				a := p.Cells[0]
				if c.Case != a.Case || c.Seed != a.Seed || c.Representation != a.Representation || c.AfterRepresentation != a.AfterRepresentation {
					return errors.New("sweep workload contracts differ")
				}
			}
		}
		return nil
	case "daily-selected":
		if p.Preset == nil || p.Sweep != nil || len(p.Cells) != 2 || p.Preset.Version < 1 || p.Preset.SourceSweepID == "" || p.Preset.SourceInputHash == "" || strings.TrimSpace(p.Preset.Reason) == "" {
			return errors.New("daily requires two justified frozen sweep points")
		}
		hash, err := hex.DecodeString(p.Preset.SourceInputHash)
		minL1 := minimumL1D(p.Preset.Host)
		if err != nil || len(hash) != 32 || minL1 == 0 || p.Preset.L1DTargetBytes > minL1 || p.Preset.RuntimeMethod == "" {
			return errors.New("invalid preset source hash, host evidence, or runtime method")
		}
		a, b := p.Cells[0], p.Cells[1]

		if a.ID != p.Preset.SmallCell || b.ID != p.Preset.LargeCell || a.Shape != workload.ShapeRepeated || b.Shape != a.Shape || a.Case != b.Case || a.Seed != b.Seed || a.Layout != b.Layout || a.LayoutSeed != b.LayoutSeed || a.Representation != b.Representation || a.AfterRepresentation != b.AfterRepresentation || expectedNodes(a) >= expectedNodes(b) || p.Preset.L1DTargetBytes == 0 || p.Preset.LargeL1Multiplier != 4 || math.IsNaN(p.Preset.EstimatedSeconds) || math.IsInf(p.Preset.EstimatedSeconds, 0) || p.Preset.EstimatedSeconds <= 0 || p.Preset.EstimatedSeconds > 90 || p.Preset.Status != "provisional: small cache-capacity target unresolved" {
			return errors.New("selected preset contract mismatch")
		}
		return nil
	default:

		return fmt.Errorf("unsupported mode %q", p.Mode)
	}
}

// SelectDaily uses explicitly chosen cells and recorded capacity/time evidence; it never selects on A/B speedup.
func SelectDaily(runDir, small, large, reason string, l1 uint64, version int) (Plan, error) {
	c, e := Collect(runDir)
	if e != nil {
		return Plan{}, e
	}
	if !c.Complete || !c.Valid {
		return Plan{}, errors.New("daily selection requires complete valid sweep including A/A")
	}
	var p Plan
	if e = readJSON(filepath.Join(runDir, "plan.json"), &p); e != nil {
		return p, e
	}
	if p.Mode != "sweep" || p.Sweep == nil {
		return p, errors.New("source is not a size sweep")
	}
	a, ok := findCell(p.Cells, small)
	if !ok {
		return p, errors.New("unknown small cell")
	}
	b, ok := findCell(p.Cells, large)
	if !ok || expectedNodes(a) >= expectedNodes(b) || a.Case != b.Case {
		return p, errors.New("large must be a larger point of same visitor")
	}
	if strings.TrimSpace(reason) == "" || version < 1 || l1 == 0 {
		return p, errors.New("selection requires reason, preset version and explicit L1D target bytes")
	}
	minL1 := minimumL1D(p.Sweep.Host)
	if minL1 == 0 || l1 > minL1 {
		return p, errors.New("L1D target requires host evidence and must not exceed the smallest observed L1D capacity")
	}
	var seconds float64

	found := map[string]map[string]bool{}
	for _, x := range c.Attempts {
		if x.Metadata.CellID != small && x.Metadata.CellID != large {
			continue
		}
		if !x.Result.Complete || !x.Result.Sample.Valid {
			return p, errors.New("selected point has failed attempt; contamination unresolved")
		}
		s := x.Result.Sample
		if s.MeasurementOverheadNS <= 0 || s.MeasurementOverheadMethod == "" {
			return p, errors.New("selection requires measured timer overhead")
		}
		if s.MeasurementOverheadNS/(s.NSPerOp*float64(x.Metadata.Config.Batch)) > 0.01 {
			return p, errors.New("timer overhead exceeds 1% of batch interval; repeat pilot with a larger batch")
		}

		foot := s.Memory.AccessFootprintEstimate.Bytes
		if foot == nil || *foot == 0 || strings.TrimSpace(s.Memory.AccessFootprintEstimate.Method) == "" {
			return p, errors.New("positive access footprint with calculation method required; cannot justify selected sizes")
		}
		if x.Metadata.CellID == small && *foot > l1 {
			return p, errors.New("small exceeds L1D field-footprint target")
		}
		if x.Metadata.CellID == large && float64(*foot) < 4*float64(l1) {
			return p, errors.New("large must exceed L1D field-footprint target by at least 4x")
		}
		seconds += x.Metadata.FinishedAt.Sub(x.Metadata.StartedAt).Seconds()
		if found[x.Metadata.CellID] == nil {
			found[x.Metadata.CellID] = map[string]bool{}
		}
		found[x.Metadata.CellID][x.Metadata.Variant] = true
	}
	for _, cellID := range []string{small, large} {
		var record VerificationRecord
		if err := readJSON(filepath.Join(runDir, "control", "verification-"+cellID+".json"), &record); err != nil {
			return p, err
		}
		if record.ElapsedNS <= 0 {
			return p, errors.New("selection requires recorded verification duration")
		}
		seconds += float64(record.ElapsedNS) / 1e9
	}
	if seconds > 90 {

		return p, fmt.Errorf("selected daily runtime %.2fs exceeds 90s budget", seconds)
	}
	if len(found[small]) != 2 || len(found[large]) != 2 {
		return p, errors.New("missing selected representation evidence")
	}
	hash, e := analysisInputHash(runDir)
	if e != nil {
		return p, e
	}
	sel := &PresetSelection{Status: "provisional: small cache-capacity target unresolved", RuntimeMethod: "sum of selected successful worker and full trace-verification durations; excludes scheduler gaps and final report rendering", Version: version, SourceSweepID: p.RunID, SourceInputHash: hash, SmallCell: small, LargeCell: large, Reason: reason, L1DTargetBytes: l1, LargeL1Multiplier: 4, EstimatedSeconds: seconds, Host: p.Sweep.Host, Limitations: "small field-footprint lower bound cannot establish L1D fit; small target remains unresolved; field-footprint estimates exclude cache-line fetches and do not prove cache residency; worker core unknown; target is supplied explicitly; 4 pairs are directional only"}
	p.Mode = "daily-selected"
	p.Preset = sel
	p.Sweep = nil
	p.RunID = ""
	p.Identity = IdentityRef{}
	p.Cells = []Cell{a, b}
	p.Order = nil
	p.CreatedAt = time.Now().UTC()
	err := EnsureOrder(&p)
	return p, err
}

func comparisonKind(c Cell) string {
	after := c.AfterRepresentation
	if after == "" {
		after = c.Representation
	}
	if c.Representation == "pointer" && after == "store" {
		return "real-pointer-vs-real-store"
	}
	if c.Representation == "store" && after == "pointer" {
		return "real-store-vs-real-pointer"
	}
	if c.Representation == "store" {
		return "real-store-before-after"
	}
	return "real-pointer-before-after"
}

func minimumL1D(h HostInfo) uint64 {
	var min uint64
	for key, value := range h.Cache {
		if strings.HasSuffix(key, "l1dcachesize") {
			n, e := strconv.ParseUint(value, 10, 64)
			if e == nil && n > 0 && (min == 0 || n < min) {
				min = n
			}
		}
	}
	return min
}

// A saved selection belongs to its observed CPU/cache configuration. Layout
// changes preserve node counts; a changed host requires a newly versioned selection.
func validatePresetHost(p Plan, actual HostInfo) error {
	if p.Mode != "daily-selected" {
		return nil
	}
	saved := p.Preset.Host
	if actual.OS != saved.OS || actual.Arch != saved.Arch || actual.CPU != saved.CPU || minimumL1D(actual) == 0 {
		return errors.New("preset host changed or unavailable; select a new version from a sweep on this host")
	}
	for key, want := range saved.Cache {
		if strings.HasPrefix(want, "unavailable:") {
			continue
		}
		if actual.Cache[key] != want {
			return fmt.Errorf("preset host observation %s changed or unavailable; select a new preset version", key)
		}
	}
	return nil
}
