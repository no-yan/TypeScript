// Package storeparser parses TypeScript into a Store AST (internal/ast/store)
// without building the Pointer AST first. It is TODO 7b of
// internal/ast/docs/store-ast-design-20260922.md (sections 2.7 and 6); the
// instructions it was written to are
// internal/ast/docs/store-parser-implementation-instructions-20260922.md.
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
// # Not ported (7c and later)
//
// JSDoc nodes and the JS reparse (jsdoc.go, reparser.go): every file only gets
// the HasJSDoc and PossiblyContainsDeprecatedTag flags, as TS files do in the
// Pointer parser. The external module indicator and the module references
// (references.go), and with them the top-level await reparse that depends on
// the indicator. Incremental parsing, the language service and the api
// encoder.
//
// getCommentPragmas needs an ast.NodeFactory because
// scanner.GetLeadingCommentRanges takes one to make CommentRange values; the
// parser keeps one for that call only.
package storeparser
