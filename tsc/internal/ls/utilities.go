package ls

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"iter"
	"slices"
	"strings"
)

var quoteReplacer = /*includeJSDoc*/ strings.NewReplacer("'", `\'`, `\"`, `"`)

func IsInString(sourceFile *ast.SourceFile, position int, previousToken ast.Handle) bool {
	if !previousToken.IsNil() && ast.IsStringTextContainingNode(previousToken) {
		start := astnav.GetStartOfNode(previousToken, sourceFile, false)
		end := previousToken.End()
		if start < position && position < end {
			return true
		}
		if position == end {
			return ast.IsUnterminatedLiteral(previousToken)
		}
	}
	return false
}
func isModuleSpecifierLike(node ast.Handle) bool {
	if !ast.IsStringLiteralLike(node) {
		return false
	}
	if ast.IsRequireCall(node.Parent(), false) || ast.IsImportCall(node.Parent()) {
		return node.Parent().Arguments()[0] == node
	}
	return node.Parent().Kind == ast.KindExternalModuleReference || node.Parent().Kind == ast.KindImportDeclaration || node.Parent().Kind == ast.KindJSImportDeclaration
}
func getNonModuleSymbolOfMergedModuleSymbol(symbol *ast.Symbol) *ast.Symbol {
	if len(symbol.Declarations) == 0 || (symbol.Flags&(ast.SymbolFlagsModule|ast.SymbolFlagsTransient)) == 0 {
		return nil
	}
	if decl := ast.FindSymbolDeclaration(symbol, func(d ast.Handle) bool {
		return !ast.IsSourceFile(d) && !ast.IsModuleDeclaration(d)
	}); !decl.IsNil() {
		return decl.Symbol()
	}
	return nil
}
func getLocalSymbolForExportSpecifier(referenceLocation ast.Handle, referenceSymbol *ast.Symbol, exportSpecifier ast.Handle, ch *checker.Checker) *ast.Symbol {
	if isExportSpecifierAlias(referenceLocation, exportSpecifier) {
		if symbol := ch.GetExportSpecifierLocalTargetSymbol(exportSpecifier); symbol != nil {
			return symbol
		}
	}
	return referenceSymbol
}
func isExportSpecifierAlias(referenceLocation ast.Handle, exportSpecifier ast.Handle) bool {
	debug.Assert(exportSpecifier.PropertyName() == referenceLocation || exportSpecifier.Name() == referenceLocation, "referenceLocation is not export specifier name or property name")
	propertyName := exportSpecifier.PropertyName()
	if !propertyName.IsNil() {
		return propertyName == referenceLocation
	} else {
		return exportSpecifier.Parent().Parent().ModuleSpecifier().IsNil()
	}
}
func isInComment(file *ast.SourceFile, position int, tokenAtPosition ast.Handle) *ast.CommentRange {
	return getRangeOfEnclosingComment(file, position, astnav.FindPrecedingToken(file, position), tokenAtPosition)
}
func positionBelongsToNode(candidate ast.Handle, position int, file *ast.SourceFile) bool {
	return lsutil.PositionBelongsToNode(candidate, position, file)
}

type PossibleTypeArgumentInfo struct {
	called         ast.Handle
	nTypeArguments int
}

func getPossibleTypeArgumentsInfo(tokenIn ast.Handle, sourceFile *ast.SourceFile) *PossibleTypeArgumentInfo {
	if strings.LastIndexByte(sourceFile.Text(), '<') == -1 {
		return nil
	}
	token := tokenIn
	remainingLessThanTokens := 0
	nTypeArguments := 0
	for !token.IsNil() {
		switch token.Kind {
		case ast.KindLessThanToken:
			token = astnav.FindPrecedingToken(sourceFile, token.Pos())
			if !token.IsNil() && token.Kind == ast.KindQuestionDotToken {
				token = astnav.FindPrecedingToken(sourceFile, token.Pos())
			}
			if token.IsNil() || !ast.IsIdentifier(token) {
				return nil
			}
			if remainingLessThanTokens == 0 {
				if ast.IsDeclarationName(token) {
					return nil
				}
				return &PossibleTypeArgumentInfo{called: token, nTypeArguments: nTypeArguments}
			}
			remainingLessThanTokens--
		case ast.KindGreaterThanGreaterThanGreaterThanToken:
			remainingLessThanTokens += 3
		case ast.KindGreaterThanGreaterThanToken:
			remainingLessThanTokens += 2
		case ast.KindGreaterThanToken:
			remainingLessThanTokens++
		case ast.KindCloseBraceToken:
			token = findPrecedingMatchingToken(token, ast.KindOpenBraceToken, sourceFile)
			if token.IsNil() {
				return nil
			}
		case ast.KindCloseParenToken:
			token = findPrecedingMatchingToken(token, ast.KindOpenParenToken, sourceFile)
			if token.IsNil() {
				return nil
			}
		case ast.KindCloseBracketToken:
			token = findPrecedingMatchingToken(token, ast.KindOpenBracketToken, sourceFile)
			if token.IsNil() {
				return nil
			}
		case ast.KindCommaToken:
			nTypeArguments++
		case ast.KindEqualsGreaterThanToken, ast.KindIdentifier, ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindTypeOfKeyword, ast.KindExtendsKeyword, ast.KindKeyOfKeyword, ast.KindDotToken, ast.KindBarToken, ast.KindQuestionToken, ast.KindColonToken:
		default:
			if !ast.IsTypeNode(token) {
				return nil
			}
		}
		token = astnav.FindPrecedingToken(sourceFile, token.Pos())
	}
	return nil
}
func isNameOfModuleDeclaration(node ast.Handle) bool {
	if node.Parent().Kind != ast.KindModuleDeclaration {
		return false
	}
	return node.Parent().Name() == node
}
func isExpressionOfExternalModuleImportEqualsDeclaration(node ast.Handle) bool {
	return ast.IsExternalModuleImportEqualsDeclaration(node.Parent().Parent()) && ast.GetExternalModuleImportEqualsDeclarationExpression(node.Parent().Parent()) == node
}
func isNamespaceReference(node ast.Handle) bool {
	return isQualifiedNameNamespaceReference(node) || isPropertyAccessNamespaceReference(node)
}
func isQualifiedNameNamespaceReference(node ast.Handle) bool {
	root := node
	isLastClause := true
	if root.Parent().Kind == ast.KindQualifiedName {
		for !root.Parent().IsNil() && root.Parent().Kind == ast.KindQualifiedName {
			root = root.Parent()
		}
		isLastClause = root.QualifiedNameRight() == node
	}
	return root.Parent().Kind == ast.KindTypeReference && !isLastClause
}
func isPropertyAccessNamespaceReference(node ast.Handle) bool {
	root := node
	isLastClause := true
	if root.Parent().Kind == ast.KindPropertyAccessExpression {
		for !root.Parent().IsNil() && root.Parent().Kind == ast.KindPropertyAccessExpression {
			root = root.Parent()
		}
		isLastClause = root.Name() == node
	}
	if !isLastClause && root.Parent().Kind == ast.KindExpressionWithTypeArguments && root.Parent().Parent().Kind == ast.KindHeritageClause {
		decl := root.Parent().Parent().Parent()
		return (decl.Kind == ast.KindClassDeclaration && root.Parent().Parent().HeritageClauseToken() == ast.KindImplementsKeyword) || (decl.Kind == ast.KindInterfaceDeclaration && root.Parent().Parent().HeritageClauseToken() == ast.KindExtendsKeyword)
	}
	return false
}
func isThis(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindThisKeyword:
		return true
	case ast.KindIdentifier:
		return node.Text() == "this" && node.Parent().Kind == ast.KindParameter
	default:
		return false
	}
}
func isTypeReference(node ast.Handle) bool {
	if ast.IsRightSideOfQualifiedNameOrPropertyAccess(node) {
		node = node.Parent()
	}
	switch node.Kind {
	case ast.KindThisKeyword:
		return !ast.IsExpressionNode(node)
	case ast.KindThisType:
		return true
	}
	switch node.Parent().Kind {
	case ast.KindTypeReference:
		return true
	case ast.KindImportType:
		return !node.Parent().ImportTypeNodeIsTypeOf()
	case ast.KindExpressionWithTypeArguments:
		return ast.IsPartOfTypeNode(node.Parent())
	}
	return false
}
func isInRightSideOfInternalImportEqualsDeclaration(node ast.Handle) bool {
	if node.Parent().IsNil() {
		return false
	}
	for node.Parent().Kind == ast.KindQualifiedName {
		node = node.Parent()
	}
	return ast.IsInternalModuleImportEqualsDeclaration(node.Parent()) && node.Parent().ImportEqualsDeclarationModuleReference() == node
}
func (l *LanguageService) createLspRangeFromNode(node ast.Handle, file *ast.SourceFile) (lsproto.Range, spanmap.Fidelity) {
	return l.createLspRangeFromBounds(scanner.GetTokenPosOfNode(node, file, false), node.End(), file)
}
func (l *LanguageService) createLspRangeFromNodeForFeature(node ast.Handle, file *ast.SourceFile, feature spanmap.Feature) (lsproto.Range, spanmap.Fidelity) {
	return l.converters.ToLSPRangeForFeature(file, createRangeFromNode(node, file), feature)
}
func createRangeFromNode(node ast.Handle, file *ast.SourceFile) core.TextRange {
	return core.NewTextRange(scanner.GetTokenPosOfNode(node, file, false), node.End())
}
func (l *LanguageService) createLspRangeFromBounds(start, end int, file *ast.SourceFile) (lsproto.Range, spanmap.Fidelity) {
	return l.converters.ToLSPRange(file, core.NewTextRange(start, end))
}
func (l *LanguageService) createLspRangeFromRange(textRange core.TextRange, script lsconv.Script) (lsproto.Range, spanmap.Fidelity) {
	return l.converters.ToLSPRange(script, textRange)
}
func (l *LanguageService) createLspPosition(position int, file *ast.SourceFile) (lsproto.Position, spanmap.Fidelity) {
	return l.converters.ToLSPPosition(file, core.TextPos(position))
}
func quote(file *ast.SourceFile, preferences lsutil.UserPreferences, text string) string {
	quotePreference := lsutil.GetQuotePreference(file, preferences)
	quoted, _ := core.StringifyJson(text, "", "")
	if quotePreference == lsutil.QuotePreferenceSingle {
		quoted = "'" + quoteReplacer.Replace(stringutil.StripQuotes(quoted)) + "'"
	}
	return quoted
}

var typeKeywords *collections.Set[ast.Kind] = collections.NewSetFromItems(ast.KindAnyKeyword, ast.KindAssertsKeyword, ast.KindBigIntKeyword, ast.KindBooleanKeyword, ast.KindFalseKeyword, ast.KindInferKeyword, ast.KindKeyOfKeyword, ast.KindNeverKeyword, ast.KindNullKeyword, ast.KindNumberKeyword, ast.KindObjectKeyword, ast.KindReadonlyKeyword, ast.KindStringKeyword, ast.KindSymbolKeyword, ast.KindTypeOfKeyword, ast.KindTrueKeyword, ast.KindVoidKeyword, ast.KindUndefinedKeyword, ast.KindUniqueKeyword, ast.KindUnknownKeyword)

func isTypeKeyword(kind ast.Kind) bool {
	return typeKeywords.Has(kind)
}
func isSeparator(node ast.Handle, candidate ast.Handle) bool {
	return !candidate.IsNil() && !node.Parent().IsNil() && (candidate.Kind == ast.KindCommaToken || (candidate.Kind == ast.KindSemicolonToken && node.Parent().Kind == ast.KindObjectLiteralExpression))
}
func isLiteralNameOfPropertyDeclarationOrIndexAccess(node ast.Handle) bool {
	switch node.Parent().Kind {
	case ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindPropertyAssignment, ast.KindEnumMember, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindModuleDeclaration:
		return ast.GetNameOfDeclaration(node.Parent()) == node
	case ast.KindElementAccessExpression:
		return node.Parent().ElementAccessExpressionArgumentExpression() == node
	case ast.KindComputedPropertyName:
		return true
	case ast.KindLiteralType:
		return node.Parent().Parent().Kind == ast.KindIndexedAccessType
	default:
		return false
	}
}
func isObjectBindingElementWithoutPropertyName(bindingElement ast.Handle) bool {
	return bindingElement.Kind == ast.KindBindingElement && bindingElement.Parent().Kind == ast.KindObjectBindingPattern && bindingElement.Name().Kind == ast.KindIdentifier && bindingElement.PropertyName().IsNil()
}
func isRightSideOfPropertyAccess(node ast.Handle) bool {
	return !node.Parent().IsNil() && node.Parent().Kind == ast.KindPropertyAccessExpression && node.Parent().Name() == node
}
func isStaticSymbol(symbol *ast.Symbol) bool {
	if symbol.ValueDeclaration == 0 {
		return false
	}
	modifierFlags := ast.NodeOf(symbol.ValueDeclaration).ModifierFlags()
	return modifierFlags&ast.ModifierFlagsStatic != 0
}
func isImplementation(node ast.Handle) bool {
	if node.Flags()&ast.NodeFlagsAmbient != 0 {
		return !(node.Kind == ast.KindInterfaceDeclaration || node.Kind == ast.KindTypeAliasDeclaration)
	}
	if ast.IsVariableLike(node) {
		return ast.HasInitializer(node)
	}
	if ast.IsFunctionLikeDeclaration(node) {
		return !node.Body().IsNil()
	}
	return ast.IsClassLike(node) || ast.IsModuleOrEnumDeclaration(node)
}
func isImplementationExpression(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindParenthesizedExpression:
		return isImplementationExpression(node.Expression())
	case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindObjectLiteralExpression, ast.KindClassExpression, ast.KindArrayLiteralExpression:
		return true
	default:
		return false
	}
}
func isReadonlyTypeOperator(node ast.Handle) bool {
	return node.Kind == ast.KindReadonlyKeyword && node.Parent().Kind == ast.KindTypeOperator && node.Parent().TypeOperatorNodeOperator() == ast.KindReadonlyKeyword
}
func isJumpStatementTarget(node ast.Handle) bool {
	return node.Kind == ast.KindIdentifier && ast.IsBreakOrContinueStatement(node.Parent()) && node.Parent().Label() == node
}
func isLabelOfLabeledStatement(node ast.Handle) bool {
	return node.Kind == ast.KindIdentifier && node.Parent().Kind == ast.KindLabeledStatement && node.Parent().Label() == node
}
func findReferenceInPosition(refs []*ast.FileReference, pos int) *ast.FileReference {
	return core.Find(refs, func(ref *ast.FileReference) bool {
		return ref.TextRange.ContainsInclusive(pos)
	})
}
func getContainingNodeIfInHeritageClause(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindIdentifier || node.Kind == ast.KindQualifiedName || node.Kind == ast.KindPropertyAccessExpression {
		return getContainingNodeIfInHeritageClause(node.Parent())
	}
	if (node.Kind == ast.KindExpressionWithTypeArguments || node.Kind == ast.KindTypeReference) && ast.IsHeritageClause(node.Parent()) && (ast.IsClassLike(node.Parent().Parent()) || node.Parent().Parent().Kind == ast.KindInterfaceDeclaration) {
		return node.Parent().Parent()
	}
	return ast.Handle{}
}
func getContainerNode(node ast.Handle) ast.Handle {
	for parent := node.Parent(); !parent.IsNil(); parent = parent.Parent() {
		switch parent.Kind {
		case ast.KindSourceFile, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindEnumDeclaration, ast.KindModuleDeclaration:
			return parent
		}
	}
	return ast.Handle{}
}
func getAdjustedLocation(node ast.Handle, forRename bool, sourceFile *ast.SourceFile) ast.Handle {
	parent := node.Parent()
	isModifier := func(node ast.Handle) bool {
		if ast.IsModifier(node) && (forRename || node.Kind != ast.KindDefaultKeyword) {
			return ast.CanHaveModifiers(parent) && slices.Contains(parent.ModifierNodes(), node)
		}
		switch node.Kind {
		case ast.KindClassKeyword:
			return ast.IsClassDeclaration(parent) || ast.IsClassExpression(node)
		case ast.KindFunctionKeyword:
			return ast.IsFunctionDeclaration(parent) || ast.IsFunctionExpression(node)
		case ast.KindInterfaceKeyword:
			return ast.IsInterfaceDeclaration(parent)
		case ast.KindEnumKeyword:
			return ast.IsEnumDeclaration(parent)
		case ast.KindTypeKeyword:
			return ast.IsTypeAliasDeclaration(parent)
		case ast.KindNamespaceKeyword, ast.KindModuleKeyword:
			return ast.IsModuleDeclaration(parent)
		case ast.KindImportKeyword:
			return ast.IsImportEqualsDeclaration(parent)
		case ast.KindGetKeyword:
			return ast.IsGetAccessorDeclaration(parent)
		case ast.KindSetKeyword:
			return ast.IsSetAccessorDeclaration(parent)
		}
		return false
	}
	if isModifier(node) {
		if sourceFile == nil {
			sourceFile = ast.GetSourceFileOfNode(node)
		}
		if location := getAdjustedLocationForDeclaration(parent, forRename, sourceFile); !location.IsNil() {
			return location
		}
	}
	if (node.Kind == ast.KindVarKeyword || node.Kind == ast.KindConstKeyword || node.Kind == ast.KindLetKeyword) && ast.IsVariableDeclarationList(parent) && parent.Store().ListLen(parent.VariableDeclarationListDeclarations()) == 1 {
		declaration := parent.Store().ListAt(parent.VariableDeclarationListDeclarations(), 0)
		if ast.IsIdentifier(declaration.Name()) {
			return declaration.Name()
		}
	}
	if node.Kind == ast.KindTypeKeyword {
		if ast.IsImportClause(parent) && parent.IsTypeOnly() {
			if location := getAdjustedLocationForImportDeclaration(parent.Parent(), forRename); !location.IsNil() {
				return location
			}
		}
		if ast.IsExportDeclaration(parent) && parent.IsTypeOnly() {
			if location := getAdjustedLocationForExportDeclaration(parent, forRename); !location.IsNil() {
				return location
			}
		}
	}
	if node.Kind == ast.KindAsKeyword {
		if parent.Kind == ast.KindImportSpecifier && !parent.PropertyName().IsNil() || parent.Kind == ast.KindExportSpecifier && !parent.PropertyName().IsNil() || parent.Kind == ast.KindNamespaceImport || parent.Kind == ast.KindNamespaceExport {
			return parent.Name()
		}
		if parent.Kind == ast.KindExportDeclaration {
			if exportClause := parent.ExportDeclarationExportClause(); !exportClause.IsNil() && exportClause.Kind == ast.KindNamespaceExport {
				return exportClause.Name()
			}
		}
	}
	if node.Kind == ast.KindImportKeyword && parent.Kind == ast.KindImportDeclaration {
		if location := getAdjustedLocationForImportDeclaration(parent, forRename); !location.IsNil() {
			return location
		}
	}
	if node.Kind == ast.KindExportKeyword {
		if parent.Kind == ast.KindExportDeclaration {
			if location := getAdjustedLocationForExportDeclaration(parent, forRename); !location.IsNil() {
				return location
			}
		}
		if parent.Kind == ast.KindExportAssignment {
			return ast.SkipOuterExpressions(parent.Expression(), ast.OEKAll)
		}
	}
	if node.Kind == ast.KindRequireKeyword && parent.Kind == ast.KindExternalModuleReference {
		return parent.Expression()
	}
	if node.Kind == ast.KindFromKeyword {
		if (parent.Kind == ast.KindImportDeclaration || parent.Kind == ast.KindExportDeclaration) && !parent.ModuleSpecifier().IsNil() {
			return parent.ModuleSpecifier()
		}
	}
	if (node.Kind == ast.KindExtendsKeyword || node.Kind == ast.KindImplementsKeyword) && parent.Kind == ast.KindHeritageClause && parent.HeritageClauseToken() == node.Kind {
		getAdjustedLocationForHeritageClause := func(node ast.Handle) ast.Handle {
			types := node.Types()
			if len(types) == 1 {
				return ast.GetHeritageClauseElementName(types[0])
			}
			return ast.Handle{}
		}
		if location := getAdjustedLocationForHeritageClause(parent); !location.IsNil() {
			return location
		}
	}
	if node.Kind == ast.KindExtendsKeyword {
		if parent.Kind == ast.KindTypeParameter {
			if constraint := parent.TypeParameterDeclarationConstraint(); !constraint.IsNil() && constraint.Kind == ast.KindTypeReference {
				return constraint.TypeReferenceNodeTypeName()
			}
		}
		if parent.Kind == ast.KindConditionalType {
			if extendsType := parent.ConditionalTypeNodeExtendsType(); !extendsType.IsNil() && extendsType.Kind == ast.KindTypeReference {
				return extendsType.TypeReferenceNodeTypeName()
			}
		}
	}
	if node.Kind == ast.KindInferKeyword && parent.Kind == ast.KindInferType {
		return parent.InferTypeNodeTypeParameter().Name()
	}
	if node.Kind == ast.KindInKeyword && parent.Kind == ast.KindTypeParameter && parent.Parent().Kind == ast.KindMappedType {
		return parent.Name()
	}
	if node.Kind == ast.KindKeyOfKeyword && parent.Kind == ast.KindTypeOperator && parent.TypeOperatorNodeOperator() == ast.KindKeyOfKeyword {
		if parentType := parent.Type(); !parentType.IsNil() && parentType.Kind == ast.KindTypeReference {
			return parentType.TypeReferenceNodeTypeName()
		}
	}
	if node.Kind == ast.KindReadonlyKeyword && parent.Kind == ast.KindTypeOperator && parent.TypeOperatorNodeOperator() == ast.KindReadonlyKeyword {
		if parentType := parent.Type(); !parentType.IsNil() && parentType.Kind == ast.KindArrayType && parentType.ArrayTypeNodeElementType().Kind == ast.KindTypeReference {
			return parentType.ArrayTypeNodeElementType().TypeReferenceNodeTypeName()
		}
	}
	if !forRename {
		if node.Kind == ast.KindNewKeyword && parent.Kind == ast.KindNewExpression || node.Kind == ast.KindVoidKeyword && parent.Kind == ast.KindVoidExpression || node.Kind == ast.KindTypeOfKeyword && parent.Kind == ast.KindTypeOfExpression || node.Kind == ast.KindAwaitKeyword && parent.Kind == ast.KindAwaitExpression || node.Kind == ast.KindYieldKeyword && parent.Kind == ast.KindYieldExpression || node.Kind == ast.KindDeleteKeyword && parent.Kind == ast.KindDeleteExpression {
			if expr := parent.Expression(); !expr.IsNil() {
				return ast.SkipOuterExpressions(expr, ast.OEKAll)
			}
		}
		if (node.Kind == ast.KindInKeyword || node.Kind == ast.KindInstanceOfKeyword) && parent.Kind == ast.KindBinaryExpression && parent.BinaryExpressionOperatorToken() == node {
			return ast.SkipOuterExpressions(parent.BinaryExpressionRight(), ast.OEKAll)
		}
		if node.Kind == ast.KindAsKeyword && parent.Kind == ast.KindAsExpression {
			if asExprType := parent.Type(); !asExprType.IsNil() && asExprType.Kind == ast.KindTypeReference {
				return asExprType.TypeReferenceNodeTypeName()
			}
		}
		if node.Kind == ast.KindInKeyword && parent.Kind == ast.KindForInStatement || node.Kind == ast.KindOfKeyword && parent.Kind == ast.KindForOfStatement {
			return ast.SkipOuterExpressions(parent.Expression(), ast.OEKAll)
		}
	}
	return node
}
func getAdjustedLocationForDeclaration(node ast.Handle, forRename bool, sourceFile *ast.SourceFile) ast.Handle {
	if !node.Name().IsNil() {
		return node.Name()
	}
	if forRename {
		return ast.Handle{}
	}
	switch node.Kind {
	case ast.KindClassDeclaration, ast.KindFunctionDeclaration:
		return core.Find(node.ModifierNodes(), func(ast.Handle) bool {
			return node.Kind == ast.KindDefaultKeyword
		})
	case ast.KindClassExpression:
		return astnav.FindChildOfKind(node, ast.KindClassKeyword, sourceFile)
	case ast.KindFunctionExpression:
		return astnav.FindChildOfKind(node, ast.KindFunctionKeyword, sourceFile)
	case ast.KindConstructor:
		return node
	}
	return ast.Handle{}
}
func getAdjustedLocationForImportDeclaration(node ast.Handle, forRename bool) ast.Handle {
	if !node.ImportClause().IsNil() {
		if name := node.ImportClause().Name(); !name.IsNil() {
			if !node.ImportClause().NamedBindings().IsNil() {
				return ast.Handle{}
			}
			return node.ImportClause().Name()
		}
		if namedBindings := node.ImportClause().NamedBindings(); !namedBindings.IsNil() {
			switch namedBindings.Kind {
			case ast.KindNamedImports:
				elements := namedBindings.Elements()
				if len(elements) != 1 {
					return ast.Handle{}
				}
				return elements[0].Name()
			case ast.KindNamespaceImport:
				return namedBindings.Name()
			}
		}
	}
	if !forRename {
		return node.ModuleSpecifier()
	}
	return ast.Handle{}
}
func getAdjustedLocationForExportDeclaration(node ast.Handle, forRename bool) ast.Handle {
	if !node.ExportClause().IsNil() {
		switch node.ExportClause().Kind {
		case ast.KindNamedExports:
			elements := node.ExportClause().Elements()
			if len(elements) != 1 {
				return ast.Handle{}
			}
			return elements[0].Name()
		case ast.KindNamespaceExport:
			return node.ExportClause().Name()
		}
	}
	if !forRename {
		return node.ModuleSpecifier()
	}
	return ast.Handle{}
}
func symbolFlagsHaveMeaning(flags ast.SymbolFlags, meaning ast.SemanticMeaning) bool {
	if meaning == ast.SemanticMeaningAll {
		return true
	}
	if meaning&ast.SemanticMeaningValue != 0 {
		return flags&ast.SymbolFlagsValue != 0
	}
	if meaning&ast.SemanticMeaningType != 0 {
		return flags&ast.SymbolFlagsType != 0
	}
	if meaning&ast.SemanticMeaningNamespace != 0 {
		return flags&ast.SymbolFlagsNamespace != 0
	}
	return false
}
func getMeaningFromLocation(node ast.Handle) ast.SemanticMeaning {
	node = getAdjustedLocation(ast.GetReparsedHandle(node), false, nil)
	parent := node.Parent()
	switch {
	case ast.IsSourceFile(node):
		return ast.SemanticMeaningValue
	case ast.NodeKindIs(parent, ast.KindExportAssignment, ast.KindExportSpecifier, ast.KindExternalModuleReference, ast.KindImportSpecifier, ast.KindImportClause) || parent.Kind == ast.KindImportEqualsDeclaration && node == parent.Name():
		return ast.SemanticMeaningAll
	case isInRightSideOfInternalImportEqualsDeclaration(node):
		name := node
		if node.Kind != ast.KindQualifiedName {
			name = core.IfElse(node.Parent().Kind == ast.KindQualifiedName && node.Parent().QualifiedNameRight() == node, node.Parent(), ast.Handle{})
		}
		if !name.IsNil() && name.Parent().Kind == ast.KindImportEqualsDeclaration {
			return ast.SemanticMeaningAll
		}
		return ast.SemanticMeaningNamespace
	case ast.IsDeclarationName(node):
		return getMeaningFromDeclaration(parent)
	case ast.IsEntityName(node) && ast.IsJSDocNameReferenceContext(node):
		return ast.SemanticMeaningAll
	case isTypeReference(node):
		return ast.SemanticMeaningType
	case isNamespaceReference(node):
		return ast.SemanticMeaningNamespace
	case ast.IsTypeParameterDeclaration(parent):
		return ast.SemanticMeaningType
	case ast.IsLiteralTypeNode(parent):
		return ast.SemanticMeaningType | ast.SemanticMeaningValue
	default:
		return ast.SemanticMeaningValue
	}
}
func getMeaningFromDeclaration(node ast.Handle) ast.SemanticMeaning {
	switch node.Kind {
	case ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement, ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindCatchClause, ast.KindJsxAttribute:
		return ast.SemanticMeaningValue
	case ast.KindTypeParameter, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindTypeLiteral:
		return ast.SemanticMeaningType
	case ast.KindEnumMember, ast.KindClassDeclaration:
		return ast.SemanticMeaningValue | ast.SemanticMeaningType
	case ast.KindModuleDeclaration:
		if ast.IsAmbientModule(node) {
			return ast.SemanticMeaningNamespace | ast.SemanticMeaningValue
		} else if ast.GetModuleInstanceState(node) == ast.ModuleInstanceStateInstantiated {
			return ast.SemanticMeaningNamespace | ast.SemanticMeaningValue
		} else {
			return ast.SemanticMeaningNamespace
		}
	case ast.KindEnumDeclaration, ast.KindNamedImports, ast.KindImportSpecifier, ast.KindImportEqualsDeclaration, ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportAssignment, ast.KindExportDeclaration:
		return ast.SemanticMeaningAll
	case ast.KindSourceFile:
		return ast.SemanticMeaningNamespace | ast.SemanticMeaningValue
	}
	return ast.SemanticMeaningAll
}
func getIntersectingMeaningFromDeclarations(node ast.Handle, symbol *ast.Symbol, defaultMeaning ast.SemanticMeaning) ast.SemanticMeaning {
	if node.IsNil() {
		return defaultMeaning
	}
	meaning := getMeaningFromLocation(node)
	declarations := ast.DeclarationNodes(symbol)
	if declarations.Len() == 0 {
		return meaning
	}
	lastIterationMeaning := meaning
	iteration := func(m ast.SemanticMeaning) ast.SemanticMeaning {
		for _, declaration := range declarations.All() {
			declarationMeaning := getMeaningFromDeclaration(declaration)
			if declarationMeaning&m != 0 {
				m |= declarationMeaning
			}
		}
		return m
	}
	meaning = iteration(meaning)
	for meaning != lastIterationMeaning {
		lastIterationMeaning = meaning
		meaning = iteration(meaning)
	}
	return meaning
}

func getAllSuperTypeNodes(node ast.Handle) []ast.Handle {
	if ast.IsInterfaceDeclaration(node) {
		return ast.GetHeritageElements(node, ast.KindExtendsKeyword)
	}
	if ast.IsClassLike(node) {
		var elems []ast.Handle
		if el := ast.GetClassExtendsHeritageElement(node); !el.IsNil() {
			elems = append(elems, el)
		}
		return append(elems, ast.GetImplementsHeritageClauseElements(node)...)
	}
	return nil
}
func getParentSymbolsOfPropertyAccess(location ast.Handle, symbol *ast.Symbol, ch *checker.Checker) []*ast.Symbol {
	if !isRightSideOfPropertyAccess(location) {
		return nil
	}
	lhsType := ch.GetTypeAtLocation(location.Parent().Expression())
	if lhsType == nil {
		return nil
	}
	var possibleSymbols []*checker.Type
	if lhsType.Flags()&checker.TypeFlagsUnionOrIntersection != 0 {
		possibleSymbols = lhsType.Types()
	} else if lhsType.Symbol() != symbol.Parent {
		possibleSymbols = []*checker.Type{lhsType}
	}
	return core.MapNonNil(possibleSymbols, func(t *checker.Type) *ast.Symbol {
		if t.Symbol() != nil && t.Symbol().Flags&(ast.SymbolFlagsClass|ast.SymbolFlagsInterface) != 0 {
			return t.Symbol()
		}
		return nil
	})
}

func getPropertySymbolsFromBaseTypes(symbol *ast.Symbol, propertyName string, checker *checker.Checker, cb func(base *ast.Symbol) *ast.Symbol) *ast.Symbol {
	var seen collections.Set[*ast.Symbol]
	var recur func(*ast.Symbol) *ast.Symbol
	recur = func(symbol *ast.Symbol) *ast.Symbol {
		if symbol.Flags&(ast.SymbolFlagsClass|ast.SymbolFlagsInterface) == 0 || !seen.AddIfAbsent(symbol) {
			return nil
		}
		for _, declaration := range ast.DeclarationNodes(symbol).All() {
			for _, typeReference := range getAllSuperTypeNodes(declaration) {
				if propertyType := checker.GetTypeAtLocation(typeReference); propertyType != nil && propertyType.Symbol() != nil {
					if propertySymbol := checker.GetPropertyOfType(propertyType, propertyName); propertySymbol != nil {
						for _, rootSymbol := range checker.GetRootSymbols(propertySymbol) {
							if result := cb(rootSymbol); result != nil {
								return result
							}
						}
					}
					if result := recur(propertyType.Symbol()); result != nil {
						return result
					}
				}
			}
		}
		return nil
	}
	return recur(symbol)
}
func getPropertySymbolFromBindingElement(checker *checker.Checker, bindingElement ast.Handle) *ast.Symbol {
	if typeOfPattern := checker.GetTypeAtLocation(bindingElement.Parent()); typeOfPattern != nil {
		return checker.GetPropertyOfType(typeOfPattern, bindingElement.Name().Text())
	}
	return nil
}
func getPropertySymbolOfObjectBindingPatternWithoutPropertyName(symbol *ast.Symbol, checker *checker.Checker) *ast.Symbol {
	bindingElement := ast.GetDeclarationOfKind(symbol, ast.KindBindingElement)
	if !bindingElement.IsNil() && isObjectBindingElementWithoutPropertyName(bindingElement) {
		return getPropertySymbolFromBindingElement(checker, bindingElement)
	}
	return nil
}
func getTargetLabel(referenceNode ast.Handle, labelName string) ast.Handle {
	for !referenceNode.IsNil() {
		if referenceNode.Kind == ast.KindLabeledStatement && referenceNode.Label().Text() == labelName {
			return referenceNode.Label()
		}
		referenceNode = referenceNode.Parent()
	}
	return ast.Handle{}
}
func skipConstraint(t *checker.Type, typeChecker *checker.Checker) *checker.Type {
	if t.IsTypeParameter() {
		c := typeChecker.GetBaseConstraintOfType(t)
		if c != nil {
			return c
		}
	}
	return t
}

type caseClauseTrackerState struct {
	existingStrings collections.Set[string]
	existingNumbers collections.Set[jsnum.Number]
	existingBigInts collections.Set[jsnum.PseudoBigInt]
}

type trackerAddValue = any

type trackerHasValue = any
type caseClauseTracker interface {
	addValue(value trackerAddValue)
	hasValue(value trackerHasValue) bool
}

func (c *caseClauseTrackerState) addValue(value trackerAddValue) {
	switch v := value.(type) {
	case string:
		c.existingStrings.Add(v)
	case jsnum.Number:
		c.existingNumbers.Add(v)
	default:
		panic(fmt.Sprintf("Unsupported type: %T", v))
	}
}
func (c *caseClauseTrackerState) hasValue(value trackerHasValue) bool {
	switch v := value.(type) {
	case string:
		return c.existingStrings.Has(v)
	case jsnum.Number:
		return c.existingNumbers.Has(v)
	case jsnum.PseudoBigInt:
		return c.existingBigInts.Has(v)
	default:
		panic(fmt.Sprintf("Unsupported type: %T", v))
	}
}
func newCaseClauseTracker(typeChecker *checker.Checker, clauses []ast.Handle) caseClauseTracker {
	c := &caseClauseTrackerState{existingStrings: collections.Set[string]{}, existingNumbers: collections.Set[jsnum.Number]{}, existingBigInts: collections.Set[jsnum.PseudoBigInt]{}}
	for _, clause := range clauses {
		if !ast.IsDefaultClause(clause) {
			expression := ast.SkipParentheses(clause.Expression())
			if ast.IsLiteralExpression(expression) {
				switch expression.Kind {
				case ast.KindNoSubstitutionTemplateLiteral, ast.KindStringLiteral:
					c.existingStrings.Add(expression.Text())
				case ast.KindNumericLiteral:
					c.existingNumbers.Add(jsnum.FromString(expression.Text()))
				case ast.KindBigIntLiteral:
					c.existingBigInts.Add(jsnum.ParseValidBigInt(expression.Text()))
				}
			} else {
				symbol := typeChecker.GetSymbolAtLocation(clause.Expression())
				if symbol != nil && symbol.ValueDeclaration != 0 && ast.IsEnumMember(ast.NodeOf(symbol.ValueDeclaration)) {
					enumValue := typeChecker.GetConstantValue(ast.NodeOf(symbol.ValueDeclaration))
					if enumValue != nil {
						c.addValue(enumValue)
					}
				}
			}
		}
	}
	return c
}
func RangeContainsRange(r1 core.TextRange, r2 core.TextRange) bool {
	return startEndContainsRange(r1.Pos(), r1.End(), r2)
}
func startEndContainsRange(start int, end int, textRange core.TextRange) bool {
	return start <= textRange.Pos() && end >= textRange.End()
}
func getPossibleGenericSignatures(called ast.Handle, typeArgumentCount int, c *checker.Checker) []*checker.Signature {
	typeAtLocation := c.GetTypeAtLocation(called)
	if ast.IsOptionalChain(called.Parent()) {
		typeAtLocation = removeOptionality(typeAtLocation, ast.IsOptionalChainRoot(called.Parent()), true, c)
	}
	var signatures []*checker.Signature
	if ast.IsNewExpression(called.Parent()) {
		signatures = c.GetSignaturesOfType(typeAtLocation, checker.SignatureKindConstruct)
	} else {
		signatures = c.GetSignaturesOfType(typeAtLocation, checker.SignatureKindCall)
	}
	return core.Filter(signatures, func(s *checker.Signature) bool {
		return s.TypeParameters() != nil && len(s.TypeParameters()) >= typeArgumentCount
	})
}
func removeOptionality(t *checker.Type, isOptionalExpression bool, isOptionalChain bool, c *checker.Checker) *checker.Type {
	if isOptionalExpression {
		return c.GetNonNullableType(t)
	} else if isOptionalChain {
		return c.GetNonOptionalType(t)
	}
	return t
}
func isNoSubstitutionTemplateLiteral(node ast.Handle) bool {
	return node.Kind == ast.KindNoSubstitutionTemplateLiteral
}
func isTaggedTemplateExpression(node ast.Handle) bool {
	return node.Kind == ast.KindTaggedTemplateExpression
}
func isInsideTemplateLiteral(node ast.Handle, position int, sourceFile *ast.SourceFile) bool {
	return ast.IsTemplateLiteralKind(node.Kind) && (scanner.GetTokenPosOfNode(node, sourceFile, false) < position && position < node.End() || (ast.IsUnterminatedLiteral(node) && position == node.End()))
}

func isTemplateHead(node ast.Handle) bool {
	return node.Kind == ast.KindTemplateHead
}
func isTemplateTail(node ast.Handle) bool {
	return node.Kind == ast.KindTemplateTail
}
func findPrecedingMatchingToken(token ast.Handle, matchingTokenKind ast.Kind, sourceFile *ast.SourceFile) ast.Handle {
	closeTokenText := scanner.TokenToString(token.Kind)
	matchingTokenText := scanner.TokenToString(matchingTokenKind)
	bestGuessIndex := strings.LastIndex(sourceFile.Text(), matchingTokenText)
	if bestGuessIndex == -1 {
		return ast.Handle{}
	}
	if strings.LastIndex(sourceFile.Text(), closeTokenText) < bestGuessIndex {
		nodeAtGuess := astnav.FindPrecedingToken(sourceFile, bestGuessIndex+1)
		if !nodeAtGuess.IsNil() && nodeAtGuess.Kind == matchingTokenKind {
			return nodeAtGuess
		}
	}
	tokenKind := token.Kind
	remainingMatchingTokens := 0
	for {
		preceding := astnav.FindPrecedingToken(sourceFile, token.Pos())
		if preceding.IsNil() {
			return ast.Handle{}
		}
		token = preceding
		switch token.Kind {
		case matchingTokenKind:
			if remainingMatchingTokens == 0 {
				return token
			}
			remainingMatchingTokens--
		case tokenKind:
			remainingMatchingTokens++
		}
	}
}
func findContainingList(node ast.Handle, file *ast.SourceFile) ast.ListRef {
	var list ast.ListRef
	visitNode := func(n ast.Handle, visitor *ast.HandleVisitor) ast.Handle {
		return n
	}
	visitNodes := func(nodes ast.ListRef, visitor *ast.HandleVisitor) ast.ListRef {
		if nodes != 0 && RangeContainsRange(node.Store().ListLoc(nodes), node.Loc()) {
			list = nodes
		}
		return nodes
	}
	astnav.VisitEachChildAndJSDoc(node.Parent(), file, visitNode, visitNodes)
	return list
}
func getLeadingCommentRangesOfNode(node ast.Handle, file *ast.SourceFile) iter.Seq[ast.CommentRange] {
	if node.Kind == ast.KindJsxText {
		return nil
	}
	return scanner.GetLeadingCommentRanges(file.Text(), node.Pos())
}

func getChildrenFromNonJSDocNode(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	var childNodes []ast.Handle
	node.ForEachChild(func(child ast.Handle) bool {
		childNodes = append(childNodes, child)
		return false
	})
	if len(childNodes) == 0 {
		return nil
	}
	var children []ast.Handle
	pos := node.Pos()
	for _, child := range childNodes {
		scanner := scanner.GetScannerForSourceFile(sourceFile, pos)
		for pos < child.Pos() {
			token := scanner.Token()
			tokenFullStart := scanner.TokenFullStart()
			tokenEnd := scanner.TokenEnd()
			children = append(children, sourceFile.GetOrCreateToken(token, tokenFullStart, tokenEnd, node, scanner.TokenFlags()))
			pos = tokenEnd
			scanner.Scan()
		}
		children = append(children, child)
		pos = child.End()
	}
	scanner := scanner.GetScannerForSourceFile(sourceFile, pos)
	for pos < node.End() {
		token := scanner.Token()
		tokenFullStart := scanner.TokenFullStart()
		tokenEnd := scanner.TokenEnd()
		children = append(children, sourceFile.GetOrCreateToken(token, tokenFullStart, tokenEnd, node, scanner.TokenFlags()))
		pos = tokenEnd
		scanner.Scan()
	}
	return children
}

func getContainingObjectLiteralElement(node ast.Handle) ast.Handle {
	element := getContainingObjectLiteralElementWorker(node)
	if !element.IsNil() && (ast.IsObjectLiteralExpression(element.Parent()) || ast.IsJsxAttributes(element.Parent())) {
		return element
	}
	return ast.Handle{}
}
func getContainingObjectLiteralElementWorker(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindNumericLiteral:
		if node.Parent().Kind == ast.KindComputedPropertyName {
			if isObjectLiteralOrJsxElement(node.Parent().Parent()) {
				return node.Parent().Parent()
			}
			return ast.Handle{}
		}
		fallthrough
	case ast.KindIdentifier, ast.KindJsxNamespacedName:
		if isObjectLiteralOrJsxElement(node.Parent()) && (node.Parent().Parent().Kind == ast.KindObjectLiteralExpression || node.Parent().Parent().Kind == ast.KindJsxAttributes) && node.Parent().Name() == node {
			return node.Parent()
		}
	}
	return ast.Handle{}
}
func isObjectLiteralOrJsxElement(node ast.Handle) bool {
	return ast.IsObjectLiteralElement(node) || ast.IsJsxAttribute(node) || ast.IsJsxSpreadAttribute(node)
}

func nodeSeenTracker() func(ast.Handle) bool {
	var seen collections.Set[ast.Handle]
	return func(node ast.Handle) bool {
		return seen.AddIfAbsent(node)
	}
}

func toContextRange(textRange *core.TextRange, contextFile *ast.SourceFile, context ast.Handle) *core.TextRange {
	if context.IsNil() {
		return textRange
	}
	contextRange := getRangeOfNode(context, contextFile, ast.Handle{})
	if contextRange.Pos() != textRange.Pos() || contextRange.End() != textRange.End() {
		return &contextRange
	}
	return nil
}
func getReferenceAtPosition(sourceFile *ast.SourceFile, position int, program *compiler.Program) *refInfo {
	if referencePath := findReferenceInPosition(sourceFile.ReferencedFiles, position); referencePath != nil {
		if file := program.GetSourceFileFromReference(sourceFile, referencePath); file != nil {
			return &refInfo{reference: referencePath, fileName: file.FileName(), file: file, unverified: false}
		}
		return nil
	}
	if typeReferenceDirective := findReferenceInPosition(sourceFile.TypeReferenceDirectives, position); typeReferenceDirective != nil {
		if reference := program.GetResolvedTypeReferenceDirectiveFromTypeReferenceDirective(typeReferenceDirective, sourceFile); reference != nil {
			if file := program.GetSourceFile(reference.ResolvedFileName); file != nil {
				return &refInfo{reference: typeReferenceDirective, fileName: file.FileName(), file: file, unverified: false}
			}
		}
		return nil
	}
	if libReferenceDirective := findReferenceInPosition(sourceFile.LibReferenceDirectives, position); libReferenceDirective != nil {
		if file := program.GetLibFileFromReference(libReferenceDirective); file != nil {
			return &refInfo{reference: libReferenceDirective, fileName: file.FileName(), file: file, unverified: false}
		}
		return nil
	}
	if len(sourceFile.Imports()) == 0 && len(sourceFile.ModuleAugmentations) == 0 {
		return nil
	}
	node := astnav.GetTouchingToken(sourceFile, position)
	if !isModuleSpecifierLike(node) || !tspath.IsExternalModuleNameRelative(node.Text()) {
		return nil
	}
	if resolution := program.GetResolvedModuleFromModuleSpecifier(sourceFile, node); resolution != nil {
		verifiedFileName := resolution.ResolvedFileName
		fileName := resolution.ResolvedFileName
		if fileName == "" {
			fileName = tspath.ResolvePath(tspath.GetDirectoryPath(sourceFile.FileName()), node.Text())
		}
		return &refInfo{file: program.GetSourceFile(fileName), fileName: fileName, reference: nil, unverified: verifiedFileName != ""}
	}
	return nil
}
func getContextualTypeFromParent(node ast.Handle, typeChecker *checker.Checker, contextFlags checker.ContextFlags) *checker.Type {
	parent := ast.WalkUpParenthesizedExpressions(node.Parent())
	switch parent.Kind {
	case ast.KindNewExpression:
		return typeChecker.GetContextualType(parent, contextFlags)
	case ast.KindBinaryExpression:
		if isEqualityOperatorKind(parent.BinaryExpressionOperatorToken().Kind) {
			return typeChecker.GetTypeAtLocation(core.IfElse(node == parent.BinaryExpressionRight(), parent.BinaryExpressionLeft(), parent.BinaryExpressionRight()))
		}
		return typeChecker.GetContextualType(node, contextFlags)
	case ast.KindCaseClause:
		return getSwitchedType(parent, typeChecker)
	default:
		return typeChecker.GetContextualType(node, contextFlags)
	}
}
func getContextualTypeFromParentOrAncestorTypeNode(node ast.Handle, typeChecker *checker.Checker) *checker.Type {
	if node.Flags()&ast.NodeFlagsJSDoc != 0 && node.Flags()&ast.NodeFlagsJavaScriptFile == 0 {
		return nil
	}
	contextualType := getContextualTypeFromParent(node, typeChecker, checker.ContextFlagsNone)
	if contextualType != nil {
		return contextualType
	}
	if ancestorTypeNode := getAncestorTypeNode(node); !ancestorTypeNode.IsNil() {
		return typeChecker.GetTypeAtLocation(ancestorTypeNode)
	}
	return nil
}
func getAncestorTypeNode(node ast.Handle) ast.Handle {
	var lastTypeNode ast.Handle
	ast.FindAncestor(node, func(n ast.Handle) bool {
		if ast.IsTypeNode(n) {
			lastTypeNode = n
		}
		return !ast.IsQualifiedName(n.Parent()) && !ast.IsTypeNode(n.Parent()) && !ast.IsTypeElement(n.Parent())
	})
	return lastTypeNode
}
func isSourceFileWithGlobalExports(node ast.Handle) bool {
	if node.IsNil() || !ast.IsSourceFile(node) {
		return false
	}
	sf := ast.GetSourceFileOfNode(node)
	return sf != nil && sf.GlobalExports != nil
}
