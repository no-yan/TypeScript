package format_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/format"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"gotest.tools/v3/assert"
)

func TestGetContainingList_NamedImports(t *testing.T) {
	t.Parallel()

	text := `import type {
    AAA,
    BBB,
} from "./bar";`

	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/test.ts",
		Path:     "/test.ts",
	}, text, core.ScriptKindTS)

	var importSpecifiers []ast.Handle
	forEachDescendantOfKind(sourceFile.ParseRoot(), ast.KindImportSpecifier, func(node ast.Handle) {
		importSpecifiers = append(importSpecifiers, node)
	})

	assert.Assert(t, len(importSpecifiers) == 2, "Expected 2 import specifiers, got %d", len(importSpecifiers))

	for _, specifier := range importSpecifiers {
		list := format.GetContainingList(specifier, sourceFile)
		assert.Assert(t, list != 0, "GetContainingList should return a list for import specifier")
		assert.Assert(t, sourceFile.ParseStore().ListLen(list) == 2, "Expected list with 2 elements, got %d", sourceFile.ParseStore().ListLen(list))
	}
}

func forEachDescendantOfKind(node ast.Handle, kind ast.Kind, action func(ast.Handle)) {
	node.ForEachChild(func(child ast.Handle) bool {
		if child.Kind == kind {
			action(child)
		}
		forEachDescendantOfKind(child, kind, action)
		return false
	})
}
