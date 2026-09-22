// Package store is the Store AST: a flat arena of 24-byte node headers plus a
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
// # Scope (7a)
//
// This package is not wired into anything: the parser, binder, checker,
// emitter and language service are untouched, and nothing outside this
// package and its own tests imports it. The only way to obtain a Store is
// ./convert, which walks an already-parsed *ast.SourceFile. Not yet built
// (left to TODO 7b and later):
//
//   - constructing a Store directly from the parser (id writes, scratch
//     buffers, Compact);
//   - symbol, locals and flow-node columns (section 5 of the design);
//   - a synthetic Store for checker/emitter-created nodes;
//   - JSDoc node placement (JSDoc is not converted at all: Convert follows
//     exactly the children (*ast.Node).ForEachChild visits).
//
// # Relationship to storeexp
//
// internal/ast/storeexp was the hand-written, build-tagged experiment that
// this package replaces; it was deleted once this package reproduced its
// numbers (internal/ast/docs/store-generator-verification-report-20260922.md).
// storeexp covered only seven representative kinds and only Identifier's text
// as payload; this package covers every kind and every non-child member,
// generated from tools/scripts/tsc/ast.json.
package store
