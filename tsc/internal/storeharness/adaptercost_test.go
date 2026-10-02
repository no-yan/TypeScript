package storeharness

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
)

// The adapter's own cost in one check of the fixture
// (store-checker-7d2c-abc-implementation-instructions-20261002.md 3.2): the
// node-keyed calls of one check are recorded, then replayed. One op is one
// check's worth of calls.

type nodeCall struct {
	method int // 0 EmitSyntax, 1 UsageMode, 2 FromSpecifier
	file   ast.HasFileName
	node   store.Node
}

type nodeCallRecorder struct {
	*FixtureProgram
	calls []nodeCall
}

func (r *nodeCallRecorder) GetEmitSyntaxForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	r.calls = append(r.calls, nodeCall{0, file, usage})
	return r.FixtureProgram.GetEmitSyntaxForUsageLocation(file, usage)
}

func (r *nodeCallRecorder) GetModeForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	r.calls = append(r.calls, nodeCall{1, file, usage})
	return r.FixtureProgram.GetModeForUsageLocation(file, usage)
}

func (r *nodeCallRecorder) GetResolvedModuleFromModuleSpecifier(file ast.HasFileName, moduleSpecifier store.Node) *module.ResolvedModule {
	r.calls = append(r.calls, nodeCall{2, file, moduleSpecifier})
	return r.FixtureProgram.GetResolvedModuleFromModuleSpecifier(file, moduleSpecifier)
}

func BenchmarkAdapterCost(b *testing.B) {
	fixture := readFixture(b)
	program := NewFixtureProgram(fixture)
	program.BindSourceFiles()
	r := &nodeCallRecorder{FixtureProgram: program}
	program.Calls = [numCalls]int{}
	checkStore(b, r, fixtureDiagFiles(fixture, program))
	counts := program.Calls
	total := 0
	for _, n := range counts {
		total += n
	}
	b.Logf("node-keyed calls %d, all calls %d", len(r.calls), total)
	b.Run("struct", func(b *testing.B) {
		for range b.N {
			for _, c := range r.calls {
				switch c.method {
				case 0:
					program.GetEmitSyntaxForUsageLocation(c.file, c.node)
				case 1:
					program.GetModeForUsageLocation(c.file, c.node)
				case 2:
					program.GetResolvedModuleFromModuleSpecifier(c.file, c.node)
				}
			}
		}
	})
	// lookup is struct without the Calls counter.
	b.Run("lookup", func(b *testing.B) {
		maps := [...]map[nodeKeyOf]int32{program.emitSyntax, program.usageMode}
		for range b.N {
			for _, c := range r.calls {
				if c.method == 2 {
					_ = program.fromSpecifier[program.nodeKeyFor(c.file, c.node)]
				} else {
					_ = maps[c.method][program.nodeKeyFor(c.file, c.node)]
				}
			}
		}
	})
	// string is the lookup before 7d-2c abc C, for comparison.
	b.Run("string", func(b *testing.B) {
		for range b.N {
			for _, c := range r.calls {
				k := nodeKey(c.file, c.node)
				switch c.method {
				case 0:
					_ = fixture.EmitSyntax[k]
				case 1:
					_ = fixture.UsageMode[k]
				case 2:
					_ = fixture.FromSpecifier[k]
				}
			}
		}
	})
	// calls is the Calls counter of every adapter call of the check.
	b.Run("calls", func(b *testing.B) {
		var calls [numCalls]int
		for range b.N {
			for m, n := range counts {
				for range n {
					calls[m]++
				}
			}
		}
	})
}
