// Package store is the Store AST: a flat arena of 32-byte node headers plus a
// uniformly laid out uint32 payload column, replacing the pointer-linked
// *ast.Node graph one file at a time.
//
// The design is recorded in internal/ast/docs/store-ast-design-20260922.md;
// this package implements TODO 7a of that document's section 6 ("generator
// で全 kind に展開"): a real generator (tools/scripts/tsc/generate-go-store.ts,
// run from tools/scripts/tsc/generate.ts) emits shapes, typed views,
// polymorphic accessors, ForEachChild, and per-kind constructors for every
// kind, and a Pointer-AST-to-Store converter (./convert) exercises the
// generated Builder API end to end.
//
// # Scope (7a to 7d-0)
//
// This package is not wired into anything: the checker, emitter and language
// service are untouched, and nothing outside this package, internal/storeparser
// (7b), internal/storebinder (7c) and their tests imports it. A Store comes
// from the Store parser, or from ./convert, which walks an already-parsed
// *ast.SourceFile.
//
// The binder's output (7c, internal/ast/docs/store-binder-design-20260922.md)
// lives in the header (flow and symbol, 32 bytes), in the reserved slots of
// the kinds that have them (Locals, LocalSymbol, EndFlowNode, ReturnFlowNode,
// FallthroughFlowNode; generated with the payload) and in Bound (bound.go:
// the flow slabs, the symbols, the locals tables, the file's fields). Symbol
// is symbol.go; the predicates of internal/ast that read a node's contents
// are utilities.go, translated from ast by tools/cmd/storetranslate, with the
// hand-written P2 replacements in utilities_manual.go.
//
// # 7d-0
//
// The base of the Store checker (internal/ast/docs/
// store-checker-design-20260923.md 2.1, and
// store-checker-7d0-implementation-instructions-20260924.md): StoreID and
// NodeKey (idspace.go, store.go), edges that may name a node or list of
// another Store through a foreign table (store.go), the zero Node as nil and
// Node not comparable (store-node-zero-design-20260926.md), the declarations
// of a Symbol as Nodes and its process-wide gid (symbol.go), the resolving
// layer that carries the owner (bound.go: Flow, FlowList), and the synthetic
// Stores of a checker with their Factory and writers (factory.go,
// factory_generated.go). Not yet built (7d-1 and later): the JSDoc nodes (JSDoc
// is not converted at all: Convert follows exactly the children
// (*ast.Node).ForEachChild visits) and the checker.
package store
