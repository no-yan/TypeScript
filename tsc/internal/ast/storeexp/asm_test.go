//go:build storeexp

package storeexp

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
)

// The asm wrappers give each access operation of bench_test.go a symbol of its
// own, so that its instructions can be read with
//
//	go tool objdump -s 'storeexp\.asm' storeexp.test
//
// The benchmarks do not use them: there the operations are inlined into loops.

//go:noinline
func asmBaselinePointer(n *ast.Node) uint64 { return opBaselinePointer(n) }

//go:noinline
func asmBaselineStore(n Node) uint64 { return opBaselineStore(n) }

//go:noinline
func asmHeaderPointer(n *ast.Node) uint64 { return opHeaderPointer(n) }

//go:noinline
func asmHeaderStore(n Node) uint64 { return opHeaderStore(n) }

// known, list and modflags carry their body here: see bench_test.go.

//go:noinline
func asmKnownPointer(n *ast.Node) uint64 {
	switch n.Kind {
	case ast.KindCallExpression:
		return uint64(n.AsCallExpression().Expression.Kind)
	case ast.KindBinaryExpression:
		b := n.AsBinaryExpression()
		return uint64(b.Left.Kind) + uint64(b.OperatorToken.Kind) + uint64(b.Right.Kind)
	default:
		p := n.AsPropertyAccessExpression()
		return uint64(p.Expression.Kind) + uint64(p.Name().Kind)
	}
}

//go:noinline
func asmKnownStore(n Node) uint64 {
	switch n.Kind() {
	case ast.KindCallExpression:
		return uint64(n.AsCallExpression().Expression().Kind())
	case ast.KindBinaryExpression:
		b := n.AsBinaryExpression()
		return uint64(b.Left().Kind()) + uint64(b.OperatorToken().Kind()) + uint64(b.Right().Kind())
	default:
		p := n.AsPropertyAccessExpression()
		return uint64(p.Expression().Kind()) + uint64(p.Name().Kind())
	}
}

//go:noinline
func asmPolyPresentPointer(n *ast.Node) uint64 { return opPolyPresentPointer(n) }

//go:noinline
func asmPolyPresentStore(n Node) uint64 { return opPolyPresentStore(n) }

//go:noinline
func asmPolyAllPointer(n *ast.Node) uint64 { return opPolyAllPointer(n) }

//go:noinline
func asmPolyAllStore(n Node) uint64 { return opPolyAllStore(n) }

//go:noinline
func asmListPointer(n *ast.Node) (sum uint64) {
	for _, arg := range n.AsCallExpression().Arguments.Nodes {
		sum += uint64(arg.Kind)
	}
	return sum
}

//go:noinline
func asmListStore(n Node) (sum uint64) {
	for _, ref := range n.AsCallExpression().Arguments().Refs() {
		sum += uint64(n.s.node(ref).Kind())
	}
	return sum
}

//go:noinline
func asmTextPointer(n *ast.Node) uint64 { return opTextPointer(n) }

//go:noinline
func asmTextStore(n Node) uint64 { return opTextStore(n) }

//go:noinline
func asmModFlagsPointer(n *ast.Node) uint64 { return uint64(n.ModifierFlags()) }

//go:noinline
func asmModFlagsStore(n Node) uint64 { return uint64(n.ModifierFlags()) }

//go:noinline
func asmParentChainPointer(n *ast.Node) uint64 { return opParentChainPointer(n) }

//go:noinline
func asmParentChainStore(n Node) uint64 { return opParentChainStore(n) }

//go:noinline
func asmParentChainRefStore(n Node) uint64 { return opParentChainRefStore(n) }

//go:noinline
func asmRefRoundTripStore(n Node) uint64 { return opRefRoundTripStore(n) }

// TestAsmWrappers keeps the wrappers in the linked binary and checks each
// operation on a source that reaches every branch of known.
func TestAsmWrappers(t *testing.T) {
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/a.ts", Path: "/a.ts"},
		`export function f() { return fn(left + right, object.property); }`, core.ScriptKindTS)
	store := Convert(file, 0)
	pointers := preorderPointer(file.AsNode())
	for i, n := range preorderStore(store.Root(), Node.ForEachChild) {
		p := pointers[i]
		check := func(op string, pointer, store uint64) {
			if pointer != store {
				t.Errorf("%s on %v at %d: Store = %d, Pointer = %d", op, p.Kind, p.Pos(), store, pointer)
			}
		}
		// No result to compare: these only need to be linked.
		asmBaselinePointer(p)
		asmBaselineStore(n)
		asmRefRoundTripStore(n)
		check("header", asmHeaderPointer(p), asmHeaderStore(n))
		check("polyAll", asmPolyAllPointer(p), asmPolyAllStore(n))
		check("modflags", asmModFlagsPointer(p), asmModFlagsStore(n))
		if p.Name() != nil {
			check("polyPresent", asmPolyPresentPointer(p), asmPolyPresentStore(n))
		}
		switch p.Kind {
		case ast.KindCallExpression:
			check("list", asmListPointer(p), asmListStore(n))
			check("known", asmKnownPointer(p), asmKnownStore(n))
		case ast.KindBinaryExpression, ast.KindPropertyAccessExpression:
			check("known", asmKnownPointer(p), asmKnownStore(n))
		case ast.KindIdentifier:
			check("text", asmTextPointer(p), asmTextStore(n))
			check("parentChain", asmParentChainPointer(p), asmParentChainStore(n))
			check("parentChainRef", asmParentChainPointer(p), asmParentChainRefStore(n))
		}
	}
}
