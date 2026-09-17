package astbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astbench/workload"
)

type SampleConfig struct {
	Subtrees       int    `json:"subtrees,omitempty"`
	Case           string `json:"case"`
	Shape          string `json:"shape"`
	Nodes          int    `json:"nodes"`
	Seed           int64  `json:"seed"`
	LayoutSeed     int64  `json:"layout_seed"`
	Layout         string `json:"layout"`
	Representation string `json:"representation"`
	Batch          int    `json:"batch"`
}

type TraceVerification struct {
	CellID  string                `json:"cell_id,omitempty"`
	Valid   bool                  `json:"valid"`
	Reason  string                `json:"reason,omitempty"`
	Entries int                   `json:"entries,omitempty"`
	Store   []workload.TraceEntry `json:"store_trace,omitempty"`
	Pointer []workload.TraceEntry `json:"pointer_trace,omitempty"`
}

type VerificationRecord struct {
	ElapsedNS    int64             `json:"elapsed_ns"`
	CellID       string            `json:"cell_id"`
	Status       string            `json:"status"`
	Config       SampleConfig      `json:"config"`
	BeforeSHA256 string            `json:"before_sha256"`
	AfterSHA256  string            `json:"after_sha256"`
	Before       TraceVerification `json:"before"`
	After        TraceVerification `json:"after"`
}

func traceWork(trace []workload.TraceEntry) Sample {
	s := Sample{Visits: uint64(len(trace))}
	for _, e := range trace {
		s.EdgeReads += uint64(e.Children)
		s.AttributeReads += 4
		if e.Kind == ast.KindNumericLiteral || e.Kind == ast.KindIdentifier {
			s.AttributeReads++
		}
		v := uint64(e.Kind) + uint64(e.Flags) + uint64(uint32(e.Pos)) + uint64(uint32(e.End)) + uint64(len(e.Text))
		if len(e.Text) > 0 {
			v += uint64(e.Text[0]) + uint64(e.Text[len(e.Text)-1])
		}
		s.Checksum += v
	}
	return s
}

func configFromCell(c Cell, batch int) SampleConfig {
	return SampleConfig{Subtrees: c.Subtrees, Case: c.Case, Shape: c.Shape, Nodes: c.Nodes, Seed: c.Seed, LayoutSeed: c.LayoutSeed, Layout: c.Layout, Representation: c.Representation, Batch: batch}
}

func (c SampleConfig) workload() workload.Config {
	d := workload.DefaultConfig()
	if c.Case != "" {
		d.Case = c.Case
	}
	if c.Shape != "" {
		d.Shape = c.Shape
	}
	if c.Nodes != 0 || c.Shape == "fixture" {
		d.Nodes = c.Nodes
	}
	d.Subtrees = c.Subtrees
	d.Seed = c.Seed
	d.LayoutSeed = c.LayoutSeed
	if c.Layout != "" {
		d.Layout = c.Layout
	}
	if c.Representation != "" {
		d.Representation = c.Representation
	}
	return d
}

func convertSample(s workload.Sample) Sample {
	return Sample{MeasurementOverheadNS: s.MeasurementOverheadNS, MeasurementOverheadMethod: s.MeasurementOverheadMethod, Generation: s.Generation, Memory: s.Memory, NSPerOp: s.NsPerOp, BytesPerOp: s.BytesPerOp, AllocsPerOp: s.AllocsPerOp, Visits: s.Visits, Checksum: s.Checksum, LogicalNodes: s.LogicalNodes, EdgeReads: s.EdgeReads, AttributeReads: s.AttributeReads, Revisits: s.Revisits, GCCycles: s.GCCycles, AllocatedBytes: s.AllocatedBytes, Allocations: s.Allocations, Valid: s.Valid, Reason: s.Reason}
}

func SampleJSON(in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	c, err := parseSampleConfig(raw)
	if err != nil {
		return err
	}
	result, err := workload.Measure(c.workload(), c.Batch)
	if err != nil {
		// Preserve the invalid sample in JSON for the collector. A process error
		// still exits non-zero so a caller cannot mistake it for a valid sample.
		result.Valid = false
		if result.Reason == "" {
			result.Reason = err.Error()
		}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if encodeErr := enc.Encode(convertSample(result)); encodeErr != nil {
		return encodeErr
	}
	return err
}

func SampleJSONBytes(raw []byte) (Sample, error) {
	c, err := parseSampleConfig(raw)
	if err != nil {
		return Sample{}, err
	}
	s, measureErr := workload.Measure(c.workload(), c.Batch)
	result := convertSample(s)
	if measureErr != nil && result.Reason == "" {
		result.Reason = measureErr.Error()
	}
	if err := validateSample(result); err != nil {
		result.Valid = false
		result.Reason = err.Error()
		if measureErr == nil {
			measureErr = err
		}
	}
	return result, measureErr
}

func VerifyJSONBytes(raw []byte) (TraceVerification, error) {
	c, err := parseSampleConfig(raw)
	if err != nil {
		return TraceVerification{}, err
	}
	cfg := c.workload()
	if cfg.Shape == "fixture" {
		cfg.Representation = "store"
		trace, traceErr := workload.Trace(cfg)
		if traceErr != nil {
			return TraceVerification{}, traceErr
		}
		return TraceVerification{Valid: true, Entries: len(trace), Store: trace}, nil
	}
	cfg.Representation = "store"
	store, err := workload.Trace(cfg)
	if err != nil {
		return TraceVerification{}, err
	}
	cfg.Representation = "pointer"
	pointer, err := workload.Trace(cfg)
	if err != nil {
		return TraceVerification{}, err
	}
	storeJSON, err := json.Marshal(store)
	if err != nil {
		return TraceVerification{}, err
	}
	pointerJSON, err := json.Marshal(pointer)
	if err != nil {
		return TraceVerification{}, err
	}
	result := TraceVerification{Valid: bytes.Equal(storeJSON, pointerJSON), Entries: len(store), Store: store, Pointer: pointer}
	if !result.Valid {
		result.Reason = "full trace mismatch"
	}
	return result, nil
}

func parseSampleConfig(raw []byte) (SampleConfig, error) {
	var c SampleConfig
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return c, errors.New("sample config has trailing JSON")
	}
	if c.Batch < 1 {
		return c, errors.New("batch must be positive")
	}
	if c.Case == "" || c.Shape == "" || c.Layout == "" || c.Representation == "" {
		return c, errors.New("sample config requires case, shape, layout, and representation")
	}
	if c.Nodes == 0 && c.Shape != "fixture" && !(c.Shape == workload.ShapeRepeated && c.Subtrees > 0) {
		return c, errors.New("sample config requires positive nodes for synthetic workloads")
	}
	return c, nil
}

func sampleFromConfigFile(path string) (Sample, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Sample{}, err
	}
	c, err := parseSampleConfig(b)
	if err != nil {
		return Sample{}, err
	}
	s, err := workload.Measure(c.workload(), c.Batch)
	result := convertSample(s)
	if err != nil && result.Reason == "" {
		result.Reason = err.Error()
	}
	return result, err
}

func validateSample(s Sample) error {
	if !s.Valid {
		return fmt.Errorf("invalid sample: %s", s.Reason)
	}
	if math.IsNaN(s.NSPerOp) || math.IsInf(s.NSPerOp, 0) || s.NSPerOp <= 0 || s.BytesPerOp < 0 || s.AllocsPerOp < 0 {
		return errors.New("sample contains negative metric")
	}
	if s.Visits == 0 {
		return errors.New("sample has zero visits")
	}
	if s.GCCycles != 0 || s.Allocations != 0 || s.AllocatedBytes != 0 || s.BytesPerOp != 0 || s.AllocsPerOp != 0 {
		return errors.New("synthetic allocation/GC contract violation")
	}
	return nil
}
