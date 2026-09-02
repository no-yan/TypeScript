package printer_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"gotest.tools/v3/assert"
)

func TestEmitContextAppendsIntoParseStore(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	file := parser.ParseSourceFile(opts, "export const x = 1;\n", core.ScriptKindTS)
	ast.RegisterFile(file)
	file.ParseStore().Freeze()
	store := file.ParseStore()
	before := store.Len()

	context := printer.NewEmitContext()
	release := context.LockParseStoreWriter(file)
	generated := context.Factory.NewIdentifier("generated")
	root := file.ParseRoot()
	stmts := append([]ast.Handle(nil), root.Statements()...)
	stmts = append(stmts, context.Factory.NewEmptyStatement())
	updated := context.Factory.UpdateSourceFile(root, context.Factory.NewList(stmts), root.SourceFileEndOfFileToken())
	file.SetParseRoot(updated)
	assert.Equal(t, ast.KindSourceFile, updated.Kind())
	assert.Assert(t, context.NodeIdentity(updated).StoreID() != 0)
	context.SetEmitFlags(updated, printer.EFNoComments)
	assert.Equal(t, printer.EFNoComments, context.EmitFlags(updated))
	release()

	assert.Assert(t, store.Len() > before)
	assert.Assert(t, !generated.IsNil())
	assert.Equal(t, generated.Store(), store)
}
