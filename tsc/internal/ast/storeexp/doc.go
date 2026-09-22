// Package storeexp is a throwaway EXPERIMENT. It is not production code.
//
// Every other file in this package is behind the storeexp build tag, so a
// production build cannot import anything from it.
//
// # Purpose
//
// It exists only to measure experiments E1-E6, E10 and E11 of section 4 of the
// Store AST design: a flat arena with a 24-byte node header and a uniformly
// laid out uint32 payload column, compared against the Pointer AST.
//
//   - design:       internal/ast/docs/store-ast-design-20260922.md
//   - this package: internal/ast/docs/storeexp-implementation-instructions-20260922.md
//   - measurements: internal/ast/docs/storeexp-verification-instructions-20260922.md
//
// # How it differs from a real Store AST
//
//   - The Store is converted from a Pointer AST. A real one is built directly
//     by the parser.
//   - The generator is a small Go program (_gen). A real one is a change to
//     tools/scripts/tsc/generate-go-ast.ts.
//   - Construction goes through one shape-driven Builder. A real one has a
//     generated constructor per kind.
//   - Typed views and slot constants are hand-written for seven representative
//     kinds. A real one generates them for every kind.
//   - Only Identifier has a data word (its text). A real one stores every
//     non-child member.
//   - It is not connected to the binder, checker, emitter or language service.
//   - Its location says nothing about where a real Store AST would live.
//
// # Do not extend
//
// Add nothing that the experiments in the verification instructions do not
// need. Do not connect it to the binder, checker, parser, emitter or language
// service. Do not add typed views for every kind. Do not import it from a
// production package. If you feel the need for any of these, go back to the
// design document instead.
//
// # Exit
//
// Decided 2026-09-22 after the verification report: the design is adopted
// (Walk gate on cycles, +15%). This package stays only as a reference until
// generate-go-ast.ts emits the real Store, and is deleted then. Adoption
// means reimplementing it in generate-go-ast.ts. It does not mean renaming or
// moving this package.
package storeexp
