package checker

import (
	"sync/atomic"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"gotest.tools/v3/assert"
)

const (
	mergedSymbolBenchCount    = 8192
	mergedSymbolBenchMerged   = 16
	mergedSymbolLookupAlways  = "always_map"
	mergedSymbolLookupGuarded = "atomic_guard"
)

type mergedProbe struct {
	hasMerged atomic.Uint32
}

func getMergedAlways(m map[*mergedProbe]*mergedProbe, symbol *mergedProbe) *mergedProbe {
	if symbol != nil {
		merged := m[symbol]
		if merged != nil {
			return merged
		}
	}
	return symbol
}

func getMergedGuarded(m map[*mergedProbe]*mergedProbe, symbol *mergedProbe) *mergedProbe {
	if symbol != nil && symbol.hasMerged.Load() != 0 {
		merged := m[symbol]
		if merged != nil {
			return merged
		}
	}
	return symbol
}

func newMergedProbeSet() (symbols []*mergedProbe, merged map[*mergedProbe]*mergedProbe) {
	symbols = make([]*mergedProbe, mergedSymbolBenchCount)
	merged = make(map[*mergedProbe]*mergedProbe, mergedSymbolBenchMerged)
	for i := range symbols {
		symbols[i] = &mergedProbe{}
	}
	for i := range mergedSymbolBenchMerged {
		target := &mergedProbe{}
		source := symbols[i]
		merged[source] = target
		source.hasMerged.Store(1)
	}
	return symbols, merged
}

func TestGetMergedSymbolLookupStrategies(t *testing.T) {
	t.Parallel()
	source := &mergedProbe{}
	target := &mergedProbe{}
	m := map[*mergedProbe]*mergedProbe{source: target}

	assert.Equal(t, getMergedAlways(m, nil), (*mergedProbe)(nil))
	assert.Equal(t, getMergedGuarded(m, nil), (*mergedProbe)(nil))
	assert.Equal(t, getMergedAlways(m, source), target)

	unmarked := &mergedProbe{}
	m[unmarked] = target
	assert.Equal(t, getMergedGuarded(m, unmarked), unmarked)

	source.hasMerged.Store(1)
	assert.Equal(t, getMergedGuarded(m, source), target)

	other := &mergedProbe{}
	other.hasMerged.Store(1)
	assert.Equal(t, getMergedGuarded(m, other), other)
	assert.Equal(t, getMergedAlways(m, other), other)
}

func TestGetMergedSymbol(t *testing.T) {
	t.Parallel()
	c := &Checker{mergedSymbols: make(map[*ast.Symbol]*ast.Symbol)}
	source := &ast.Symbol{Name: "source"}
	target := &ast.Symbol{Name: "target"}
	other := &ast.Symbol{Name: "other"}

	assert.Equal(t, c.getMergedSymbol(nil), (*ast.Symbol)(nil))
	assert.Equal(t, c.getMergedSymbol(source), source)
	c.recordMergedSymbol(target, source)
	assert.Equal(t, c.getMergedSymbol(source), target)
	assert.Equal(t, c.getMergedSymbol(other), other)
}

func BenchmarkMergedSymbolLookup(b *testing.B) {
	symbols, merged := newMergedProbeSet()
	b.Run(mergedSymbolLookupAlways, func(b *testing.B) {
		var n int
		for b.Loop() {
			for _, symbol := range symbols {
				if getMergedAlways(merged, symbol) != nil {
					n++
				}
			}
		}
		_ = n
	})
	b.Run(mergedSymbolLookupGuarded, func(b *testing.B) {
		var n int
		for b.Loop() {
			for _, symbol := range symbols {
				if getMergedGuarded(merged, symbol) != nil {
					n++
				}
			}
		}
		_ = n
	})
}

func BenchmarkGetMergedSymbol(b *testing.B) {
	c := &Checker{mergedSymbols: make(map[*ast.Symbol]*ast.Symbol, mergedSymbolBenchMerged)}
	symbols := make([]*ast.Symbol, mergedSymbolBenchCount)
	for i := range symbols {
		symbols[i] = &ast.Symbol{}
	}
	for i := range mergedSymbolBenchMerged {
		c.recordMergedSymbol(&ast.Symbol{}, symbols[i])
	}
	var n int
	for b.Loop() {
		for _, symbol := range symbols {
			if c.getMergedSymbol(symbol) != nil {
				n++
			}
		}
	}
	_ = n
}
