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
	store := file.ParseStore()
	before := store.Len()

	context := printer.NewEmitContext()
	release := context.AttachStore(file)
	generated := context.Factory.NewIdentifier("generated")
	nodes := append([]*ast.Node(nil), file.Statements.Nodes...)
	nodes = append(nodes, context.Factory.NewEmptyStatement())
	statements := context.Factory.NewNodeList(nodes)
	updated := context.Factory.UpdateSourceFile(file, statements, file.EndOfFileToken).AsSourceFile()
	updatedHandle := context.Factory.HandleOf(updated.AsNode())
	assert.Equal(t, store, updated.ParseStore())
	assert.Equal(t, updatedHandle.Ref(), updated.ParseRoot().Ref())
	assert.Equal(t, file.AsNode(), context.MostOriginal(updated.AsNode()))
	assert.Assert(t, context.NodeIdentity(updated.AsNode()).StoreID() != 0)
	context.SetEmitFlags(updated.AsNode(), printer.EFNoComments)
	assert.Equal(t, printer.EFNoComments, context.EmitFlags(updated.AsNode()))
	release()

	assert.Assert(t, store.Len() > before)
	assert.Equal(t, generated, file.NodeFor(file.HandleOf(generated).Ref()))
	assert.Equal(t, updated.AsNode(), file.NodeFor(updatedHandle.Ref()))
	assert.Assert(t, context.Factory.Store() == nil)
}
