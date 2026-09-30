package storechecker

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// The P2 declarations of checker/tracer.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// Symbol answers nil: tracing.TracedType takes Pointer nodes and symbols,
// which the Store checker does not have (2026-09-30, the user's decision). A
// trace of the Store checker has no symbol or node of a type.
//
// storetranslate:pointer-hash 3c72ee447e582875 tracer.go:tracedTypeAdapter.Symbol
func (a *tracedTypeAdapter) Symbol() *ast.Symbol {
	return nil
}

// AliasSymbol answers nil: tracing.TracedType takes Pointer nodes and symbols,
// which the Store checker does not have (2026-09-30, the user's decision). A
// trace of the Store checker has no symbol or node of a type.
//
// storetranslate:pointer-hash 5ce9e9dae7fb1cbb tracer.go:tracedTypeAdapter.AliasSymbol
func (a *tracedTypeAdapter) AliasSymbol() *ast.Symbol {
	return nil
}

// ReferenceNode answers nil: tracing.TracedType takes Pointer nodes and symbols,
// which the Store checker does not have (2026-09-30, the user's decision). A
// trace of the Store checker has no symbol or node of a type.
//
// storetranslate:pointer-hash b77a5b64f4ab6f4a tracer.go:tracedTypeAdapter.ReferenceNode
func (a *tracedTypeAdapter) ReferenceNode() *ast.Node {
	return nil
}

// Pattern answers nil: tracing.TracedType takes Pointer nodes and symbols,
// which the Store checker does not have (2026-09-30, the user's decision). A
// trace of the Store checker has no symbol or node of a type.
//
// storetranslate:pointer-hash 35a8865864897f52 tracer.go:tracedTypeAdapter.Pattern
func (a *tracedTypeAdapter) Pattern() *ast.Node {
	return nil
}
