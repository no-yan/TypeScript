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
	store := file.ParseStore()
	before := store.Len()

	context := printer.NewEmitContext()
	release := context.AttachStore(file)
	generated := context.Factory.NewIdentifier("generated")
	release()

	assert.Equal(t, before+1, store.Len())
	assert.Equal(t, generated, file.NodeFor(file.HandleOf(generated).Ref()))
	assert.Assert(t, context.Factory.Store() == nil)
}
