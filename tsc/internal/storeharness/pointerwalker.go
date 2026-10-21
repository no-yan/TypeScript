package storeharness

import (
	"fmt"
	"slices"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

// The Pointer's type and symbol walker (6.3): the functions of
// internal/testutil/tsbaseline/type_symbol_baseline.go copied, with the
// driver's checker for the program's (tsbaseline is not exported, and is not
// changed). The copy is held to the committed baselines by
// TestPointerWalkerBaselines.

type pointerWalker struct {
	checker   *checker.Checker
	hadErrors bool
}

func (w *pointerWalker) walk(file *ast.SourceFile, isSymbolWalk bool) []walkRecord {
	var records []walkRecord
	for _, n := range forEachASTNode(file.AsNode()) {
		if pointerWalked(n, isSymbolWalk) {
			records = append(records, w.writeTypeOrSymbol(file, n, isSymbolWalk))
		}
	}
	return records
}

// pointerWalked is whether the walk writes a record for n.
func pointerWalked(n *ast.Node, isSymbolWalk bool) bool {
	return ast.IsExpressionNode(n) || n.Kind == ast.KindIdentifier || ast.IsDeclarationName(n) ||
		ast.IsQualifiedName(n) && ast.IsNameOfHeritageClauseTypeReference(n) && (isSymbolWalk || ast.IsQualifiedName(n.Parent))
}

func forEachASTNode(node *ast.Node) []*ast.Node {
	var result []*ast.Node
	work := []*ast.Node{node}

	var resChildren []*ast.Node
	addChild := func(child *ast.Node) bool {
		resChildren = append(resChildren, child)
		return false
	}

	for len(work) > 0 {
		elem := work[len(work)-1]
		work = work[:len(work)-1]
		if elem.Flags&ast.NodeFlagsReparsed == 0 || elem.Kind == ast.KindAsExpression || elem.Kind == ast.KindSatisfiesExpression ||
			((elem.Parent.Kind == ast.KindSatisfiesExpression || elem.Parent.Kind == ast.KindAsExpression) && elem == elem.Parent.Expression()) {
			if elem.Flags&ast.NodeFlagsReparsed == 0 || elem.Parent.Kind == ast.KindAsExpression || elem.Parent.Kind == ast.KindSatisfiesExpression {
				result = append(result, elem)
			}
			elem.ForEachChild(addChild)
			slices.Reverse(resChildren)
			work = append(work, resChildren...)
			resChildren = resChildren[:0]
		}
	}
	return result
}

// typeOf is the type walk's type of node: ok is false where the walk gives
// nothing, t is nil where it gives the intrinsic name of any (without the
// node builder), and the type to print otherwise.
func (w *pointerWalker) typeOf(file *ast.SourceFile, node *ast.Node) (t *checker.Type, intrinsic string, ok bool) {
	fileChecker := w.checker
	// Don't try to get the type of something that's already a type.
	// Exception for `T` in `type T = something` because that may evaluate to some interesting type.
	if ast.IsPartOfTypeNode(node) ||
		(node.Kind == ast.KindAsExpression || node.Kind == ast.KindSatisfiesExpression) && node.Type().Flags&ast.NodeFlagsReparsed != 0 ||
		ast.IsIdentifier(node) &&
			(ast.GetMeaningFromDeclaration(node.Parent)&ast.SemanticMeaningValue) == 0 &&
			!(ast.IsTypeOrJSTypeAliasDeclaration(node.Parent) && node == node.Parent.Name()) {
		return nil, "", false
	}

	if ast.IsOmittedExpression(node) {
		return nil, "", false
	}

	// Workaround to ensure we output 'C' instead of 'typeof C' for base class expressions
	if ast.IsExpressionWithTypeArgumentsInClassExtendsClause(node.Parent) {
		t = fileChecker.GetTypeAtLocation(node.Parent)
	}
	if t == nil || checker.IsTypeAny(t) {
		t = fileChecker.GetTypeAtLocation(node)
	}
	if !w.hadErrors &&
		checker.IsTypeAny(t) &&
		!ast.IsBindingElement(node.Parent) &&
		!ast.IsPropertyAccessOrQualifiedName(node.Parent) &&
		!ast.IsLabelName(node) &&
		!ast.IsGlobalScopeAugmentation(node.Parent) &&
		!ast.IsMetaProperty(node.Parent) &&
		!pointerIsImportStatementName(node) &&
		!pointerIsExportStatementName(node) &&
		!pointerIsIntrinsicJsxTag(node, file) {
		return nil, t.AsIntrinsicType().IntrinsicName(), true
	}
	return t, "", true
}

// pointerPrintType is the type walk's string of t at node: the node builder,
// the printer and the writer's string, a copy (the writer grew to the file's
// length in printer.Write).
func pointerPrintType(fileChecker *checker.Checker, t *checker.Type, file *ast.SourceFile, node *ast.Node) string {
	ctx := printer.NewEmitContext()
	builder := checker.NewNodeBuilder(fileChecker, ctx)
	typeFormatFlags := checker.TypeFormatFlagsNoTruncation | checker.TypeFormatFlagsAllowUniqueESSymbolType | checker.TypeFormatFlagsGenerateNamesForShadowedTypeParams
	typeNode := builder.TypeToTypeNode(t, node.Parent, nodebuilder.Flags(typeFormatFlags&checker.TypeFormatFlagsNodeBuilderFlagsMask)|nodebuilder.FlagsIgnoreErrors, nodebuilder.InternalFlagsAllowUnresolvedNames, nil)
	if ast.IsIdentifier(node) && ast.IsTypeAliasDeclaration(node.Parent) && node.Parent.Name() == node && ast.IsIdentifier(typeNode) && typeNode.Text() == node.Text() {
		// for a complex type alias `type T = ...`, showing "T : T" isn't very helpful for type tests. When the type produced is the same as
		// the name of the type alias, recreate the type string without reusing the alias name
		typeNode = builder.TypeToTypeNode(t, node.Parent, nodebuilder.Flags((typeFormatFlags|checker.TypeFormatFlagsInTypeAlias)&checker.TypeFormatFlagsNodeBuilderFlagsMask)|nodebuilder.FlagsIgnoreErrors, nodebuilder.InternalFlagsAllowUnresolvedNames, nil)
	}

	writer := printer.NewTextWriter("", 0)
	printer := printer.NewPrinter(printer.PrinterOptions{RemoveComments: true}, printer.PrintHandlers{}, ctx)
	printer.Write(typeNode, file, writer, nil)
	return strings.Clone(writer.String())
}

func (w *pointerWalker) writeTypeOrSymbol(file *ast.SourceFile, node *ast.Node, isSymbolWalk bool) walkRecord {
	record := walkRecord{
		pos:  node.Pos(),
		kind: node.Kind,
		text: scanner.GetSourceTextOfNodeFromSourceFile(file, node, false /*includeTrivia*/),
	}
	fileChecker := w.checker

	if !isSymbolWalk {
		t, intrinsic, ok := w.typeOf(file, node)
		if !ok {
			return record
		}
		typeString := intrinsic
		if t != nil {
			typeString = pointerPrintType(fileChecker, t, file, node)
		}
		record.value, record.present = typeString, true
		return record
	}

	symbol := fileChecker.GetSymbolAtLocation(node)
	if symbol == nil {
		return record
	}

	var symbolString strings.Builder
	symbolString.Grow(256)
	symbolString.WriteString("Symbol(")
	symbolString.WriteString(ast.EscapeAllInternalSymbolNames(fileChecker.SymbolToStringEx(symbol, node.Parent, ast.SymbolFlagsNone, checker.SymbolFormatFlagsAllowAnyNodeKind)))
	count := 0
	for _, declaration := range symbol.Declarations {
		if count >= 5 {
			fmt.Fprintf(&symbolString, " ... and %d more", len(symbol.Declarations)-count)
			break
		}
		count++
		symbolString.WriteString(", ")

		declSourceFile := ast.GetSourceFileOfNode(declaration)
		declLine, declChar := scanner.GetECMALineAndUTF16CharacterOfPosition(declSourceFile, declaration.Pos())
		fileName := declSourceFile.FileName().BaseName()
		symbolString.WriteString("Decl(")
		symbolString.WriteString(fileName)
		symbolString.WriteString(", ")
		if isDefaultLibraryFile(fileName) {
			symbolString.WriteString("--, --)")
		} else {
			fmt.Fprintf(&symbolString, "%d, %d)", declLine, int(declChar))
		}
	}
	symbolString.WriteString(")")
	record.value, record.present = symbolString.String(), true
	record.symbol = symbol
	for _, d := range symbol.Declarations {
		var path string
		if f := ast.GetSourceFileOfNode(d); f != nil {
			path = string(f.PathKey())
		}
		record.decls = append(record.decls, declKey{path: path, pos: d.Pos(), end: d.End(), kind: d.Kind})
	}
	return record
}

func pointerIsImportStatementName(node *ast.Node) bool {
	if ast.IsImportSpecifier(node.Parent) && (node == node.Parent.Name() || node == node.Parent.PropertyName()) {
		return true
	}
	if ast.IsImportClause(node.Parent) && node == node.Parent.Name() {
		return true
	}
	if ast.IsImportEqualsDeclaration(node.Parent) && node == node.Parent.Name() {
		return true
	}
	return false
}

func pointerIsExportStatementName(node *ast.Node) bool {
	if ast.IsExportAssignment(node.Parent) && node == node.Parent.Expression() {
		return true
	}
	if ast.IsExportSpecifier(node.Parent) && (node == node.Parent.Name() || node == node.Parent.PropertyName()) {
		return true
	}
	return false
}

func pointerIsIntrinsicJsxTag(node *ast.Node, sourceFile *ast.SourceFile) bool {
	if !(ast.IsJsxOpeningElement(node.Parent) || ast.IsJsxClosingElement(node.Parent) || ast.IsJsxSelfClosingElement(node.Parent)) {
		return false
	}
	if node.Parent.TagName() != node {
		return false
	}
	text := scanner.GetSourceTextOfNodeFromSourceFile(sourceFile, node, false /*includeTrivia*/)
	return scanner.IsIntrinsicJsxName(text)
}
