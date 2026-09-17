package astbench

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/astbench/workload"
)

func ratioStatus(lo, hi float64) string {
	if lo <= 1 && hi >= 1 {
		return "noisy: daily direction check; uncertainty includes no difference"
	}
	return "directional: daily only; not a confirmed improvement or calibrated A/A decision"
}

func writeTSV(path string, rows [][]string) error {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = '\t'
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0644)
}
func number(n float64) string { return strconv.FormatFloat(n, 'g', -1, 64) }
func integer(n uint64) string { return strconv.FormatUint(n, 10) }

// Every row is one process sample, including rejected attempts. Internal batch
// repetitions never become independent observations.
func writeScalingAnalysis(out string, p Plan, c Collection, slots map[string]Collected, estimates []pairEstimate) error {
	order := make(map[string]int, len(p.Order))
	for i, s := range p.Order {
		order[s.ID] = i + 1
	}
	attempts := append([]Collected(nil), c.Attempts...)
	sort.SliceStable(attempts, func(i, j int) bool {
		a, b := attempts[i].Metadata, attempts[j].Metadata
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.Before(b.StartedAt)
		}
		return a.AttemptID < b.AttemptID
	})
	rows := [][]string{{"attempt_order", "planned_slot_order", "attempt", "slot", "cell", "label", "representation", "visitor", "logical_nodes", "visits_per_op", "ns_per_op", "bytes_per_op", "allocs_per_op", "ns_per_visit", "batch", "started_at", "complete", "valid", "reason"}}
	mem := [][]string{{"attempt", "cell", "label", "logical_nodes", "metric", "bytes", "method", "reason"}}
	for i, a := range attempts {
		m, s := a.Metadata, a.Result.Sample
		valid := a.Result.Complete && s.Valid && a.Result.Error == "" && !m.Timeout && !m.Cancelled && m.ExitCode == 0 && !m.FinishedAt.IsZero() && validateSample(s) == nil
		normalized := "null"
		if valid && s.Visits > 0 {
			normalized = number(s.NSPerOp / float64(s.Visits))
		}
		reason := s.Reason
		if a.Result.Error != "" {
			reason += " " + a.Result.Error
		}
		if !valid && reason == "" {
			reason = "incomplete, failed, cancelled, timed out, or invalid sample"
		}
		cell, _ := findCell(p.Cells, m.CellID)
		representation := cell.Representation
		if m.Variant == "after" && cell.AfterRepresentation != "" {
			representation = cell.AfterRepresentation
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), strconv.Itoa(order[m.SlotID]), m.AttemptID, m.SlotID, m.CellID, m.Label, representation, cell.Case, integer(s.LogicalNodes), integer(s.Visits), number(s.NSPerOp), number(s.BytesPerOp), number(s.AllocsPerOp), normalized, strconv.Itoa(cell.Batch), m.StartedAt.Format(time.RFC3339Nano), strconv.FormatBool(a.Result.Complete), strconv.FormatBool(valid), reason})
		metrics := []struct {
			name  string
			value workload.ByteEstimate
		}{
			{"representation_used_bytes", s.Memory.RepresentationUsedBytes},
			{"representation_capacity_bytes", s.Memory.RepresentationCapacityBytes},
			{"reachable_heap_bytes_estimate", s.Memory.ReachableHeapBytesEstimate},
			{"process_peak_rss", s.Memory.ProcessPeakRSS},
			{"access_footprint_estimate", s.Memory.AccessFootprintEstimate},
		}
		for _, metric := range metrics {
			value, reason := "null", metric.value.Reason
			if metric.value.Bytes != nil {
				value = integer(*metric.value.Bytes)
			} else if reason == "" {
				reason = "missing: metric unavailable in saved sample"
			}
			mem = append(mem, []string{m.AttemptID, m.CellID, m.Label, integer(s.LogicalNodes), metric.name, value, metric.value.Method, reason})
		}
	}
	if err := writeTSV(filepath.Join(out, "samples.tsv"), rows); err != nil {
		return err
	}
	if err := writeTSV(filepath.Join(out, "memory.tsv"), mem); err != nil {
		return err
	}
	summary := [][]string{{"cell", "visitor", "logical_nodes", "visits_per_op", "before_geomean_ns_per_visit", "before_low", "before_high", "after_geomean_ns_per_visit", "after_low", "after_high", "after_before_ratio", "ratio_low", "ratio_high", "pairs", "status", "comparison", "before_representation", "after_representation", "visitor_status"}}
	estimateMap := map[string]pairEstimate{}
	for _, e := range estimates {
		estimateMap[e.Cell] = e
	}
	var points []scalingPoint
	for _, cell := range p.Cells {
		var a, b []float64
		var nodes, visits uint64
		for _, slot := range p.Order {
			if slot.CellID != cell.ID || (slot.Label != "before" && slot.Label != "after") {
				continue
			}
			sample := slots[slot.ID].Result.Sample
			nodes, visits = sample.LogicalNodes, sample.Visits
			if sample.Visits == 0 {
				return fmt.Errorf("zero visits in cell %s", cell.ID)
			}
			value := sample.NSPerOp / float64(sample.Visits)
			if slot.Label == "before" {
				a = append(a, value)
			} else {
				b = append(b, value)
			}
		}
		ap, al, ah := bootstrapMean(a, p.Seed)
		bp, bl, bh := bootstrapMean(b, p.Seed)
		e := estimateMap[cell.ID]
		summary = append(summary, []string{cell.ID, cell.Case, integer(nodes), integer(visits), number(ap), number(al), number(ah), number(bp), number(bl), number(bh), number(e.AfterBeforeRatio), number(e.Low), number(e.High), strconv.Itoa(e.Pairs), e.Status, cell.Comparison, cell.Representation, afterRepresentation(cell), cell.VisitorStatus})
		points = append(points, scalingPoint{cell.ID, cell.Case + " / " + cell.Comparison, nodes, a, b, ap, al, ah, bp, bl, bh})
	}
	if err := writeTSV(filepath.Join(out, "scaling.tsv"), summary); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "scaling.svg"), scalingSVG(points), 0644)
}

// Deterministic nonparametric process-sample bootstrap. These small-sample
// intervals visualize uncertainty; they do not promote daily results to a
// decision lane or establish a calibrated false-positive rate.
func afterRepresentation(cell Cell) string {
	if cell.AfterRepresentation != "" {
		return cell.AfterRepresentation
	}
	return cell.Representation
}

func bootstrapMean(values []float64, seed int64) (float64, float64, float64) {
	// Reuse the log-ratio resampler with log(value): the plotted point and
	// interval are geometric means, matching paired multiplicative comparisons.
	logs := make([]float64, len(values))
	for i, v := range values {
		logs[i] = math.Log(v)
	}
	return bootstrapLogRatios(logs, seed)
}

type scalingPoint struct {
	cell, visitor          string
	nodes                  uint64
	before, after          []float64
	ap, al, ah, bp, bl, bh float64
}

func scalingSVG(points []scalingPoint) []byte {
	visitors := map[string][]scalingPoint{}
	var names []string
	for _, p := range points {
		if _, ok := visitors[p.visitor]; !ok {
			names = append(names, p.visitor)
		}
		visitors[p.visitor] = append(visitors[p.visitor], p)
	}
	sort.Strings(names)
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="960" height="%d" viewBox="0 0 960 %d"><rect width="100%%" height="100%%" fill="white"/><g font-family="sans-serif" font-size="12" fill="#222">`, 120+360*len(names), 120+360*len(names))
	b.WriteString(`<text x="70" y="24" font-size="17">AST traversal size scaling: daily directional/noisy evidence</text><text x="70" y="46">Dots: process samples; bars: 95% bootstrap geometric-mean intervals (small-sample uncertainty).</text><text x="70" y="66">Blue: before (A); orange: after (B). Main visitor provisional. Warm-repeat; cache causes unconfirmed.</text>`)
	for panel, name := range names {
		ps := visitors[name]
		sort.Slice(ps, func(i, j int) bool { return ps[i].nodes < ps[j].nodes })
		minX, maxX, maxY := math.Inf(1), math.Inf(-1), 0.0
		for _, p := range ps {
			x := math.Log2(float64(p.nodes))
			minX = math.Min(minX, x)
			maxX = math.Max(maxX, x)
			for _, v := range append(append([]float64{}, p.before...), p.after...) {
				maxY = math.Max(maxY, v)
			}
		}
		if minX == maxX {
			minX -= 0.5
			maxX += 0.5
		}
		if maxY <= 0 {
			maxY = 1
		}
		maxY *= 1.15
		top := float64(115 + 360*panel)
		xcoord := func(n uint64) float64 { return 90 + 760*(math.Log2(float64(n))-minX)/(maxX-minX) }
		ycoord := func(v float64) float64 { return top + 240 - 230*v/maxY }
		fmt.Fprintf(&b, `<text x="90" y="%.1f" font-size="15">%s</text><path d="M90 %.1f V%.1f H870" stroke="#333" fill="none"/><text x="370" y="%.1f">Actual logical nodes (log2 axis)</text><text x="12" y="%.1f">ns/visit</text>`, top-12, html.EscapeString(name), top, top+240, top+290, top)
		// Write title separately to keep all user-derived text XML escaped.
		for tick := 0; tick <= 4; tick++ {
			v := maxY * float64(tick) / 4
			y := ycoord(v)
			fmt.Fprintf(&b, `<path d="M90 %.1f H870" stroke="#ddd"/><text x="30" y="%.1f">%.3g</text>`, y, y+4, v)
		}
		seen := map[uint64]bool{}
		for _, p := range ps {
			x := xcoord(p.nodes)
			if !seen[p.nodes] {
				fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle">%d</text>`, x, top+260, p.nodes)
				seen[p.nodes] = true
			}
		}
		for arm, color := range []string{"#1565c0", "#d95f02"} {
			var path bytes.Buffer
			for i, p := range ps {
				mean, lo, hi, values := p.ap, p.al, p.ah, p.before
				if arm == 1 {
					mean, lo, hi, values = p.bp, p.bl, p.bh, p.after
				}
				x := xcoord(p.nodes)
				cmd := "L"
				if i == 0 {
					cmd = "M"
				}
				fmt.Fprintf(&path, "%s%.2f %.2f ", cmd, x, ycoord(mean))
				fmt.Fprintf(&b, `<path d="M%.2f %.2f V%.2f M%.2f %.2f H%.2f M%.2f %.2f H%.2f" stroke="%s" fill="none"/>`, x, ycoord(lo), ycoord(hi), x-4, ycoord(lo), x+4, x-4, ycoord(hi), x+4, color)
				for _, v := range values {
					fmt.Fprintf(&b, `<circle cx="%.2f" cy="%.2f" r="3" fill="%s" opacity="0.65"><title>%s: %.6g ns/visit</title></circle>`, x, ycoord(v), color, html.EscapeString(p.cell), v)
				}
			}
			fmt.Fprintf(&b, `<path d="%s" stroke="%s" fill="none" stroke-width="2"/>`, path.String(), color)
		}
	}
	b.WriteString(`</g></svg>`)
	return b.Bytes()
}
