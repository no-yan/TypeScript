// Package storeparser parses TypeScript into a Store AST (internal/ast/store)
// without building the Pointer AST first. It is TODO 7b of
// internal/ast/docs/store-ast-design-20260922.md (sections 2.7 and 6).
//
// # A mechanical port
//
// parser.go is internal/parser/parser.go with the Pointer AST replaced by the
// Store: function names, order, comments and locals are kept so that a diff
// of the two files is the record of the port. The rules of the port:
//
//   - *ast.Node is store.NodeRef and *ast.NodeList is store.ListRef, with
//     store.NoNodeRef and store.NoListRef for nil.
//   - p.finishNode(p.factory.NewXxx(children), pos) is
//     p.b.NewXxx(p.flags(), pos, p.nodePos(), children): the generated
//     constructor writes the children's parent, so nothing sets a parent after
//     the fact. Where the Pointer parser parsed a child inside the constructor
//     call, the child is parsed into a local first, because the end position
//     and the parse-error flag must be read after the child.
//   - Lists are built on the parser's elems scratch (a LIFO) and written as one
//     block by Builder.List.
//   - Speculation (lookAhead, tryParse) truncates the node and payload columns
//     back to the mark; nodes that the Pointer parser discards without a rewind
//     stay as dead nodes.
//   - What the Pointer parser reads off a node after making it (kind, position,
//     a member) is read through Builder.View, whose result is valid only until
//     the next node is appended.
//   - A file is parsed into the builder's reusable scratch and copied to its
//     exact size by Builder.Finish.
//
// The external module indicator (internal/ast/parseoptions.go) and the module
// references (internal/parser/references.go) are in references.go: the
// indicator is computed on the scratch before the top-level await reparse
// that depends on it, the references on the finished Store.
//
// # JSDoc and the JS reparse (7d-1)
//
// jsdoc.go and reparser.go are internal/parser/jsdoc.go and reparser.go,
// ported by the same rules (internal/ast/docs/store-checker-design-20260923.md
// 2.2 and store-checker-7d1-implementation-instructions-20260928.md 4). The
// JSDoc of a JS file, and of a TS comment with @see or @link, is parsed into
// the file's Store with its host, and the JS reparse writes its nodes there
// too, through the Builder's reparse API (store/builder_reparse.go); the
// file's JSDoc cache is made after Finish. The rest of a TS file's JSDoc is
// parsed on the first access (parseJSDocForNode, registered with
// store.SetParseJSDocForNode), each comment set into a sealed Store of its
// own.
//
// # Not ported (7d and later)
//
// Incremental parsing, the language service and the api encoder.
//
// getCommentPragmas and GetJSDocCommentRanges need an ast.NodeFactory because
// scanner.GetLeadingCommentRanges takes one to make CommentRange values; the
// parser keeps one for those calls only.
package storeparser
