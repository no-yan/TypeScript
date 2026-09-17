package workload

import (
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

func TestDefaultConfigIsValid(t *testing.T) {
	c := DefaultConfig()
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	if c.Case == "" || c.Shape == "" || c.Layout == "" || c.Representation == "" {
		t.Fatalf("default config is incomplete: %+v", c)
	}
}

func TestTraceParityAndDeterminism(t *testing.T) {
	for _, tc := range []struct{ name, shape, useCase string }{
		{"wide-full", ShapeWide, CaseFullTree},
		{"deep-full", ShapeDeep, CaseFullTree},
		{"mixed-full", ShapeMixed, CaseFullTree},
		{"wide-expression", ShapeWide, CaseExpression},
		{"deep-expression", ShapeDeep, CaseExpression},
		{"mixed-expression", ShapeMixed, CaseExpression},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			c.Case, c.Shape, c.Nodes = tc.useCase, tc.shape, 48
			c.Layout = LayoutConstruction
			c.Representation = RepresentationStore
			storeTrace, err := Trace(c)
			if err != nil {
				t.Fatal(err)
			}
			c.Representation = RepresentationPointer
			modelTrace, err := Trace(c)
			if err != nil {
				t.Fatal(err)
			}
			if len(storeTrace) == 0 || len(storeTrace) != len(modelTrace) {
				t.Fatalf("trace lengths: store=%d model=%d", len(storeTrace), len(modelTrace))
			}
			for i := range storeTrace {
				if storeTrace[i] != modelTrace[i] {
					t.Fatalf("trace mismatch at %d: store=%+v model=%+v", i, storeTrace[i], modelTrace[i])
				}
			}
			again, err := Trace(c)
			if err != nil {
				t.Fatal(err)
			}
			for i := range modelTrace {
				if modelTrace[i] != again[i] {
					t.Fatalf("nondeterministic trace at %d", i)
				}
			}
		})
	}
}

func TestParserFixtureIsStoreOnlyAndVerified(t *testing.T) {
	c := DefaultConfig()
	c.Shape, c.Nodes = ShapeFixture, 0
	c.Representation = RepresentationStore
	if err := Verify(c); err != nil {
		t.Fatal(err)
	}
	trace, err := Trace(c)
	if err != nil || len(trace) < 4 {
		t.Fatalf("fixture trace: n=%d err=%v", len(trace), err)
	}
	c.Representation = RepresentationPointer
	if err := Validate(c); err == nil {
		t.Fatal("parser fixture unexpectedly accepted pointer representation")
	}
}

func TestVerificationUnregistersStores(t *testing.T) {
	base := ast.RegisteredStoreCount()
	c := DefaultConfig()
	c.Nodes = 32
	if err := Verify(c); err != nil {
		t.Fatal(err)
	}
	if got := ast.RegisteredStoreCount(); got != base {
		t.Fatalf("registered stores leaked after Verify: before=%d after=%d", base, got)
	}
	if _, err := Trace(c); err != nil {
		t.Fatal(err)
	}
	if got := ast.RegisteredStoreCount(); got != base {
		t.Fatalf("registered stores leaked after Trace: before=%d after=%d", base, got)
	}
}

func TestSyntheticSeedChangesMixedShape(t *testing.T) {
	c := DefaultConfig()
	c.Shape, c.Nodes, c.Representation = ShapeMixed, 64, RepresentationStore
	a, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Seed++
	b, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("seed changed logical node count: %d vs %d", len(a), len(b))
	}
	different := false
	for i := range a {
		if a[i] != b[i] {
			different = true
			break
		}
	}
	if !different {
		t.Fatal("mixed synthetic seed did not affect the trace")
	}
}

func TestLayoutSeedDoesNotChangeLogicalTrace(t *testing.T) {
	c := DefaultConfig()
	c.Shape, c.Nodes, c.Representation = ShapeMixed, 64, RepresentationStore
	a, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	c.LayoutSeed = 0
	b, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("layout seed changed node count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("layout seed changed logical trace at %d", i)
		}
	}
}

func TestSyntheticTraceHasExactNodeCount(t *testing.T) {
	for _, shape := range []string{ShapeWide, ShapeDeep, ShapeMixed} {
		for n := 1; n <= 20; n++ {
			c := DefaultConfig()
			c.Shape, c.Nodes, c.Representation = shape, n, RepresentationStore
			got, err := Trace(c)
			if err != nil {
				t.Fatalf("%s/%d: %v", shape, n, err)
			}
			if len(got) != n {
				t.Fatalf("%s/%d: trace has %d nodes", shape, n, len(got))
			}
		}
	}
}

func TestExpressionWorkloadReadsFewerAttributes(t *testing.T) {
	full := DefaultConfig()
	full.Shape, full.Nodes, full.Representation = ShapeMixed, 64, RepresentationStore
	expr := full
	expr.Case = CaseExpression
	fullSample, err := Measure(full, 4)
	if err != nil {
		t.Fatal(err)
	}
	exprSample, err := Measure(expr, 4)
	if err != nil {
		t.Fatal(err)
	}
	if exprSample.AttributeReads >= fullSample.AttributeReads {
		t.Fatalf("expression case did not narrow attributes: full=%d expression=%d", fullSample.AttributeReads, exprSample.AttributeReads)
	}
}

func TestRejectsInvalidConfig(t *testing.T) {
	cases := []Config{
		{Case: "unknown", Shape: ShapeWide, Nodes: 2, Layout: LayoutConstruction, Representation: RepresentationStore},
		{Case: CaseFullTree, Shape: ShapeWide, Nodes: 0, Layout: LayoutConstruction, Representation: RepresentationStore},
		{Case: CaseFullTree, Shape: ShapeWide, Nodes: 2, Layout: "unknown", Representation: RepresentationStore},
		{Case: CaseFullTree, Shape: ShapeWide, Nodes: 2, Layout: LayoutConstruction, Representation: "unknown"},
	}
	for _, c := range cases {
		if err := Validate(c); err == nil {
			t.Fatalf("expected invalid config: %+v", c)
		}
	}
}

func TestMeasureContract(t *testing.T) {
	c := DefaultConfig()
	c.Nodes = 64
	for _, rep := range []string{RepresentationStore, RepresentationPointer} {
		c.Representation = rep
		baseStores := ast.RegisteredStoreCount()
		sample, err := Measure(c, 8)
		if err != nil {
			t.Fatalf("%s: %v", rep, err)
		}
		if !sample.Valid {
			t.Fatalf("%s: invalid sample: %+v", rep, sample)
		}
		if sample.Allocations != 0 || sample.AllocatedBytes != 0 || sample.GCCycles != 0 {
			t.Fatalf("%s: timed work allocated: %+v", rep, sample)
		}
		if sample.Visits == 0 || sample.Checksum == 0 {
			t.Fatalf("%s: missing timed result: %+v", rep, sample)
		}
		if got := ast.RegisteredStoreCount(); got != baseStores {
			t.Fatalf("%s: Measure leaked stores: before=%d after=%d", rep, baseStores, got)
		}
	}
}

func TestMeasureRestoresRuntimeControls(t *testing.T) {
	c := DefaultConfig()
	c.Nodes, c.Representation = 32, RepresentationStore
	wantP := runtime.GOMAXPROCS(0)
	wantGC := debug.SetGCPercent(-1)
	debug.SetGCPercent(wantGC)
	wantLimit := debug.SetMemoryLimit(-1)
	if _, err := Measure(c, 2); err != nil {
		t.Fatal(err)
	}
	if got := runtime.GOMAXPROCS(0); got != wantP {
		t.Fatalf("GOMAXPROCS not restored: got=%d want=%d", got, wantP)
	}
	if got := debug.SetGCPercent(-1); got != wantGC {
		t.Fatalf("GOGC not restored: got=%d want=%d", got, wantGC)
	}
	debug.SetGCPercent(wantGC)
	if got := debug.SetMemoryLimit(-1); got != wantLimit {
		t.Fatalf("memory limit not restored: got=%d want=%d", got, wantLimit)
	}
}
