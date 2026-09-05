package parser_test

import (
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
	"gotest.tools/v3/assert"
)

func BenchmarkParse(b *testing.B) {
	for _, f := range fixtures.BenchFixtures {
		b.Run(f.Name(), func(b *testing.B) {
			f.SkipIfNotExist(b)

			fileName := tspath.GetNormalizedAbsolutePath(f.Path(), "/")
			path := tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames())
			sourceText := f.ReadFile(b)
			scriptKind := core.GetScriptKindFromFileName(fileName)

			opts := ast.SourceFileParseOptions{
				FileName: fileName,
				Path:     path,
			}

			for b.Loop() {
				parser.ParseSourceFile(opts, sourceText, scriptKind)
			}
		})
	}
}

func BenchmarkWarmJSDoc(b *testing.B) {
	for _, f := range fixtures.BenchFixtures {
		if f.Name() != "empty.ts" && f.Name() != "checker.ts" && f.Name() != "dom.generated.d.ts" {
			continue
		}
		b.Run(f.Name(), func(b *testing.B) {
			f.SkipIfNotExist(b)
			fileName := tspath.GetNormalizedAbsolutePath(f.Path(), "/")
			path := tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames())
			sourceText := f.ReadFile(b)
			scriptKind := core.GetScriptKindFromFileName(fileName)
			opts := ast.SourceFileParseOptions{FileName: fileName, Path: path}
			files := make([]*ast.SourceFile, b.N)
			for i := range b.N {
				files[i] = parser.ParseSourceFile(opts, sourceText, scriptKind)
			}
			runtime.GC()
			runtime.GC()
			b.ResetTimer()
			for i := range b.N {
				files[i].WarmJSDoc()
			}
		})
	}
}

func TestJSONParseWalksStoreTree(t *testing.T) {
	t.Parallel()
	const sourceText = `{
		"name": "tsgo",
		"values": [1, -2, true, null,],
	}`
	opts := ast.SourceFileParseOptions{FileName: "/config.json", Path: "/config.json"}
	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindJSON)

	assert.Equal(t, 0, len(file.Diagnostics()))
	assert.Equal(t, file.NodeCount, file.ParseStore().Len())
	root := file.ParseRoot()
	assert.Assert(t, !root.IsNil())

	count := 0
	var visit func(ast.Handle)
	visit = func(node ast.Handle) {
		count++
		assert.Assert(t, node.Ref() != 0)
		node.ForEachChild(func(child ast.Handle) bool {
			assert.Equal(t, node, child.Parent())
			visit(child)
			return false
		})
	}
	visit(root)
	assert.Assert(t, count > 0)
}

func TestMalformedJSONHasDiagnostics(t *testing.T) {
	t.Parallel()
	const sourceText = `{"name": }`
	opts := ast.SourceFileParseOptions{FileName: "/config.json", Path: "/config.json"}
	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindJSON)

	assert.Assert(t, len(file.Diagnostics()) > 0)
	assert.Assert(t, file.ParseStore().Len() > 0)
	assert.Assert(t, !file.ParseRoot().IsNil())
}

func TestTypeScriptExpressionParseWalksStoreTree(t *testing.T) {
	t.Parallel()
	const sourceText = `({
		base,
		...extra,
		answer: client?.items[index + 1]?.(arg) ? [1, , ...rest] : { fallback: true },
		pattern: /^user-/gi,
		created: new Registry<Map<string, Entry[]>>().build(),
		message: tag<string>` + "`hello ${++index}: ${items[index++]?.name}`" + `,
		[key + suffix]: value,
	});`
	opts := ast.SourceFileParseOptions{FileName: "/expression.ts", Path: "/expression.ts"}
	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)

	assert.Equal(t, 0, len(file.Diagnostics()))
	assert.Equal(t, file.NodeCount, file.ParseStore().Len())

	seen := make(map[ast.Kind]bool)
	count := 0
	var visit func(ast.Handle)
	visit = func(node ast.Handle) {
		count++
		seen[node.Kind] = true
		node.ForEachChild(func(child ast.Handle) bool {
			assert.Equal(t, node, child.Parent())
			visit(child)
			return false
		})
	}
	visit(file.ParseRoot())
	assert.Assert(t, count > 0)

	for _, kind := range []ast.Kind{
		ast.KindIdentifier,
		ast.KindNumericLiteral,
		ast.KindTrueKeyword,
		ast.KindPropertyAccessExpression,
		ast.KindElementAccessExpression,
		ast.KindCallExpression,
		ast.KindBinaryExpression,
		ast.KindConditionalExpression,
		ast.KindArrayLiteralExpression,
		ast.KindObjectLiteralExpression,
		ast.KindSpreadElement,
		ast.KindSpreadAssignment,
		ast.KindPropertyAssignment,
		ast.KindShorthandPropertyAssignment,
		ast.KindComputedPropertyName,
		ast.KindRegularExpressionLiteral,
		ast.KindNewExpression,
		ast.KindPrefixUnaryExpression,
		ast.KindPostfixUnaryExpression,
		ast.KindTemplateExpression,
		ast.KindTemplateSpan,
		ast.KindTemplateHead,
		ast.KindTemplateMiddle,
		ast.KindTemplateTail,
		ast.KindTaggedTemplateExpression,
		ast.KindTypeReference,
		ast.KindArrayType,
		ast.KindStringKeyword,
	} {
		assert.Assert(t, seen[kind], "missing native kind %s", kind)
	}
}

func TestMalformedTypeScriptExpressionHasDiagnostics(t *testing.T) {
	t.Parallel()
	const sourceText = `value ? yes`
	opts := ast.SourceFileParseOptions{FileName: "/malformed.ts", Path: "/malformed.ts"}
	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)

	assert.Assert(t, len(file.Diagnostics()) > 0)
	assert.Assert(t, file.ParseStore().Len() > 0)
	assert.Assert(t, !file.ParseRoot().IsNil())
}

type parsableFile struct {
	path string
	name string
}

func allParsableFiles(tb testing.TB, root string) iter.Seq[parsableFile] {
	tb.Helper()
	return func(yield func(parsableFile) bool) {
		tb.Helper()
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() || tspath.TryGetExtensionFromPath(path) == "" {
				return nil
			}

			testName, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			testName = filepath.ToSlash(testName)

			if !yield(parsableFile{path, testName}) {
				return filepath.SkipAll
			}
			return nil
		})
		assert.NilError(tb, err)
	}
}

func FuzzParser(f *testing.F) {
	var extensions collections.Set[string]
	for _, es := range tspath.AllSupportedExtensionsWithJson {
		for _, e := range es {
			extensions.Add(e)
		}
	}

	roots := []string{
		filepath.Join(repo.TestDataPath(), "fixtures"),
	}
	for _, root := range roots {
		for file := range allParsableFiles(f, root) {
			sourceText, err := os.ReadFile(file.path)
			assert.NilError(f, err)
			extension := tspath.TryGetExtensionFromPath(file.path)
			f.Add(extension, string(sourceText), false, false)
		}
	}

	testDirs := []string{
		filepath.Join(repo.TestDataPath(), "tests/cases/compiler"),
		filepath.Join(repo.TestDataPath(), "tests/cases/conformance"),
	}

	for _, testDir := range testDirs {
		if _, err := os.Stat(testDir); os.IsNotExist(err) {
			continue
		}

		for file := range allParsableFiles(f, testDir) {
			sourceText, err := os.ReadFile(file.path)
			assert.NilError(f, err)

			type testFile struct {
				content string
				name    string
			}

			testUnits, _, _, _, err := testrunner.ParseTestFilesAndSymlinks(
				string(sourceText),
				file.path,
				func(filename string, content string, fileOptions map[string]string) (testFile, error) {
					return testFile{content: content, name: filename}, nil
				},
			)
			assert.NilError(f, err)

			for _, unit := range testUnits {
				extension := tspath.TryGetExtensionFromPath(unit.name)
				if extension == "" {
					continue
				}
				f.Add(extension, unit.content, false, false)
			}
		}
	}

	f.Fuzz(func(t *testing.T, extension string, sourceText string, externalModuleIndicatorOptionsJSX bool, externalModuleIndicatorOptionsForce bool) {
		if !extensions.Has(extension) {
			t.Skip()
		}

		fileName := "/index" + extension
		path := tspath.Path(fileName)

		opts := ast.SourceFileParseOptions{
			FileName: fileName,
			Path:     path,
			ExternalModuleIndicatorOptions: ast.ExternalModuleIndicatorOptions{
				JSX:   externalModuleIndicatorOptionsJSX,
				Force: externalModuleIndicatorOptionsForce,
			},
		}

		parser.ParseSourceFile(opts, sourceText, core.GetScriptKindFromFileName(fileName))
	})
}

func TestHeritageClauseElementKinds(t *testing.T) {
	t.Parallel()
	sourceText := `
class C extends Base<number> implements Contract<string> {}
interface I extends Parent<boolean> {}
interface Invalid implements Recovery {}
interface MissingExtends extends A. {}
class MissingImplements implements B. {}
`
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/index.ts",
		Path:     "/index.ts",
	}, sourceText, core.ScriptKindTS)

	classDecl := file.ParseRoot().Statements()[0]
	assert.Equal(t, classDecl.Store().ListSlice(classDecl.HeritageClauses())[0].Types()[0].Kind, ast.KindExpressionWithTypeArguments)
	assert.Equal(t, classDecl.Store().ListSlice(classDecl.HeritageClauses())[1].Types()[0].Kind, ast.KindTypeReference)

	interfaceDecl := file.ParseRoot().Statements()[1]
	assert.Equal(t, interfaceDecl.Store().ListSlice(interfaceDecl.HeritageClauses())[0].Types()[0].Kind, ast.KindTypeReference)

	invalidInterfaceDecl := file.ParseRoot().Statements()[2]
	assert.Equal(t, invalidInterfaceDecl.Store().ListSlice(invalidInterfaceDecl.HeritageClauses())[0].Types()[0].Kind, ast.KindExpressionWithTypeArguments)

	missingExtendsDecl := file.ParseRoot().Statements()[3]
	assert.Equal(t, missingExtendsDecl.Store().ListSlice(missingExtendsDecl.HeritageClauses())[0].Types()[0].Kind, ast.KindExpressionWithTypeArguments)

	missingImplementsDecl := file.ParseRoot().Statements()[4]
	assert.Equal(t, missingImplementsDecl.Store().ListSlice(missingImplementsDecl.HeritageClauses())[0].Types()[0].Kind, ast.KindExpressionWithTypeArguments)
}

func TestJSDocImportTypeParentChain(t *testing.T) {
	t.Parallel()
	sourceText := `test("", async function () {
  ;(/** @type {typeof import("a")} */ ({}))
})

test("", async function () {
  ;(/** @type {typeof import("a")} */ a)
})

test("", async function () {
  (/** @type {typeof import("a")} */ ({}))
  ;(/** @type {typeof import("a")} */ ({}))
})

test("", async function () {
  (/** @type {typeof import("a")} */ a)
  ;(/** @type {typeof import("a")} */ a)
})

test("", async function () {
  (/** @type {typeof import("a")} */ ({}))
  ;(/** @type {typeof import("a")} */ ({}))
})
`
	opts := ast.SourceFileParseOptions{
		FileName: "/index.js",
		Path:     "/index.js",
	}

	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindJS)

	for i := 1; i < len(file.ReparsedClones); i++ {
		a, b := file.ReparsedClones[i-1], file.ReparsedClones[i]
		if a.Pos() == b.Pos() && a.End() == b.End() && a.Kind == b.Kind {
			t.Errorf("duplicate ReparsedClones at [%d] and [%d]: %s pos=%d end=%d", i-1, i, a.Kind.String(), a.Pos(), a.End())
		}
	}

	for _, imp := range file.Imports() {
		if ast.GetSourceFileOfNode(imp) == nil {
			t.Errorf("reparsed import at pos=%d has broken parent chain", imp.Pos())
		}
	}
}

func TestJSDocTypeSourceSurvivesReparse(t *testing.T) {
	t.Parallel()
	sourceText := `/**
 * @typedef {(
 *   "a" |
 *   "b"
 * )[]} T
 */
const value = 0;`
	opts := ast.SourceFileParseOptions{
		FileName: "/index.js",
		Path:     "/index.js",
	}

	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindJS)
	var typeAlias ast.Handle
	for _, statement := range file.ParseRoot().Statements() {
		if ast.IsJSTypeAliasDeclaration(statement) {
			typeAlias = statement
			break
		}
	}
	assert.Assert(t, !typeAlias.IsNil())

	jsDocs := typeAlias.JSDoc(file)
	assert.Equal(t, len(jsDocs), 1)
	assert.Assert(t, jsDocs[0].JSDocTags() != 0)
	assert.Equal(t, len((jsDocs[0]).Store().ListSlice(jsDocs[0].JSDocTags())), 1)

	typeExpression := (jsDocs[0]).Store().ListSlice(jsDocs[0].JSDocTags())[0].TypeExpression()
	assert.Assert(t, !typeExpression.IsNil())

	expected := strings.Join([]string{"(", `"a" |`, `"b"`, ")[]"}, core.NewLineKindLF.GetNewLineCharacter())
	tests := []struct {
		name string
		node ast.Handle
	}{
		{name: "original", node: typeExpression.Type()},
		{name: "reparsed", node: typeAlias.Type()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, scanner.GetTextOfNode(test.node), expected)
		})
	}
}

func TestJSDocTypeSourcePropagatesToConstructedReparse(t *testing.T) {
	t.Parallel()
	sourceText := `/**
 * @param {{
 *   value: string
 * }} options
 */
function foo(options) {}`
	opts := ast.SourceFileParseOptions{
		FileName: "/index.js",
		Path:     "/index.js",
	}

	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindJS)
	function := file.ParseRoot().Statements()[0]
	assert.Assert(t, ast.IsFunctionDeclaration(function))
	assert.Equal(t, len(function.Parameters()), 1)

	typeNode := function.Parameters()[0].Type()
	assert.Assert(t, !typeNode.IsNil())
	assert.Assert(t, typeNode.Flags()&ast.NodeFlagsReparsed != 0)

	expected := strings.Join([]string{"{", "value: string", "}"}, core.NewLineKindLF.GetNewLineCharacter())
	assert.Equal(t, scanner.GetTextOfNode(typeNode), expected)
	assert.Equal(t, scanner.GetTokenPosOfNode(typeNode, file, false /*includeJSDoc*/), strings.Index(sourceText, "{{")+1)
}

func TestJSDocTypeCastDoesNotErrorInJavaScript(t *testing.T) {
	t.Parallel()
	sourceText := "const x = /** @type {string} */ ('value');\n"
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/index.js", Path: "/index.js"}, sourceText, core.ScriptKindJS)
	for _, d := range file.JSDiagnostics() {
		assert.Assert(t, d.Code() != 8016, "JSDoc @type cast reported TS8016: %s", d.MessageText())
	}
	stmt := file.ParseRoot().Statements()[0]
	decls := stmt.VariableStatementDeclarationList().VariableDeclarationListDeclarations()
	init := file.ParseStore().ListAt(decls, 0).Initializer()
	assert.Assert(t, ast.IsParenthesizedExpression(init), "got kind %v", init.Kind)
	inner := init.Expression()
	assert.Assert(t, ast.IsAsExpression(inner), "inner kind %v", inner.Kind)
	assert.Assert(t, inner.Type().Flags()&ast.NodeFlagsReparsed != 0)
	assert.Assert(t, ast.IsJSDocTypeAssertion(init))
}

func TestJSDocDeprecatedTagParses(t *testing.T) {
	t.Parallel()
	sourceText := "/** @deprecated */ export const x = 1;\nexport const y = 2;\n"
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}, sourceText, core.ScriptKindTS)
	file.WarmJSDoc()
	stmts := file.ParseRoot().Statements()
	assert.Assert(t, !ast.GetJSDocDeprecatedTag(stmts[0]).IsNil(), "expected @deprecated tag on the export")
	assert.Assert(t, ast.GetJSDocDeprecatedTag(stmts[1]).IsNil(), "unmarked statement should stay cold")
}

func TestSourceFilePositionMapWithNonASCIIStringLiteral(t *testing.T) {
	t.Parallel()
	sourceText := `const x = "─";

namespace N {
  export const y = x;
}
`
	opts := ast.SourceFileParseOptions{
		FileName: "/index.ts",
		Path:     "/index.ts",
	}

	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)

	positionMap := file.GetPositionMap()
	assert.Assert(t, !positionMap.IsAsciiOnly())
	afterBoxDrawingCharacter := strings.Index(sourceText, "─") + len("─")
	assert.Equal(t, positionMap.UTF8ToUTF16(afterBoxDrawingCharacter), afterBoxDrawingCharacter-2)
	assert.Equal(t, positionMap.UTF8ToUTF16(len(sourceText)), len(sourceText)-2)
}

func TestParseStoreNonempty(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{
		FileName: "/index.ts",
		Path:     "/index.ts",
	}
	sourceText := "const x = 1;\n"
	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)
	store := file.ParseStore()
	assert.Assert(t, store != nil, "ParseSourceFile must allocate a Store")
	assert.Assert(t, store.Len() > 0, "Store must be nonempty after parse")
	assert.Equal(t, ast.KindSourceFile, file.ParseRoot().Kind)
}

func TestCollectsGlobalScopeAugmentations(t *testing.T) {
	t.Parallel()

	t.Run("top-level declare global in a module .d.ts", func(t *testing.T) {
		t.Parallel()
		sourceText := "export {};\ndeclare global { var foo: number; }\n"
		opts := ast.SourceFileParseOptions{FileName: "/types.d.ts", Path: "/types.d.ts"}
		file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)
		assert.Equal(t, 1, len(file.ModuleAugmentations))
		name := file.ModuleAugmentations[0]
		assert.Assert(t, ast.IsGlobalScopeAugmentation(name.Parent()))
		assert.Equal(t, "global", name.Text())
	})

	t.Run("nested global inside ambient module", func(t *testing.T) {
		t.Parallel()
		sourceText := "declare module \"node:buffer\" { global { var Buffer: object; } }\n"
		opts := ast.SourceFileParseOptions{FileName: "/buffer.d.ts", Path: "/buffer.d.ts"}
		file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)
		assert.Equal(t, 1, len(file.AmbientModuleNames))
		assert.Equal(t, "node:buffer", file.AmbientModuleNames[0])
		assert.Equal(t, 1, len(file.ModuleAugmentations))
		assert.Assert(t, ast.IsGlobalScopeAugmentation(file.ModuleAugmentations[0].Parent()))
	})
}

func TestIsolatedEntityName(t *testing.T) {
	t.Parallel()
	assert.Assert(t, parser.IsIsolatedEntityName("React.createElement"))
	assert.Assert(t, parser.IsIsolatedEntityName("a"))
	assert.Assert(t, !parser.IsIsolatedEntityName("a + b"))
	assert.Assert(t, !parser.IsIsolatedEntityName(""))

	f := ast.NewFactoryHint(ast.FactoryHooks{}, 16)
	h := parser.ParseIsolatedEntityName(f, "a.b.c")
	assert.Assert(t, !h.IsNil())
	assert.Equal(t, ast.KindQualifiedName, h.Kind)
	assert.Equal(t, "c", h.QualifiedNameRight().IdentifierText())
	assert.Equal(t, f.Store(), h.Store())
	assert.Assert(t, parser.ParseIsolatedEntityName(f, "1foo").IsNil())
}

func TestLazyTSJSDocAllocatesIntoParseStore(t *testing.T) {
	t.Parallel()
	sourceText := `/** docs */
export function f() {}
`
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)
	fn := file.ParseRoot().Statements()[0]
	assert.Equal(t, ast.KindFunctionDeclaration, fn.Kind)
	docs := fn.JSDoc(file)
	assert.Equal(t, 1, len(docs))
	assert.Equal(t, ast.KindJSDoc, docs[0].Kind)
	assert.Equal(t, file.ParseStore(), docs[0].Store())
}

func TestNoParserHandleIsClones(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repo.RootPath(), "internal/parser")
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "func handleIs") {
				t.Errorf("%s:%d: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	assert.NilError(t, err)
}
