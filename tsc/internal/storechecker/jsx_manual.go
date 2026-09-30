package storechecker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
)

// The P2 declarations of checker/jsx.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// parseIsolatedEntityName parses the name into a Store of the program, whose
// nodes storeparser.ParseIsolatedEntityName makes synthetic (-1, -1) before it
// seals it (R16): markAsSynthetic, which writes them, is not called.
//
// storetranslate:pointer-hash f1d7affd9be46a3d jsx.go:Checker.parseIsolatedEntityName
func (c *Checker) parseIsolatedEntityName(name string) store.Node {
	return storeparser.ParseIsolatedEntityName(c.program.IDSpace(), name)
}
