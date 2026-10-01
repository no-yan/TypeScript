package storeharness

import (
	"fmt"
	"slices"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storeprinter"
	"github.com/microsoft/TypeScript/tsc/internal/storescanner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The type and symbol walker of the Store (6.3), written by hand from
// internal/testutil/tsbaseline/type_symbol_baseline.go, rule by rule: it is
// not translated, so that it does not share the translation's blind spots
// with the checker it observes (7d design 2.7, rule 6). Its Pointer
// counterpart is pointerwalker.go.

// walkRecord is a node the walker considers (visitNode) and what
// writeTypeOrSymbol gives for it: present is false where it gives nothing.
// The symbol walk keeps the symbol, for the correspondence (6.4), and its
// declarations.
type walkRecord struct {
	pos     int
	kind    ast.Kind
	text    string
	value   string
	present bool
	symbol  any
	decls   []declKey
}

// declKey is a declaration of a symbol: its file, range and kind.
type declKey struct {
	path     string
	pos, end int
	kind     ast.Kind
}

func (d declKey) String() string {
	return fmt.Sprintf("%s %d-%d %v", d.path, d.pos, d.end, d.kind)
}

type storeWalker struct {
	checker   *storechecker.Checker
	hadErrors bool
	// ctx is the walker's EmitContext, in a region of the program: the
	// Pointer takes one from its pool for each node and resets it, and the
	// Store resets this one for each node.
	ctx *storeprinter.EmitContext
}

func newStoreWalker(c *storechecker.Checker, space *store.IDSpace, hadErrors bool) *storeWalker {
	return &storeWalker{checker: c, hadErrors: hadErrors, ctx: storeprinter.NewEmitContextIn(store.NewNodeRegion(space))}
}

// walk is getTypes or getSymbols of one file: visitNode on its root.
func (w *storeWalker) walk(file *store.File, isSymbolWalk bool) []walkRecord {
	var records []walkRecord
	for _, n := range forEachStoreNode(file.Root()) {
		if store.IsExpressionNode(n) || n.Kind() == ast.KindIdentifier || store.IsDeclarationName(n) ||
			store.IsQualifiedName(n) && store.IsNameOfHeritageClauseTypeReference(n) && (isSymbolWalk || store.IsQualifiedName(n.Parent())) {
			records = append(records, w.writeTypeOrSymbol(file, n, isSymbolWalk))
		}
	}
	return records
}

// forEachStoreNode is forEachASTNode: the preorder of ForEachChild, into a
// reparsed node only for a JSDoc cast (as and satisfies) and its expression.
func forEachStoreNode(node store.Node) []store.Node {
	var result []store.Node
	work := []store.Node{node}

	var resChildren []store.Node
	addChild := func(child store.Node) bool {
		resChildren = append(resChildren, child)
		return false
	}

	for len(work) > 0 {
		elem := work[len(work)-1]
		work = work[:len(work)-1]
		if elem.Flags()&ast.NodeFlagsReparsed == 0 || elem.Kind() == ast.KindAsExpression || elem.Kind() == ast.KindSatisfiesExpression ||
			((elem.Parent().Kind() == ast.KindSatisfiesExpression || elem.Parent().Kind() == ast.KindAsExpression) && store.SameNode(elem, elem.Parent().Expression())) {
			if elem.Flags()&ast.NodeFlagsReparsed == 0 || elem.Parent().Kind() == ast.KindAsExpression || elem.Parent().Kind() == ast.KindSatisfiesExpression {
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

func (w *storeWalker) writeTypeOrSymbol(file *store.File, node store.Node, isSymbolWalk bool) walkRecord {
	record := walkRecord{
		pos:  int(node.Pos()),
		kind: node.Kind(),
		text: storescanner.GetSourceTextOfNodeFromSourceFile(file, node, false /*includeTrivia*/),
	}
	c := w.checker
	w.ctx.Reset()

	if !isSymbolWalk {
		// Don't try to get the type of something that's already a type.
		// Exception for `T` in `type T = something` because that may evaluate to some interesting type.
		if store.IsPartOfTypeNode(node) ||
			(node.Kind() == ast.KindAsExpression || node.Kind() == ast.KindSatisfiesExpression) && node.Type().Flags()&ast.NodeFlagsReparsed != 0 ||
			store.IsIdentifier(node) &&
				(getMeaningFromDeclaration(node.Parent())&ast.SemanticMeaningValue) == 0 &&
				!(store.IsTypeOrJSTypeAliasDeclaration(node.Parent()) && store.SameNode(node, node.Parent().Name())) {
			return record
		}

		if store.IsOmittedExpression(node) {
			return record
		}

		var t *storechecker.Type
		// Workaround to ensure we output 'C' instead of 'typeof C' for base class expressions
		if store.IsExpressionWithTypeArgumentsInClassExtendsClause(node.Parent()) {
			t = c.GetTypeAtLocation(node.Parent())
		}
		if t == nil || storechecker.IsTypeAny(t) {
			t = c.GetTypeAtLocation(node)
		}
		var typeString string
		if !w.hadErrors &&
			storechecker.IsTypeAny(t) &&
			!store.IsBindingElement(node.Parent()) &&
			!store.IsPropertyAccessOrQualifiedName(node.Parent()) &&
			!isLabelName(node) &&
			!store.IsGlobalScopeAugmentation(node.Parent()) &&
			!store.IsMetaProperty(node.Parent()) &&
			!isImportStatementName(node) &&
			!isExportStatementName(node) &&
			!isIntrinsicJsxTag(node, file) {
			typeString = t.AsIntrinsicType().IntrinsicName()
		} else {
			w.ctx.Reset()
			builder := storechecker.NewNodeBuilder(c, w.ctx)
			typeFormatFlags := storechecker.TypeFormatFlagsNoTruncation | storechecker.TypeFormatFlagsAllowUniqueESSymbolType | storechecker.TypeFormatFlagsGenerateNamesForShadowedTypeParams
			typeNode := builder.TypeToTypeNode(t, node.Parent(), nodebuilder.Flags(typeFormatFlags&storechecker.TypeFormatFlagsNodeBuilderFlagsMask)|nodebuilder.FlagsIgnoreErrors, nodebuilder.InternalFlagsAllowUnresolvedNames, nil)
			if store.IsIdentifier(node) && store.IsTypeAliasDeclaration(node.Parent()) && store.SameNode(node.Parent().Name(), node) && store.IsIdentifier(typeNode) && typeNode.Text() == node.Text() {
				// for a complex type alias `type T = ...`, showing "T : T" isn't very helpful for type tests. When the type produced is the same as
				// the name of the type alias, recreate the type string without reusing the alias name
				typeNode = builder.TypeToTypeNode(t, node.Parent(), nodebuilder.Flags((typeFormatFlags|storechecker.TypeFormatFlagsInTypeAlias)&storechecker.TypeFormatFlagsNodeBuilderFlagsMask)|nodebuilder.FlagsIgnoreErrors, nodebuilder.InternalFlagsAllowUnresolvedNames, nil)
			}

			writer := storeprinter.NewTextWriter("", 0)
			printer := storeprinter.NewPrinter(storeprinter.PrinterOptions{RemoveComments: true}, storeprinter.PrintHandlers{}, w.ctx)
			printer.Write(typeNode, file, writer, nil)
			typeString = writer.String()
		}
		// The writer grew to the file's length (printer.Write): the record
		// keeps a copy, not the writer's buffer.
		record.value, record.present = strings.Clone(typeString), true
		return record
	}

	symbol := c.GetSymbolAtLocation(node)
	if symbol == nil {
		return record
	}

	var symbolString strings.Builder
	symbolString.Grow(256)
	symbolString.WriteString("Symbol(")
	symbolString.WriteString(store.EscapeAllInternalSymbolNames(c.SymbolToStringEx(symbol, node.Parent(), ast.SymbolFlagsNone, storechecker.SymbolFormatFlagsAllowAnyNodeKind)))
	count := 0
	for _, declaration := range symbol.Declarations {
		if count >= 5 {
			fmt.Fprintf(&symbolString, " ... and %d more", len(symbol.Declarations)-count)
			break
		}
		count++
		symbolString.WriteString(", ")

		declSourceFile := declaration.File()
		declLine, declChar := scanner.GetECMALineAndUTF16CharacterOfPosition(declSourceFile, int(declaration.Pos()))
		fileName := tspath.GetBaseFileName(declSourceFile.FileName())
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
		if f := d.File(); f != nil {
			path = string(f.Path())
		}
		record.decls = append(record.decls, declKey{path: path, pos: int(d.Pos()), end: int(d.End()), kind: d.Kind()})
	}
	return record
}

func isImportStatementName(node store.Node) bool {
	parent := node.Parent()
	if store.IsImportSpecifier(parent) && (store.SameNode(node, parent.Name()) || store.SameNode(node, parent.PropertyName())) {
		return true
	}
	if store.IsImportClause(parent) && store.SameNode(node, parent.Name()) {
		return true
	}
	if store.IsImportEqualsDeclaration(parent) && store.SameNode(node, parent.Name()) {
		return true
	}
	return false
}

func isExportStatementName(node store.Node) bool {
	parent := node.Parent()
	if store.IsExportAssignment(parent) && store.SameNode(node, parent.Expression()) {
		return true
	}
	if store.IsExportSpecifier(parent) && (store.SameNode(node, parent.Name()) || store.SameNode(node, parent.PropertyName())) {
		return true
	}
	return false
}

func isIntrinsicJsxTag(node store.Node, file *store.File) bool {
	parent := node.Parent()
	if !(store.IsJsxOpeningElement(parent) || parent.Kind() == ast.KindJsxClosingElement || store.IsJsxSelfClosingElement(parent)) {
		return false
	}
	if !store.SameNode(parent.TagName(), node) {
		return false
	}
	text := storescanner.GetSourceTextOfNodeFromSourceFile(file, node, false /*includeTrivia*/)
	return scanner.IsIntrinsicJsxName(text)
}

// isDefaultLibraryFile is tsbaseline's (util.go): a file name, no node.
func isDefaultLibraryFile(filePath string) bool {
	fileName := tspath.GetBaseFileName(filePath)
	return strings.HasPrefix(fileName, "lib.") && strings.HasSuffix(fileName, tspath.ExtensionDts)
}

// The predicates of ast that package store lacks, written from the Pointer's
// (ast/utilities.go).

// getMeaningFromDeclaration is ast.GetMeaningFromDeclaration.
func getMeaningFromDeclaration(node store.Node) ast.SemanticMeaning {
	switch node.Kind() {
	case ast.KindVariableDeclaration:
		return ast.SemanticMeaningValue
	case ast.KindParameter,
		ast.KindBindingElement,
		ast.KindPropertyDeclaration,
		ast.KindPropertySignature,
		ast.KindPropertyAssignment,
		ast.KindShorthandPropertyAssignment,
		ast.KindMethodDeclaration,
		ast.KindMethodSignature,
		ast.KindConstructor,
		ast.KindGetAccessor,
		ast.KindSetAccessor,
		ast.KindFunctionDeclaration,
		ast.KindFunctionExpression,
		ast.KindArrowFunction,
		ast.KindCatchClause,
		ast.KindJsxAttribute:
		return ast.SemanticMeaningValue

	case ast.KindTypeParameter,
		ast.KindInterfaceDeclaration,
		ast.KindTypeAliasDeclaration,
		ast.KindJSTypeAliasDeclaration,
		ast.KindTypeLiteral:
		return ast.SemanticMeaningType
	case ast.KindEnumMember, ast.KindClassDeclaration:
		return ast.SemanticMeaningValue | ast.SemanticMeaningType

	case ast.KindModuleDeclaration:
		if store.IsAmbientModule(node) {
			return ast.SemanticMeaningNamespace | ast.SemanticMeaningValue
		} else if store.GetModuleInstanceState(node) == ast.ModuleInstanceStateInstantiated {
			return ast.SemanticMeaningNamespace | ast.SemanticMeaningValue
		} else {
			return ast.SemanticMeaningNamespace
		}

	case ast.KindEnumDeclaration,
		ast.KindNamedImports,
		ast.KindImportSpecifier,
		ast.KindImportEqualsDeclaration,
		ast.KindImportDeclaration,
		ast.KindJSImportDeclaration,
		ast.KindExportAssignment,
		ast.KindExportDeclaration:
		return ast.SemanticMeaningAll

	// An external module can be a Value
	case ast.KindSourceFile:
		return ast.SemanticMeaningNamespace | ast.SemanticMeaningValue
	}

	return ast.SemanticMeaningAll
}

// isLabelName is ast.IsLabelName.
func isLabelName(node store.Node) bool {
	return isLabelOfLabeledStatement(node) || isJumpStatementTarget(node)
}

// isLabelOfLabeledStatement is ast.IsLabelOfLabeledStatement.
func isLabelOfLabeledStatement(node store.Node) bool {
	if !store.IsIdentifier(node) {
		return false
	}
	if !store.IsLabeledStatement(node.Parent()) {
		return false
	}
	return store.SameNode(node, node.Parent().Label())
}

// isJumpStatementTarget is ast.IsJumpStatementTarget, with
// ast.IsBreakOrContinueStatement written out.
func isJumpStatementTarget(node store.Node) bool {
	if !store.IsIdentifier(node) {
		return false
	}
	if !store.NodeKindIs(node.Parent(), ast.KindBreakStatement, ast.KindContinueStatement) {
		return false
	}
	return store.SameNode(node, node.Parent().Label())
}
