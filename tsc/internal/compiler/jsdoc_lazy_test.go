package compiler_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

// TestCheckParsesDeferredJSDocOutsideParseStore covers the compile path for
// deferred TS JSDoc: the checker parses it on demand into its own Store, the
// diagnostics that depend on it are still produced, the parse Store keeps its
// post-parse length, and the LS-only shared side Store is never created.
func TestCheckParsesDeferredJSDocOutsideParseStore(t *testing.T) {
	t.Parallel()
	if !bundled.Embedded {
		t.Skip("bundled files are not embedded")
	}
	fs := vfstest.FromMap[any](nil, false /*useCaseSensitiveFileNames*/)
	fs = bundled.WrapFS(fs)
	_ = fs.WriteFile("c:/dev/src/index.ts", `
/** @deprecated use g */
export const f = 1;
/** @param {number} missing not a parameter */
export function g(x: number): number { return x; }
export const h = f + 1;
`)
	opts := core.CompilerOptions{Target: core.ScriptTargetESNext, SkipLibCheck: core.TSTrue}
	program := compiler.NewProgram(compiler.ProgramOptions{
		Config: &tsoptions.ParsedCommandLine{
			ParsedConfig: &tsoptions.ParsedOptions{
				FileNames:       []string{"c:/dev/src/index.ts"},
				CompilerOptions: &opts,
			},
		},
		Host: compiler.NewCompilerHost("c:/dev/src", fs, bundled.LibPath(), nil, nil, nil),
	})
	defer program.Close()

	program.BindSourceFiles()
	var index *ast.SourceFile
	lens := map[string]int{}
	for _, file := range program.GetSourceFiles() {
		lens[file.FileName()] = file.ParseStore().Len()
		if strings.HasSuffix(file.FileName(), "index.ts") {
			index = file
		}
	}
	assert.Assert(t, index != nil)
	assert.Assert(t, index.HasLazyJSDoc(), "TS JSDoc without @see/@link is deferred by the parser")

	ctx := context.Background()
	assert.Equal(t, len(program.GetSemanticDiagnostics(ctx, index)), 0)
	var codes []int32
	for _, d := range program.GetSuggestionDiagnostics(ctx, index) {
		codes = append(codes, d.Code())
	}
	assert.Assert(t, slices.Contains(codes, diagnostics.X_0_is_deprecated.Code()), "expected the @deprecated suggestion, got %v", codes)
	assert.Assert(t, slices.Contains(codes, diagnostics.JSDoc_param_tag_has_name_0_but_there_is_no_parameter_with_that_name.Code()), "expected the @param suggestion, got %v", codes)

	for _, file := range program.GetSourceFiles() {
		assert.Equal(t, file.ParseStore().Len(), lens[file.FileName()], "%s: parse Store grew after bind", file.FileName())
		assert.Assert(t, file.SharedJSDocStore() == nil, "%s: check must not warm the shared JSDoc Store", file.FileName())
	}
}
