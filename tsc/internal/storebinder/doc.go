// Package storebinder binds a Store AST (internal/ast/store) as
// internal/binder binds the Pointer AST. It is TODO 7c of
// internal/ast/docs/store-ast-design-20260922.md (section 6); its design is
// internal/ast/docs/store-binder-design-20260922.md and the instructions it
// was written to are
// internal/ast/docs/store-binder-implementation-instructions-20260922.md.
//
// # A mechanical port
//
// binder.go is internal/binder/binder.go with the Pointer AST replaced by the
// Store: function names, order, comments and locals are kept so that a diff of
// the two files is the record of the port. The rules of the port:
//
//   - A visited node and the container locals are store.Node; nil is the
//     sentinel node (IsNil). A flow node, label or list is an index into the
//     slabs of store.Bound (FlowRef, FlowListRef; 0 is nil), so no *FlowNode
//     is held across an append. A symbol is *store.Symbol; the nodes it holds
//     are store.Ref.
//   - The output goes where store-binder-design-20260922.md 2.3 puts it: the
//     node's flow and symbol in its 32-byte header (SetFlow, SetSymbol), its
//     flags through AddFlags and ClearFlags, Locals, LocalSymbol, EndFlowNode,
//     ReturnFlowNode and FallthroughFlowNode in the kind's reserved slot
//     (the generated role setters), and the file's fields, the flow slabs and
//     the symbol table in File.Bound. The file's symbol is the root header's.
//   - Children are read through the typed views and the role accessors; a
//     list is walked as Refs() and resolved with Store.Node. The visit order,
//     including the hand-written orders (functions first, the destructuring
//     and IIFE orders), is the Pointer binder's.
//   - The predicates of internal/ast that read a node's contents are the
//     same-named functions of store/utilities.go; those that read only a kind
//     are ast's. The scanner helpers that took a *ast.SourceFile are in
//     utilities.go here, on a *store.File.
//   - The binder seals the Store when it is done: in a storechecks build,
//     any later write through the Store's write API panics.
//
// # Not ported (7d and later)
//
// nameresolver.go and referenceresolver.go; the JSDoc-derived nodes of JS
// files (the JS reparse is not in the Store parser, so the branches that
// only they reach are kept but never taken); attaching the file to the bind
// diagnostics (their file is nil); the checker, and the program that would
// call BindSourceFile.
package storebinder
