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

func TestDecodeSourceFile_Basic(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = 1;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)
	assert.Equal(t, decoded.AsNode().Kind, ast.KindSourceFile)
	assert.Equal(t, decoded.FileName(), "/test.ts")
	assert.Equal(t, decoded.Text(), "let x = 1;")
	assert.Assert(t, decoded.Statements != nil)
	assert.Assert(t, decoded.EndOfFileToken != nil)
}

func TestDecodeSourceFile_Statements(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let a = 1;\nlet b = 2;\nlet c = 3;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)
	assert.Equal(t, len(decoded.Statements()), 3)
	for i, stmt := range decoded.Statements() {
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

	varStmt := decoded.Statements()[0]
	assert.Assert(t, varStmt.DeclarationList != nil)
	declList := varStmt.DeclarationList
	assert.Assert(t, declList.Declarations != nil)
	assert.Equal(t, len(declList.Declarations()), 1)

	decl := declList.Declarations()[0]
	assert.Equal(t, decl.Name().Kind, ast.KindIdentifier)
	assert.Equal(t, decl.Name().IdentifierText(), "x")
	assert.Assert(t, decl.Initializer != nil)
	assert.Equal(t, decl.Initializer.Kind, ast.KindNumericLiteral)
	assert.Equal(t, decl.Initializer.NumericLiteralText(), "1")
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

			declList := decoded.Statements()[0].VariableStatementDeclarationList()
			got := declList.Flags & (ast.NodeFlagsLet | ast.NodeFlagsConst)
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

	funcDecl := decoded.Statements()[0]
	assert.Assert(t, funcDecl.Name() != nil)
	assert.Equal(t, funcDecl.Name().IdentifierText(), "add")
	assert.Assert(t, funcDecl.Parameters != nil)
	assert.Equal(t, len(funcDecl.Parameters()), 2)
	assert.Assert(t, funcDecl.Type != nil)
	assert.Assert(t, funcDecl.Body != nil)

	param0 := funcDecl.Parameters()[0]
	assert.Equal(t, param0.Name().IdentifierText(), "a")
	assert.Assert(t, param0.Type != nil)
}

func TestDecodeSourceFile_ImportDeclaration(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile(`import { bar } from "bar";`)
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	imp := decoded.Statements()[0]
	assert.Assert(t, imp.ImportClause != nil)
	assert.Assert(t, imp.ModuleSpecifier != nil)
	assert.Equal(t, imp.ModuleSpecifier.StringLiteralText(), "bar")

	clause := imp.ImportClause
	assert.Assert(t, clause.NamedBindings != nil)
	namedImports := clause.NamedBindings
	assert.Assert(t, namedImports.Elements != nil)
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

	ifStmt := decoded.Statements()[0]
	assert.Assert(t, ifStmt.Expression != nil)
	assert.Assert(t, ifStmt.ThenStatement != nil)
	assert.Assert(t, ifStmt.ElseStatement != nil)
	assert.Equal(t, ifStmt.ThenStatement.Kind, ast.KindBlock)
	assert.Equal(t, ifStmt.ElseStatement.Kind, ast.KindBlock)
}

func TestDecodeSourceFile_TemplateExpression(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = `hello ${name} world`;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	varDecl := (decoded.Statements()[0].VariableStatementDeclarationList()).Store().ListSlice(decoded.Statements()[0].VariableStatementDeclarationList().VariableDeclarationListDeclarations())[0]
	tmplExpr := varDecl.Initializer
	assert.Assert(t, tmplExpr.Head != nil)
	assert.Equal(t, tmplExpr.Head.TemplateHeadText(), "hello ")
	assert.Assert(t, tmplExpr.TemplateSpans != nil)
	assert.Equal(t, len(tmplExpr.TemplateSpans()), 1)

	span := tmplExpr.TemplateSpans()[0]
	assert.Assert(t, span.Expression != nil)
	assert.Equal(t, span.Expression.Kind, ast.KindIdentifier)
	assert.Assert(t, span.Literal != nil)
	assert.Equal(t, span.Literal.TemplateTailText(), " world")
}

func TestDecodeSourceFile_ExportModifier(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("export function foo() {}")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	funcDecl := decoded.Statements()[0]
	assert.Assert(t, funcDecl.Modifiers() != nil)
	assert.Equal(t, len(funcDecl.Store().ListSlice(funcDecl.Modifiers())), 1)
	assert.Equal(t, funcDecl.Store().ListSlice(funcDecl.Modifiers())[0].Kind, ast.KindExportKeyword)
}

func TestDecodeSourceFile_Positions(t *testing.T) {
	t.Parallel()
	code := "let x = 1;"
	sf := parseSourceFile(code)
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	assert.Equal(t, decoded.AsNode().Pos(), 0)
	assert.Equal(t, decoded.AsNode().End(), len(code))
}

func TestDecodeSourceFile_ClassDeclaration(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("class Foo { bar(): void {} }")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	classDecl := decoded.Statements()[0]
	assert.Assert(t, classDecl.Name() != nil)
	assert.Equal(t, classDecl.Name().IdentifierText(), "Foo")
	assert.Assert(t, classDecl.Members != nil)
	assert.Equal(t, len(classDecl.Members()), 1)
	assert.Equal(t, classDecl.Members()[0].Kind, ast.KindMethodDeclaration)
}

func TestDecodeNodes_SubtreeRoundTrip(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("function greet(name: string) { return `Hello, ${name}!`; }")

	var funcNode *ast.Node
	visitor := &ast.NodeVisitor{}
	visitor.Visit = func(node *ast.Node) *ast.Node {
		if node.Kind == ast.KindFunctionDeclaration && funcNode == nil {
			funcNode = node
		}
		return node
	}
	visitor.VisitEachChild(sf.AsNode())
	assert.Assert(t, funcNode != nil)

	buf, _, err := encoder.EncodeNode(funcNode, sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeNodes(buf)
	assert.NilError(t, err)

	assert.Equal(t, decoded.Kind, ast.KindFunctionDeclaration)
	funcDecl := decoded
	assert.Assert(t, funcDecl.Name() != nil)
	assert.Equal(t, funcDecl.Name().IdentifierText(), "greet")
	assert.Assert(t, funcDecl.Parameters != nil)
	assert.Equal(t, len(funcDecl.Parameters()), 1)
	assert.Assert(t, funcDecl.Body != nil)
}

func TestDecodeSourceFile_BinaryExpression(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = 1 + 2;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	decl := (decoded.Statements()[0].VariableStatementDeclarationList()).Store().ListSlice(decoded.Statements()[0].VariableStatementDeclarationList().VariableDeclarationListDeclarations())[0]
	binExpr := decl.Initializer
	assert.Assert(t, binExpr.Left != nil)
	assert.Assert(t, binExpr.Right != nil)
	assert.Assert(t, binExpr.OperatorToken != nil)
	assert.Equal(t, binExpr.Left.Kind, ast.KindNumericLiteral)
	assert.Equal(t, binExpr.Right.Kind, ast.KindNumericLiteral)
}

func TestDecodeSourceFile_KeywordExpressions(t *testing.T) {
	t.Parallel()
	// "this" must decode as KeywordExpression, not Token, or the printer panics
	sf := parseSourceFile("const x = this;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	// Navigate: const x = this -> VariableStatement -> declaration -> initializer
	decl := (decoded.Statements()[0].VariableStatementDeclarationList()).Store().ListSlice(decoded.Statements()[0].VariableStatementDeclarationList().VariableDeclarationListDeclarations())[0]
	thisExpr := decl.Initializer
	assert.Equal(t, thisExpr.Kind, ast.KindThisKeyword)
	// This would panic if decoded as Token instead of KeywordExpression
	assert.Assert(t, thisExpr != nil)
}

func TestDecodeSourceFile_EmptyModuleBlock(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("namespace N { }")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	// Navigate: namespace N { } -> ModuleDeclaration -> ModuleBlock
	mod := decoded.Statements()[0]
	assert.Assert(t, mod.Body != nil)
	block := mod.Body
	// Statements must be non-nil even when empty, otherwise the printer panics
	assert.Assert(t, block.Statements != nil)
	assert.Equal(t, len(block.Statements()), 0)
}

func TestDecodeSourceFile_EmptyBlockAndParams(t *testing.T) {
	t.Parallel()
	// Empty blocks and parameter lists must decode with non-nil NodeLists (not nil),
	// matching parser behavior. Previously the decoder left them nil, crashing the printer.
	sf := parseSourceFile("function foo() {}")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	funcDecl := decoded.Statements()[0]
	assert.Assert(t, funcDecl.Parameters != nil, "FunctionDeclaration.Parameters must be non-nil for foo()")
	assert.Equal(t, len(funcDecl.Parameters()), 0)
	assert.Assert(t, funcDecl.Body != nil)
	block := funcDecl.Body
	assert.Assert(t, block.Statements != nil, "Block.Statements must be non-nil for empty blocks")
	assert.Equal(t, len(block.Statements()), 0)
}

func TestDecodeSourceFile_ArrowFunctionEmptyParams(t *testing.T) {
	t.Parallel()
	// `() => {}` must decode with non-nil Parameters (empty NodeList),
	// matching parser behavior. Previously the decoder left it nil, crashing the printer.
	sf := parseSourceFile("const f = () => {};")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	decl := (decoded.Statements()[0].VariableStatementDeclarationList()).Store().ListSlice(decoded.Statements()[0].VariableStatementDeclarationList().VariableDeclarationListDeclarations())[0]
	arrow := decl.Initializer
	assert.Assert(t, arrow.Parameters != nil, "ArrowFunction.Parameters must be non-nil for () => {}")
	assert.Equal(t, len(arrow.Parameters()), 0)
	assert.Assert(t, arrow.Body != nil)
	block := arrow.Body
	assert.Assert(t, block.Statements != nil, "Block.Statements must be non-nil for empty body")
	assert.Equal(t, len(block.Statements()), 0)
}

func TestDecodeSourceFile_FunctionExpressionEmptyParams(t *testing.T) {
	t.Parallel()
	// `function() {}` must decode with non-nil Parameters (empty NodeList).
	sf := parseSourceFile("const f = function() {};")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	decl := (decoded.Statements()[0].VariableStatementDeclarationList()).Store().ListSlice(decoded.Statements()[0].VariableStatementDeclarationList().VariableDeclarationListDeclarations())[0]
	funcExpr := decl.Initializer
	assert.Assert(t, funcExpr.Parameters != nil, "FunctionExpression.Parameters must be non-nil for function() {}")
	assert.Equal(t, len(funcExpr.Parameters()), 0)
}

func TestDecodeSourceFile_PostfixUnaryOperator(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let i = 0; i++;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	exprStmt := decoded.Statements()[1]
	postfix := exprStmt.Expression
	assert.Equal(t, postfix.Operator, ast.KindPlusPlusToken)
	assert.Equal(t, postfix.Operand.Kind, ast.KindIdentifier)
}

func TestDecodeSourceFile_PrefixUnaryOperator(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let x = true; !x;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	exprStmt := decoded.Statements()[1]
	prefix := exprStmt.Expression
	assert.Equal(t, prefix.Operator, ast.KindExclamationToken)
	assert.Equal(t, prefix.Operand.Kind, ast.KindIdentifier)
}

func TestDecodeSourceFile_PostfixDecrement(t *testing.T) {
	t.Parallel()
	sf := parseSourceFile("let n = 5; n--;")
	buf, _, err := encoder.EncodeSourceFile(sf)
	assert.NilError(t, err)

	decoded, err := encoder.DecodeSourceFile(buf)
	assert.NilError(t, err)

	exprStmt := decoded.Statements()[1]
	postfix := exprStmt.Expression
	assert.Equal(t, postfix.Operator, ast.KindMinusMinusToken)
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
