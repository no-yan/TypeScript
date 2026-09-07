package printer_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"gotest.tools/v3/assert"
)

func TestEmitContextKeepsParseStoreImmutable(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	file := parser.ParseSourceFile(opts, "export const x = 1;\n", core.ScriptKindTS)
	ast.RegisterFile(file)
	file.ParseStore().Freeze()
	store := file.ParseStore()
	before := store.Len()

	context := printer.NewEmitContext()
	release := context.BeginFile(file)
	generated := context.Factory.NewIdentifier("generated")
	root := file.ParseRoot()
	stmts := append([]ast.Handle(nil), root.Statements()...)
	stmts = append(stmts, context.Factory.NewEmptyStatement())
	updated := context.Factory.UpdateSourceFile(root, context.Factory.NewList(stmts), root.SourceFileEndOfFileToken())
	view := file.CloneWrapper()
	view.SetParseRoot(updated)
	assert.Equal(t, root, file.ParseRoot())
	assert.Equal(t, root.Statements()[0], updated.Statements()[0])
	assert.Equal(t, ast.KindSourceFile, updated.Kind)
	assert.Assert(t, context.NodeIdentity(updated).StoreID() != 0)
	context.SetEmitFlags(updated, printer.EFNoComments)
	assert.Equal(t, printer.EFNoComments, context.EmitFlags(updated))
	release()

	assert.Equal(t, before, store.Len())
	assert.Assert(t, !generated.IsNil())
	assert.Assert(t, generated.Store() != store)
	store.Freeze()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on write after emit lease")
		}
	}()
	store.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
}

func TestCrossStoreClonePreservesOriginalDescendants(t *testing.T) {
	t.Parallel()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}, "const x = a.b;", core.ScriptKindTS)
	file.ParseStore().Freeze()
	context := printer.NewEmitContext()
	defer context.Reset()
	original := file.ParseRoot().Statements()[0]
	clone := context.Factory.DeepCloneNode(original)
	assert.Assert(t, clone.Store() != original.Store())
	var compare func(ast.Handle, ast.Handle)
	compare = func(copied, source ast.Handle) {
		assert.Equal(t, source, context.MostOriginal(copied))
		var sourceChildren []ast.Handle
		source.ForEachChild(func(n ast.Handle) bool { sourceChildren = append(sourceChildren, n); return false })
		i := 0
		copied.ForEachChild(func(n ast.Handle) bool { compare(n, sourceChildren[i]); i++; return false })
		assert.Equal(t, len(sourceChildren), i)
	}
	compare(clone, original)
}
