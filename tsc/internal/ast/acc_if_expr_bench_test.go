package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// Frozen harness for proposal 1 (raw column accessor). Metric is
// BenchmarkAccIfExpr ns/op. Lower is better. Store size and order are fixed.

const accIfN = 1 << 16

func buildIfExprStore(n int) (*Store, []NodeRef) {
	s := NewStore(n * 2)
	refs := make([]NodeRef, 0, n)
	loc := core.NewTextRange(0, 1)
	for i := 0; i < n; i++ {
		leaf := s.Alloc(KindIdentifier, 0, loc, 0)
		h := s.Alloc(KindIfStatement, 0, loc, 3)
		h.SetChild(slotIfStatementExpression, leaf)
		refs = append(refs, h.id)
	}
	order := make([]NodeRef, n)
	x := uint32(12345)
	for i := range order {
		x = x*1664525 + 1013904223
		order[i] = refs[int(x>>8)%n]
	}
	return s, order
}

func BenchmarkAccIfExpr(b *testing.B) {
	s, order := buildIfExprStore(accIfN)
	var sink NodeRef
	b.ReportAllocs()
	for b.Loop() {
		for _, id := range order {
			sink += s.At(id).IfStatementExpression().id
		}
	}
	_ = sink
}

func BenchmarkAccIfExprChild(b *testing.B) {
	s, order := buildIfExprStore(accIfN)
	var sink NodeRef
	b.ReportAllocs()
	for b.Loop() {
		for _, id := range order {
			sink += s.At(id).Child(slotIfStatementExpression).id
		}
	}
	_ = sink
}
