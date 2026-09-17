package ast_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/astbench/workload"
)

// BenchmarkTraversalWorkload exposes the fixed-batch workload metrics to Go's
// benchmark runner. Calibration includes setup; reported metrics use only
// Measure's controlled traversal interval. Campaigns use the fixed-batch CLI.
func BenchmarkTraversalWorkload(b *testing.B) {
	for _, tc := range []struct {
		name    string
		shape   string
		useCase string
	}{
		{"full-wide", workload.ShapeWide, workload.CaseFullTree},
		{"full-deep", workload.ShapeDeep, workload.CaseFullTree},
		{"expression-mixed", workload.ShapeMixed, workload.CaseExpression},
	} {
		for _, representation := range []string{workload.RepresentationStore, workload.RepresentationPointer} {
			name := tc.name + "/" + representation
			b.Run(name, func(b *testing.B) {
				c := workload.DefaultConfig()
				c.Case, c.Shape, c.Nodes, c.Representation = tc.useCase, tc.shape, 256, representation
				b.ReportAllocs()
				b.ResetTimer()
				sample, err := workload.Measure(c, b.N)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				// These are the fixed-batch values from Measure. The benchmark's
				// own timer includes setup, so the reported metrics come from the
				// controlled measurement window instead.
				b.ReportMetric(sample.NsPerOp, "ns/op")
				b.ReportMetric(sample.BytesPerOp, "B/op")
				b.ReportMetric(sample.AllocsPerOp, "allocs/op")
				if !sample.Valid {
					b.Fatalf("invalid sample: %+v", sample)
				}
			})
		}
	}
}
