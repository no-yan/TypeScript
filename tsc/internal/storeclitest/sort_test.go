package storeclitest

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The Store's sort and deduplication (storecompiler.SortAndDeduplicateDiagnostics,
// translated with store.CompareDiagnostics) order and merge the same
// diagnostics as the Pointer's, config and source files mixed: the same
// diagnostics made on Pointer files and converted to the Store files of the
// same names (store.DiagnosticFromAST), shuffled, sort into the same lines.
func TestSortAndDeduplicateDiagnostics(t *testing.T) {
	texts := map[string]string{
		"/p/tsconfig.json": `{"compilerOptions": {"x": 1}}`,
		"/p/a.ts":          "const a = 1;\nconst b = 2;\n",
		"/p/b.ts":          "let c = 3;\n",
	}
	pointerFiles := map[string]*ast.SourceFile{}
	storeFiles := map[*ast.SourceFile]*store.File{}
	for name, text := range texts {
		opts := ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}
		kind := core.GetScriptKindFromFileName(name)
		pf := parser.ParseSourceFile(opts, text, kind)
		pointerFiles[name] = pf
		storeFiles[pf] = storeparser.ParseSourceFile(opts, text, kind)
	}
	fileOf := func(f *ast.SourceFile) *store.File { return storeFiles[f] }
	at := func(name string, pos, end int, message *diagnostics.Message, args ...any) *ast.Diagnostic {
		var file *ast.SourceFile
		if name != "" {
			file = pointerFiles[name]
		}
		return ast.NewDiagnostic(file, core.NewTextRange(pos, end), message, args...)
	}
	related := func(d *ast.Diagnostic, rs ...*ast.Diagnostic) *ast.Diagnostic { return d.SetRelatedInfo(rs) }

	var pointer []*ast.Diagnostic
	add := func(ds ...*ast.Diagnostic) { pointer = append(pointer, ds...) }
	add(
		// The config's and the sources', and a global one.
		at("/p/tsconfig.json", 20, 23, diagnostics.Unknown_compiler_option_0, "x"),
		at("", -1, -1, diagnostics.Cannot_find_global_type_0, "Array"),
		at("/p/a.ts", 6, 7, diagnostics.Cannot_redeclare_block_scoped_variable_0, "a"),
		at("/p/b.ts", 4, 5, diagnostics.Type_0_is_not_assignable_to_type_1, "number", "string"),
		// Duplicates.
		at("/p/b.ts", 4, 5, diagnostics.Type_0_is_not_assignable_to_type_1, "number", "string"),
		at("/p/a.ts", 6, 7, diagnostics.Cannot_redeclare_block_scoped_variable_0, "a"),
		// The same diagnostic with different related information: merged.
		related(at("/p/a.ts", 19, 20, diagnostics.Duplicate_identifier_0, "b"), at("/p/b.ts", 4, 5, diagnostics.X_0_was_also_declared_here, "b")),
		related(at("/p/a.ts", 19, 20, diagnostics.Duplicate_identifier_0, "b"), at("/p/a.ts", 6, 7, diagnostics.X_0_was_also_declared_here, "b")),
		related(at("/p/a.ts", 19, 20, diagnostics.Duplicate_identifier_0, "b"), at("/p/b.ts", 4, 5, diagnostics.X_0_was_also_declared_here, "b")),
		// The same place, other codes and args.
		at("/p/a.ts", 6, 7, diagnostics.Duplicate_identifier_0, "a"),
		at("/p/a.ts", 6, 7, diagnostics.Duplicate_identifier_0, "A"),
		at("/p/a.ts", 6, 8, diagnostics.Duplicate_identifier_0, "a"),
	)
	describePointer := func(ds []*ast.Diagnostic) []string {
		var out []string
		for _, d := range ds {
			name := ""
			if d.File() != nil {
				name = d.File().FileName()
			}
			out = append(out, fmt.Sprintf("%s %d-%d %d %v related=%d", name, d.Pos(), d.End(), d.Code(), d.MessageArgs(), len(d.RelatedInformation())))
		}
		return out
	}
	describeStore := func(ds []*store.Diagnostic) []string {
		var out []string
		for _, d := range ds {
			name := ""
			if d.File() != nil {
				name = d.File().FileName()
			}
			out = append(out, fmt.Sprintf("%s %d-%d %d %v related=%d", name, d.Pos(), d.End(), d.Code(), d.MessageArgs(), len(d.RelatedInformation())))
		}
		return out
	}
	want := describePointer(compiler.SortAndDeduplicateDiagnostics(pointer))
	if len(want) != 8 {
		t.Fatalf("the Pointer sort gives %d lines, want 8:\n%v", len(want), want)
	}
	rng := rand.New(rand.NewPCG(20261004, 7))
	for range 20 {
		shuffled := slices.Clone(pointer)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := describeStore(storecompiler.SortAndDeduplicateDiagnostics(store.DiagnosticsFromAST(shuffled, fileOf)))
		if !slices.Equal(got, want) {
			t.Fatalf("Store:\n%v\nPointer:\n%v", got, want)
		}
	}
}
