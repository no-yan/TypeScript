package storeparser

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// ParseIsolatedEntityName is parser.ParseIsolatedEntityName for the Store
// checker (R16 of internal/ast/docs/store-checker-7d2-design-remainder-20260929.md
// 2.1, store-checker-7d2a-implementation-instructions-20260929.md 4.4): the
// text parsed as JS into a Store of its own, numbered from the program's
// IDSpace as a lazy JSDoc Store is (Y3), with every node at (-1, -1) and
// sealed. It has no file, as the Pointer result has no SourceFile. The name,
// or nil when the parse does not reach the end of the text or reports a
// diagnostic.
func ParseIsolatedEntityName(space *store.IDSpace, text string) store.Node {
	p := getParser()
	defer putParser(p)
	p.initializeState(space, space.Next(), ast.SourceFileParseOptions{}, text, core.ScriptKindJS)
	p.nextToken()
	entityName := p.parseEntityName(true, false, nil)
	if p.token != ast.KindEndOfFile || len(p.diagnostics) != 0 {
		return store.Node{}
	}
	return p.b.FinishSynthetic(entityName)
}
