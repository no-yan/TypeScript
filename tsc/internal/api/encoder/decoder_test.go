package encoder_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/api/encoder"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"gotest.tools/v3/assert"
)

func parseSourceFile(code string) *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/test.ts",
		Path:     "/test.ts",
	}, code, core.ScriptKindTS)
}

func firstVarDecl(root ast.Handle) ast.Handle {
	declList := root.Statements()[0].VariableStatementDeclarationList()
	return declList.Store().ListAt(declList.VariableDeclarationListDeclarations(), 0)
}

func TestDecodeSourceFile_Basic(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = 1;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)
	root := decoded.ParseRoot()
	assert.Equal(t, root.Kind, ast.KindSourceFile)
	assert.Equal(t, decoded.FileName(), "/test.ts")
	assert.Equal(t, decoded.Text(), "let x = 1;")
	assert.Assert(t, root.StatementList() != 0)
	assert.Assert(t, !root.SourceFileEndOfFileToken().IsNil())
}

func TestDecodeSourceFile_Statements(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let a = 1;\nlet b = 2;\nlet c = 3;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)
	stmts := decoded.ParseRoot().Statements()
	assert.Equal(t, len(stmts), 3)
	for i, stmt := range stmts {
		assert.Equal(t, stmt.Kind, ast.KindVariableStatement, "statement %d", i)
	}
}

func TestDecodeSourceFile_VariableDeclaration(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = 1;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	varStmt := decoded.ParseRoot().Statements()[0]
	declList := varStmt.VariableStatementDeclarationList()
	assert.Assert(t, !declList.IsNil())
	decls := declList.VariableDeclarationListDeclarations()
	assert.Equal(t, declList.Store().ListLen(decls), 1)

	decl := declList.Store().ListAt(decls, 0)
	assert.Equal(t, decl.Name().Kind, ast.KindIdentifier)
	assert.Equal(t, decl.Name().IdentifierText(), "x")
	assert.Assert(t, !decl.Initializer().IsNil())
	assert.Equal(t, decl.Initializer().Kind, ast.KindNumericLiteral)
	assert.Equal(t, decl.Initializer().NumericLiteralText(), "1")
}

func TestDecodeSourceFile_VariableDeclarationListFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     string
		expected ast.NodeFlags
	}{
		{"const", "const x = 1;", ast.NodeFlagsConst},
		{"let", "let x = 1;", ast.NodeFlagsLet},
		{"var", "var x = 1;", ast.NodeFlagsNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sf := parseSourceFile(tt.code)
			buf, _, err := encoder.EncodeSourceFile(sf)
			assert.NilError(t, err)

			decoded, err := encoder.DecodeSourceFile(buf)
			assert.NilError(t, err)

			declList := decoded.ParseRoot().Statements()[0].VariableStatementDeclarationList()
			got := declList.Flags() & (ast.NodeFlagsLet | ast.NodeFlagsConst)
			assert.Equal(t, got, tt.expected, "flags for %q: got %d, want %d", tt.code, got, tt.expected)
		})
	}
}

func TestDecodeSourceFile_FunctionDeclaration(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("function add(a: number, b: number): number { return a + b; }")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	funcDecl := decoded.ParseRoot().Statements()[0]
	assert.Assert(t, !funcDecl.Name().IsNil())
	assert.Equal(t, funcDecl.Name().IdentifierText(), "add")
	assert.Assert(t, funcDecl.ParameterList() != 0)
	assert.Equal(t, len(funcDecl.Parameters()), 2)
	assert.Assert(t, !funcDecl.Type().IsNil())
	assert.Assert(t, !funcDecl.Body().IsNil())

	param0 := funcDecl.Parameters()[0]
	assert.Equal(t, param0.Name().IdentifierText(), "a")
	assert.Assert(t, !param0.Type().IsNil())
}

func TestDecodeSourceFile_ImportDeclaration(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile(`import { bar } from "bar";`)
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	imp := decoded.ParseRoot().Statements()[0]
	assert.Assert(t, !imp.ImportClause().IsNil())
	assert.Assert(t, !imp.ModuleSpecifier().IsNil())
	assert.Equal(t, imp.ModuleSpecifier().StringLiteralText(), "bar")

	clause := imp.ImportClause()
	assert.Assert(t, !clause.NamedBindings().IsNil())
	namedImports := clause.NamedBindings()
	assert.Assert(t, namedImports.ElementList() != 0)
	assert.Equal(t, len(namedImports.Elements()), 1)
	spec := namedImports.Elements()[0]
	assert.Equal(t, spec.Name().IdentifierText(), "bar")
}

func TestDecodeSourceFile_IfStatement(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("if (true) { } else { }")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	ifStmt := decoded.ParseRoot().Statements()[0]
	assert.Assert(t, !ifStmt.Expression().IsNil())
	assert.Assert(t, !ifStmt.ThenStatement().IsNil())
	assert.Assert(t, !ifStmt.ElseStatement().IsNil())
	assert.Equal(t, ifStmt.ThenStatement().Kind, ast.KindBlock)
	assert.Equal(t, ifStmt.ElseStatement().Kind, ast.KindBlock)
}

func TestDecodeSourceFile_TemplateExpression(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = `hello ${name} world`;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	varDecl := firstVarDecl(decoded.ParseRoot())
	tmplExpr := varDecl.Initializer()
	assert.Assert(t, !tmplExpr.Head().IsNil())
	assert.Equal(t, tmplExpr.Head().TemplateHeadText(), "hello ")
	assert.Equal(t, len(tmplExpr.TemplateSpans()), 1)

	span := tmplExpr.TemplateSpans()[0]
	assert.Assert(t, !span.Expression().IsNil())
	assert.Equal(t, span.Expression().Kind, ast.KindIdentifier)
	assert.Assert(t, !span.Literal().IsNil())
	assert.Equal(t, span.Literal().TemplateTailText(), " world")
}

func TestDecodeSourceFile_ExportModifier(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("export function foo() {}")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	funcDecl := decoded.ParseRoot().Statements()[0]
	assert.Assert(t, funcDecl.Modifiers() != 0)
	assert.Equal(t, funcDecl.Store().ListLen(funcDecl.Modifiers()), 1)
	assert.Equal(t, funcDecl.Store().ListAt(funcDecl.Modifiers(), 0).Kind, ast.KindExportKeyword)
}

func TestDecodeSourceFile_Positions(t *testing.T) {
	t.Parallel()
	code := "let x = 1;"
	sf := parseSourceFile(code)
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	root := decoded.ParseRoot()
	assert.Equal(t, root.Pos(), 0)
	assert.Equal(t, root.End(), len(code))
}

func TestDecodeSourceFile_ClassDeclaration(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("class Foo { bar(): void {} }")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	classDecl := decoded.ParseRoot().Statements()[0]
	assert.Assert(t, !classDecl.Name().IsNil())
	assert.Equal(t, classDecl.Name().IdentifierText(), "Foo")
	assert.Assert(t, classDecl.MemberList() != 0)
	assert.Equal(t, len(classDecl.Members()), 1)
	assert.Equal(t, classDecl.Members()[0].Kind, ast.KindMethodDeclaration)
}

func TestDecodeNodes_SubtreeRoundTrip(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("function greet(name: string) { return `Hello, ${name}!`; }")

	var funcNode ast.Handle
	visitor := &ast.HandleVisitor{}
	visitor.Visit = func(node ast.Handle) ast.Handle {
		if node.Kind == ast.KindFunctionDeclaration && funcNode.IsNil() {
			funcNode = node
		}
		return node
	}
	visitor.VisitEachChild(sf.ParseRoot())
	assert.Assert(t, !funcNode.IsNil())

	buf, _, err := encoder.EncodeNode(funcNode, sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeNodes(buf)
	assert.NilError(t, err)

	assert.Equal(t, decoded.Kind, ast.KindFunctionDeclaration)
	assert.Assert(t, !decoded.Name().IsNil())
	assert.Equal(t, decoded.Name().IdentifierText(), "greet")
	assert.Assert(t, decoded.ParameterList() != 0)
	assert.Equal(t, len(decoded.Parameters()), 1)
	assert.Assert(t, !decoded.Body().IsNil())
}

func TestDecodeSourceFile_BinaryExpression(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = 1 + 2;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	decl := firstVarDecl(decoded.ParseRoot())
	binExpr := decl.Initializer()
	assert.Assert(t, !binExpr.Left().IsNil())
	assert.Assert(t, !binExpr.Right().IsNil())
	assert.Assert(t, !binExpr.OperatorToken().IsNil())
	assert.Equal(t, binExpr.Left().Kind, ast.KindNumericLiteral)
	assert.Equal(t, binExpr.Right().Kind, ast.KindNumericLiteral)
}

func TestDecodeSourceFile_KeywordExpressions(t *testing.T) {
	t.Parallel()
	// "this" must decode as KeywordExpression, not Token, or the printer panics
	sf := parseSourceFile("const x = this;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	decl := firstVarDecl(decoded.ParseRoot())
	thisExpr := decl.Initializer()
	assert.Equal(t, thisExpr.Kind, ast.KindThisKeyword)
	assert.Assert(t, !thisExpr.IsNil())
}

func TestDecodeSourceFile_EmptyModuleBlock(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("namespace N { }")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	mod := decoded.ParseRoot().Statements()[0]
	assert.Assert(t, !mod.Body().IsNil())
	block := mod.Body()
	// Statements list must be present even when empty, otherwise the printer panics
	assert.Assert(t, block.StatementList() != 0)
	assert.Equal(t, len(block.Statements()), 0)
}

func TestDecodeSourceFile_EmptyBlockAndParams(t *testing.T) {
	t.Parallel()
	// Empty blocks and parameter lists must decode with non-nil lists (not missing),
	// matching parser behavior. Previously the decoder left them missing, crashing the printer.
	sf := parseSourceFile("function foo() {}")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	funcDecl := decoded.ParseRoot().Statements()[0]
	assert.Assert(t, funcDecl.ParameterList() != 0, "FunctionDeclaration.Parameters must be present for foo()")
	assert.Equal(t, len(funcDecl.Parameters()), 0)
	assert.Assert(t, !funcDecl.Body().IsNil())
	block := funcDecl.Body()
	assert.Assert(t, block.StatementList() != 0, "Block.Statements must be present for empty blocks")
	assert.Equal(t, len(block.Statements()), 0)
}

func TestDecodeSourceFile_ArrowFunctionEmptyParams(t *testing.T) {
	t.Parallel()
	// `() => {}` must decode with present Parameters (empty list),
	// matching parser behavior. Previously the decoder left it missing, crashing the printer.
	sf := parseSourceFile("const f = () => {};")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	decl := firstVarDecl(decoded.ParseRoot())
	arrow := decl.Initializer()
	assert.Assert(t, arrow.ParameterList() != 0, "ArrowFunction.Parameters must be present for () => {}")
	assert.Equal(t, len(arrow.Parameters()), 0)
	assert.Assert(t, !arrow.Body().IsNil())
	block := arrow.Body()
	assert.Assert(t, block.StatementList() != 0, "Block.Statements must be present for empty body")
	assert.Equal(t, len(block.Statements()), 0)
}

func TestDecodeSourceFile_FunctionExpressionEmptyParams(t *testing.T) {
	t.Parallel()
	// `function() {}` must decode with present Parameters (empty list).
	sf := parseSourceFile("const f = function() {};")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	decl := firstVarDecl(decoded.ParseRoot())
	funcExpr := decl.Initializer()
	assert.Assert(t, funcExpr.ParameterList() != 0, "FunctionExpression.Parameters must be present for function() {}")
	assert.Equal(t, len(funcExpr.Parameters()), 0)
}

func TestDecodeSourceFile_PostfixUnaryOperator(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let i = 0; i++;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	exprStmt := decoded.ParseRoot().Statements()[1]
	postfix := exprStmt.Expression()
	assert.Equal(t, postfix.PostfixUnaryExpressionOperator(), ast.KindPlusPlusToken)
	assert.Equal(t, postfix.Operand().Kind, ast.KindIdentifier)
}

func TestDecodeSourceFile_PrefixUnaryOperator(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = true; !x;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	exprStmt := decoded.ParseRoot().Statements()[1]
	prefix := exprStmt.Expression()
	assert.Equal(t, prefix.PrefixUnaryExpressionOperator(), ast.KindExclamationToken)
	assert.Equal(t, prefix.Operand().Kind, ast.KindIdentifier)
}

func TestDecodeSourceFile_PostfixDecrement(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let n = 5; n--;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	exprStmt := decoded.ParseRoot().Statements()[1]
	postfix := exprStmt.Expression()
	assert.Equal(t, postfix.PostfixUnaryExpressionOperator(), ast.KindMinusMinusToken)
}

func BenchmarkDecodeSourceFile(b *testing.B) {
	filePath := filepath.Join(repo.TestDataPath(), "fixtures/compiler/checker.ts")
	fileContent, err := os.ReadFile(filePath)
	assert.NilError(b, err)
	code := string(fileContent)
	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/checker.ts",
		Path:     "/checker.ts",
	}, code, core.ScriptKindTS)

	buf, _, err := encoder.EncodeSourceFile(sourceFile)
	assert.NilError(b, err)

	b.Run("parse", func(b *testing.B) {
		for b.Loop() {
			parser.ParseSourceFile(ast.SourceFileParseOptions{
				FileName: "/checker.ts",
				Path:     "/checker.ts",
			}, code, core.ScriptKindTS)
		}
	})

	b.Run("decode", func(b *testing.B) {
		for b.Loop() {
			_, decodeErr := encoder.DecodeSourceFile(buf)
			assert.NilError(b, decodeErr)
		}
	})
}
