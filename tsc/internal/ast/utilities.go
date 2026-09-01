package ast

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// Atomic ids

var (
	nextNodeId   atomic.Uint64
	nextSymbolId atomic.Uint64
)

func GetNodeId(node *Node) NodeId {
	id := node.id.Load()
	if id == 0 {
		// Worst case, we burn a few ids if we have to CAS.
		id = nextNodeId.Add(1)
		if !node.id.CompareAndSwap(0, id) {
			id = node.id.Load()
		}
	}
	return NodeId(id)
}

func GetSymbolId(symbol *Symbol) SymbolId {
	id := symbol.id.Load()
	if id == 0 {
		// Worst case, we burn a few ids if we have to CAS.
		id = nextSymbolId.Add(1)
		if !symbol.id.CompareAndSwap(0, id) {
			id = symbol.id.Load()
		}
	}
	return SymbolId(id)
}

func GetSymbolTable(data *SymbolTable) SymbolTable {
	if *data == nil {
		*data = make(SymbolTable)
	}
	return *data
}

func GetMembers(symbol *Symbol) SymbolTable {
	return GetSymbolTable(&symbol.Members)
}

func GetExports(symbol *Symbol) SymbolTable {
	return GetSymbolTable(&symbol.Exports)
}

func GetLocals(container Handle) SymbolTable {
	if container.IsNil() {
		return nil
	}
	if locals := container.Locals(); locals != nil {
		return locals
	}
	locals := make(SymbolTable)
	container.SetLocals(locals)
	return locals
}

// Determines if a node is missing (either `nil` or empty)
func NodeIsMissing(node Handle) bool {
	return node.IsNil() || node.Loc().Pos() == node.Loc().End() && node.Loc().Pos() >= 0 && node.Kind() != KindEndOfFile
}

// Determines if a node is present
func NodeIsPresent(node Handle) bool {
	return !NodeIsMissing(node)
}

// Determines if a node contains synthetic positions
func NodeIsSynthesized(node Handle) bool {
	return PositionIsSynthesized(node.Loc().Pos()) || PositionIsSynthesized(node.Loc().End())
}

func RangeIsSynthesized(loc core.TextRange) bool {
	return PositionIsSynthesized(loc.Pos()) || PositionIsSynthesized(loc.End())
}

// Determines whether a position is synthetic
func PositionIsSynthesized(pos int) bool {
	return pos < 0
}

func FindLastVisibleNode(nodes []Handle) Handle {
	fromEnd := 1
	for fromEnd <= len(nodes) && nodes[len(nodes)-fromEnd].Flags()&NodeFlagsReparsed != 0 {
		fromEnd++
	}
	if fromEnd <= len(nodes) {
		return nodes[len(nodes)-fromEnd]
	}
	return Handle{}
}

func NodeKindIs(node Handle, kinds ...Kind) bool {
	return slices.Contains(kinds, node.Kind())
}

func IsModifier(node Handle) bool {
	return IsModifierKind(node.Kind())
}

func IsModifierLike(node Handle) bool {
	return IsModifier(node) || IsDecorator(node)
}

func IsCompoundAssignment(token Kind) bool {
	return token >= KindFirstCompoundAssignment && token <= KindLastCompoundAssignment
}

func IsAssignmentExpression(node Handle, excludeCompoundAssignment bool) bool {
	if node.Kind() == KindBinaryExpression {
		return (node.BinaryExpressionOperatorToken().Kind() == KindEqualsToken || !excludeCompoundAssignment && IsAssignmentOperator(node.BinaryExpressionOperatorToken().Kind())) &&
			IsLeftHandSideExpression(node.BinaryExpressionLeft())
	}
	return false
}

func GetRightMostAssignedExpression(node Handle) Handle {
	for IsAssignmentExpression(node, false /*excludeCompoundAssignment*/) {
		node = node.BinaryExpressionRight()
	}
	return node
}

func IsDestructuringAssignment(node Handle) bool {
	if IsAssignmentExpression(node, true /*excludeCompoundAssignment*/) {
		kind := node.BinaryExpressionLeft().Kind()
		return kind == KindObjectLiteralExpression || kind == KindArrayLiteralExpression
	}
	return false
}

func IsObjectBindingOrAssignmentElement(node Handle) bool {
	switch node.Kind() {
	case KindBindingElement,
		KindPropertyAssignment,
		KindShorthandPropertyAssignment,
		KindSpreadAssignment:
		return true
	}
	return false
}

func IsArrayBindingOrAssignmentElement(node Handle) bool {
	switch node.Kind() {
	case KindBindingElement,
		KindOmittedExpression,
		KindSpreadElement,
		KindArrayLiteralExpression,
		KindObjectLiteralExpression,
		KindIdentifier,
		KindPropertyAccessExpression,
		KindElementAccessExpression:
		return true
	}
	return IsAssignmentExpression(node, true /*excludeCompoundAssignment*/)
}

func IsBindingPattern(node Handle) bool {
	return node.Kind() == KindObjectBindingPattern || node.Kind() == KindArrayBindingPattern
}

func IsForInOrOfStatement(node Handle) bool {
	return !node.IsNil() && (node.Kind() == KindForInStatement || node.Kind() == KindForOfStatement)
}

// A node is an assignment target if it is on the left hand side of an '=' token, if it is parented by a property
// assignment in an object literal that is an assignment target, or if it is parented by an array literal that is
// an assignment target. Examples include 'a = xxx', '{ p: a } = xxx', '[{ a }] = xxx'.
// (Note that `p` is not a target in the above examples, only `a`.)
func IsAssignmentTarget(node Handle) bool {
	return !GetAssignmentTarget(node).IsNil()
}

// Returns the BinaryExpression, PrefixUnaryExpression, PostfixUnaryExpression, or ForInOrOfStatement that references
// the given node as an assignment target
func GetAssignmentTarget(node Handle) Handle {
	for {
		parent := node.Parent()
		switch parent.Kind() {
		case KindBinaryExpression:
			if IsAssignmentOperator(parent.BinaryExpressionOperatorToken().Kind()) && parent.BinaryExpressionLeft() == node {
				return parent
			}
			return Handle{}
		case KindPrefixUnaryExpression:
			if parent.PrefixUnaryExpressionOperator() == KindPlusPlusToken || parent.PrefixUnaryExpressionOperator() == KindMinusMinusToken {
				return parent
			}
			return Handle{}
		case KindPostfixUnaryExpression:
			if parent.PostfixUnaryExpressionOperator() == KindPlusPlusToken || parent.PostfixUnaryExpressionOperator() == KindMinusMinusToken {
				return parent
			}
			return Handle{}
		case KindForInStatement, KindForOfStatement:
			if parent.Initializer() == node {
				return parent
			}
			return Handle{}
		case KindParenthesizedExpression, KindArrayLiteralExpression, KindSpreadElement, KindNonNullExpression:
			node = parent
		case KindSpreadAssignment:
			node = parent.Parent()
		case KindShorthandPropertyAssignment:
			if parent.ShorthandPropertyAssignmentName() != node {
				return Handle{}
			}
			node = parent.Parent()
		case KindPropertyAssignment:
			if parent.PropertyAssignmentName() == node {
				return Handle{}
			}
			node = parent.Parent()
		default:
			return Handle{}
		}
	}
}

func IsLogicalBinaryOperator(token Kind) bool {
	return token == KindBarBarToken || token == KindAmpersandAmpersandToken
}

func IsLogicalOrCoalescingBinaryOperator(token Kind) bool {
	return IsLogicalBinaryOperator(token) || token == KindQuestionQuestionToken
}

func IsLogicalOrCoalescingBinaryExpression(expr Handle) bool {
	return IsBinaryExpression(expr) && IsLogicalOrCoalescingBinaryOperator(expr.BinaryExpressionOperatorToken().Kind())
}

func IsLogicalOrCoalescingAssignmentExpression(expr Handle) bool {
	return IsBinaryExpression(expr) && IsLogicalOrCoalescingAssignmentOperator(expr.BinaryExpressionOperatorToken().Kind())
}

func IsLogicalExpression(node Handle) bool {
	for {
		if node.Kind() == KindParenthesizedExpression {
			node = node.Expression()
		} else if node.Kind() == KindPrefixUnaryExpression && node.PrefixUnaryExpressionOperator() == KindExclamationToken {
			node = node.PrefixUnaryExpressionOperand()
		} else {
			return IsLogicalOrCoalescingBinaryExpression(node)
		}
	}
}

func IsAccessor(node Handle) bool {
	return node.Kind() == KindGetAccessor || node.Kind() == KindSetAccessor
}

func IsPropertyNameLiteral(node Handle) bool {
	switch node.Kind() {
	case KindIdentifier,
		KindStringLiteral,
		KindNoSubstitutionTemplateLiteral,
		KindNumericLiteral:
		return true
	}
	return false
}

func IsMemberName(node Handle) bool {
	return node.Kind() == KindIdentifier || node.Kind() == KindPrivateIdentifier
}

func IsEntityName(node Handle) bool {
	return node.Kind() == KindIdentifier || node.Kind() == KindQualifiedName
}

func IsPropertyName(node Handle) bool {
	switch node.Kind() {
	case KindIdentifier,
		KindPrivateIdentifier,
		KindStringLiteral,
		KindNumericLiteral,
		KindComputedPropertyName:
		return true
	}
	return false
}

// Return true if the given identifier is classified as an IdentifierName by inspecting the parent of the node
func IsIdentifierName(node Handle) bool {
	parent := node.Parent()
	switch parent.Kind() {
	case KindPropertyDeclaration, KindPropertySignature, KindMethodDeclaration, KindMethodSignature, KindGetAccessor,
		KindSetAccessor, KindEnumMember, KindPropertyAssignment, KindPropertyAccessExpression:
		return parent.Name() == node
	case KindQualifiedName:
		return parent.QualifiedNameRight() == node
	case KindBindingElement:
		return parent.PropertyName() == node
	case KindImportSpecifier:
		return parent.PropertyName() == node
	case KindExportSpecifier, KindJsxAttribute, KindJsxSelfClosingElement, KindJsxOpeningElement, KindJsxClosingElement:
		return true
	}
	return false
}

func IsPushOrUnshiftIdentifier(node Handle) bool {
	text := node.Text()
	return text == "push" || text == "unshift"
}

func IsBooleanLiteral(node Handle) bool {
	return node.Kind() == KindTrueKeyword || node.Kind() == KindFalseKeyword
}

func IsLiteralExpression(node Handle) bool {
	return IsLiteralKind(node.Kind())
}

func IsStringLiteralLike(node Handle) bool {
	switch node.Kind() {
	case KindStringLiteral, KindNoSubstitutionTemplateLiteral:
		return true
	}
	return false
}

func IsStringOrNumericLiteralLike(node Handle) bool {
	return IsStringLiteralLike(node) || IsNumericLiteral(node)
}

func IsSignedNumericLiteral(node Handle) bool {
	if node.Kind() == KindPrefixUnaryExpression {
		return (node.PrefixUnaryExpressionOperator() == KindPlusToken || node.PrefixUnaryExpressionOperator() == KindMinusToken) && IsNumericLiteral(node.PrefixUnaryExpressionOperand())
	}
	return false
}

// Determines if a node is part of an OptionalChain
func IsOptionalChain(node Handle) bool {
	if node.Flags()&NodeFlagsOptionalChain != 0 {
		switch node.Kind() {
		case KindPropertyAccessExpression,
			KindElementAccessExpression,
			KindCallExpression,
			KindNonNullExpression:
			return true
		}
	}
	return false
}

func getQuestionDotToken(node Handle) Handle {
	return node.QuestionDotToken()
}

// Determines if node is the root expression of an OptionalChain
func IsOptionalChainRoot(node Handle) bool {
	return IsOptionalChain(node) && !IsNonNullExpression(node) && !getQuestionDotToken(node).IsNil()
}

// Determines whether a node is the outermost `OptionalChain` in an ECMAScript `OptionalExpression`:
//
//  1. For `a?.b.c`, the outermost chain is `a?.b.c` (`c` is the end of the chain starting at `a?.`)
//  2. For `a?.b!`, the outermost chain is `a?.b` (`b` is the end of the chain starting at `a?.`)
//  3. For `(a?.b.c).d`, the outermost chain is `a?.b.c` (`c` is the end of the chain starting at `a?.` since parens end the chain)
//  4. For `a?.b.c?.d`, both `a?.b.c` and `a?.b.c?.d` are outermost (`c` is the end of the chain starting at `a?.`, and `d` is
//     the end of the chain starting at `c?.`)
//  5. For `a?.(b?.c).d`, both `b?.c` and `a?.(b?.c)d` are outermost (`c` is the end of the chain starting at `b`, and `d` is
//     the end of the chain starting at `a?.`)
func IsOutermostOptionalChain(node Handle) bool {
	parent := node.Parent()
	return !IsOptionalChain(parent) || // cases 1, 2, and 3
		IsOptionalChainRoot(parent) || // case 4
		node != parent.Expression() // case 5
}

// Determines whether a node is the expression preceding an optional chain (i.e. `a` in `a?.b`).
func IsExpressionOfOptionalChainRoot(node Handle) bool {
	return IsOptionalChainRoot(node.Parent()) && node.Parent().Expression() == node
}

func IsNullishCoalesce(node Handle) bool {
	return node.Kind() == KindBinaryExpression && node.BinaryExpressionOperatorToken().Kind() == KindQuestionQuestionToken
}

func IsAssertionExpression(node Handle) bool {
	kind := node.Kind()
	return kind == KindTypeAssertionExpression || kind == KindAsExpression
}

func isLeftHandSideExpressionKind(kind Kind) bool {
	switch kind {
	case KindPropertyAccessExpression, KindElementAccessExpression, KindNewExpression, KindCallExpression,
		KindJsxElement, KindJsxSelfClosingElement, KindJsxFragment, KindTaggedTemplateExpression, KindArrayLiteralExpression,
		KindParenthesizedExpression, KindObjectLiteralExpression, KindClassExpression, KindFunctionExpression, KindIdentifier,
		KindPrivateIdentifier, KindRegularExpressionLiteral, KindNumericLiteral, KindBigIntLiteral, KindStringLiteral,
		KindNoSubstitutionTemplateLiteral, KindTemplateExpression, KindFalseKeyword, KindNullKeyword, KindThisKeyword,
		KindTrueKeyword, KindSuperKeyword, KindNonNullExpression, KindExpressionWithTypeArguments, KindMetaProperty,
		KindImportKeyword, KindMissingDeclaration:
		return true
	}
	return false
}

// Determines whether a node is a LeftHandSideExpression based only on its kind.
func IsLeftHandSideExpression(node Handle) bool {
	return isLeftHandSideExpressionKind(SkipPartiallyEmittedExpressions(node).Kind())
}

func isUnaryExpressionKind(kind Kind) bool {
	switch kind {
	case KindPrefixUnaryExpression,
		KindPostfixUnaryExpression,
		KindDeleteExpression,
		KindTypeOfExpression,
		KindVoidExpression,
		KindAwaitExpression,
		KindTypeAssertionExpression:
		return true
	}
	return isLeftHandSideExpressionKind(kind)
}

// Determines whether a node is a UnaryExpression based only on its kind.
func IsUnaryExpression(node Handle) bool {
	return isUnaryExpressionKind(SkipPartiallyEmittedExpressions(node).Kind())
}

func isExpressionKind(kind Kind) bool {
	switch kind {
	case KindConditionalExpression,
		KindYieldExpression,
		KindArrowFunction,
		KindBinaryExpression,
		KindSpreadElement,
		KindAsExpression,
		KindOmittedExpression,
		KindPartiallyEmittedExpression,
		KindSatisfiesExpression:
		return true
	}
	return isUnaryExpressionKind(kind)
}

// Determines whether a node is an expression based only on its kind.
func IsExpression(node Handle) bool {
	return isExpressionKind(SkipPartiallyEmittedExpressions(node).Kind())
}

func IsCommaExpression(node Handle) bool {
	return node.Kind() == KindBinaryExpression && node.BinaryExpressionOperatorToken().Kind() == KindCommaToken
}

func IsCommaSequence(node Handle) bool {
	return IsCommaExpression(node)
}

func IsIterationStatement(node Handle, lookInLabeledStatements bool) bool {
	switch node.Kind() {
	case KindForStatement,
		KindForInStatement,
		KindForOfStatement,
		KindDoStatement,
		KindWhileStatement:
		return true
	case KindLabeledStatement:
		return lookInLabeledStatements && IsIterationStatement(node.Statement(), lookInLabeledStatements)
	}

	return false
}

// Determines if a node is a property or element access expression
func IsAccessExpression(node Handle) bool {
	return node.Kind() == KindPropertyAccessExpression || node.Kind() == KindElementAccessExpression
}

func isFunctionLikeDeclarationKind(kind Kind) bool {
	switch kind {
	case KindFunctionDeclaration,
		KindMethodDeclaration,
		KindConstructor,
		KindGetAccessor,
		KindSetAccessor,
		KindFunctionExpression,
		KindArrowFunction:
		return true
	}
	return false
}

// Determines if a node is function-like (but is not a signature declaration)
func IsFunctionLikeDeclaration(node Handle) bool {
	// TODO(rbuckton): Move `!node.IsNil()` test to call sites
	return !node.IsNil() && isFunctionLikeDeclarationKind(node.Kind())
}

func IsFunctionLikeKind(kind Kind) bool {
	switch kind {
	case KindMethodSignature,
		KindCallSignature,
		KindJSDocSignature,
		KindConstructSignature,
		KindIndexSignature,
		KindFunctionType,
		KindConstructorType:
		return true
	}
	return isFunctionLikeDeclarationKind(kind)
}

// Determines if a node is function- or signature-like.
func IsFunctionLike(node Handle) bool {
	// TODO(rbuckton): Move `!node.IsNil()` test to call sites
	return !node.IsNil() && IsFunctionLikeKind(node.Kind())
}

func IsFunctionLikeOrClassStaticBlockDeclaration(node Handle) bool {
	return !node.IsNil() && (IsFunctionLike(node) || IsClassStaticBlockDeclaration(node))
}

func IsFunctionOrSourceFile(node Handle) bool {
	return IsFunctionLike(node) || IsSourceFile(node)
}

func IsClassLike(node Handle) bool {
	return node.Kind() == KindClassDeclaration || node.Kind() == KindClassExpression
}

func IsClassOrInterfaceLike(node Handle) bool {
	return node.Kind() == KindClassDeclaration || node.Kind() == KindClassExpression || node.Kind() == KindInterfaceDeclaration
}

func IsClassElement(node Handle) bool {
	switch node.Kind() {
	case KindConstructor,
		KindPropertyDeclaration,
		KindMethodDeclaration,
		KindGetAccessor,
		KindSetAccessor,
		KindIndexSignature,
		KindClassStaticBlockDeclaration,
		KindSemicolonClassElement:
		return true
	}
	return false
}

func IsMethodOrAccessor(node Handle) bool {
	switch node.Kind() {
	case KindMethodDeclaration, KindGetAccessor, KindSetAccessor:
		return true
	}
	return false
}

func IsPrivateIdentifierClassElementDeclaration(node Handle) bool {
	return (IsPropertyDeclaration(node) || IsMethodOrAccessor(node)) && IsPrivateIdentifier(node.Name())
}

func IsObjectLiteralOrClassExpressionMethodOrAccessor(node Handle) bool {
	kind := node.Kind()
	return (kind == KindMethodDeclaration || kind == KindGetAccessor || kind == KindSetAccessor) &&
		(node.Parent().Kind() == KindObjectLiteralExpression || node.Parent().Kind() == KindClassExpression)
}

func IsTypeElement(node Handle) bool {
	switch node.Kind() {
	case KindConstructSignature,
		KindCallSignature,
		KindPropertySignature,
		KindMethodSignature,
		KindIndexSignature,
		KindGetAccessor,
		KindSetAccessor,
		KindNotEmittedTypeElement:
		return true
	}
	return false
}

func IsObjectLiteralElement(node Handle) bool {
	switch node.Kind() {
	case KindPropertyAssignment,
		KindShorthandPropertyAssignment,
		KindSpreadAssignment,
		KindMethodDeclaration,
		KindGetAccessor,
		KindSetAccessor:
		return true
	}
	return false
}

func IsObjectLiteralMethod(node Handle) bool {
	return !node.IsNil() && node.Kind() == KindMethodDeclaration && node.Parent().Kind() == KindObjectLiteralExpression
}

func IsAutoAccessorPropertyDeclaration(node Handle) bool {
	return IsPropertyDeclaration(node) && HasAccessorModifier(node)
}

func IsParameterPropertyDeclaration(node Handle, parent Handle) bool {
	return IsParameterDeclaration(node) && HasSyntacticModifier(node, ModifierFlagsParameterPropertyModifier) && parent.Kind() == KindConstructor
}

func IsJsxChild(node Handle) bool {
	switch node.Kind() {
	case KindJsxElement,
		KindJsxExpression,
		KindJsxSelfClosingElement,
		KindJsxText,
		KindJsxFragment:
		return true
	}
	return false
}

func IsJsxAttributeLike(node Handle) bool {
	return IsJsxAttribute(node) || IsJsxSpreadAttribute(node)
}

func isDeclarationStatementKind(kind Kind) bool {
	switch kind {
	case KindFunctionDeclaration,
		KindMissingDeclaration,
		KindClassDeclaration,
		KindInterfaceDeclaration,
		KindTypeAliasDeclaration,
		KindJSTypeAliasDeclaration,
		KindEnumDeclaration,
		KindModuleDeclaration,
		KindImportDeclaration,
		KindJSImportDeclaration,
		KindImportEqualsDeclaration,
		KindExportDeclaration,
		KindExportAssignment,
		KindNamespaceExportDeclaration:
		return true
	}
	return false
}

// Determines whether a node is a DeclarationStatement. Ideally this does not use Parent pointers, but it may use them
// to rule out a Block node that is part of `try` or `catch` or is the Block-like body of a function.
//
// NOTE: ECMA262 would just call this a Declaration
func IsDeclarationStatement(node Handle) bool {
	return isDeclarationStatementKind(node.Kind())
}

func isStatementKindButNotDeclarationKind(kind Kind) bool {
	switch kind {
	case KindBreakStatement,
		KindContinueStatement,
		KindDebuggerStatement,
		KindDoStatement,
		KindExpressionStatement,
		KindEmptyStatement,
		KindForInStatement,
		KindForOfStatement,
		KindForStatement,
		KindIfStatement,
		KindLabeledStatement,
		KindReturnStatement,
		KindSwitchStatement,
		KindThrowStatement,
		KindTryStatement,
		KindVariableStatement,
		KindWhileStatement,
		KindWithStatement,
		KindNotEmittedStatement:
		return true
	}
	return false
}

// Determines whether a node is a Statement that is not also a Declaration. Ideally this does not use Parent pointers,
// but it may use them to rule out a Block node that is part of `try` or `catch` or is the Block-like body of a function.
//
// NOTE: ECMA262 would just call this a Statement
func IsStatementButNotDeclaration(node Handle) bool {
	return isStatementKindButNotDeclarationKind(node.Kind())
}

// Determines whether a node is a Statement. Ideally this does not use Parent pointers, but it may use
// them to rule out a Block node that is part of `try` or `catch` or is the Block-like body of a function.
//
// NOTE: ECMA262 would call this either a StatementListItem or ModuleListItem
func IsStatement(node Handle) bool {
	kind := node.Kind()
	return isStatementKindButNotDeclarationKind(kind) || isDeclarationStatementKind(kind) || isBlockStatement(node)
}

// Determines whether a node is a BlockStatement. If parents are available, this ensures the Block is
// not part of a `try` statement, `catch` clause, or the Block-like body of a function
func isBlockStatement(node Handle) bool {
	if node.Kind() != KindBlock {
		return false
	}
	if !node.Parent().IsNil() && (node.Parent().Kind() == KindTryStatement || node.Parent().Kind() == KindCatchClause) {
		return false
	}
	return !IsFunctionBlock(node)
}

// Determines whether a node is the Block-like body of a function by walking the parent of the node
func IsFunctionBlock(node Handle) bool {
	return !node.IsNil() && node.Kind() == KindBlock && !node.Parent().IsNil() && IsFunctionLike(node.Parent())
}

func IsBlockOrCatchScoped(declaration Handle) bool {
	return GetCombinedNodeFlags(declaration)&NodeFlagsBlockScoped != 0 || IsCatchClauseVariableDeclarationOrBindingElement(declaration)
}

func IsCatchClauseVariableDeclarationOrBindingElement(declaration Handle) bool {
	node := GetRootDeclaration(declaration)
	return node.Kind() == KindVariableDeclaration && node.Parent().Kind() == KindCatchClause
}

func IsTypeNodeKind(kind Kind) bool {
	switch kind {
	case KindAnyKeyword,
		KindUnknownKeyword,
		KindNumberKeyword,
		KindBigIntKeyword,
		KindObjectKeyword,
		KindBooleanKeyword,
		KindStringKeyword,
		KindSymbolKeyword,
		KindVoidKeyword,
		KindUndefinedKeyword,
		KindNeverKeyword,
		KindIntrinsicKeyword,
		KindExpressionWithTypeArguments,
		KindJSDocAllType,
		KindJSDocNullableType,
		KindJSDocNonNullableType,
		KindJSDocOptionalType,
		KindJSDocVariadicType:
		return true
	}
	return kind >= KindFirstTypeNode && kind <= KindLastTypeNode
}

func IsTypeNode(node Handle) bool {
	return IsTypeNodeKind(node.Kind())
}

func IsJSDocKind(kind Kind) bool {
	return KindFirstJSDocNode <= kind && kind <= KindLastJSDocNode
}

func IsJSDocTypeAssertion(node Handle) bool {
	if node.IsNil() || !IsParenthesizedExpression(node) || !IsInJSFile(node) {
		return false
	}
	expr := node.Expression()
	return IsAsExpression(expr) && !expr.Type().IsNil() && expr.Type().Flags()&NodeFlagsReparsed != 0
}

func IsPrologueDirective(node Handle) bool {
	return node.Kind() == KindExpressionStatement &&
		node.Expression().Kind() == KindStringLiteral
}

type OuterExpressionKinds uint16

const (
	OEKParentheses                                       OuterExpressionKinds = 1 << 0
	OEKTypeAssertions                                    OuterExpressionKinds = 1 << 1
	OEKNonNullAssertions                                 OuterExpressionKinds = 1 << 2
	OEKPartiallyEmittedExpressions                       OuterExpressionKinds = 1 << 3
	OEKExpressionsWithTypeArguments                      OuterExpressionKinds = 1 << 4
	OEKSatisfies                                         OuterExpressionKinds = 1 << 5
	OEKExcludeJSDocTypeAssertion                         OuterExpressionKinds = 1 << 6
	OEKAssignments                                       OuterExpressionKinds = 1 << 7
	OEKComma                                             OuterExpressionKinds = 1 << 8
	OEKAssertions                                                             = OEKTypeAssertions | OEKNonNullAssertions | OEKSatisfies
	OEKAll                                                                    = OEKParentheses | OEKAssertions | OEKPartiallyEmittedExpressions | OEKExpressionsWithTypeArguments
	OEKAllExceptAssertionsOrExpressionsWithTypeArguments                      = OEKAll &^ OEKAssertions &^ OEKExpressionsWithTypeArguments
	OEKExpressionTypePassthrough                                              = OEKParentheses | OEKAssignments | OEKComma
)

// Determines whether node is an "outer expression" of the provided kinds
func IsOuterExpression(node Handle, kinds OuterExpressionKinds) bool {
	switch node.Kind() {
	case KindParenthesizedExpression:
		return kinds&OEKParentheses != 0 && !(kinds&OEKExcludeJSDocTypeAssertion != 0 && IsJSDocTypeAssertion(node))
	case KindTypeAssertionExpression, KindAsExpression:
		return kinds&OEKTypeAssertions != 0
	case KindSatisfiesExpression:
		return kinds&(OEKExpressionsWithTypeArguments|OEKSatisfies) != 0
	case KindExpressionWithTypeArguments:
		return kinds&OEKExpressionsWithTypeArguments != 0
	case KindNonNullExpression:
		return kinds&OEKNonNullAssertions != 0
	case KindPartiallyEmittedExpression:
		return kinds&OEKPartiallyEmittedExpressions != 0
	case KindBinaryExpression:
		switch node.BinaryExpressionOperatorToken().Kind() {
		case KindEqualsToken:
			return kinds&OEKAssignments != 0
		case KindCommaToken:
			return kinds&OEKComma != 0
		}
	}
	return false
}

// Descends into an expression, skipping past "outer expressions" of the provided kinds
func SkipOuterExpressions(node Handle, kinds OuterExpressionKinds) Handle {
	for IsOuterExpression(node, kinds) {
		if IsBinaryExpression(node) {
			node = node.BinaryExpressionRight()
		} else {
			node = node.Expression()
		}
	}
	return node
}

// Skips past the parentheses of an expression
func SkipParentheses(node Handle) Handle {
	return SkipOuterExpressions(node, OEKParentheses)
}

func SkipTypeParentheses(node Handle) Handle {
	for IsParenthesizedTypeNode(node) {
		node = node.Type()
	}
	return node
}

func SkipPartiallyEmittedExpressions(node Handle) Handle {
	return SkipOuterExpressions(node, OEKPartiallyEmittedExpressions)
}

// Walks up the parents of a parenthesized expression to find the containing node
func WalkUpParenthesizedExpressions(node Handle) Handle {
	for !node.IsNil() && node.Kind() == KindParenthesizedExpression {
		node = node.Parent()
	}
	return node
}

// Walks up the parents of a parenthesized type to find the containing node
func WalkUpParenthesizedTypes(node Handle) Handle {
	for !node.IsNil() && node.Kind() == KindParenthesizedType {
		node = node.Parent()
	}
	return node
}

// Walks up the parents of a node to find the containing SourceFile
func GetSourceFileOfNode(node Handle) *SourceFile {
	if node.IsNil() {
		return nil
	}
	return node.Store().SourceFile()
}

var setParentInChildrenPool = sync.Pool{
	New: func() any {
		return newParentInChildrenSetter()
	},
}

func newParentInChildrenSetter() func(node *Node) bool {
	// Consolidate state into one allocation.
	// Similar to https://go.dev/cl/552375.
	var state struct {
		parent *Node
		visit  func(*Node) bool
	}

	state.visit = func(node *Node) bool {
		if state.parent != nil {
			node.Parent = state.parent
		}
		saveParent := state.parent
		state.parent = node
		node.ForEachChild(state.visit)
		state.parent = saveParent
		return false
	}

	return state.visit
}

func SetParentInChildren(node *Node) {
	fn := setParentInChildrenPool.Get().(func(node *Node) bool)
	defer setParentInChildrenPool.Put(fn)
	fn(node)
}

// This should never be called outside the parser
func SetImportsOfSourceFile(node *SourceFile, imports []Handle) {
	node.imports = imports
}

// Walks up the parents of a node to find the ancestor that matches the callback
func FindAncestor(node Handle, callback func(Handle) bool) Handle {
	for !node.IsNil() {
		if callback(node) {
			return node
		}
		node = node.Parent()
	}
	return Handle{}
}

func FindManyAncestors(node Handle, callbacks ...func(Handle) bool) []Handle {
	ancestors := make([]Handle, len(callbacks))
	found := 0
	for !node.IsNil() {
		for i, callback := range callbacks {
			if ancestors[i].IsNil() && callback(node) {
				ancestors[i] = node
				found++
				if found == len(callbacks) {
					return ancestors
				}
				break
			}
		}
		node = node.Parent()
	}
	return ancestors
}

// Walks up the parents of a node to find the ancestor that matches the kind
func FindAncestorKind(node Handle, kind Kind) Handle {
	for !node.IsNil() {
		if node.Kind() == kind {
			return node
		}
		node = node.Parent()
	}
	return Handle{}
}

type FindAncestorResult int32

const (
	FindAncestorFalse FindAncestorResult = iota
	FindAncestorTrue
	FindAncestorQuit
)

func ToFindAncestorResult(b bool) FindAncestorResult {
	if b {
		return FindAncestorTrue
	}
	return FindAncestorFalse
}

// Walks up the parents of a node to find the ancestor that matches the callback
func FindAncestorOrQuit(node Handle, callback func(Handle) FindAncestorResult) Handle {
	for !node.IsNil() {
		switch callback(node) {
		case FindAncestorQuit:
			return Handle{}
		case FindAncestorTrue:
			return node
		}
		node = node.Parent()
	}
	return Handle{}
}

func IsNodeDescendantOf(node Handle, ancestor Handle) bool {
	for !node.IsNil() {
		if node == ancestor {
			return true
		}
		node = node.Parent()
	}
	return false
}

func ModifierToFlag(token Kind) ModifierFlags {
	switch token {
	case KindStaticKeyword:
		return ModifierFlagsStatic
	case KindPublicKeyword:
		return ModifierFlagsPublic
	case KindProtectedKeyword:
		return ModifierFlagsProtected
	case KindPrivateKeyword:
		return ModifierFlagsPrivate
	case KindAbstractKeyword:
		return ModifierFlagsAbstract
	case KindAccessorKeyword:
		return ModifierFlagsAccessor
	case KindExportKeyword:
		return ModifierFlagsExport
	case KindDeclareKeyword:
		return ModifierFlagsAmbient
	case KindConstKeyword:
		return ModifierFlagsConst
	case KindDefaultKeyword:
		return ModifierFlagsDefault
	case KindAsyncKeyword:
		return ModifierFlagsAsync
	case KindReadonlyKeyword:
		return ModifierFlagsReadonly
	case KindOverrideKeyword:
		return ModifierFlagsOverride
	case KindInKeyword:
		return ModifierFlagsIn
	case KindOutKeyword:
		return ModifierFlagsOut
	case KindDecorator:
		return ModifierFlagsDecorator
	}
	return ModifierFlagsNone
}

func ModifiersToFlags(modifiers []Handle) ModifierFlags {
	var flags ModifierFlags
	for _, modifier := range modifiers {
		flags |= ModifierToFlag(modifier.Kind())
	}
	return flags
}

func HasSyntacticModifier(node Handle, flags ModifierFlags) bool {
	return node.ModifierFlags()&flags != 0
}

func HasAccessorModifier(node Handle) bool {
	return HasSyntacticModifier(node, ModifierFlagsAccessor)
}

func HasStaticModifier(node Handle) bool {
	return HasSyntacticModifier(node, ModifierFlagsStatic)
}

func IsStatic(node Handle) bool {
	// https://tc39.es/ecma262/#sec-static-semantics-isstatic
	return IsClassElement(node) && HasStaticModifier(node) || IsClassStaticBlockDeclaration(node)
}

func CanHaveSymbol(node Handle) bool {
	switch node.Kind() {
	case KindArrowFunction, KindBinaryExpression, KindBindingElement, KindCallExpression, KindCallSignature,
		KindClassDeclaration, KindClassExpression, KindClassStaticBlockDeclaration, KindConstructor, KindConstructorType,
		KindConstructSignature, KindElementAccessExpression, KindEnumDeclaration, KindEnumMember, KindExportAssignment,
		KindExportDeclaration, KindExportSpecifier, KindFunctionDeclaration, KindFunctionExpression, KindFunctionType,
		KindGetAccessor, KindImportClause, KindImportEqualsDeclaration, KindImportSpecifier, KindIndexSignature,
		KindInterfaceDeclaration, KindJSTypeAliasDeclaration,
		KindJsxAttribute, KindJsxAttributes, KindJsxSpreadAttribute, KindMappedType, KindMethodDeclaration,
		KindMethodSignature, KindModuleDeclaration, KindNamedTupleMember, KindNamespaceExport, KindNamespaceExportDeclaration,
		KindNamespaceImport, KindNewExpression, KindNoSubstitutionTemplateLiteral, KindNumericLiteral, KindObjectLiteralExpression,
		KindParameter, KindPropertyAccessExpression, KindPropertyAssignment, KindPropertyDeclaration, KindPropertySignature,
		KindSetAccessor, KindShorthandPropertyAssignment, KindSourceFile, KindSpreadAssignment, KindStringLiteral,
		KindTypeAliasDeclaration, KindTypeLiteral, KindTypeParameter, KindVariableDeclaration:
		return true
	}
	return false
}

func CanHaveIllegalDecorators(node Handle) bool {
	switch node.Kind() {
	case KindPropertyAssignment, KindShorthandPropertyAssignment,
		KindFunctionDeclaration, KindConstructor,
		KindIndexSignature, KindClassStaticBlockDeclaration,
		KindMissingDeclaration, KindVariableStatement,
		KindInterfaceDeclaration, KindTypeAliasDeclaration,
		KindEnumDeclaration, KindModuleDeclaration,
		KindImportEqualsDeclaration, KindImportDeclaration, KindJSImportDeclaration,
		KindNamespaceExportDeclaration, KindExportDeclaration,
		KindExportAssignment:
		return true
	}
	return false
}

func CanHaveIllegalModifiers(node Handle) bool {
	switch node.Kind() {
	case KindClassStaticBlockDeclaration,
		KindPropertyAssignment,
		KindShorthandPropertyAssignment,
		KindMissingDeclaration,
		KindNamespaceExportDeclaration:
		return true
	}
	return false
}

func CanHaveModifiers(node Handle) bool {
	switch node.Kind() {
	case KindTypeParameter,
		KindParameter,
		KindPropertySignature,
		KindPropertyDeclaration,
		KindMethodSignature,
		KindMethodDeclaration,
		KindConstructor,
		KindGetAccessor,
		KindSetAccessor,
		KindIndexSignature,
		KindConstructorType,
		KindFunctionExpression,
		KindArrowFunction,
		KindClassExpression,
		KindVariableStatement,
		KindFunctionDeclaration,
		KindClassDeclaration,
		KindInterfaceDeclaration,
		KindTypeAliasDeclaration,
		KindEnumDeclaration,
		KindModuleDeclaration,
		KindImportEqualsDeclaration,
		KindImportDeclaration,
		KindJSImportDeclaration,
		KindExportAssignment,
		KindExportDeclaration:
		return true
	}
	return false
}

func CanHaveDecorators(node Handle) bool {
	switch node.Kind() {
	case KindParameter,
		KindPropertyDeclaration,
		KindMethodDeclaration,
		KindGetAccessor,
		KindSetAccessor,
		KindClassExpression,
		KindClassDeclaration:
		return true
	}
	return false
}

func IsFunctionOrModuleBlock(node Handle) bool {
	return IsSourceFile(node) || IsModuleBlock(node) || IsBlock(node) && IsFunctionLike(node.Parent())
}

func IsFunctionExpressionOrArrowFunction(node Handle) bool {
	return IsFunctionExpression(node) || IsArrowFunction(node)
}

// Warning: This has the same semantics as the forEach family of functions in that traversal terminates
// in the event that 'visitor' returns true.
func ForEachReturnStatement(body Handle, visitor func(stmt Handle) bool) bool {
	var traverse StoreVisitor
	traverse = func(node Handle) bool {
		switch node.Kind() {
		case KindReturnStatement:
			return visitor(node)
		case KindCaseBlock, KindBlock, KindIfStatement, KindDoStatement, KindWhileStatement, KindForStatement, KindForInStatement,
			KindForOfStatement, KindWithStatement, KindSwitchStatement, KindCaseClause, KindDefaultClause, KindLabeledStatement,
			KindTryStatement, KindCatchClause:
			return node.ForEachChild(traverse)
		}
		return false
	}
	return traverse(body)
}

func GetRootDeclaration(node Handle) Handle {
	for node.Kind() == KindBindingElement {
		node = node.Parent().Parent()
	}
	return node
}

func GetCombinedModifierFlags(node Handle) ModifierFlags {
	node = GetRootDeclaration(node)
	flags := node.ModifierFlags()
	if node.Kind() == KindVariableDeclaration {
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == KindVariableDeclarationList {
		flags |= node.ModifierFlags()
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == KindVariableStatement {
		flags |= node.ModifierFlags()
	}
	return flags
}

func GetCombinedNodeFlags(node Handle) NodeFlags {
	node = GetRootDeclaration(node)
	flags := node.Flags()
	if node.Kind() == KindVariableDeclaration {
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == KindVariableDeclarationList {
		flags |= node.Flags()
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == KindVariableStatement {
		flags |= node.Flags()
	}
	return flags
}

// Gets whether a bound `VariableDeclaration` or `VariableDeclarationList` is part of an `await using` declaration.
func IsVarAwaitUsing(node Handle) bool {
	return GetCombinedNodeFlags(node)&NodeFlagsBlockScoped == NodeFlagsAwaitUsing
}

// Gets whether a bound `VariableDeclaration` or `VariableDeclarationList` is part of a `using` declaration.
func IsVarUsing(node Handle) bool {
	return GetCombinedNodeFlags(node)&NodeFlagsBlockScoped == NodeFlagsUsing
}

// GetJSDocDeprecatedTag returns the first @deprecated JSDoc tag for the given node, or nil if none exists.
func GetJSDocDeprecatedTag(node Handle) Handle {
	for _, jsdoc := range node.JSDoc(nil) {
		for _, tag := range jsdoc.Tags() {
			if IsJSDocDeprecatedTag(tag) {
				return tag
			}
		}
	}
	return Handle{}
}

// IsDeprecatedDeclaration reports whether the given declaration is marked as @deprecated.
// It checks NodeFlagsPossiblyContainsDeprecatedTag on combined node flags, then confirms
// by walking up to find the node with the flag and performing a JSDoc lookup.
func IsDeprecatedDeclaration(declaration Handle) bool {
	return IsDeprecatedDeclarationWithCachedFlags(declaration, GetCombinedNodeFlags(declaration))
}

// IsDeprecatedDeclarationWithCachedFlags is the core logic for IsDeprecatedDeclaration,
// parameterized on pre-computed combined flags so the checker can supply cached flags.
func IsDeprecatedDeclarationWithCachedFlags(declaration Handle, combinedFlags NodeFlags) bool {
	if combinedFlags&NodeFlagsPossiblyContainsDeprecatedTag == 0 {
		return false
	}
	// Walk up to find the node that directly has the flag, since JSDoc is
	// attached to that node (e.g. VariableStatement, not VariableDeclaration).
	for n := declaration; !n.IsNil(); n = n.Parent() {
		if n.Flags()&NodeFlagsPossiblyContainsDeprecatedTag != 0 {
			return !GetJSDocDeprecatedTag(n).IsNil()
		}
	}
	return false
}

// Gets whether a bound `VariableDeclaration` or `VariableDeclarationList` is part of a `const` declaration.
func IsVarConst(node Handle) bool {
	return GetCombinedNodeFlags(node)&NodeFlagsBlockScoped == NodeFlagsConst
}

// Gets whether a bound `VariableDeclaration` or `VariableDeclarationList` is part of a `const`, `using` or `await using` declaration.
func IsVarConstLike(node Handle) bool {
	switch GetCombinedNodeFlags(node) & NodeFlagsBlockScoped {
	case NodeFlagsConst, NodeFlagsUsing, NodeFlagsAwaitUsing:
		return true
	}
	return false
}

// Gets whether a bound `VariableDeclaration` or `VariableDeclarationList` is part of a `let` declaration.
func IsVarLet(node Handle) bool {
	return GetCombinedNodeFlags(node)&NodeFlagsBlockScoped == NodeFlagsLet
}

func IsImportMeta(node Handle) bool {
	if node.Kind() == KindMetaProperty {
		return node.MetaPropertyKeywordToken() == KindImportKeyword && node.MetaPropertyName().Text() == "meta"
	}
	return false
}

func WalkUpBindingElementsAndPatterns(binding Handle) Handle {
	node := binding.Parent()
	for IsBindingElement(node.Parent()) {
		node = node.Parent().Parent()
	}
	return node.Parent()
}

func IsSourceFileJS(file *SourceFile) bool {
	return file.ScriptKind == core.ScriptKindJS || file.ScriptKind == core.ScriptKindJSX
}

func IsInJSFile(node Handle) bool {
	return !node.IsNil() && node.Flags()&NodeFlagsJavaScriptFile != 0
}

func IsDeclaration(node Handle) bool {
	if node.Kind() == KindTypeParameter {
		return !node.Parent().IsNil()
	}
	return IsDeclarationNode(node)
}

// True if `name` is the name of a declaration node
func IsDeclarationName(name Handle) bool {
	return !IsSourceFile(name) && !IsBindingPattern(name) && IsDeclaration(name.Parent()) && name.Parent().Name() == name
}

// Like 'isDeclarationName', but returns true for LHS of `import { x as y }` or `export { x as y }`.
func IsDeclarationNameOrImportPropertyName(name Handle) bool {
	switch name.Parent().Kind() {
	case KindImportSpecifier, KindExportSpecifier:
		return IsIdentifier(name) || name.Kind() == KindStringLiteral
	default:
		return IsDeclarationName(name)
	}
}

func IsLiteralComputedPropertyDeclarationName(node Handle) bool {
	return IsStringOrNumericLiteralLike(node) &&
		node.Parent().Kind() == KindComputedPropertyName &&
		IsDeclaration(node.Parent().Parent())
}

func IsExternalModuleImportEqualsDeclaration(node Handle) bool {
	return node.Kind() == KindImportEqualsDeclaration && node.ImportEqualsDeclarationModuleReference().Kind() == KindExternalModuleReference
}

func IsModuleOrEnumDeclaration(node Handle) bool {
	return node.Kind() == KindModuleDeclaration || node.Kind() == KindEnumDeclaration
}

func IsLiteralImportTypeNode(node Handle) bool {
	return IsImportTypeNode(node) && IsLiteralTypeNode(node.ImportTypeNodeArgument()) && IsStringLiteral(node.ImportTypeNodeArgument().LiteralTypeNodeLiteral())
}

func IsJsxTagName(node Handle) bool {
	parent := node.Parent()
	switch parent.Kind() {
	case KindJsxOpeningElement, KindJsxClosingElement, KindJsxSelfClosingElement:
		return parent.TagName() == node
	}
	return false
}

func IsImportOrExportSpecifier(node Handle) bool {
	return IsImportSpecifier(node) || IsExportSpecifier(node)
}

func IsVoidZero(node Handle) bool {
	return IsVoidExpression(node) && IsNumericLiteral(node.Expression()) && node.Expression().Text() == "0"
}

func IsExportsIdentifier(node Handle) bool {
	return IsIdentifier(node) && node.Text() == "exports"
}

func IsModuleIdentifier(node Handle) bool {
	return IsIdentifier(node) && node.Text() == "module"
}

func IsThisIdentifier(node Handle) bool {
	return IsIdentifier(node) && node.Text() == "this"
}

func IsThisParameter(node Handle) bool {
	return IsParameterDeclaration(node) && !node.Name().IsNil() && IsThisIdentifier(node.Name())
}

func IsBindableStaticAccessExpression(node Handle, excludeThisKeyword bool) bool {
	return IsPropertyAccessExpression(node) &&
		(!excludeThisKeyword && node.Expression().Kind() == KindThisKeyword || IsIdentifier(node.Name()) && IsBindableStaticNameExpression(node.Expression(), true /*excludeThisKeyword*/)) ||
		IsBindableStaticElementAccessExpression(node, excludeThisKeyword)
}

func IsBindableStaticElementAccessExpression(node Handle, excludeThisKeyword bool) bool {
	return IsLiteralLikeElementAccess(node) &&
		((!excludeThisKeyword && node.Expression().Kind() == KindThisKeyword) ||
			IsEntityNameExpression(node.Expression()) ||
			IsBindableStaticAccessExpression(node.Expression(), true /*excludeThisKeyword*/))
}

func IsPrototypeAccess(node Handle) bool {
	if IsBindableStaticAccessExpression(node, false /*excludeThisKeyword*/) {
		if name := GetElementOrPropertyAccessName(node); !name.IsNil() {
			return name.Text() == "prototype"
		}
	}
	return false
}

func IsLiteralLikeElementAccess(node Handle) bool {
	return IsElementAccessExpression(node) && IsStringOrNumericLiteralLike(node.ElementAccessExpressionArgumentExpression())
}

func IsBindableStaticNameExpression(node Handle, excludeThisKeyword bool) bool {
	return IsEntityNameExpression(node) || IsBindableStaticAccessExpression(node, excludeThisKeyword)
}

// Does not handle signed numeric names like `a[+0]` - handling those would require handling prefix unary expressions
// throughout late binding handling as well, which is awkward (but ultimately probably doable if there is demand)
func GetElementOrPropertyAccessName(node Handle) Handle {
	switch node.Kind() {
	case KindPropertyAccessExpression:
		if IsIdentifier(node.Name()) {
			return node.Name()
		}
		return Handle{}
	case KindElementAccessExpression:
		if arg := SkipParentheses(node.ElementAccessExpressionArgumentExpression()); IsStringOrNumericLiteralLike(arg) {
			return arg
		}
		return Handle{}
	}
	panic("Unhandled case in GetElementOrPropertyAccessName")
}

func GetInitializerOfBinaryExpression(expr Handle) Handle {
	for IsBinaryExpression(expr.Right()) {
		expr = expr.Right()
	}
	return expr.Right()
}

func IsExpressionWithTypeArgumentsInClassExtendsClause(node Handle) bool {
	return !TryGetClassExtendingExpressionWithTypeArguments(node).IsNil()
}

func TryGetClassExtendingExpressionWithTypeArguments(node Handle) Handle {
	if !IsExpressionWithTypeArguments(node) {
		return Handle{}
	}
	cls, isImplements := TryGetClassImplementingOrExtendingHeritageClauseElement(node)
	if !cls.IsNil() && !isImplements {
		return cls
	}
	return Handle{}
}

func TryGetClassImplementingOrExtendingHeritageClauseElement(node Handle) (class Handle, isImplements bool) {
	if (IsExpressionWithTypeArguments(node) || IsTypeReferenceNode(node)) &&
		IsHeritageClause(node.Parent()) && IsClassLike(node.Parent().Parent()) {
		return node.Parent().Parent(), node.Parent().HeritageClauseToken() == KindImplementsKeyword
	}
	return Handle{}, false
}

func GetNameOfDeclaration(declaration Handle) Handle {
	if declaration.IsNil() {
		return Handle{}
	}
	nonAssignedName := GetNonAssignedNameOfDeclaration(declaration)
	if !nonAssignedName.IsNil() {
		return nonAssignedName
	}
	if IsFunctionExpression(declaration) || IsArrowFunction(declaration) || IsClassExpression(declaration) {
		return GetAssignedName(declaration)
	}
	return Handle{}
}

func GetNonAssignedNameOfDeclaration(declaration Handle) Handle {
	// !!!
	switch declaration.Kind() {
	case KindBinaryExpression, KindCallExpression:
		switch GetAssignmentDeclarationKind(declaration) {
		case JSDeclarationKindProperty, JSDeclarationKindThisProperty, JSDeclarationKindExportsProperty:
			left := declaration.BinaryExpressionLeft()
			if name := GetElementOrPropertyAccessName(left); !name.IsNil() {
				return name
			}
			return left
		case JSDeclarationKindObjectDefinePropertyValue, JSDeclarationKindObjectDefinePropertyExports:
			return declaration.Arguments()[1]
		}
		return Handle{}
	case KindExportAssignment:
		expr := declaration.Expression()
		if IsIdentifier(expr) {
			return expr
		}
		return Handle{}
	}
	return declaration.Name()
}

func GetAssignedName(node Handle) Handle {
	parent := node.Parent()
	if !parent.IsNil() {
		switch parent.Kind() {
		case KindPropertyAssignment:
			return parent.PropertyAssignmentName()
		case KindBindingElement:
			return parent.BindingElementName()
		case KindBinaryExpression:
			if node == parent.BinaryExpressionRight() {
				left := parent.BinaryExpressionLeft()
				switch left.Kind() {
				case KindIdentifier:
					return left
				case KindPropertyAccessExpression:
					return left.PropertyAccessExpressionName()
				case KindElementAccessExpression:
					arg := SkipParentheses(left.ElementAccessExpressionArgumentExpression())
					if IsStringOrNumericLiteralLike(arg) {
						return arg
					}
				}
			}
		case KindVariableDeclaration:
			name := parent.VariableDeclarationName()
			if IsIdentifier(name) {
				return name
			}
		}
	}
	return Handle{}
}

type JSDeclarationKind int

const (
	JSDeclarationKindNone JSDeclarationKind = iota
	// module.exports = expr, except for module.exports = exports
	JSDeclarationKindModuleExports
	// exports.name = expr
	// module.exports.name = expr
	JSDeclarationKindExportsProperty
	// this.name = expr
	JSDeclarationKindThisProperty
	// F.name = expr, F[name] = expr, in JS or TS file
	JSDeclarationKindProperty
	// Object.defineProperty(x, 'name', { value: any, writable?: boolean (false by default) });
	// Object.defineProperty(x, 'name', { get: Function, set: Function });
	// Object.defineProperty(x, 'name', { get: Function });
	// Object.defineProperty(x, 'name', { set: Function });
	JSDeclarationKindObjectDefinePropertyValue
	// Object.defineProperty(exports || module.exports, 'name', ...);
	JSDeclarationKindObjectDefinePropertyExports
)

func GetAssignmentDeclarationKind(node Handle) JSDeclarationKind {
	switch node.Kind() {
	case KindBinaryExpression:
		if node.BinaryExpressionOperatorToken().Kind() == KindEqualsToken && IsAccessExpression(node.BinaryExpressionLeft()) {
			if IsInJSFile(node.BinaryExpressionLeft()) {
				if IsModuleExportsAccessExpression(node.BinaryExpressionLeft()) && !IsExportsIdentifier(node.BinaryExpressionRight()) {
					return JSDeclarationKindModuleExports
				}
				if (IsModuleExportsAccessExpression(node.BinaryExpressionLeft().Expression()) || IsExportsIdentifier(node.BinaryExpressionLeft().Expression())) &&
					!GetElementOrPropertyAccessName(node.BinaryExpressionLeft()).IsNil() {
					return JSDeclarationKindExportsProperty
				}
				if node.BinaryExpressionLeft().Expression().Kind() == KindThisKeyword {
					return JSDeclarationKindThisProperty
				}
			}
			if node.BinaryExpressionLeft().Kind() == KindPropertyAccessExpression && IsEntityNameExpressionEx(node.BinaryExpressionLeft().Expression(), IsInJSFile(node.BinaryExpressionLeft())) && IsIdentifier(node.BinaryExpressionLeft().Name()) ||
				node.BinaryExpressionLeft().Kind() == KindElementAccessExpression && IsEntityNameExpressionEx(node.BinaryExpressionLeft().Expression(), IsInJSFile(node.BinaryExpressionLeft())) {
				return JSDeclarationKindProperty
			}
		}
	case KindCallExpression:
		if IsInJSFile(node) && IsBindableObjectDefinePropertyCall(node) {
			entityName := node.Arguments()[0]
			if IsExportsIdentifier(entityName) || IsModuleExportsAccessExpression(entityName) {
				return JSDeclarationKindObjectDefinePropertyExports
			}
			return JSDeclarationKindObjectDefinePropertyValue
		}
	}
	return JSDeclarationKindNone
}

func IsBindableObjectDefinePropertyCall(node Handle) bool {
	if args := node.Arguments(); len(args) == 3 {
		if expr := node.Expression(); IsPropertyAccessExpression(expr) &&
			IsIdentifier(expr.Expression()) && expr.Expression().Text() == "Object" &&
			expr.Name().Text() == "defineProperty" &&
			IsStringOrNumericLiteralLike(args[1]) &&
			IsBindableStaticNameExpression(args[0] /*excludeThisKeyword*/, true) {
			return true
		}
	}
	return false
}

/**
 * A declaration has a dynamic name if all of the following are true:
 *   1. The declaration has a computed property name.
 *   2. The computed name is *not* expressed as a StringLiteral.
 *   3. The computed name is *not* expressed as a NumericLiteral.
 *   4. The computed name is *not* expressed as a PlusToken or MinusToken
 *      immediately followed by a NumericLiteral.
 */
func HasDynamicName(declaration Handle) bool {
	name := GetNameOfDeclaration(declaration)
	return !name.IsNil() && IsDynamicName(name)
}

func IsDynamicName(name Handle) bool {
	var expr Handle
	switch name.Kind() {
	case KindComputedPropertyName:
		expr = name.Expression()
	case KindElementAccessExpression:
		expr = SkipParentheses(name.ElementAccessExpressionArgumentExpression())
	default:
		return false
	}
	return !IsStringOrNumericLiteralLike(expr) && !IsSignedNumericLiteral(expr)
}

func IsEntityNameExpression(node Handle) bool {
	return IsEntityNameExpressionEx(node, false /*allowJS*/)
}

func IsEntityNameExpressionEx(node Handle, allowJS bool) bool {
	return IsIdentifier(node) ||
		IsPropertyAccessEntityNameExpression(node, allowJS) ||
		allowJS && (node.Kind() == KindThisKeyword || isElementAccessEntityNameExpression(node, allowJS))
}

func IsPropertyAccessEntityNameExpression(node Handle, allowJS bool) bool {
	return IsPropertyAccessExpression(node) && IsIdentifier(node.Name()) && IsEntityNameExpressionEx(node.Expression(), allowJS)
}

func isElementAccessEntityNameExpression(node Handle, allowJS bool) bool {
	return IsElementAccessExpression(node) && IsStringOrNumericLiteralLike(node.ElementAccessExpressionArgumentExpression()) && IsEntityNameExpressionEx(node.Expression(), allowJS)
}

func IsDottedName(node Handle) bool {
	switch node.Kind() {
	case KindIdentifier, KindThisKeyword, KindSuperKeyword, KindMetaProperty:
		return true
	case KindPropertyAccessExpression, KindParenthesizedExpression:
		return IsDottedName(node.Expression())
	}
	return false
}

func HasSamePropertyAccessName(node1, node2 Handle) bool {
	if node1.Kind() == KindIdentifier && node2.Kind() == KindIdentifier {
		return node1.Text() == node2.Text()
	} else if node1.Kind() == KindPropertyAccessExpression && node2.Kind() == KindPropertyAccessExpression {
		return node1.PropertyAccessExpressionName().Text() == node2.PropertyAccessExpressionName().Text() &&
			HasSamePropertyAccessName(node1.Expression(), node2.Expression())
	}
	return false
}

func IsAmbientModule(node Handle) bool {
	return IsModuleDeclaration(node) && (node.ModuleDeclarationName().Kind() == KindStringLiteral || IsGlobalScopeAugmentation(node))
}

func IsAmbientModuleSymbolName(s string) bool {
	return strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")
}

func IsExternalModule(file *SourceFile) bool {
	return !file.ExternalModuleIndicator.IsNil()
}

func IsExternalOrCommonJSModule(file *SourceFile) bool {
	return !file.ExternalModuleIndicator.IsNil() || !file.CommonJSModuleIndicator.IsNil()
}

func IsEffectiveExternalModule(node *SourceFile, compilerOptions *core.CompilerOptions) bool {
	return IsExternalModule(node) || (isCommonJSContainingModuleKind(compilerOptions.GetEmitModuleKind()) && !node.CommonJSModuleIndicator.IsNil())
}

func isCommonJSContainingModuleKind(kind core.ModuleKind) bool {
	return kind == core.ModuleKindCommonJS || core.ModuleKindNode16 <= kind && kind <= core.ModuleKindNodeNext
}

func IsExternalModuleIndicator(node Handle) bool {
	// Exported top-level member indicates moduleness
	return IsAnyImportOrReExport(node) || IsExportAssignment(node) || HasSyntacticModifier(node, ModifierFlagsExport)
}

func IsExportNamespaceAsDefaultDeclaration(node Handle) bool {
	if IsExportDeclaration(node) {
		return IsNamespaceExport(node.ExportDeclarationExportClause()) && ModuleExportNameIsDefault(node.ExportDeclarationExportClause().Name())
	}
	return false
}

func IsGlobalScopeAugmentation(node Handle) bool {
	return IsModuleDeclaration(node) && node.ModuleDeclarationKeyword() == KindGlobalKeyword
}

func IsModuleAugmentationExternal(node Handle) bool {
	// external module augmentation is a ambient module declaration that is either:
	// - defined in the top level scope and source file is an external module
	// - defined inside ambient module declaration located in the top level scope and source file not an external module
	switch node.Parent().Kind() {
	case KindSourceFile:
		return IsExternalModule(node.Parent().Store().SourceFile())
	case KindModuleBlock:
		grandParent := node.Parent().Parent()
		return IsAmbientModule(grandParent) && IsSourceFile(grandParent.Parent()) && !IsExternalModule(grandParent.Parent().Store().SourceFile())
	}
	return false
}

func IsModuleWithStringLiteralName(node Handle) bool {
	return IsModuleDeclaration(node) && node.Name().Kind() == KindStringLiteral
}

func GetContainingClass(node Handle) Handle {
	return FindAncestor(node.Parent(), IsClassLike)
}

func GetExtendsHeritageClauseElements(node Handle) []Handle {
	return GetHeritageElements(node, KindExtendsKeyword)
}

func GetImplementsHeritageClauseElements(node Handle) []Handle {
	return GetHeritageElements(node, KindImplementsKeyword)
}

func GetHeritageElements(node Handle, kind Kind) []Handle {
	clause := GetHeritageClause(node, kind)
	if clause.IsNil() {
		return nil
	}
	return clause.Types()
}

// GetHeritageClauseElementName returns the expression or type name of a heritage clause element.
func GetHeritageClauseElementName(node Handle) Handle {
	if IsTypeReferenceNode(node) {
		return node.TypeReferenceNodeTypeName()
	}
	return node.ExpressionWithTypeArgumentsExpression()
}

func IsNameOfHeritageClauseTypeReference(node Handle) bool {
	for IsQualifiedName(node.Parent()) {
		node = node.Parent()
	}
	return IsTypeReferenceNode(node.Parent()) &&
		node.Parent().TypeReferenceNodeTypeName() == node &&
		IsHeritageClause(node.Parent().Parent())
}

func GetHeritageClause(node Handle, kind Kind) Handle {
	clauses := node.HeritageClauses()
	if clauses != 0 {
		for _, clause := range node.Store().ListSlice(clauses) {
			if clause.HeritageClauseToken() == kind {
				return clause
			}
		}
	}
	return Handle{}
}

func IsPartOfTypeQuery(node Handle) bool {
	for node.Kind() == KindQualifiedName || node.Kind() == KindIdentifier {
		node = node.Parent()
	}
	return node.Kind() == KindTypeQuery
}

/**
 * This function returns true if the this node's root declaration is a parameter.
 * For example, passing a `ParameterDeclaration` will return true, as will passing a
 * binding element that is a child of a `ParameterDeclaration`.
 *
 * If you are looking to test that a `Node` is a `ParameterDeclaration`, use `isParameter`.
 */
func IsPartOfParameterDeclaration(node Handle) bool {
	return GetRootDeclaration(node).Kind() == KindParameter
}

func IsInTopLevelContext(node Handle) bool {
	// The name of a class or function declaration is a BindingIdentifier in its surrounding scope.
	if IsIdentifier(node) {
		parent := node.Parent()
		if (IsClassDeclaration(parent) || IsFunctionDeclaration(parent)) && parent.Name() == node {
			node = parent
		}
	}
	container := GetThisContainer(node, true /*includeArrowFunctions*/, false /*includeClassComputedPropertyName*/)
	return IsSourceFile(container)
}

func GetThisContainer(node Handle, includeArrowFunctions bool, includeClassComputedPropertyName bool) Handle {
	for {
		node = node.Parent()
		if node.IsNil() {
			panic("nil parent in getThisContainer")
		}
		switch node.Kind() {
		case KindComputedPropertyName:
			if includeClassComputedPropertyName && IsClassLike(node.Parent().Parent()) {
				return node
			}
			node = node.Parent().Parent()
		case KindDecorator:
			if node.Parent().Kind() == KindParameter && IsClassElement(node.Parent().Parent()) {
				// If the decorator's parent is a ParameterDeclaration, we resolve the this container from
				// the grandparent class declaration.
				node = node.Parent().Parent()
			} else if IsClassElement(node.Parent()) {
				// If the decorator's parent is a class element, we resolve the 'this' container
				// from the parent class declaration.
				node = node.Parent()
			}
		case KindArrowFunction:
			if includeArrowFunctions {
				return node
			}
		case KindFunctionDeclaration, KindFunctionExpression, KindModuleDeclaration, KindClassStaticBlockDeclaration,
			KindPropertyDeclaration, KindPropertySignature, KindMethodDeclaration, KindMethodSignature, KindConstructor,
			KindGetAccessor, KindSetAccessor, KindCallSignature, KindConstructSignature, KindIndexSignature,
			KindEnumDeclaration, KindSourceFile:
			return node
		}
	}
}

func GetSuperContainer(node Handle, stopOnFunctions bool) Handle {
	for node = node.Parent(); !node.IsNil(); node = node.Parent() {
		switch node.Kind() {
		case KindComputedPropertyName:
			node = node.Parent()
		case KindFunctionDeclaration, KindFunctionExpression, KindArrowFunction:
			if !stopOnFunctions {
				continue
			}
			return node
		case KindPropertyDeclaration, KindPropertySignature, KindMethodDeclaration, KindMethodSignature, KindConstructor, KindGetAccessor, KindSetAccessor, KindClassStaticBlockDeclaration:
			return node
		case KindDecorator:
			// Decorators are always applied outside of the body of a class or method.
			if node.Parent().Kind() == KindParameter && IsClassElement(node.Parent().Parent()) {
				// If the decorator's parent is a ParameterDeclaration, we resolve the this container from
				// the grandparent class declaration.
				node = node.Parent().Parent()
			} else if IsClassElement(node.Parent()) {
				// If the decorator's parent is a class element, we resolve the 'this' container
				// from the parent class declaration.
				node = node.Parent()
			}
		}
	}
	return Handle{}
}

func GetImmediatelyInvokedFunctionExpression(fn Handle) Handle {
	if IsFunctionExpressionOrArrowFunction(fn) {
		prev := fn
		parent := fn.Parent()
		for IsParenthesizedExpression(parent) {
			prev = parent
			parent = parent.Parent()
		}
		if IsCallExpression(parent) && parent.Expression() == prev {
			return parent
		}
	}
	return Handle{}
}

func IsEnumConst(node Handle) bool {
	return GetCombinedModifierFlags(node)&ModifierFlagsConst != 0
}

func ExpressionIsAlias(node Handle) bool {
	return IsEntityNameExpression(node) || IsClassExpression(node)
}

func IsInstanceOfExpression(node Handle) bool {
	return IsBinaryExpression(node) && node.BinaryExpressionOperatorToken().Kind() == KindInstanceOfKeyword
}

func IsAnyImportOrReExport(node Handle) bool {
	return IsImportNode(node) || IsExportDeclaration(node)
}

func IsImportNode(node Handle) bool {
	return IsAnyImportSyntax(node) || NodeKindIs(node, KindJSImportDeclaration)
}

// Checks if the node is a genuine import declation. In particular the re-parsed KindJSImportDeclaration
// is explicitly excluded because the callers of this function are typically not prepared to handle it properly.
// For more permissive check, use IsImportNode.
func IsAnyImportSyntax(node Handle) bool {
	return NodeKindIs(node, KindImportDeclaration, KindImportEqualsDeclaration)
}

func IsJsonSourceFile(file *SourceFile) bool {
	return file.ScriptKind == core.ScriptKindJSON
}

func IsInJsonFile(node Handle) bool {
	return node.Flags()&NodeFlagsJsonFile != 0
}

func GetExternalModuleName(node Handle) Handle {
	switch node.Kind() {
	case KindImportDeclaration, KindJSImportDeclaration, KindExportDeclaration:
		return node.ModuleSpecifier()
	case KindImportEqualsDeclaration:
		if node.ImportEqualsDeclarationModuleReference().Kind() == KindExternalModuleReference {
			return node.ImportEqualsDeclarationModuleReference().Expression()
		}
		return Handle{}
	case KindImportType:
		return getImportTypeNodeLiteral(node)
	case KindCallExpression:
		return core.FirstOrNil(node.Arguments())
	case KindModuleDeclaration:
		if IsStringLiteral(node.ModuleDeclarationName()) {
			return node.ModuleDeclarationName()
		}
		return Handle{}
	}
	panic("Unhandled case in getExternalModuleName")
}

func GetImportAttributes(node Handle) Handle {
	switch node.Kind() {
	case KindImportDeclaration, KindJSImportDeclaration:
		return node.ImportDeclarationAttributes()
	case KindExportDeclaration:
		return node.ExportDeclarationAttributes()
	}
	panic("Unhandled case in getImportAttributes")
}

func getImportTypeNodeLiteral(node Handle) Handle {
	if IsImportTypeNode(node) {
		if IsLiteralTypeNode(node.ImportTypeNodeArgument()) {
			literal := node.ImportTypeNodeArgument().LiteralTypeNodeLiteral()
			if IsStringLiteral(literal) {
				return literal
			}
		}
	}
	return Handle{}
}

func IsExpressionNode(node Handle) bool {
	switch node.Kind() {
	case KindSuperKeyword, KindNullKeyword, KindTrueKeyword, KindFalseKeyword, KindRegularExpressionLiteral,
		KindArrayLiteralExpression, KindObjectLiteralExpression, KindPropertyAccessExpression, KindElementAccessExpression,
		KindCallExpression, KindNewExpression, KindTaggedTemplateExpression, KindAsExpression, KindTypeAssertionExpression,
		KindSatisfiesExpression, KindNonNullExpression, KindParenthesizedExpression, KindFunctionExpression,
		KindClassExpression, KindArrowFunction, KindVoidExpression, KindDeleteExpression, KindTypeOfExpression,
		KindPrefixUnaryExpression, KindPostfixUnaryExpression, KindBinaryExpression, KindConditionalExpression,
		KindSpreadElement, KindTemplateExpression, KindOmittedExpression, KindJsxElement, KindJsxSelfClosingElement,
		KindJsxFragment, KindYieldExpression, KindAwaitExpression:
		return true
	case KindMetaProperty:
		// `import.defer` in `import.defer(...)` is not an expression
		return !IsImportCall(node.Parent()) || node.Parent().Expression() != node
	case KindExpressionWithTypeArguments:
		return !IsHeritageClause(node.Parent())
	case KindQualifiedName:
		for node.Parent().Kind() == KindQualifiedName {
			node = node.Parent()
		}
		return IsTypeQueryNode(node.Parent()) || IsJSDocLinkLike(node.Parent()) || IsJSDocNameReference(node.Parent()) || IsJsxTagName(node)
	case KindPrivateIdentifier:
		return IsBinaryExpression(node.Parent()) && node.Parent().BinaryExpressionLeft() == node && node.Parent().BinaryExpressionOperatorToken().Kind() == KindInKeyword
	case KindIdentifier:
		if IsTypeQueryNode(node.Parent()) || IsJSDocLinkLike(node.Parent()) || IsJSDocNameReference(node.Parent()) || IsJsxTagName(node) {
			return true
		}
		fallthrough
	case KindNumericLiteral, KindBigIntLiteral, KindStringLiteral, KindNoSubstitutionTemplateLiteral, KindThisKeyword:
		return IsInExpressionContext(node)
	default:
		return false
	}
}

func IsInExpressionContext(node Handle) bool {
	parent := node.Parent()
	switch parent.Kind() {
	case KindVariableDeclaration, KindParameter, KindPropertyDeclaration, KindPropertySignature, KindEnumMember, KindPropertyAssignment, KindBindingElement:
		return parent.Initializer() == node
	case KindExpressionStatement, KindIfStatement, KindDoStatement, KindWhileStatement, KindReturnStatement, KindWithStatement, KindSwitchStatement,
		KindCaseClause, KindDefaultClause, KindThrowStatement, KindTypeAssertionExpression, KindAsExpression, KindTemplateSpan, KindComputedPropertyName,
		KindSatisfiesExpression:
		return parent.Expression() == node
	case KindForStatement:
		return parent.ForStatementInitializer() == node && parent.ForStatementInitializer().Kind() != KindVariableDeclarationList || parent.ForStatementCondition() == node || parent.ForStatementIncrementor() == node
	case KindForInStatement, KindForOfStatement:
		return parent.ForInOrOfStatementInitializer() == node && parent.ForInOrOfStatementInitializer().Kind() != KindVariableDeclarationList || parent.ForInOrOfStatementExpression() == node
	case KindDecorator, KindJsxExpression, KindJsxSpreadAttribute, KindSpreadAssignment:
		return true
	case KindExpressionWithTypeArguments:
		return parent.Expression() == node && !IsPartOfTypeNode(parent)
	case KindShorthandPropertyAssignment:
		return parent.ShorthandPropertyAssignmentObjectAssignmentInitializer() == node
	default:
		return IsExpressionNode(parent)
	}
}

func IsPartOfTypeNode(node Handle) bool {
	kind := node.Kind()
	if kind >= KindFirstTypeNode && kind <= KindLastTypeNode {
		return true
	}
	switch node.Kind() {
	case KindAnyKeyword, KindUnknownKeyword, KindNumberKeyword, KindBigIntKeyword, KindStringKeyword,
		KindBooleanKeyword, KindSymbolKeyword, KindObjectKeyword, KindUndefinedKeyword, KindNullKeyword,
		KindNeverKeyword:
		return true
	case KindVoidKeyword:
		return node.Parent().Kind() != KindVoidExpression
	case KindExpressionWithTypeArguments:
		return isPartOfTypeExpressionWithTypeArguments(node)
	case KindTypeParameter:
		return node.Parent().Kind() == KindMappedType || node.Parent().Kind() == KindInferType
	case KindIdentifier:
		parent := node.Parent()
		if IsQualifiedName(parent) && parent.QualifiedNameRight() == node {
			return isPartOfTypeNodeInParent(parent)
		}
		if IsPropertyAccessExpression(parent) && parent.PropertyAccessExpressionName() == node {
			return isPartOfTypeNodeInParent(parent)
		}
		return isPartOfTypeNodeInParent(node)
	case KindQualifiedName, KindPropertyAccessExpression, KindThisKeyword:
		return isPartOfTypeNodeInParent(node)
	}
	return false
}

func isPartOfTypeNodeInParent(node Handle) bool {
	parent := node.Parent()
	if parent.Kind() == KindTypeQuery {
		return false
	}
	if parent.Kind() == KindImportType {
		return !parent.ImportTypeNodeIsTypeOf()
	}

	// Do not recursively call isPartOfTypeNode on the parent. In the example:
	//
	//     let a: A.B.C;
	//
	// Calling isPartOfTypeNode would consider the qualified name A.B a type node.
	// Only C and A.B.C are type nodes.
	if parent.Kind() >= KindFirstTypeNode && parent.Kind() <= KindLastTypeNode {
		return true
	}
	switch parent.Kind() {
	case KindExpressionWithTypeArguments:
		return isPartOfTypeExpressionWithTypeArguments(parent)
	case KindTypeParameter:
		return node == parent.TypeParameterDeclarationConstraint()
	case KindVariableDeclaration, KindParameter, KindPropertyDeclaration, KindPropertySignature, KindFunctionDeclaration,
		KindFunctionExpression, KindArrowFunction, KindConstructor, KindMethodDeclaration, KindMethodSignature,
		KindGetAccessor, KindSetAccessor, KindCallSignature, KindConstructSignature, KindIndexSignature,
		KindTypeAssertionExpression:
		return node == parent.Type()
	case KindCallExpression, KindNewExpression, KindTaggedTemplateExpression:
		return slices.Contains(parent.TypeArguments(), node)
	}
	return false
}

func isPartOfTypeExpressionWithTypeArguments(node Handle) bool {
	parent := node.Parent()
	return IsHeritageClause(parent) && (!IsClassLike(parent.Parent()) || parent.HeritageClauseToken() == KindImplementsKeyword) ||
		IsJSDocImplementsTag(parent) ||
		IsJSDocAugmentsTag(parent)
}

func IsJSDocLinkLike(node Handle) bool {
	return NodeKindIs(node, KindJSDocLink, KindJSDocLinkCode, KindJSDocLinkPlain)
}

func IsJSDocTag(node Handle) bool {
	return node.Kind() >= KindFirstJSDocTagNode && node.Kind() <= KindLastJSDocTagNode
}

func IsSuperCall(node Handle) bool {
	return IsCallExpression(node) && node.Expression().Kind() == KindSuperKeyword
}

func IsImportCall(node Handle) bool {
	if !IsCallExpression(node) {
		return false
	}
	e := node.Expression()
	return e.Kind() == KindImportKeyword || IsMetaProperty(e) && e.MetaPropertyKeywordToken() == KindImportKeyword && e.Text() == "defer"
}

func IsComputedNonLiteralName(name Handle) bool {
	return IsComputedPropertyName(name) && !IsStringOrNumericLiteralLike(name.Expression())
}

func IsQuestionToken(node Handle) bool {
	return !node.IsNil() && node.Kind() == KindQuestionToken
}

func EntityNameToString(name Handle, getTextOfNode func(Handle) string) string {
	switch name.Kind() {
	case KindThisKeyword:
		return "this"
	case KindIdentifier, KindPrivateIdentifier:
		if NodeIsSynthesized(name) || getTextOfNode == nil {
			return name.Text()
		}
		return getTextOfNode(name)
	case KindQualifiedName:
		return EntityNameToString(name.QualifiedNameLeft(), getTextOfNode) + "." + EntityNameToString(name.QualifiedNameRight(), getTextOfNode)
	case KindPropertyAccessExpression:
		return EntityNameToString(name.Expression(), getTextOfNode) + "." + EntityNameToString(name.PropertyAccessExpressionName(), getTextOfNode)
	case KindJsxNamespacedName:
		return EntityNameToString(name.JsxNamespacedNameNamespace(), getTextOfNode) + ":" + EntityNameToString(name.JsxNamespacedNameName(), getTextOfNode)
	}
	panic("Unhandled case in EntityNameToString")
}

func GetTextOfPropertyName(name Handle) string {
	text, _ := TryGetTextOfPropertyName(name)
	return text
}

func TryGetTextOfPropertyName(name Handle) (string, bool) {
	switch name.Kind() {
	case KindIdentifier, KindPrivateIdentifier, KindStringLiteral, KindNumericLiteral, KindBigIntLiteral,
		KindNoSubstitutionTemplateLiteral:
		return name.Text(), true
	case KindComputedPropertyName:
		if IsStringOrNumericLiteralLike(name.Expression()) {
			return name.Expression().Text(), true
		}
	case KindJsxNamespacedName:
		return name.JsxNamespacedNameNamespace().Text() + ":" + name.Name().Text(), true
	}
	return "", false
}

func IsJSDocNode(node Handle) bool {
	return node.Kind() >= KindFirstJSDocNode && node.Kind() <= KindLastJSDocNode
}

func IsNonWhitespaceToken(node Handle) bool {
	return IsTokenKind(node.Kind()) && !IsWhitespaceOnlyJsxText(node)
}

func IsWhitespaceOnlyJsxText(node Handle) bool {
	return node.Kind() == KindJsxText && node.JsxTextContainsOnlyTriviaWhiteSpaces()
}

func GetNewTargetContainer(node Handle) Handle {
	container := GetThisContainer(node, false /*includeArrowFunctions*/, false /*includeClassComputedPropertyName*/)
	if !container.IsNil() {
		switch container.Kind() {
		case KindConstructor, KindFunctionDeclaration, KindFunctionExpression:
			return container
		}
	}
	return Handle{}
}

func GetEnclosingBlockScopeContainer(node Handle) Handle {
	return FindAncestor(node.Parent(), func(current Handle) bool {
		return IsBlockScope(current, current.Parent())
	})
}

func IsBlockScope(node Handle, parentNode Handle) bool {
	switch node.Kind() {
	case KindSourceFile, KindCaseBlock, KindCatchClause, KindModuleDeclaration, KindForStatement, KindForInStatement, KindForOfStatement,
		KindConstructor, KindMethodDeclaration, KindGetAccessor, KindSetAccessor, KindFunctionDeclaration, KindFunctionExpression,
		KindArrowFunction, KindPropertyDeclaration, KindClassStaticBlockDeclaration:
		return true
	case KindBlock:
		// function block is not considered block-scope container
		// see comment in binder.ts: bind(...), case for SyntaxKind.Block
		return !IsFunctionLikeOrClassStaticBlockDeclaration(parentNode)
	}
	return false
}

type SemanticMeaning int32

const (
	SemanticMeaningNone      SemanticMeaning = 0
	SemanticMeaningValue     SemanticMeaning = 1 << 0
	SemanticMeaningType      SemanticMeaning = 1 << 1
	SemanticMeaningNamespace SemanticMeaning = 1 << 2
	SemanticMeaningAll       SemanticMeaning = SemanticMeaningValue | SemanticMeaningType | SemanticMeaningNamespace
)

func GetMeaningFromDeclaration(node Handle) SemanticMeaning {
	switch node.Kind() {
	case KindVariableDeclaration:
		return SemanticMeaningValue
	case KindParameter,
		KindBindingElement,
		KindPropertyDeclaration,
		KindPropertySignature,
		KindPropertyAssignment,
		KindShorthandPropertyAssignment,
		KindMethodDeclaration,
		KindMethodSignature,
		KindConstructor,
		KindGetAccessor,
		KindSetAccessor,
		KindFunctionDeclaration,
		KindFunctionExpression,
		KindArrowFunction,
		KindCatchClause,
		KindJsxAttribute:
		return SemanticMeaningValue

	case KindTypeParameter,
		KindInterfaceDeclaration,
		KindTypeAliasDeclaration,
		KindJSTypeAliasDeclaration,
		KindTypeLiteral:
		return SemanticMeaningType
	case KindEnumMember, KindClassDeclaration:
		return SemanticMeaningValue | SemanticMeaningType

	case KindModuleDeclaration:
		if IsAmbientModule(node) {
			return SemanticMeaningNamespace | SemanticMeaningValue
		} else if GetModuleInstanceState(node) == ModuleInstanceStateInstantiated {
			return SemanticMeaningNamespace | SemanticMeaningValue
		} else {
			return SemanticMeaningNamespace
		}

	case KindEnumDeclaration,
		KindNamedImports,
		KindImportSpecifier,
		KindImportEqualsDeclaration,
		KindImportDeclaration,
		KindJSImportDeclaration,
		KindExportAssignment,
		KindExportDeclaration:
		return SemanticMeaningAll

	// An external module can be a Value
	case KindSourceFile:
		return SemanticMeaningNamespace | SemanticMeaningValue
	}

	return SemanticMeaningAll
}

func IsPropertyAccessOrQualifiedName(node Handle) bool {
	return node.Kind() == KindPropertyAccessExpression || node.Kind() == KindQualifiedName
}

func IsLabelName(node Handle) bool {
	return IsLabelOfLabeledStatement(node) || IsJumpStatementTarget(node)
}

func IsLabelOfLabeledStatement(node Handle) bool {
	if !IsIdentifier(node) {
		return false
	}
	if !IsLabeledStatement(node.Parent()) {
		return false
	}
	return node == node.Parent().Label()
}

func IsJumpStatementTarget(node Handle) bool {
	if !IsIdentifier(node) {
		return false
	}
	if !IsBreakOrContinueStatement(node.Parent()) {
		return false
	}
	return node == node.Parent().Label()
}

func IsBreakOrContinueStatement(node Handle) bool {
	return NodeKindIs(node, KindBreakStatement, KindContinueStatement)
}

// GetModuleInstanceState is used during binding as well as in transformations and tests, and therefore may be invoked
// with a node that does not yet have its `Parent` pointer set. In this case, an `ancestors` represents a stack of
// virtual `Parent` pointers that can be used to walk up the tree. Since `getModuleInstanceStateForAliasTarget` may
// potentially walk up out of the provided `Node`, merely setting the parent pointers for a given `ModuleDeclaration`
// prior to invoking `GetModuleInstanceState` is not sufficient. It is, however, necessary that the `Parent` pointers
// for all ancestors of the `Node` provided to `GetModuleInstanceState` have been set.

// Push a virtual parent pointer onto `ancestors` and return it.
func pushAncestor(ancestors []Handle, parent Handle) []Handle {
	return append(ancestors, parent)
}

// If a virtual `Parent` exists on the stack, returns the previous stack entry and the virtual `Parent`.
// Otherwise, we return the value of `node.Parent()`.
func popAncestor(ancestors []Handle, node Handle) ([]Handle, Handle) {
	if len(ancestors) == 0 {
		return nil, node.Parent()
	}
	n := len(ancestors) - 1
	return ancestors[:n], ancestors[n]
}

type ModuleInstanceState int32

const (
	ModuleInstanceStateUnknown ModuleInstanceState = iota
	ModuleInstanceStateNonInstantiated
	ModuleInstanceStateInstantiated
	ModuleInstanceStateConstEnumOnly
)

func GetModuleInstanceState(node Handle) ModuleInstanceState {
	return getModuleInstanceState(node, nil, nil)
}

func getModuleInstanceState(node Handle, ancestors []Handle, visited map[NodeRef]ModuleInstanceState) ModuleInstanceState {
	if !node.ModuleDeclarationBody().IsNil() {
		return getModuleInstanceStateCached(node.ModuleDeclarationBody(), pushAncestor(ancestors, node), visited)
	} else {
		return ModuleInstanceStateInstantiated
	}
}

func getModuleInstanceStateCached(node Handle, ancestors []Handle, visited map[NodeRef]ModuleInstanceState) ModuleInstanceState {
	if visited == nil {
		visited = make(map[NodeRef]ModuleInstanceState)
	}
	nodeId := node.id
	if cached, ok := visited[nodeId]; ok {
		if cached != ModuleInstanceStateUnknown {
			return cached
		}
		return ModuleInstanceStateNonInstantiated
	}
	visited[nodeId] = ModuleInstanceStateUnknown
	result := getModuleInstanceStateWorker(node, ancestors, visited)
	visited[nodeId] = result
	return result
}

func getModuleInstanceStateWorker(node Handle, ancestors []Handle, visited map[NodeRef]ModuleInstanceState) ModuleInstanceState {
	// A module is uninstantiated if it contains only
	switch node.Kind() {
	case KindInterfaceDeclaration, KindTypeAliasDeclaration, KindJSTypeAliasDeclaration:
		return ModuleInstanceStateNonInstantiated
	case KindEnumDeclaration:
		if IsEnumConst(node) {
			return ModuleInstanceStateConstEnumOnly
		}
	case KindImportDeclaration, KindJSImportDeclaration, KindImportEqualsDeclaration:
		if !HasSyntacticModifier(node, ModifierFlagsExport) {
			return ModuleInstanceStateNonInstantiated
		}
	case KindExportDeclaration:
		if node.ExportDeclarationModuleSpecifier().IsNil() && !node.ExportDeclarationExportClause().IsNil() && node.ExportDeclarationExportClause().Kind() == KindNamedExports {
			state := ModuleInstanceStateNonInstantiated
			ancestors = pushAncestor(ancestors, node)
			ancestors = pushAncestor(ancestors, node.ExportDeclarationExportClause())
			for _, specifier := range node.ExportDeclarationExportClause().Elements() {
				specifierState := getModuleInstanceStateForAliasTarget(specifier, ancestors, visited)
				if specifierState > state {
					state = specifierState
				}
				if state == ModuleInstanceStateInstantiated {
					return state
				}
			}
			return state
		}
	case KindModuleBlock:
		state := ModuleInstanceStateNonInstantiated
		ancestors = pushAncestor(ancestors, node)
		node.ForEachChild(func(n Handle) bool {
			childState := getModuleInstanceStateCached(n, ancestors, visited)
			switch childState {
			case ModuleInstanceStateNonInstantiated:
				return false
			case ModuleInstanceStateConstEnumOnly:
				state = ModuleInstanceStateConstEnumOnly
				return false
			case ModuleInstanceStateInstantiated:
				state = ModuleInstanceStateInstantiated
				return true
			}
			panic("Unhandled case in getModuleInstanceStateWorker")
		})
		return state
	case KindModuleDeclaration:
		return getModuleInstanceState(node, ancestors, visited)
	}
	return ModuleInstanceStateInstantiated
}

func getModuleInstanceStateForAliasTarget(node Handle, ancestors []Handle, visited map[NodeRef]ModuleInstanceState) ModuleInstanceState {
	name := node.PropertyNameOrName()
	if name.Kind() != KindIdentifier {
		// Skip for invalid syntax like this: export { "x" }
		return ModuleInstanceStateInstantiated
	}
	for ancestors, p := popAncestor(ancestors, node); !p.IsNil(); ancestors, p = popAncestor(ancestors, p) {
		if IsBlock(p) || IsModuleBlock(p) || IsSourceFile(p) {
			found := ModuleInstanceStateUnknown
			statementsAncestors := pushAncestor(ancestors, p)
			for _, statement := range p.Statements() {
				if NodeHasName(statement, name) {
					state := getModuleInstanceStateCached(statement, statementsAncestors, visited)
					if found == ModuleInstanceStateUnknown || state > found {
						found = state
					}
					if found == ModuleInstanceStateInstantiated {
						return found
					}
					if statement.Kind() == KindImportEqualsDeclaration {
						// Treat re-exports of import aliases as instantiated since they're ambiguous. This is consistent
						// with `export import x = mod.x` being treated as instantiated:
						//   import x = mod.x;
						//   export { x };
						found = ModuleInstanceStateInstantiated
					}
				}
			}
			if found != ModuleInstanceStateUnknown {
				return found
			}
		}
	}
	// Couldn't locate, assume could refer to a value
	return ModuleInstanceStateInstantiated
}

func IsInstantiatedModule(node Handle, preserveConstEnums bool) bool {
	moduleState := GetModuleInstanceState(node)
	return moduleState == ModuleInstanceStateInstantiated ||
		(preserveConstEnums && moduleState == ModuleInstanceStateConstEnumOnly)
}

func NodeHasName(statement Handle, id Handle) bool {
	name := statement.Name()
	if !name.IsNil() {
		return IsIdentifier(name) && name.Text() == id.Text()
	}
	if IsVariableStatement(statement) {
		declarations := statement.VariableStatementDeclarationList().Declarations()
		return core.Some(declarations, func(d Handle) bool { return NodeHasName(d, id) })
	}
	return false
}

func IsInternalModuleImportEqualsDeclaration(node Handle) bool {
	return IsImportEqualsDeclaration(node) && node.ImportEqualsDeclarationModuleReference().Kind() != KindExternalModuleReference
}

func IsConstAssertion(node Handle) bool {
	switch node.Kind() {
	case KindAsExpression, KindTypeAssertionExpression:
		return IsConstTypeReference(node.Type())
	}
	return false
}

func IsConstTypeReference(node Handle) bool {
	return IsTypeReferenceNode(node) && len(node.TypeArguments()) == 0 && IsIdentifier(node.TypeReferenceNodeTypeName()) && node.TypeReferenceNodeTypeName().Text() == "const"
}

func IsGlobalSourceFile(node Handle) bool {
	return node.Kind() == KindSourceFile && !IsExternalOrCommonJSModule(node.Store().SourceFile())
}

func IsParameterLike(node Handle) bool {
	switch node.Kind() {
	case KindParameter, KindTypeParameter:
		return true
	}
	return false
}

func GetDeclarationOfKind(symbol *Symbol, kind Kind) Handle {
	return FindSymbolDeclaration(symbol, func(declaration Handle) bool {
		return declaration.Kind() == kind
	})
}

func FindConstructorDeclaration(node Handle) Handle {
	for _, member := range node.Members() {
		if IsConstructorDeclaration(member) && NodeIsPresent(member.Body()) {
			return member
		}
	}
	return Handle{}
}

func GetFirstIdentifier(node Handle) Handle {
	switch node.Kind() {
	case KindIdentifier:
		return node
	case KindQualifiedName:
		return GetFirstIdentifier(node.QualifiedNameLeft())
	case KindPropertyAccessExpression:
		return GetFirstIdentifier(node.PropertyAccessExpressionExpression())
	}
	panic("Unhandled case in GetFirstIdentifier")
}

func GetNamespaceDeclarationNode(node Handle) Handle {
	switch node.Kind() {
	case KindImportDeclaration, KindJSImportDeclaration:
		importClause := node.ImportClause()
		if !importClause.IsNil() && !importClause.ImportClauseNamedBindings().IsNil() && IsNamespaceImport(importClause.ImportClauseNamedBindings()) {
			return importClause.ImportClauseNamedBindings()
		}
	case KindImportEqualsDeclaration:
		return node
	case KindExportDeclaration:
		exportClause := node.ExportDeclarationExportClause()
		if !exportClause.IsNil() && IsNamespaceExport(exportClause) {
			return exportClause
		}
	default:
		panic("Unhandled case in getNamespaceDeclarationNode")
	}
	return Handle{}
}

func ModuleExportNameIsDefault(node Handle) bool {
	return node.Text() == InternalSymbolNameDefault
}

func IsDefaultImport(node Handle /*ImportDeclaration | ImportEqualsDeclaration | ExportDeclaration*/) bool {
	switch node.Kind() {
	case KindImportDeclaration, KindJSImportDeclaration:
		importClause := node.ImportClause()
		return !importClause.IsNil() && !importClause.ImportClauseName().IsNil()
	}
	return false
}

func GetImpliedNodeFormatForFile(path string, packageJsonType string) core.ModuleKind {
	impliedNodeFormat := core.ResolutionModeNone
	if tspath.FileExtensionIsOneOf(path, []string{tspath.ExtensionDmts, tspath.ExtensionMts, tspath.ExtensionMjs}) {
		impliedNodeFormat = core.ResolutionModeESM
	} else if tspath.FileExtensionIsOneOf(path, []string{tspath.ExtensionDcts, tspath.ExtensionCts, tspath.ExtensionCjs}) {
		impliedNodeFormat = core.ResolutionModeCommonJS
	} else if tspath.FileExtensionIsOneOf(path, []string{tspath.ExtensionDts, tspath.ExtensionTs, tspath.ExtensionTsx, tspath.ExtensionJs, tspath.ExtensionJsx}) {
		impliedNodeFormat = core.IfElse(packageJsonType == "module", core.ResolutionModeESM, core.ResolutionModeCommonJS)
	}

	return impliedNodeFormat
}

func GetEmitModuleFormatOfFileWorker(fileName string, options *core.CompilerOptions, sourceFileMetaData SourceFileMetaData) core.ModuleKind {
	result := GetImpliedNodeFormatForEmitWorker(fileName, options.GetEmitModuleKind(), sourceFileMetaData)
	if result != core.ModuleKindNone {
		return result
	}
	return options.GetEmitModuleKind()
}

func GetImpliedNodeFormatForEmitWorker(fileName string, emitModuleKind core.ModuleKind, sourceFileMetaData SourceFileMetaData) core.ResolutionMode {
	if core.ModuleKindNode16 <= emitModuleKind && emitModuleKind <= core.ModuleKindNodeNext {
		return sourceFileMetaData.ImpliedNodeFormat
	}
	if sourceFileMetaData.ImpliedNodeFormat == core.ModuleKindCommonJS &&
		(sourceFileMetaData.PackageJsonType == "commonjs" ||
			tspath.FileExtensionIsOneOf(fileName, []string{tspath.ExtensionCjs, tspath.ExtensionCts})) {
		return core.ModuleKindCommonJS
	}
	if sourceFileMetaData.ImpliedNodeFormat == core.ModuleKindESNext &&
		(sourceFileMetaData.PackageJsonType == "module" ||
			tspath.FileExtensionIsOneOf(fileName, []string{tspath.ExtensionMjs, tspath.ExtensionMts})) {
		return core.ModuleKindESNext
	}
	return core.ModuleKindNone
}

func GetDeclarationContainer(node Handle) Handle {
	return FindAncestor(GetRootDeclaration(node), func(node Handle) bool {
		switch node.Kind() {
		case KindVariableDeclaration,
			KindVariableDeclarationList,
			KindImportSpecifier,
			KindNamedImports,
			KindNamespaceImport,
			KindImportClause:
			return false
		default:
			return true
		}
	}).Parent()
}

// Indicates that a symbol is an alias that does not merge with a local declaration.
// OR Is a JSContainer which may merge an alias with a local declaration
func IsNonLocalAlias(symbol *Symbol, excludes SymbolFlags) bool {
	if symbol == nil {
		return false
	}
	return symbol.Flags&(SymbolFlagsAlias|excludes) == SymbolFlagsAlias ||
		symbol.Flags&SymbolFlagsAlias != 0 && symbol.Flags&SymbolFlagsAssignment != 0
}

// An alias symbol is created by one of the following declarations:
//
//	import <symbol> = ...
//	const <symbol> = ... (JS only)
//	const { <symbol>, ... } = ... (JS only)
//	import <symbol> from ...
//	import * as <symbol> from ...
//	import { x as <symbol> } from ...
//	export { x as <symbol> } from ...
//	export * as ns <symbol> from ...
//	export = <EntityNameExpression>
//	export default <EntityNameExpression>
//	module.exports = <EntityNameExpression> (JS only)
//	module.exports.<symbol> = <EntityNameExpression> (JS only)
//	exports.<symbol> = <EntityNameExpression> (JS only)
func IsAliasSymbolDeclaration(node Handle) bool {
	switch node.Kind() {
	case KindImportEqualsDeclaration, KindNamespaceExportDeclaration, KindNamespaceImport, KindNamespaceExport,
		KindImportSpecifier, KindExportSpecifier:
		return true
	case KindImportClause:
		return !node.ImportClauseName().IsNil()
	case KindExportAssignment:
		return ExpressionIsAlias(node.Expression())
	case KindVariableDeclaration, KindBindingElement:
		return IsVariableDeclarationInitializedToRequire(node)
	case KindBinaryExpression:
		switch GetAssignmentDeclarationKind(node) {
		case JSDeclarationKindModuleExports, JSDeclarationKindExportsProperty:
			return ExpressionIsAlias(node.BinaryExpressionRight())
		}
	}
	return false
}

func IsParseTreeNode(node Handle) bool {
	return node.Flags()&NodeFlagsSynthesized == 0
}

// Returns a token if position is in [start-of-leading-trivia, end), includes JSDoc only if requested
func GetNodeAtPosition(file *SourceFile, position int, includeJSDoc bool) Handle {
	current := file.ParseRoot()
	if current.IsNil() {
		return Handle{}
	}
	for {
		var child Handle
		if includeJSDoc {
			for _, jsdoc := range current.JSDoc(file) {
				if nodeContainsPosition(jsdoc, position) {
					child = jsdoc
					break
				}
			}
		}
		if child.IsNil() {
			current.ForEachChild(func(node Handle) bool {
				if nodeContainsPosition(node, position) {
					child = node
					return true
				}
				return false
			})
		}
		if child.IsNil() || IsMetaProperty(child) {
			return current
		}
		current = child
	}
}

func nodeContainsPosition(node Handle, position int) bool {
	return node.Kind() >= KindFirstNode && node.Pos() <= position && (position < node.End() || position == node.End() && node.Kind() == KindEndOfFile)
}

func findImportOrRequire(text string, start int) (index int, size int) {
	index = max(start, 0)
	n := len(text)
	for index < n {
		next := strings.IndexAny(text[index:], "ir")
		if next < 0 {
			break
		}
		index += next

		var expected string
		if text[index] == 'i' {
			size = 6
			expected = "import"
		} else {
			size = 7
			expected = "require"
		}
		if index+size <= n && text[index:index+size] == expected {
			return index, size
		}
		index++
	}

	return -1, 0
}

func ForEachDynamicImportOrRequireCall(
	file *SourceFile,
	includeTypeSpaceImports bool,
	requireStringLiteralLikeArgument bool,
	cb func(node Handle, argument Handle) bool,
) bool {
	isJavaScriptFile := IsSourceFileJS(file)
	lastIndex, size := findImportOrRequire(file.Text(), 0)
	for lastIndex >= 0 {
		node := GetNodeAtPosition(file, lastIndex, isJavaScriptFile && includeTypeSpaceImports)
		if isJavaScriptFile && IsRequireCall(node, requireStringLiteralLikeArgument) {
			args := node.Arguments()
			if cb(node, args[0]) {
				return true
			}
		} else if IsImportCall(node) && len(node.Arguments()) > 0 && (!requireStringLiteralLikeArgument || IsStringLiteralLike(node.Arguments()[0])) {
			if cb(node, node.Arguments()[0]) {
				return true
			}
		} else if includeTypeSpaceImports && IsLiteralImportTypeNode(node) {
			if cb(node, node.ImportTypeNodeArgument().LiteralTypeNodeLiteral()) {
				return true
			}
		}
		// skip past import/require
		lastIndex += size
		lastIndex, size = findImportOrRequire(file.Text(), lastIndex)
	}
	return false
}

// Returns true if the node is a CallExpression to the identifier 'require' with
// exactly one argument (of the form 'require("name")').
// This function does not test if the node is in a JavaScript file or not.
func IsRequireCall(node Handle, requireStringLiteralLikeArgument bool) bool {
	if !IsCallExpression(node) {
		return false
	}
	if !IsIdentifier(node.CallExpressionExpression()) || node.CallExpressionExpression().Text() != "require" {
		return false
	}
	if len(node.Arguments()) != 1 {
		return false
	}
	return !requireStringLiteralLikeArgument || IsStringLiteralLike(node.Arguments()[0])
}

func IsRequireVariableStatement(node Handle) bool {
	if IsVariableStatement(node) {
		if declarations := node.VariableStatementDeclarationList().Declarations(); len(declarations) > 0 {
			return core.Every(declarations, IsVariableDeclarationInitializedToRequire)
		}
	}
	return false
}

func GetJSXImplicitImportBase(compilerOptions *core.CompilerOptions, file *SourceFile) string {
	jsxImportSourcePragma := GetPragmaFromSourceFile(file, "jsximportsource")
	jsxRuntimePragma := GetPragmaFromSourceFile(file, "jsxruntime")
	if GetPragmaArgument(jsxRuntimePragma, "factory") == "classic" {
		return ""
	}
	if compilerOptions.Jsx == core.JsxEmitReactJSX ||
		compilerOptions.Jsx == core.JsxEmitReactJSXDev ||
		compilerOptions.JsxImportSource != "" ||
		jsxImportSourcePragma != nil ||
		GetPragmaArgument(jsxRuntimePragma, "factory") == "automatic" {
		result := GetPragmaArgument(jsxImportSourcePragma, "factory")
		if result == "" {
			result = compilerOptions.JsxImportSource
		}
		if result == "" {
			result = "react"
		}
		return result
	}
	return ""
}

func GetJSXRuntimeImport(base string, options *core.CompilerOptions) string {
	if base == "" {
		return base
	}
	return base + "/" + core.IfElse(options.Jsx == core.JsxEmitReactJSXDev, "jsx-dev-runtime", "jsx-runtime")
}

func GetPragmaFromSourceFile(file *SourceFile, name string) *Pragma {
	var result *Pragma
	if file != nil {
		for i := range file.Pragmas {
			if file.Pragmas[i].Name == name {
				result = &file.Pragmas[i] // Last one wins
			}
		}
	}
	return result
}

func GetPragmaArgument(pragma *Pragma, name string) string {
	if pragma != nil {
		if arg, ok := pragma.Args[name]; ok {
			return arg.Value
		}
	}
	return ""
}

// Of the form: `const x = require("x")` or `const { x } = require("x")` or with `var` or `let`
// The variable must not be exported and must not have a type annotation, even a jsdoc one.
// The initializer must be a call to `require` with a string literal or a string literal-like argument.
func IsVariableDeclarationInitializedToRequire(node Handle) bool {
	if node.Kind() == KindBindingElement {
		node = node.Parent().Parent()
	}
	return isVariableDeclarationInitializedWithRequireHelper(node, false /*allowAccessedRequire*/)
}

func IsVariableDeclarationInitializedToBareOrAccessedRequire(node Handle) bool {
	return isVariableDeclarationInitializedWithRequireHelper(node, true /*allowAccessedRequire*/)
}

func isVariableDeclarationInitializedWithRequireHelper(node Handle, allowAccessedRequire bool) bool {
	if !IsInJSFile(node) {
		return false
	}
	if node.Kind() != KindVariableDeclaration {
		return false
	}
	initializer := node.Initializer()
	if initializer.IsNil() {
		return false
	}
	if allowAccessedRequire {
		initializer = GetLeftmostAccessExpression(initializer)
	}

	return node.Parent().Parent().ModifierFlags()&ModifierFlagsExport == 0 &&
		node.Type().IsNil() &&
		IsRequireCall(initializer, true /*requireStringLiteralLikeArgument*/)
}

func GetModuleSpecifierOfBareOrAccessedRequire(node Handle) Handle {
	if isVariableDeclarationInitializedWithRequireHelper(node, false /*allowAccessedRequire*/) {
		return node.Initializer().Arguments()[0]
	}
	if isVariableDeclarationInitializedWithRequireHelper(node, true /*allowAccessedRequire*/) {
		leftmost := GetLeftmostAccessExpression(node.Initializer())
		if IsRequireCall(leftmost, true /*requireStringLiteralLikeArgument*/) {
			return leftmost.Arguments()[0]
		}
	}
	return Handle{}
}

func IsModuleExportsAccessExpression(node Handle) bool {
	if IsAccessExpression(node) && IsModuleIdentifier(node.Expression()) {
		if name := GetElementOrPropertyAccessName(node); !name.IsNil() {
			return name.Text() == "exports"
		}
	}
	return false
}

func IsModuleExportsQualifiedName(node Handle) bool {
	return IsQualifiedName(node) && IsModuleIdentifier(node.QualifiedNameLeft()) && node.QualifiedNameRight().Text() == "exports"
}

func IsCheckJSEnabledForFile(sourceFile *SourceFile, compilerOptions *core.CompilerOptions) bool {
	if sourceFile.CheckJsDirective != nil {
		return sourceFile.CheckJsDirective.Enabled
	}
	return compilerOptions.CheckJs == core.TSTrue
}

func IsPlainJSFile(file *SourceFile, checkJs core.Tristate) bool {
	return file != nil && (file.ScriptKind == core.ScriptKindJS || file.ScriptKind == core.ScriptKindJSX) && file.CheckJsDirective == nil && checkJs == core.TSUnknown
}

func GetLeftmostAccessExpression(expr Handle) Handle {
	for IsAccessExpression(expr) {
		expr = expr.Expression()
	}
	return expr
}

func IsTypeOnlyImportDeclaration(node Handle) bool {
	switch node.Kind() {
	case KindImportSpecifier:
		return node.IsTypeOnly() || node.Parent().Parent().IsTypeOnly()
	case KindNamespaceImport:
		return node.Parent().IsTypeOnly()
	case KindImportClause, KindImportEqualsDeclaration:
		return node.IsTypeOnly()
	}
	return false
}

func isTypeOnlyExportDeclaration(node Handle) bool {
	switch node.Kind() {
	case KindExportSpecifier:
		return node.IsTypeOnly() || node.Parent().Parent().IsTypeOnly()
	case KindExportDeclaration:
		return node.ExportDeclarationIsTypeOnly() && !node.ExportDeclarationModuleSpecifier().IsNil() && node.ExportDeclarationExportClause().IsNil()
	case KindNamespaceExport:
		return node.Parent().IsTypeOnly()
	}
	return false
}

func IsTypeOnlyImportOrExportDeclaration(node Handle) bool {
	return IsTypeOnlyImportDeclaration(node) || isTypeOnlyExportDeclaration(node)
}

func IsExclusivelyTypeOnlyImportOrExport(node Handle) bool {
	switch node.Kind() {
	case KindExportDeclaration:
		return node.IsTypeOnly()
	case KindImportDeclaration, KindJSImportDeclaration:
		if importClause := node.ImportClause(); !importClause.IsNil() {
			return importClause.IsTypeOnly()
		}
	case KindJSDocImportTag:
		if importClause := node.ImportClause(); !importClause.IsNil() {
			return importClause.IsTypeOnly()
		}
	}
	return false
}

func GetClassLikeDeclarationOfSymbol(symbol *Symbol) Handle {
	return FindSymbolDeclaration(symbol, IsClassLike)
}

func IsCallLikeExpression(node Handle) bool {
	switch node.Kind() {
	case KindJsxOpeningElement, KindJsxSelfClosingElement, KindJsxOpeningFragment, KindCallExpression, KindNewExpression,
		KindTaggedTemplateExpression, KindDecorator:
		return true
	case KindBinaryExpression:
		return node.BinaryExpressionOperatorToken().Kind() == KindInstanceOfKeyword
	}
	return false
}

func IsJsxCallLike(node Handle) bool {
	switch node.Kind() {
	case KindJsxOpeningElement, KindJsxSelfClosingElement, KindJsxOpeningFragment:
		return true
	}
	return false
}

func IsCallLikeOrFunctionLikeExpression(node Handle) bool {
	return IsCallLikeExpression(node) || IsFunctionExpressionOrArrowFunction(node)
}

func NodeHasKind(node Handle, kind Kind) bool {
	if node.IsNil() {
		return false
	}
	return node.Kind() == kind
}

func IsContextualKeyword(token Kind) bool {
	return KindFirstContextualKeyword <= token && token <= KindLastContextualKeyword
}

func IsThisInTypeQuery(node Handle) bool {
	if !IsThisIdentifier(node) {
		return false
	}
	for IsQualifiedName(node.Parent()) && node.Parent().QualifiedNameLeft() == node {
		node = node.Parent()
	}
	return node.Parent().Kind() == KindTypeQuery
}

// Gets whether a bound `VariableDeclaration` or `VariableDeclarationList` is part of a `let` declaration.
func IsLet(node Handle) bool {
	return GetCombinedNodeFlags(node)&NodeFlagsBlockScoped == NodeFlagsLet
}

func IsClassMemberModifier(token Kind) bool {
	return IsParameterPropertyModifier(token) || token == KindStaticKeyword ||
		token == KindOverrideKeyword || token == KindAccessorKeyword
}

func IsParameterPropertyModifier(kind Kind) bool {
	return ModifierToFlag(kind)&ModifierFlagsParameterPropertyModifier != 0
}

func ForEachChildAndJSDoc(node Handle, sourceFile *SourceFile, v StoreVisitor) bool {
	for _, jsdoc := range node.JSDoc(sourceFile) {
		if v(jsdoc) {
			return true
		}
	}
	return node.ForEachChild(v)
}

func HasTypeArguments(node Handle) bool {
	switch node.Kind() {
	case KindCallExpression, KindNewExpression, KindTaggedTemplateExpression,
		KindTypeReference, KindExpressionWithTypeArguments, KindImportType,
		KindTypeQuery, KindJsxOpeningElement, KindJsxSelfClosingElement:
		return true
	}
	return false
}

func IsTypeReferenceType(node Handle) bool {
	return node.Kind() == KindTypeReference || node.Kind() == KindExpressionWithTypeArguments
}

func IsVariableLike(node Handle) bool {
	switch node.Kind() {
	case KindBindingElement, KindEnumMember, KindParameter, KindPropertyAssignment, KindPropertyDeclaration,
		KindPropertySignature, KindShorthandPropertyAssignment, KindVariableDeclaration:
		return true
	}
	return false
}

func HasInitializer(node Handle) bool {
	switch node.Kind() {
	case KindVariableDeclaration, KindParameter, KindBindingElement, KindPropertyDeclaration,
		KindPropertyAssignment, KindEnumMember, KindForStatement, KindForInStatement, KindForOfStatement,
		KindJsxAttribute:
		return !node.Initializer().IsNil()
	default:
		return false
	}
}

func IsVariableParameterOrProperty(node Handle) bool {
	switch node.Kind() {
	case KindVariableDeclaration, KindParameter, KindPropertySignature, KindPropertyDeclaration:
		return true
	default:
		return false
	}
}

func GetTypeAnnotationNode(node Handle) Handle {
	switch node.Kind() {
	case KindVariableDeclaration, KindParameter, KindPropertySignature, KindPropertyDeclaration,
		KindTypePredicate, KindParenthesizedType, KindTypeOperator, KindMappedType, KindTypeAssertionExpression,
		KindAsExpression, KindSatisfiesExpression, KindTypeAliasDeclaration, KindJSTypeAliasDeclaration,
		KindNamedTupleMember, KindOptionalType, KindRestType, KindTemplateLiteralTypeSpan, KindJSDocTypeExpression,
		KindJSDocPropertyTag, KindJSDocNullableType, KindJSDocNonNullableType, KindJSDocOptionalType:
		return node.Type()
	default:
		return node.Type()
	}
}

func IsObjectTypeDeclaration(node Handle) bool {
	return IsClassLike(node) || IsInterfaceDeclaration(node) || IsTypeLiteralNode(node)
}

func IsClassOrTypeElement(node Handle) bool {
	return IsClassElement(node) || IsTypeElement(node)
}

func GetClassExtendsHeritageElement(node Handle) Handle {
	heritageElements := GetHeritageElements(node, KindExtendsKeyword)
	if len(heritageElements) > 0 {
		return heritageElements[0]
	}
	return Handle{}
}

func IsTypeKeywordToken(node Handle) bool {
	return node.Kind() == KindTypeKeyword
}

// See `IsJSDocSingleCommentNode`.
func IsJSDocSingleCommentNodeList(store *Store, nodeList ListRef) bool {
	if store == nil || nodeList == 0 || store.ListLen(nodeList) == 0 {
		return false
	}
	parent := store.ListAt(nodeList, 0).Parent()
	if parent.IsNil() {
		return false
	}
	return IsJSDocSingleCommentNode(parent) && nodeList == parent.CommentList()
}

// See `IsJSDocSingleCommentNode`.
func IsJSDocSingleCommentNodeComment(node Handle) bool {
	if node.IsNil() || node.Parent().IsNil() {
		return false
	}
	return IsJSDocSingleCommentNode(node.Parent()) && node == node.Store().ListAt(node.Parent().CommentList(), 0)
}

// In Strada, if a JSDoc node has a single comment, that comment is represented as a string property
// as a simplification, and therefore that comment is not visited by `forEachChild`.
func IsJSDocSingleCommentNode(node Handle) bool {
	return hasComment(node.Kind()) && node.CommentList() != 0 && node.Store().ListLen(node.CommentList()) == 1
}

func IsValidTypeOnlyAliasUseSite(useSite Handle) bool {
	return useSite.Flags()&(NodeFlagsAmbient|NodeFlagsJSDoc) != 0 ||
		IsPartOfTypeQuery(useSite) ||
		isIdentifierInNonEmittingHeritageClause(useSite) ||
		isPartOfPossiblyValidTypeOrAbstractComputedPropertyName(useSite) ||
		!(IsExpressionNode(useSite) || isShorthandPropertyNameUseSite(useSite))
}

func isIdentifierInNonEmittingHeritageClause(node Handle) bool {
	if !IsIdentifier(node) {
		return false
	}
	parent := node.Parent()
	for IsPropertyAccessExpression(parent) || IsExpressionWithTypeArguments(parent) {
		parent = parent.Parent()
	}
	return IsHeritageClause(parent) && (parent.HeritageClauseToken() == KindImplementsKeyword || IsInterfaceDeclaration(parent.Parent()))
}

func isPartOfPossiblyValidTypeOrAbstractComputedPropertyName(node Handle) bool {
	for NodeKindIs(node, KindIdentifier, KindPropertyAccessExpression) {
		node = node.Parent()
	}
	if node.Kind() != KindComputedPropertyName {
		return false
	}
	if HasSyntacticModifier(node.Parent(), ModifierFlagsAbstract) {
		return true
	}
	return NodeKindIs(node.Parent().Parent(), KindInterfaceDeclaration, KindTypeLiteral)
}

func isShorthandPropertyNameUseSite(useSite Handle) bool {
	return IsIdentifier(useSite) && IsShorthandPropertyAssignment(useSite.Parent()) && useSite.Parent().ShorthandPropertyAssignmentName() == useSite
}

func GetPropertyNameForPropertyNameNode(name Handle) string {
	switch name.Kind() {
	case KindIdentifier, KindPrivateIdentifier, KindStringLiteral, KindNoSubstitutionTemplateLiteral,
		KindNumericLiteral, KindBigIntLiteral, KindJsxNamespacedName:
		return name.Text()
	case KindComputedPropertyName:
		nameExpression := name.Expression()
		if IsStringOrNumericLiteralLike(nameExpression) {
			return nameExpression.Text()
		}
		if IsSignedNumericLiteral(nameExpression) {
			text := nameExpression.PrefixUnaryExpressionOperand().Text()
			if nameExpression.PrefixUnaryExpressionOperator() == KindMinusToken {
				text = "-" + text
			}
			return text
		}
		return InternalSymbolNameMissing
	}
	panic("Unhandled case in getPropertyNameForPropertyNameNode")
}

func IsPartOfTypeOnlyImportOrExportDeclaration(node Handle) bool {
	return !FindAncestor(node, IsTypeOnlyImportOrExportDeclaration).IsNil()
}

func IsPartOfExclusivelyTypeOnlyImportOrExportDeclaration(node Handle) bool {
	return !FindAncestor(node, IsExclusivelyTypeOnlyImportOrExport).IsNil()
}

func IsEmittableImport(node Handle) bool {
	switch node.Kind() {
	case KindImportDeclaration:
		return !node.ImportClause().IsNil() && !node.ImportClause().IsTypeOnly()
	case KindExportDeclaration, KindImportEqualsDeclaration:
		return !node.IsTypeOnly()
	case KindCallExpression:
		return IsImportCall(node)
	}
	return false
}

func IsResolutionModeOverrideHost(node Handle) bool {
	if node.IsNil() {
		return false
	}
	switch node.Kind() {
	case KindImportType, KindExportDeclaration, KindImportDeclaration, KindJSImportDeclaration:
		return true
	}
	return false
}

func (h Handle) ResolutionModeOverride() (core.ResolutionMode, bool) {
	if h.IsNil() || h.Kind() != KindImportAttributes {
		return core.ResolutionModeNone, false
	}
	attrs := h.ImportAttributesAttributes()
	store := h.Store()
	if store == nil || attrs == 0 || store.ListLen(attrs) != 1 {
		return core.ResolutionModeNone, false
	}
	elem := store.ListAt(attrs, 0)
	name := elem.ImportAttributeName()
	value := elem.ImportAttributeValue()
	if !IsStringLiteralLike(name) || name.Text() != "resolution-mode" {
		return core.ResolutionModeNone, false
	}
	if !IsStringLiteralLike(value) {
		return core.ResolutionModeNone, false
	}
	switch value.Text() {
	case "import":
		return core.ResolutionModeESM, true
	case "require":
		return core.ModuleKindCommonJS, true
	default:
		return core.ResolutionModeNone, false
	}
}

func HasResolutionModeOverride(node Handle) bool {
	if node.IsNil() {
		return false
	}
	var attributes Handle
	switch node.Kind() {
	case KindImportType:
		attributes = node.ImportTypeNodeAttributes()
	case KindImportDeclaration, KindJSImportDeclaration:
		attributes = node.ImportDeclarationAttributes()
	case KindExportDeclaration:
		attributes = node.ExportDeclarationAttributes()
	}
	if !attributes.IsNil() {
		_, ok := attributes.ResolutionModeOverride()
		return ok
	}
	return false
}

func IsStringTextContainingNode(node Handle) bool {
	return node.Kind() == KindStringLiteral || IsTemplateLiteralKind(node.Kind())
}

func IsTemplateLiteralKind(kind Kind) bool {
	return KindFirstTemplateToken <= kind && kind <= KindLastTemplateToken
}

func IsTemplateLiteralToken(node Handle) bool {
	return IsTemplateLiteralKind(node.Kind())
}

func GetExternalModuleImportEqualsDeclarationExpression(node Handle) Handle {
	debug.Assert(IsExternalModuleImportEqualsDeclaration(node))
	return node.ImportEqualsDeclarationModuleReference().Expression()
}

func CreateModifiersFromModifierFlags(flags ModifierFlags, createModifier func(kind Kind) Handle) []Handle {
	var result []Handle
	if flags&ModifierFlagsExport != 0 {
		result = append(result, createModifier(KindExportKeyword))
	}
	if flags&ModifierFlagsAmbient != 0 {
		result = append(result, createModifier(KindDeclareKeyword))
	}
	if flags&ModifierFlagsDefault != 0 {
		result = append(result, createModifier(KindDefaultKeyword))
	}
	if flags&ModifierFlagsConst != 0 {
		result = append(result, createModifier(KindConstKeyword))
	}
	if flags&ModifierFlagsPublic != 0 {
		result = append(result, createModifier(KindPublicKeyword))
	}
	if flags&ModifierFlagsPrivate != 0 {
		result = append(result, createModifier(KindPrivateKeyword))
	}
	if flags&ModifierFlagsProtected != 0 {
		result = append(result, createModifier(KindProtectedKeyword))
	}
	if flags&ModifierFlagsAbstract != 0 {
		result = append(result, createModifier(KindAbstractKeyword))
	}
	if flags&ModifierFlagsStatic != 0 {
		result = append(result, createModifier(KindStaticKeyword))
	}
	if flags&ModifierFlagsOverride != 0 {
		result = append(result, createModifier(KindOverrideKeyword))
	}
	if flags&ModifierFlagsReadonly != 0 {
		result = append(result, createModifier(KindReadonlyKeyword))
	}
	if flags&ModifierFlagsAccessor != 0 {
		result = append(result, createModifier(KindAccessorKeyword))
	}
	if flags&ModifierFlagsAsync != 0 {
		result = append(result, createModifier(KindAsyncKeyword))
	}
	if flags&ModifierFlagsIn != 0 {
		result = append(result, createModifier(KindInKeyword))
	}
	if flags&ModifierFlagsOut != 0 {
		result = append(result, createModifier(KindOutKeyword))
	}
	return result
}

func GetThisParameter(signature Handle) Handle {
	// callback tags do not currently support this parameters
	if len(signature.Parameters()) != 0 {
		thisParameter := signature.Parameters()[0]
		if IsThisParameter(thisParameter) {
			return thisParameter
		}
	}
	return Handle{}
}

func ReplaceModifiers(factory *NodeFactory, node *Node, modifierArray *ModifierList) *Node {
	switch node.Kind {
	case KindTypeParameter:
		return factory.UpdateTypeParameterDeclaration(
			node.AsTypeParameterDeclaration(),
			modifierArray,
			node.Name(),
			node.AsTypeParameterDeclaration().Constraint,
			node.AsTypeParameterDeclaration().Expression,
			node.AsTypeParameterDeclaration().DefaultType,
		)
	case KindParameter:
		return factory.UpdateParameterDeclaration(
			node.AsParameterDeclaration(),
			modifierArray,
			node.AsParameterDeclaration().DotDotDotToken,
			node.Name(),
			node.QuestionToken(),
			node.Type(),
			node.Initializer(),
		)
	case KindConstructorType:
		return factory.UpdateConstructorTypeNode(
			node.AsConstructorTypeNode(),
			modifierArray,
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
		)
	case KindPropertySignature:
		return factory.UpdatePropertySignatureDeclaration(
			node.AsPropertySignatureDeclaration(),
			modifierArray,
			node.Name(),
			node.PostfixToken(),
			node.Type(),
			node.Initializer(),
		)
	case KindPropertyDeclaration:
		return factory.UpdatePropertyDeclaration(
			node.AsPropertyDeclaration(),
			modifierArray,
			node.Name(),
			node.PostfixToken(),
			node.Type(),
			node.Initializer(),
		)
	case KindMethodSignature:
		return factory.UpdateMethodSignatureDeclaration(
			node.AsMethodSignatureDeclaration(),
			modifierArray,
			node.Name(),
			node.PostfixToken(),
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
		)
	case KindMethodDeclaration:
		return factory.UpdateMethodDeclaration(
			node.AsMethodDeclaration(),
			modifierArray,
			node.AsMethodDeclaration().AsteriskToken,
			node.Name(),
			node.PostfixToken(),
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
			node.AsMethodDeclaration().FullSignature,
			node.Body(),
		)
	case KindConstructor:
		return factory.UpdateConstructorDeclaration(
			node.AsConstructorDeclaration(),
			modifierArray,
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
			node.AsConstructorDeclaration().FullSignature,
			node.Body(),
		)
	case KindGetAccessor:
		return factory.UpdateGetAccessorDeclaration(
			node.AsGetAccessorDeclaration(),
			modifierArray,
			node.Name(),
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
			node.AsGetAccessorDeclaration().FullSignature,
			node.Body(),
		)
	case KindSetAccessor:
		return factory.UpdateSetAccessorDeclaration(
			node.AsSetAccessorDeclaration(),
			modifierArray,
			node.Name(),
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
			node.AsSetAccessorDeclaration().FullSignature,
			node.Body(),
		)
	case KindIndexSignature:
		return factory.UpdateIndexSignatureDeclaration(
			node.AsIndexSignatureDeclaration(),
			modifierArray,
			node.ParameterList(),
			node.Type(),
		)
	case KindFunctionExpression:
		return factory.UpdateFunctionExpression(
			node.AsFunctionExpression(),
			modifierArray,
			node.AsFunctionExpression().AsteriskToken,
			node.Name(),
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
			node.AsFunctionExpression().FullSignature,
			node.Body(),
		)
	case KindArrowFunction:
		return factory.UpdateArrowFunction(
			node.AsArrowFunction(),
			modifierArray,
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
			node.AsArrowFunction().FullSignature,
			node.AsArrowFunction().EqualsGreaterThanToken,
			node.Body(),
		)
	case KindClassExpression:
		return factory.UpdateClassExpression(
			node.AsClassExpression(),
			modifierArray,
			node.Name(),
			node.TypeParameterList(),
			node.AsClassExpression().HeritageClauses,
			node.MemberList(),
		)
	case KindVariableStatement:
		return factory.UpdateVariableStatement(
			node.AsVariableStatement(),
			modifierArray,
			node.AsVariableStatement().DeclarationList,
		)
	case KindFunctionDeclaration:
		return factory.UpdateFunctionDeclaration(
			node.AsFunctionDeclaration(),
			modifierArray,
			node.AsFunctionDeclaration().AsteriskToken,
			node.Name(),
			node.TypeParameterList(),
			node.ParameterList(),
			node.Type(),
			node.AsFunctionDeclaration().FullSignature,
			node.Body(),
		)
	case KindClassDeclaration:
		return factory.UpdateClassDeclaration(
			node.AsClassDeclaration(),
			modifierArray,
			node.Name(),
			node.TypeParameterList(),
			node.AsClassDeclaration().HeritageClauses,
			node.MemberList(),
		)
	case KindInterfaceDeclaration:
		return factory.UpdateInterfaceDeclaration(
			node.AsInterfaceDeclaration(),
			modifierArray,
			node.Name(),
			node.TypeParameterList(),
			node.AsInterfaceDeclaration().HeritageClauses,
			node.MemberList(),
		)
	case KindTypeAliasDeclaration:
		return factory.UpdateTypeAliasDeclaration(
			node.AsTypeAliasDeclaration(),
			modifierArray,
			node.Name(),
			node.TypeParameterList(),
			node.Type(),
		)
	case KindEnumDeclaration:
		return factory.UpdateEnumDeclaration(
			node.AsEnumDeclaration(),
			modifierArray,
			node.Name(),
			node.MemberList(),
		)
	case KindModuleDeclaration:
		return factory.UpdateModuleDeclaration(
			node.AsModuleDeclaration(),
			modifierArray,
			node.AsModuleDeclaration().Keyword,
			node.Name(),
			node.Body(),
		)
	case KindImportEqualsDeclaration:
		return factory.UpdateImportEqualsDeclaration(
			node.AsImportEqualsDeclaration(),
			modifierArray,
			node.IsTypeOnly(),
			node.Name(),
			node.AsImportEqualsDeclaration().ModuleReference,
		)
	case KindImportDeclaration:
		return factory.UpdateImportDeclaration(
			node.AsImportDeclaration(),
			modifierArray,
			node.ImportClause(),
			node.ModuleSpecifier(),
			node.AsImportDeclaration().Attributes,
		)
	case KindExportAssignment:
		return factory.UpdateExportAssignment(
			node.AsExportAssignment(),
			modifierArray,
			node.AsExportAssignment().IsExportEquals,
			node.Type(),
			node.Expression(),
		)
	case KindExportDeclaration:
		return factory.UpdateExportDeclaration(
			node.AsExportDeclaration(),
			modifierArray,
			node.IsTypeOnly(),
			node.AsExportDeclaration().ExportClause,
			node.ModuleSpecifier(),
			node.AsExportDeclaration().Attributes,
		)
	}
	panic(fmt.Sprintf("Node that does not have modifiers tried to have modifier replaced: %d", node.Kind))
}

func IsLateVisibilityPaintedStatement(node Handle) bool {
	switch node.Kind() {
	case KindImportDeclaration,
		KindJSImportDeclaration,
		KindImportEqualsDeclaration,
		KindVariableStatement,
		KindClassDeclaration,
		KindFunctionDeclaration,
		KindModuleDeclaration,
		KindTypeAliasDeclaration,
		KindJSTypeAliasDeclaration,
		KindInterfaceDeclaration,
		KindEnumDeclaration:
		return true
	default:
		return false
	}
}

func IsExternalModuleAugmentation(node Handle) bool {
	return IsAmbientModule(node) && IsModuleAugmentationExternal(node)
}

func GetSourceFileOfModule(module *Symbol) *SourceFile {
	declaration := NodeOf(module.ValueDeclaration)
	if declaration.IsNil() {
		declaration = GetNonAugmentationDeclaration(module)
	}
	return GetSourceFileOfNode(declaration)
}

func GetNonAugmentationDeclaration(symbol *Symbol) Handle {
	return FindSymbolDeclaration(symbol, func(d Handle) bool {
		return !IsExternalModuleAugmentation(d) && !IsGlobalScopeAugmentation(d)
	})
}

func IsTypeDeclaration(node Handle) bool {
	switch node.Kind() {
	case KindTypeParameter, KindClassDeclaration, KindInterfaceDeclaration, KindTypeAliasDeclaration, KindJSTypeAliasDeclaration, KindEnumDeclaration:
		return true
	case KindImportClause:
		return node.IsTypeOnly()
	case KindImportSpecifier, KindExportSpecifier:
		return node.Parent().Parent().IsTypeOnly()
	default:
		return false
	}
}

func IsTypeDeclarationName(name Handle) bool {
	return name.Kind() == KindIdentifier &&
		IsTypeDeclaration(name.Parent()) &&
		GetNameOfDeclaration(name.Parent()) == name
}

func IsRightSideOfPropertyAccess(node Handle) bool {
	return node.Parent().Kind() == KindPropertyAccessExpression && node.Parent().Name() == node
}

func IsArgumentExpressionOfElementAccess(node Handle) bool {
	return !node.Parent().IsNil() && node.Parent().Kind() == KindElementAccessExpression && node.Parent().ElementAccessExpressionArgumentExpression() == node
}

func ClimbPastPropertyAccess(node Handle) Handle {
	if IsRightSideOfPropertyAccess(node) {
		return node.Parent()
	}
	return node
}

func climbPastPropertyOrElementAccess(node Handle) Handle {
	if IsRightSideOfPropertyAccess(node) || IsArgumentExpressionOfElementAccess(node) {
		return node.Parent()
	}
	return node
}

func selectExpressionOfCallOrNewExpressionOrDecorator(node Handle) Handle {
	if IsCallExpression(node) || IsNewExpression(node) || IsDecorator(node) {
		return node.Expression()
	}
	return Handle{}
}

func selectTagOfTaggedTemplateExpression(node Handle) Handle {
	if IsTaggedTemplateExpression(node) {
		return node.TaggedTemplateExpressionTag()
	}
	return Handle{}
}

func selectTagNameOfJsxOpeningLikeElement(node Handle) Handle {
	if IsJsxOpeningElement(node) || IsJsxSelfClosingElement(node) {
		return node.TagName()
	}
	return Handle{}
}

func IsCallExpressionTarget(node Handle, includeElementAccess bool, skipPastOuterExpressions bool) bool {
	return isCalleeWorker(node, IsCallExpression, selectExpressionOfCallOrNewExpressionOrDecorator, includeElementAccess, skipPastOuterExpressions)
}

func IsNewExpressionTarget(node Handle, includeElementAccess bool, skipPastOuterExpressions bool) bool {
	return isCalleeWorker(node, IsNewExpression, selectExpressionOfCallOrNewExpressionOrDecorator, includeElementAccess, skipPastOuterExpressions)
}

func IsCallOrNewExpressionTarget(node Handle, includeElementAccess bool, skipPastOuterExpressions bool) bool {
	return isCalleeWorker(node, IsCallOrNewExpression, selectExpressionOfCallOrNewExpressionOrDecorator, includeElementAccess, skipPastOuterExpressions)
}

func IsTaggedTemplateTag(node Handle, includeElementAccess bool, skipPastOuterExpressions bool) bool {
	return isCalleeWorker(node, IsTaggedTemplateExpression, selectTagOfTaggedTemplateExpression, includeElementAccess, skipPastOuterExpressions)
}

func IsDecoratorTarget(node Handle, includeElementAccess bool, skipPastOuterExpressions bool) bool {
	return isCalleeWorker(node, IsDecorator, selectExpressionOfCallOrNewExpressionOrDecorator, includeElementAccess, skipPastOuterExpressions)
}

func IsJsxOpeningLikeElementTagName(node Handle, includeElementAccess bool, skipPastOuterExpressions bool) bool {
	return isCalleeWorker(node, IsJsxOpeningLikeElement, selectTagNameOfJsxOpeningLikeElement, includeElementAccess, skipPastOuterExpressions)
}

func isCalleeWorker(
	node Handle,
	pred func(Handle) bool,
	calleeSelector func(Handle) Handle,
	includeElementAccess bool,
	skipPastOuterExpressions bool,
) bool {
	var target Handle
	if includeElementAccess {
		target = climbPastPropertyOrElementAccess(node)
	} else {
		target = ClimbPastPropertyAccess(node)
	}
	if skipPastOuterExpressions {
		if IsExpression(target) {
			target = SkipOuterExpressions(target, OEKAll)
		}
	}
	parent := target.Parent()
	return !target.IsNil() && !parent.IsNil() && pred(parent) && calleeSelector(parent) == target
}

func IsRightSideOfQualifiedNameOrPropertyAccess(node Handle) bool {
	parent := node.Parent()
	switch parent.Kind() {
	case KindQualifiedName:
		return parent.QualifiedNameRight() == node
	case KindPropertyAccessExpression:
		return parent.PropertyAccessExpressionName() == node
	case KindMetaProperty:
		return parent.MetaPropertyName() == node
	}
	return false
}

func ShouldTransformImportCall(fileName string, options *core.CompilerOptions, impliedNodeFormatForEmit core.ModuleKind) bool {
	moduleKind := options.GetEmitModuleKind()
	if core.ModuleKindNode16 <= moduleKind && moduleKind <= core.ModuleKindNodeNext || moduleKind == core.ModuleKindPreserve {
		return false
	}
	return impliedNodeFormatForEmit < core.ModuleKindES2015
}

func HasQuestionToken(node Handle) bool {
	return IsQuestionToken(node.QuestionToken())
}

func IsJsxOpeningLikeElement(node Handle) bool {
	return IsJsxOpeningElement(node) || IsJsxSelfClosingElement(node)
}

func GetInvokedExpression(node Handle) Handle {
	switch node.Kind() {
	case KindTaggedTemplateExpression:
		return node.TaggedTemplateExpressionTag()
	case KindJsxOpeningElement, KindJsxSelfClosingElement:
		return node.TagName()
	case KindBinaryExpression:
		return node.BinaryExpressionRight()
	case KindJsxOpeningFragment:
		return node
	default:
		return node.Expression()
	}
}

func IsCallOrNewExpression(node Handle) bool {
	return IsCallExpression(node) || IsNewExpression(node)
}

func IndexOfNode(nodes []Handle, node Handle) int {
	index, ok := slices.BinarySearchFunc(nodes, node, func(n1, n2 Handle) int {
		return core.CompareTextRanges(n1.Loc(), n2.Loc())
	})
	if ok {
		return index
	}
	return -1
}

func CompareNodePositions(n1, n2 *Node) int {
	return core.CompareTextRanges(n1.Loc, n2.Loc)
}

func IsUnterminatedLiteral(node Handle) bool {
	return (IsLiteralKind(node.Kind()) || IsTemplateLiteralKind(node.Kind())) &&
		node.TokenFlags()&TokenFlagsUnterminated != 0
}

// Gets a value indicating whether a class element is either a static or an instance property declaration with an initializer.
func IsInitializedProperty(member Handle) bool {
	return member.Kind() == KindPropertyDeclaration &&
		!member.Initializer().IsNil()
}

func IsTrivia(token Kind) bool {
	return KindFirstTriviaToken <= token && token <= KindLastTriviaToken
}

func HasDecorators(node Handle) bool {
	return HasSyntacticModifier(node, ModifierFlagsDecorator)
}

type hasFileNameImpl struct {
	fileName string
	path     tspath.Path
}

func NewHasFileName(fileName string, path tspath.Path) HasFileName {
	return &hasFileNameImpl{
		fileName: fileName,
		path:     path,
	}
}

func (h *hasFileNameImpl) FileName() string {
	return h.fileName
}

func (h *hasFileNameImpl) Path() tspath.Path {
	return h.path
}

func GetSemanticJsxChildren(children []Handle) []Handle {
	return core.Filter(children, func(i Handle) bool {
		switch i.Kind() {
		case KindJsxExpression:
			return !i.Expression().IsNil()
		case KindJsxText:
			return !i.JsxTextContainsOnlyTriviaWhiteSpaces()
		default:
			return true
		}
	})
}

// Returns true if the node kind has a comment property.
func hasComment(kind Kind) bool {
	switch kind {
	case KindJSDoc, KindJSDocUnknownTag, KindJSDocAugmentsTag, KindJSDocImplementsTag,
		KindJSDocDeprecatedTag, KindJSDocPublicTag, KindJSDocPrivateTag, KindJSDocProtectedTag,
		KindJSDocReadonlyTag, KindJSDocOverrideTag, KindJSDocCallbackTag, KindJSDocOverloadTag,
		KindJSDocParameterTag, KindJSDocPropertyTag, KindJSDocReturnTag, KindJSDocThisTag,
		KindJSDocTypeTag, KindJSDocTemplateTag, KindJSDocTypedefTag, KindJSDocSeeTag,
		KindJSDocThrowsTag, KindJSDocSatisfiesTag, KindJSDocImportTag:
		return true
	default:
		return false
	}
}

func IsAssignmentPattern(node Handle) bool {
	return node.Kind() == KindArrayLiteralExpression || node.Kind() == KindObjectLiteralExpression
}

func GetElementsOfBindingOrAssignmentPattern(name Handle) []Handle {
	switch name.Kind() {
	case KindObjectBindingPattern, KindArrayBindingPattern, KindArrayLiteralExpression:
		// `a` in `{a}`
		// `a` in `[a]`
		return name.Elements()
	case KindObjectLiteralExpression:
		// `a` in `{a}`
		return name.Properties()
	}
	return nil
}

func IsDeclarationBindingElement(bindingElement Handle) bool {
	switch bindingElement.Kind() {
	case KindVariableDeclaration, KindParameter, KindBindingElement:
		return true
	default:
		return false
	}
}

/**
 * Gets the name of an BindingOrAssignmentElement.
 */
func GetTargetOfBindingOrAssignmentElement(bindingElement Handle) Handle {
	if IsDeclarationBindingElement(bindingElement) {
		// `a` in `let { a } = ...`
		// `a` in `let { a = 1 } = ...`
		// `b` in `let { a: b } = ...`
		// `b` in `let { a: b = 1 } = ...`
		// `a` in `let { ...a } = ...`
		// `{b}` in `let { a: {b} } = ...`
		// `{b}` in `let { a: {b} = 1 } = ...`
		// `[b]` in `let { a: [b] } = ...`
		// `[b]` in `let { a: [b] = 1 } = ...`
		// `a` in `let [a] = ...`
		// `a` in `let [a = 1] = ...`
		// `a` in `let [...a] = ...`
		// `{a}` in `let [{a}] = ...`
		// `{a}` in `let [{a} = 1] = ...`
		// `[a]` in `let [[a]] = ...`
		// `[a]` in `let [[a] = 1] = ...`
		return bindingElement.Name()
	}

	if IsObjectLiteralElement(bindingElement) {
		switch bindingElement.Kind() {
		case KindPropertyAssignment:
			// `b` in `({ a: b } = ...)`
			// `b` in `({ a: b = 1 } = ...)`
			// `{b}` in `({ a: {b} } = ...)`
			// `{b}` in `({ a: {b} = 1 } = ...)`
			// `[b]` in `({ a: [b] } = ...)`
			// `[b]` in `({ a: [b] = 1 } = ...)`
			// `b.c` in `({ a: b.c } = ...)`
			// `b.c` in `({ a: b.c = 1 } = ...)`
			// `b[0]` in `({ a: b[0] } = ...)`
			// `b[0]` in `({ a: b[0] = 1 } = ...)`
			return GetTargetOfBindingOrAssignmentElement(bindingElement.Initializer())
		case KindShorthandPropertyAssignment:
			// `a` in `({ a } = ...)`
			// `a` in `({ a = 1 } = ...)`
			return bindingElement.Name()
		case KindSpreadAssignment:
			// `a` in `({ ...a } = ...)`
			return GetTargetOfBindingOrAssignmentElement(bindingElement.Expression())
		}

		// no target
		return Handle{}
	}

	if IsAssignmentExpression(bindingElement /*excludeCompoundAssignment*/, true) {
		// `a` in `[a = 1] = ...`
		// `{a}` in `[{a} = 1] = ...`
		// `[a]` in `[[a] = 1] = ...`
		// `a.b` in `[a.b = 1] = ...`
		// `a[0]` in `[a[0] = 1] = ...`
		return GetTargetOfBindingOrAssignmentElement(bindingElement.BinaryExpressionLeft())
	}

	if IsSpreadElement(bindingElement) {
		// `a` in `[...a] = ...`
		return GetTargetOfBindingOrAssignmentElement(bindingElement.Expression())
	}

	// `a` in `[a] = ...`
	// `{a}` in `[{a}] = ...`
	// `[a]` in `[[a]] = ...`
	// `a.b` in `[a.b] = ...`
	// `a[0]` in `[a[0]] = ...`
	return bindingElement
}

func TryGetPropertyNameOfBindingOrAssignmentElement(bindingElement Handle) Handle {
	switch bindingElement.Kind() {
	case KindBindingElement:
		// `a` in `let { a: b } = ...`
		// `[a]` in `let { [a]: b } = ...`
		// `"a"` in `let { "a": b } = ...`
		// `1` in `let { 1: b } = ...`
		if !bindingElement.PropertyName().IsNil() {
			propertyName := bindingElement.PropertyName()
			// if IsPrivateIdentifier(propertyName) {
			// 	return Debug.failBadSyntaxKind(propertyName) // !!!
			// }
			if IsComputedPropertyName(propertyName) && IsStringOrNumericLiteralLike(propertyName.Expression()) {
				return propertyName.Expression()
			}
			return propertyName
		}
	case KindPropertyAssignment:
		// `a` in `({ a: b } = ...)`
		// `[a]` in `({ [a]: b } = ...)`
		// `"a"` in `({ "a": b } = ...)`
		// `1` in `({ 1: b } = ...)`
		if !bindingElement.Name().IsNil() {
			propertyName := bindingElement.Name()
			// if IsPrivateIdentifier(propertyName) {
			// 	return Debug.failBadSyntaxKind(propertyName) // !!!
			// }
			if IsComputedPropertyName(propertyName) && IsStringOrNumericLiteralLike(propertyName.Expression()) {
				return propertyName.Expression()
			}
			return propertyName
		}
	case KindSpreadAssignment:
		// `a` in `({ ...a } = ...)`
		// if IsPrivateIdentifier(bindingElement.Name()) {
		// 	return Debug.failBadSyntaxKind(bindingElement.Name()) // !!!
		// }
		return bindingElement.Name()
	}

	target := GetTargetOfBindingOrAssignmentElement(bindingElement)
	if !target.IsNil() && IsPropertyName(target) {
		return target
	}
	return Handle{}
}

/**
 * Walk an AssignmentPattern to determine if it contains object rest (`...`) syntax. We cannot rely on
 * propagation of `TransformFlags.ContainsObjectRestOrSpread` since it isn't propagated by default in
 * ObjectLiteralExpression and ArrayLiteralExpression since we do not know whether they belong to an
 * AssignmentPattern at the time the nodes are parsed.
 */
func ContainsObjectRestOrSpread(node Handle) bool {
	if node.IsNil() {
		return false
	}
	for _, element := range GetElementsOfBindingOrAssignmentPattern(node) {
		if !GetRestIndicatorOfBindingOrAssignmentElement(element).IsNil() &&
			(IsSpreadAssignment(element) || IsObjectBindingPattern(element.Parent()) ||
				IsObjectLiteralExpression(element.Parent())) {
			return true
		}
		target := GetTargetOfBindingOrAssignmentElement(element)
		if !target.IsNil() && IsAssignmentPattern(target) && ContainsObjectRestOrSpread(target) {
			return true
		}
	}
	return false
}

func IsEmptyObjectLiteral(expression Handle) bool {
	return IsObjectLiteralExpression(expression) && len(expression.Properties()) == 0
}

func IsEmptyArrayLiteral(expression Handle) bool {
	return IsArrayLiteralExpression(expression) && len(expression.Elements()) == 0
}

func GetRestIndicatorOfBindingOrAssignmentElement(bindingElement Handle) Handle {
	switch bindingElement.Kind() {
	case KindParameter:
		return bindingElement.ParameterDeclarationDotDotDotToken()
	case KindBindingElement:
		return bindingElement.BindingElementDotDotDotToken()
	case KindSpreadElement, KindSpreadAssignment:
		return bindingElement
	}
	return Handle{}
}

func IsJSDocNameReferenceContext(node Handle) bool {
	return node.Flags()&NodeFlagsJSDoc != 0 && !FindAncestor(node, func(node Handle) bool {
		return IsJSDocNameReference(node) || IsJSDocLinkLike(node)
	}).IsNil()
}

// GetJSDocRoot returns the containing JSDoc node for a node inside a JSDoc comment.
func GetJSDocRoot(node Handle) Handle {
	return FindAncestor(node.Parent(), func(n Handle) bool {
		return n.Kind() == KindJSDoc
	})
}

// GetJSDocHost returns the declaration that the JSDoc comment containing the given node is attached to.
func GetJSDocHost(node Handle) Handle {
	jsDoc := GetJSDocRoot(node)
	if jsDoc.IsNil() {
		return Handle{}
	}
	return jsDoc.Parent()
}

// GetHostSignatureFromJSDoc returns the function-like declaration that hosts the JSDoc comment
// containing the given node. This is used to resolve @link references to parameters.
func GetHostSignatureFromJSDoc(node Handle) Handle {
	host := GetJSDocHost(node)
	if host.IsNil() {
		return Handle{}
	}
	// !!! Strada's getEffectiveJSDocHost applies JS assignment pattern transforms (getSourceOfAssignment, getSourceOfDefaultedAssignment, etc.) not yet ported
	if IsPropertySignatureDeclaration(host) && !host.Type().IsNil() && IsFunctionLike(host.Type()) {
		return host.Type()
	}
	if IsFunctionLike(host) {
		return host
	}
	return Handle{}
}

// Finds the declaration that owns the JSDoc for a function-like node.
// Keep these hosts aligned with JSDoc parameter reparsing so unmatched @param diagnostics use the same attachment rules.
// Keep in sync with getNextJSDocCommentLocation in the API's src/ast/jsdoc.ts
func GetNextJSDocCommentLocation(node Handle) Handle {
	parent := node.Parent()
	if !parent.IsNil() {
		switch parent.Kind() {
		case KindPropertyAssignment, KindExportAssignment, KindPropertyDeclaration, KindVariableDeclaration,
			KindSatisfiesExpression, KindReturnStatement, KindVariableStatement, KindExpressionStatement:
			return parent
		case KindVariableDeclarationList:
			decls := parent.Declarations()
			if len(decls) > 0 && decls[0] == node {
				return parent
			}
		}
	}
	return Handle{}
}

func IsImportOrImportEqualsDeclaration(node Handle) bool {
	return IsImportDeclaration(node) || IsImportEqualsDeclaration(node)
}

func IsPrimitiveLiteralValue(node Handle, includeBigInt bool) bool {
	switch node.Kind() {
	case KindTrueKeyword,
		KindFalseKeyword,
		KindNumericLiteral,
		KindStringLiteral,
		KindNoSubstitutionTemplateLiteral:
		return true
	case KindBigIntLiteral:
		return includeBigInt
	case KindPrefixUnaryExpression:
		if node.PrefixUnaryExpressionOperator() == KindMinusToken {
			return IsNumericLiteral(node.PrefixUnaryExpressionOperand()) || (includeBigInt && IsBigIntLiteral(node.PrefixUnaryExpressionOperand()))
		}
		if node.PrefixUnaryExpressionOperator() == KindPlusToken {
			return IsNumericLiteral(node.PrefixUnaryExpressionOperand())
		}
		return false
	default:
		return false
	}
}

func HasInferredType(node Handle) bool {
	// Debug.type<HasInferredType>(node); // !!!
	switch node.Kind() {
	case KindParameter,
		KindPropertySignature,
		KindPropertyDeclaration,
		KindBindingElement,
		KindPropertyAccessExpression,
		KindElementAccessExpression,
		KindBinaryExpression,
		KindCallExpression,
		KindVariableDeclaration,
		KindExportAssignment,
		KindPropertyAssignment,
		KindShorthandPropertyAssignment,
		KindJSDocParameterTag,
		KindJSDocPropertyTag:
		return true
	default:
		// assertType<never>(node); // !!!
		return false
	}
}

func IsKeyword(token Kind) bool {
	return KindFirstKeyword <= token && token <= KindLastKeyword
}

func IsNonContextualKeyword(token Kind) bool {
	return IsKeyword(token) && !IsContextualKeyword(token)
}

func HasModifier(node Handle, flags ModifierFlags) bool {
	return node.ModifierFlags()&flags != 0
}

func IsExpandoInitializer(declaration Handle, initializer Handle) bool {
	if initializer.IsNil() {
		return false
	}
	if IsFunctionExpressionOrArrowFunction(initializer) {
		return true
	}
	if IsInJSFile(initializer) {
		return IsClassExpression(initializer) || (IsObjectLiteralExpression(initializer) && len(initializer.Properties()) == 0 && declaration.Type().IsNil())
	}
	return false
}

func GetContainingFunction(node Handle) Handle {
	return FindAncestor(node.Parent(), IsFunctionLike)
}

func ImportFromModuleSpecifier(node Handle) Handle {
	if result := TryGetImportFromModuleSpecifier(node); !result.IsNil() {
		return result
	}
	debug.FailBadSyntaxKind(node.Parent())
	return Handle{}
}

func TryGetImportFromModuleSpecifier(node Handle) Handle {
	switch node.Parent().Kind() {
	case KindImportDeclaration, KindJSImportDeclaration, KindExportDeclaration:
		return node.Parent()
	case KindExternalModuleReference:
		return node.Parent().Parent()
	case KindCallExpression:
		if IsImportCall(node.Parent()) || IsRequireCall(node.Parent(), false /*requireStringLiteralLikeArgument*/) {
			return node.Parent()
		}
		return Handle{}
	case KindLiteralType:
		if !IsStringLiteral(node) {
			return Handle{}
		}
		if IsImportTypeNode(node.Parent().Parent()) {
			return node.Parent().Parent()
		}
		return Handle{}
	}
	return Handle{}
}

func IsImplicitlyExportedJSDocDeclaration(node Handle) bool {
	if !IsSourceFile(node.Parent()) || !IsExternalOrCommonJSModule(node.Parent().Store().SourceFile()) {
		return false
	}
	if IsJSTypeAliasDeclaration(node) {
		return true
	}
	// A reparsed ModuleDeclaration synthesized from a JSDoc @typedef/@callback
	// dotted name should also be treated as implicitly exported in modules.
	return IsModuleDeclaration(node) && node.Flags()&NodeFlagsReparsed != 0
}

func HasContextSensitiveParameters(node Handle) bool {
	// Functions with type parameters are not context sensitive.
	if len(node.TypeParameters()) == 0 {
		// Functions with any parameters that lack type annotations are context sensitive.
		if core.Some(node.Parameters(), func(p Handle) bool { return p.Type().IsNil() }) {
			return true
		}
		if !IsArrowFunction(node) {
			// If the first parameter is not an explicit 'this' parameter, then the function has
			// an implicit 'this' parameter which is subject to contextual typing.
			parameter := core.FirstOrNil(node.Parameters())
			if parameter.IsNil() || !IsThisParameter(parameter) {
				return node.Flags()&NodeFlagsContainsThis != 0
			}
		}
	}
	return false
}

func IsInfinityOrNaNString(name string) bool {
	return name == "Infinity" || name == "-Infinity" || name == "NaN"
}

func GetFirstConstructorWithBody(node Handle) Handle {
	for _, member := range node.Members() {
		if IsConstructorDeclaration(member) && NodeIsPresent(member.Body()) {
			return member
		}
	}
	return Handle{}
}

// Returns true for nodes that are considered executable for the purposes of unreachable code detection.
func IsPotentiallyExecutableNode(node Handle) bool {
	if KindFirstStatement <= node.Kind() && node.Kind() <= KindLastStatement {
		if IsVariableStatement(node) {
			declarationList := node.VariableStatementDeclarationList()
			if GetCombinedNodeFlags(declarationList)&NodeFlagsBlockScoped != 0 {
				return true
			}
			declarations := declarationList.Declarations()
			return core.Some(declarations, func(d Handle) bool {
				return !d.Initializer().IsNil()
			})
		}
		return true
	}
	return IsClassDeclaration(node) || IsEnumDeclaration(node) || IsModuleDeclaration(node)
}

func HasAbstractModifier(node Handle) bool {
	return HasSyntacticModifier(node, ModifierFlagsAbstract)
}

func HasAmbientModifier(node Handle) bool {
	return HasSyntacticModifier(node, ModifierFlagsAmbient)
}

func NodeCanBeDecorated(useLegacyDecorators bool, node Handle, parent Handle, grandparent Handle) bool {
	// private names cannot be used with decorators yet
	if useLegacyDecorators && !node.Name().IsNil() && IsPrivateIdentifier(node.Name()) {
		return false
	}
	switch node.Kind() {
	case KindClassDeclaration:
		// class declarations are valid targets
		return true
	case KindClassExpression:
		// class expressions are valid targets for native decorators
		return !useLegacyDecorators
	case KindPropertyDeclaration:
		// property declarations are valid if their parent is a class declaration.
		return !parent.IsNil() && (useLegacyDecorators && IsClassDeclaration(parent) ||
			!useLegacyDecorators && IsClassLike(parent) && !HasAbstractModifier(node) && !HasAmbientModifier(node))
	case KindGetAccessor, KindSetAccessor, KindMethodDeclaration:
		// if this method has a body and its parent is a class declaration, this is a valid target.
		return !parent.IsNil() && !node.Body().IsNil() && (useLegacyDecorators && IsClassDeclaration(parent) ||
			!useLegacyDecorators && IsClassLike(parent))
	case KindParameter:
		// TODO(rbuckton): ParameterDeclaration decorator support for ES decorators must wait until it is standardized
		if !useLegacyDecorators {
			return false
		}
		// if the parameter's parent has a body and its grandparent is a class declaration, this is a valid target.
		return !parent.IsNil() && !parent.Body().IsNil() &&
			(parent.Kind() == KindConstructor || parent.Kind() == KindMethodDeclaration || parent.Kind() == KindSetAccessor) &&
			GetThisParameter(parent) != node && !grandparent.IsNil() && grandparent.Kind() == KindClassDeclaration
	}

	return false
}

func ClassOrConstructorParameterIsDecorated(useLegacyDecorators bool, node Handle) bool {
	if NodeIsDecorated(useLegacyDecorators, node, Handle{}, Handle{}) {
		return true
	}
	constructor := GetFirstConstructorWithBody(node)
	return !constructor.IsNil() && ChildIsDecorated(useLegacyDecorators, constructor, node)
}

func ClassElementOrClassElementParameterIsDecorated(useLegacyDecorators bool, node Handle, parent Handle) bool {
	var parameters ListRef
	if IsAccessor(node) {
		decls := GetAllAccessorDeclarations(parent.Members(), node)
		var firstAccessorWithDecorators Handle
		if HasDecorators(decls.FirstAccessor) {
			firstAccessorWithDecorators = decls.FirstAccessor
		} else if !decls.SecondAccessor.IsNil() && HasDecorators(decls.SecondAccessor) {
			firstAccessorWithDecorators = decls.SecondAccessor
		}
		if firstAccessorWithDecorators.IsNil() || node != firstAccessorWithDecorators {
			return false
		}
		if !decls.SetAccessor.IsNil() {
			parameters = decls.SetAccessor.ParameterList()
		}
	} else if IsMethodDeclaration(node) {
		parameters = node.ParameterList()
	}
	if NodeIsDecorated(useLegacyDecorators, node, parent, Handle{}) {
		return true
	}
	if parameters != 0 && node.Store().ListLen(parameters) > 0 {
		for _, parameter := range node.Store().ListSlice(parameters) {
			if IsThisParameter(parameter) {
				continue
			}
			if NodeIsDecorated(useLegacyDecorators, parameter, node, parent) {
				return true
			}
		}
	}
	return false
}

func NodeIsDecorated(useLegacyDecorators bool, node Handle, parent Handle, grandparent Handle) bool {
	return HasDecorators(node) && NodeCanBeDecorated(useLegacyDecorators, node, parent, grandparent)
}

func NodeOrChildIsDecorated(useLegacyDecorators bool, node Handle, parent Handle, grandparent Handle) bool {
	return NodeIsDecorated(useLegacyDecorators, node, parent, grandparent) || ChildIsDecorated(useLegacyDecorators, node, parent)
}

func ChildIsDecorated(useLegacyDecorators bool, node Handle, parent Handle) bool {
	switch node.Kind() {
	case KindClassDeclaration, KindClassExpression:
		return core.Some(node.Members(), func(m Handle) bool {
			return NodeOrChildIsDecorated(useLegacyDecorators, m, node, parent)
		})
	case KindMethodDeclaration,
		KindSetAccessor,
		KindConstructor:
		return core.Some(node.Parameters(), func(p Handle) bool {
			return NodeIsDecorated(useLegacyDecorators, p, node, parent)
		})
	default:
		return false
	}
}

type AllAccessorDeclarations struct {
	FirstAccessor  Handle
	SecondAccessor Handle
	SetAccessor    Handle
	GetAccessor    Handle
}

func GetAllAccessorDeclarationsForDeclaration(accessor Handle, declarationsOfSymbol []Handle) AllAccessorDeclarations {
	var otherKind Kind
	switch accessor.Kind() {
	case KindSetAccessor:
		otherKind = KindGetAccessor
	case KindGetAccessor:
		otherKind = KindSetAccessor
	default:
		panic(fmt.Sprintf("Unexpected node kind %q", accessor.Kind()))
	}
	var otherAccessor Handle
	for _, d := range declarationsOfSymbol {
		if d.Kind() == otherKind {
			otherAccessor = d
			break
		}
	}

	var firstAccessor Handle
	var secondAccessor Handle
	if !otherAccessor.IsNil() && otherAccessor.Pos() < accessor.Pos() {
		firstAccessor = otherAccessor
		secondAccessor = accessor
	} else {
		firstAccessor = accessor
		secondAccessor = otherAccessor
	}

	var setAccessor Handle
	var getAccessor Handle
	if accessor.Kind() == KindSetAccessor {
		setAccessor = accessor
		getAccessor = otherAccessor
	} else {
		getAccessor = accessor
		setAccessor = otherAccessor
	}

	return AllAccessorDeclarations{
		FirstAccessor:  firstAccessor,
		SecondAccessor: secondAccessor,
		SetAccessor:    setAccessor,
		GetAccessor:    getAccessor,
	}
}

func GetAllAccessorDeclarations(parentDeclarations []Handle, accessor Handle) AllAccessorDeclarations {
	if HasDynamicName(accessor) {
		return GetAllAccessorDeclarationsForDeclaration(accessor, []Handle{accessor})
	}

	accessorName := GetPropertyNameForPropertyNameNode(accessor.Name())
	accessorStatic := IsStatic(accessor)
	var matches []Handle
	for _, member := range parentDeclarations {
		if !IsAccessor(member) || IsStatic(member) != accessorStatic {
			continue
		}
		memberName := GetPropertyNameForPropertyNameNode(member.Name())
		if memberName == accessorName {
			matches = append(matches, member)
		}
	}
	return GetAllAccessorDeclarationsForDeclaration(accessor, matches)
}

func IsAsyncFunction(node Handle) bool {
	switch node.Kind() {
	case KindFunctionDeclaration, KindFunctionExpression, KindArrowFunction, KindMethodDeclaration:
		return !node.Body().IsNil() && node.AsteriskToken().IsNil() && HasSyntacticModifier(node, ModifierFlagsAsync)
	}
	return false
}

/**
 * Gets the most likely element type for a TypeNode. This is not an exhaustive test
 * as it assumes a rest argument can only be an array type (either T[], or Array<T>).
 *
 * @param node The type node.
 *
 * @internal
 */
func GetRestParameterElementType(node Handle) Handle {
	if node.IsNil() {
		return node
	}
	if node.Kind() == KindArrayType {
		return node.ArrayTypeNodeElementType()
	}
	if node.Kind() == KindTypeReference {
		args := node.TypeArguments()
		return core.FirstOrNil(args)
	}
	return Handle{}
}

func TagNamesAreEquivalent(lhs Handle, rhs Handle) bool {
	if lhs.Kind() != rhs.Kind() {
		return false
	}
	switch lhs.Kind() {
	case KindIdentifier:
		return lhs.Text() == rhs.Text()
	case KindThisKeyword:
		return true
	case KindJsxNamespacedName:
		return lhs.JsxNamespacedNameNamespace().Text() == rhs.JsxNamespacedNameNamespace().Text() &&
			lhs.JsxNamespacedNameName().Text() == rhs.JsxNamespacedNameName().Text()
	case KindPropertyAccessExpression:
		return lhs.Name().Text() == rhs.Name().Text() &&
			TagNamesAreEquivalent(lhs.Expression(), rhs.Expression())
	}
	panic("Unhandled case in TagNamesAreEquivalent")
}

func IsTagName(node Handle) bool {
	return !node.Parent().IsNil() && IsJSDocTag(node.Parent()) && node.Parent().TagName() == node
}

// We want to store any numbers/strings if they were a name that could be
// related to a declaration.  So, if we have 'import x = require("something")'
// then we want 'something' to be in the name table.  Similarly, if we have
// "a['propname']" then we want to store "propname" in the name table.
func literalIsName(node *Node) bool {
	return pointerDeclarationName(node) ||
		(node.Parent != nil && node.Parent.Kind == KindExternalModuleReference) ||
		isArgumentOfElementAccessExpression(node) ||
		pointerLiteralComputedPropertyDeclarationName(node)
}

func pointerDeclarationName(name *Node) bool {
	if name == nil || name.Kind == KindSourceFile || pointerBindingPattern(name) || name.Parent == nil {
		return false
	}
	parent := name.Parent
	if parent.Kind == KindTypeParameter {
		return parent.Name() == name
	}
	return parent.DeclarationData() != nil && parent.Name() == name
}

func pointerLiteralComputedPropertyDeclarationName(node *Node) bool {
	return pointerStringOrNumericLiteralLike(node) &&
		node.Parent != nil &&
		node.Parent.Kind == KindComputedPropertyName &&
		node.Parent.Parent != nil &&
		node.Parent.Parent.DeclarationData() != nil
}

func isArgumentOfElementAccessExpression(node *Node) bool {
	return node != nil && node.Parent != nil &&
		node.Parent.Kind == KindElementAccessExpression &&
		node.Parent.AsElementAccessExpression().ArgumentExpression == node
}

// If the given node is part of a subtree of JSDoc nodes that have been cloned into a reparsed construct,
// return the corresponding reparsed clone in the subtree. Otherwise, just return the node.
func GetReparsedNodeForNode(node *Node) *Node {
	if node != nil && node.Flags&NodeFlagsJSDoc != 0 && node.Flags&NodeFlagsReparsed == 0 {
		if file := sourceFileOfPointer(node); file != nil && len(file.ReparsedClones) != 0 {
			pos, found := slices.BinarySearchFunc(file.ReparsedClones, node, CompareNodePositions)
			if !found && pos > 0 {
				pos--
			}
			candidate := file.ReparsedClones[pos]
			if node.Loc.ContainedBy(candidate.Loc) {
				if reparsed := findCloneInNode(candidate, node); reparsed != nil {
					return reparsed
				}
			}
		}
	}
	return node
}

func findCloneInNode(node *Node, original *Node) *Node {
	for {
		if node.Kind == original.Kind && node.Loc == original.Loc {
			return node
		}
		foundContainingChild := node.ForEachChild(func(n *Node) bool {
			if original.Loc.ContainedBy(n.Loc) {
				node = n
				return true
			}
			return false
		})
		if !foundContainingChild {
			return nil
		}
	}
}

func IsExpandoPropertyDeclaration(node Handle) bool {
	return !node.IsNil() && IsBinaryExpression(node)
}

// IsSuperProperty checks if a node is super.x or super[x].
func IsSuperProperty(node Handle) bool {
	return (IsPropertyAccessExpression(node) || IsElementAccessExpression(node)) &&
		node.Expression().Kind() == KindSuperKeyword
}

// Indicates whether a node is a potential source of an assigned name for a class, function, or arrow function.
func IsNamedEvaluationSource(node Handle) bool {
	switch node.Kind() {
	case KindPropertyAssignment:
		return !IsProtoSetter(node.PropertyAssignmentName())
	case KindShorthandPropertyAssignment:
		return !node.ShorthandPropertyAssignmentObjectAssignmentInitializer().IsNil()
	case KindVariableDeclaration:
		return IsIdentifier(node.VariableDeclarationName()) && !node.Initializer().IsNil()
	case KindParameter:
		return IsIdentifier(node.ParameterDeclarationName()) && !node.Initializer().IsNil() && node.ParameterDeclarationDotDotDotToken().IsNil()
	case KindBindingElement:
		return IsIdentifier(node.BindingElementName()) && !node.Initializer().IsNil() && node.BindingElementDotDotDotToken().IsNil()
	case KindPropertyDeclaration:
		return !node.Initializer().IsNil()
	case KindBinaryExpression:
		switch node.BinaryExpressionOperatorToken().Kind() {
		case KindEqualsToken, KindAmpersandAmpersandEqualsToken, KindBarBarEqualsToken, KindQuestionQuestionEqualsToken:
			return IsIdentifier(node.BinaryExpressionLeft())
		}
	case KindExportAssignment:
		return true
	}
	return false
}

// Indicates whether a property name is the special `__proto__` property.
// Per the ECMA-262 spec, this only matters for property assignments whose name is
// the Identifier `__proto__`, or the string literal `"__proto__"`, but not for
// computed property names.
func IsProtoSetter(node Handle) bool {
	return (IsIdentifier(node) || IsStringLiteral(node)) && node.Text() == "__proto__"
}
