package store

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// The predicates and helpers of internal/ast/utilities.go (and ast.go) that
// the binder and the module reference collector call on a node's contents,
// ported by hand in the same order and with the same branches. Predicates
// that look only at the kind stay in ast. A function that reads the file
// (IsExternalModule) takes the *File, because a Node cannot reach it.
//
// Differences from the Pointer versions, all forced by the Store: a nil node
// is the sentinel (IsNil), a list is a List, a []*Node is a []NodeRef, the
// module instance state is keyed by NodeRef instead of NodeId, and
// GetModuleInstanceState's ancestor stack holds Nodes.

// nilNode is the sentinel node of the Store that n belongs to.
func (n Node) nilNode() Node { return n.s.node(NoNodeRef) }

// Determines if a node is missing (either the sentinel or empty)
func NodeIsMissing(node Node) bool {
	return node.IsNil() || node.Pos() == node.End() && node.Pos() >= 0 && node.Kind() != ast.KindEndOfFile
}

// Determines if a node is present
func NodeIsPresent(node Node) bool {
	return !NodeIsMissing(node)
}

func NodeKindIs(node Node, kinds ...ast.Kind) bool {
	return slices.Contains(kinds, node.Kind())
}

func IsAssignmentExpression(node Node, excludeCompoundAssignment bool) bool {
	if node.Kind() == ast.KindBinaryExpression {
		expr := node.AsBinaryExpression()
		return (expr.OperatorToken().Kind() == ast.KindEqualsToken || !excludeCompoundAssignment && ast.IsAssignmentOperator(expr.OperatorToken().Kind())) &&
			IsLeftHandSideExpression(expr.Left())
	}
	return false
}

func IsDestructuringAssignment(node Node) bool {
	if IsAssignmentExpression(node, true /*excludeCompoundAssignment*/) {
		kind := node.AsBinaryExpression().Left().Kind()
		return kind == ast.KindObjectLiteralExpression || kind == ast.KindArrayLiteralExpression
	}
	return false
}

func IsBindingPattern(node Node) bool {
	return node.Kind() == ast.KindObjectBindingPattern || node.Kind() == ast.KindArrayBindingPattern
}

// A node is an assignment target if it is on the left hand side of an '=' token, if it is parented by a property
// assignment in an object literal that is an assignment target, or if it is parented by an array literal that is
// an assignment target. Examples include 'a = xxx', '{ p: a } = xxx', '[{ a }] = xxx'.
// (Note that `p` is not a target in the above examples, only `a`.)
func IsAssignmentTarget(node Node) bool {
	return !GetAssignmentTarget(node).IsNil()
}

// Returns the BinaryExpression, PrefixUnaryExpression, PostfixUnaryExpression, or ForInOrOfStatement that references
// the given node as an assignment target
func GetAssignmentTarget(node Node) Node {
	for {
		parent := node.Parent()
		switch parent.Kind() {
		case ast.KindBinaryExpression:
			if ast.IsAssignmentOperator(parent.AsBinaryExpression().OperatorToken().Kind()) && parent.AsBinaryExpression().Left() == node {
				return parent
			}
			return node.nilNode()
		case ast.KindPrefixUnaryExpression:
			if parent.AsPrefixUnaryExpression().Operator() == ast.KindPlusPlusToken || parent.AsPrefixUnaryExpression().Operator() == ast.KindMinusMinusToken {
				return parent
			}
			return node.nilNode()
		case ast.KindPostfixUnaryExpression:
			if parent.AsPostfixUnaryExpression().Operator() == ast.KindPlusPlusToken || parent.AsPostfixUnaryExpression().Operator() == ast.KindMinusMinusToken {
				return parent
			}
			return node.nilNode()
		case ast.KindForInStatement, ast.KindForOfStatement:
			if parent.Initializer() == node {
				return parent
			}
			return node.nilNode()
		case ast.KindParenthesizedExpression, ast.KindArrayLiteralExpression, ast.KindSpreadElement, ast.KindNonNullExpression:
			node = parent
		case ast.KindSpreadAssignment:
			node = parent.Parent()
		case ast.KindShorthandPropertyAssignment:
			if parent.AsShorthandPropertyAssignment().Name() != node {
				return node.nilNode()
			}
			node = parent.Parent()
		case ast.KindPropertyAssignment:
			if parent.AsPropertyAssignment().Name() == node {
				return node.nilNode()
			}
			node = parent.Parent()
		default:
			return node.nilNode()
		}
	}
}

func IsLogicalOrCoalescingBinaryExpression(expr Node) bool {
	return IsBinaryExpression(expr) && ast.IsLogicalOrCoalescingBinaryOperator(expr.AsBinaryExpression().OperatorToken().Kind())
}

func IsLogicalOrCoalescingAssignmentExpression(expr Node) bool {
	return IsBinaryExpression(expr) && ast.IsLogicalOrCoalescingAssignmentOperator(expr.AsBinaryExpression().OperatorToken().Kind())
}

func IsLogicalExpression(node Node) bool {
	for {
		if node.Kind() == ast.KindParenthesizedExpression {
			node = node.Expression()
		} else if node.Kind() == ast.KindPrefixUnaryExpression && node.AsPrefixUnaryExpression().Operator() == ast.KindExclamationToken {
			node = node.AsPrefixUnaryExpression().Operand()
		} else {
			return IsLogicalOrCoalescingBinaryExpression(node)
		}
	}
}

func IsPropertyNameLiteral(node Node) bool {
	switch node.Kind() {
	case ast.KindIdentifier,
		ast.KindStringLiteral,
		ast.KindNoSubstitutionTemplateLiteral,
		ast.KindNumericLiteral:
		return true
	}
	return false
}

// Return true if the given identifier is classified as an IdentifierName by inspecting the parent of the node
func IsIdentifierName(node Node) bool {
	parent := node.Parent()
	switch parent.Kind() {
	case ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindGetAccessor,
		ast.KindSetAccessor, ast.KindEnumMember, ast.KindPropertyAssignment, ast.KindPropertyAccessExpression:
		return parent.Name() == node
	case ast.KindQualifiedName:
		return parent.AsQualifiedName().Right() == node
	case ast.KindBindingElement:
		return parent.PropertyName() == node
	case ast.KindImportSpecifier:
		return parent.PropertyName() == node
	case ast.KindExportSpecifier, ast.KindJsxAttribute, ast.KindJsxSelfClosingElement, ast.KindJsxOpeningElement, ast.KindJsxClosingElement:
		return true
	}
	return false
}

func IsPushOrUnshiftIdentifier(node Node) bool {
	text := node.Text()
	return text == "push" || text == "unshift"
}

func IsBooleanLiteral(node Node) bool {
	return node.Kind() == ast.KindTrueKeyword || node.Kind() == ast.KindFalseKeyword
}

func IsStringLiteralLike(node Node) bool {
	switch node.Kind() {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return true
	}
	return false
}

func IsStringOrNumericLiteralLike(node Node) bool {
	return IsStringLiteralLike(node) || IsNumericLiteral(node)
}

func IsSignedNumericLiteral(node Node) bool {
	if node.Kind() == ast.KindPrefixUnaryExpression {
		node := node.AsPrefixUnaryExpression()
		return (node.Operator() == ast.KindPlusToken || node.Operator() == ast.KindMinusToken) && IsNumericLiteral(node.Operand())
	}
	return false
}

// Determines if a node is part of an OptionalChain
func IsOptionalChain(node Node) bool {
	if node.Flags()&ast.NodeFlagsOptionalChain != 0 {
		switch node.Kind() {
		case ast.KindPropertyAccessExpression,
			ast.KindElementAccessExpression,
			ast.KindCallExpression,
			ast.KindNonNullExpression:
			return true
		}
	}
	return false
}

func getQuestionDotToken(node Node) Node {
	return node.QuestionDotToken()
}

// Determines if node is the root expression of an OptionalChain
func IsOptionalChainRoot(node Node) bool {
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
func IsOutermostOptionalChain(node Node) bool {
	parent := node.Parent()
	return !IsOptionalChain(parent) || // cases 1, 2, and 3
		IsOptionalChainRoot(parent) || // case 4
		node != parent.Expression() // case 5
}

// Determines whether a node is the expression preceding an optional chain (i.e. `a` in `a?.b`).
func IsExpressionOfOptionalChainRoot(node Node) bool {
	parent := node.Parent()
	return IsOptionalChainRoot(parent) && parent.Expression() == node
}

func IsNullishCoalesce(node Node) bool {
	return node.Kind() == ast.KindBinaryExpression && node.AsBinaryExpression().OperatorToken().Kind() == ast.KindQuestionQuestionToken
}

func isLeftHandSideExpressionKind(kind ast.Kind) bool {
	switch kind {
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression, ast.KindNewExpression, ast.KindCallExpression,
		ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment, ast.KindTaggedTemplateExpression, ast.KindArrayLiteralExpression,
		ast.KindParenthesizedExpression, ast.KindObjectLiteralExpression, ast.KindClassExpression, ast.KindFunctionExpression, ast.KindIdentifier,
		ast.KindPrivateIdentifier, ast.KindRegularExpressionLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression, ast.KindFalseKeyword, ast.KindNullKeyword, ast.KindThisKeyword,
		ast.KindTrueKeyword, ast.KindSuperKeyword, ast.KindNonNullExpression, ast.KindExpressionWithTypeArguments, ast.KindMetaProperty,
		ast.KindImportKeyword, ast.KindMissingDeclaration:
		return true
	}
	return false
}

// Determines whether a node is a LeftHandSideExpression based only on its kind.
func IsLeftHandSideExpression(node Node) bool {
	return isLeftHandSideExpressionKind(SkipPartiallyEmittedExpressions(node).Kind())
}

// Determines if a node is a property or element access expression
func IsAccessExpression(node Node) bool {
	return node.Kind() == ast.KindPropertyAccessExpression || node.Kind() == ast.KindElementAccessExpression
}

func isFunctionLikeDeclarationKind(kind ast.Kind) bool {
	switch kind {
	case ast.KindFunctionDeclaration,
		ast.KindMethodDeclaration,
		ast.KindConstructor,
		ast.KindGetAccessor,
		ast.KindSetAccessor,
		ast.KindFunctionExpression,
		ast.KindArrowFunction:
		return true
	}
	return false
}

func IsFunctionLikeKind(kind ast.Kind) bool {
	switch kind {
	case ast.KindMethodSignature,
		ast.KindCallSignature,
		ast.KindJSDocSignature,
		ast.KindConstructSignature,
		ast.KindIndexSignature,
		ast.KindFunctionType,
		ast.KindConstructorType:
		return true
	}
	return isFunctionLikeDeclarationKind(kind)
}

// Determines if a node is function- or signature-like.
func IsFunctionLike(node Node) bool {
	return !node.IsNil() && IsFunctionLikeKind(node.Kind())
}

func IsClassLike(node Node) bool {
	return node.Kind() == ast.KindClassDeclaration || node.Kind() == ast.KindClassExpression
}

func IsClassElement(node Node) bool {
	switch node.Kind() {
	case ast.KindConstructor,
		ast.KindPropertyDeclaration,
		ast.KindMethodDeclaration,
		ast.KindGetAccessor,
		ast.KindSetAccessor,
		ast.KindIndexSignature,
		ast.KindClassStaticBlockDeclaration,
		ast.KindSemicolonClassElement:
		return true
	}
	return false
}

func IsMethodOrAccessor(node Node) bool {
	switch node.Kind() {
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		return true
	}
	return false
}

func IsPrivateIdentifierClassElementDeclaration(node Node) bool {
	return (IsPropertyDeclaration(node) || IsMethodOrAccessor(node)) && IsPrivateIdentifier(node.Name())
}

func IsObjectLiteralOrClassExpressionMethodOrAccessor(node Node) bool {
	kind := node.Kind()
	if kind != ast.KindMethodDeclaration && kind != ast.KindGetAccessor && kind != ast.KindSetAccessor {
		return false
	}
	parentKind := node.Parent().Kind()
	return parentKind == ast.KindObjectLiteralExpression || parentKind == ast.KindClassExpression
}

func IsObjectLiteralMethod(node Node) bool {
	return !node.IsNil() && node.Kind() == ast.KindMethodDeclaration && node.Parent().Kind() == ast.KindObjectLiteralExpression
}

func IsAutoAccessorPropertyDeclaration(node Node) bool {
	return IsPropertyDeclaration(node) && HasAccessorModifier(node)
}

func IsParameterPropertyDeclaration(node Node, parent Node) bool {
	return IsParameterDeclaration(node) && HasSyntacticModifier(node, ast.ModifierFlagsParameterPropertyModifier) && parent.Kind() == ast.KindConstructor
}

func isDeclarationStatementKind(kind ast.Kind) bool {
	switch kind {
	case ast.KindFunctionDeclaration,
		ast.KindMissingDeclaration,
		ast.KindClassDeclaration,
		ast.KindInterfaceDeclaration,
		ast.KindTypeAliasDeclaration,
		ast.KindJSTypeAliasDeclaration,
		ast.KindEnumDeclaration,
		ast.KindModuleDeclaration,
		ast.KindImportDeclaration,
		ast.KindJSImportDeclaration,
		ast.KindImportEqualsDeclaration,
		ast.KindExportDeclaration,
		ast.KindExportAssignment,
		ast.KindNamespaceExportDeclaration:
		return true
	}
	return false
}

// Determines whether a node is a DeclarationStatement. Ideally this does not use Parent pointers, but it may use them
// to rule out a Block node that is part of `try` or `catch` or is the Block-like body of a function.
//
// NOTE: ECMA262 would just call this a Declaration
func IsDeclarationStatement(node Node) bool {
	return isDeclarationStatementKind(node.Kind())
}

func IsBlockOrCatchScoped(declaration Node) bool {
	return GetCombinedNodeFlags(declaration)&ast.NodeFlagsBlockScoped != 0 || IsCatchClauseVariableDeclarationOrBindingElement(declaration)
}

func IsCatchClauseVariableDeclarationOrBindingElement(declaration Node) bool {
	node := GetRootDeclaration(declaration)
	return node.Kind() == ast.KindVariableDeclaration && node.Parent().Kind() == ast.KindCatchClause
}

func IsPrologueDirective(node Node) bool {
	return node.Kind() == ast.KindExpressionStatement &&
		node.Expression().Kind() == ast.KindStringLiteral
}

// Determines whether node is an "outer expression" of the provided kinds. The
// JSDoc type assertion exclusion is not here: the Store has no JSDoc.
func IsOuterExpression(node Node, kinds ast.OuterExpressionKinds) bool {
	switch node.Kind() {
	case ast.KindParenthesizedExpression:
		return kinds&ast.OEKParentheses != 0
	case ast.KindTypeAssertionExpression, ast.KindAsExpression:
		return kinds&ast.OEKTypeAssertions != 0
	case ast.KindSatisfiesExpression:
		return kinds&(ast.OEKExpressionsWithTypeArguments|ast.OEKSatisfies) != 0
	case ast.KindExpressionWithTypeArguments:
		return kinds&ast.OEKExpressionsWithTypeArguments != 0
	case ast.KindNonNullExpression:
		return kinds&ast.OEKNonNullAssertions != 0
	case ast.KindPartiallyEmittedExpression:
		return kinds&ast.OEKPartiallyEmittedExpressions != 0
	case ast.KindBinaryExpression:
		switch node.AsBinaryExpression().OperatorToken().Kind() {
		case ast.KindEqualsToken:
			return kinds&ast.OEKAssignments != 0
		case ast.KindCommaToken:
			return kinds&ast.OEKComma != 0
		}
	}
	return false
}

// Descends into an expression, skipping past "outer expressions" of the provided kinds
func SkipOuterExpressions(node Node, kinds ast.OuterExpressionKinds) Node {
	for IsOuterExpression(node, kinds) {
		if IsBinaryExpression(node) {
			node = node.AsBinaryExpression().Right()
		} else {
			node = node.Expression()
		}
	}
	return node
}

// Skips past the parentheses of an expression
func SkipParentheses(node Node) Node {
	return SkipOuterExpressions(node, ast.OEKParentheses)
}

func SkipPartiallyEmittedExpressions(node Node) Node {
	return SkipOuterExpressions(node, ast.OEKPartiallyEmittedExpressions)
}

// Walks up the parents of a node to find the ancestor that matches the callback
func FindAncestor(node Node, callback func(Node) bool) Node {
	for !node.IsNil() {
		if callback(node) {
			return node
		}
		node = node.Parent()
	}
	return node
}

func HasSyntacticModifier(node Node, flags ast.ModifierFlags) bool {
	return node.ModifierFlags()&flags != 0
}

func HasAccessorModifier(node Node) bool {
	return HasSyntacticModifier(node, ast.ModifierFlagsAccessor)
}

func HasStaticModifier(node Node) bool {
	return HasSyntacticModifier(node, ast.ModifierFlagsStatic)
}

func IsStatic(node Node) bool {
	// https://tc39.es/ecma262/#sec-static-semantics-isstatic
	return IsClassElement(node) && HasStaticModifier(node) || IsClassStaticBlockDeclaration(node)
}

func IsFunctionExpressionOrArrowFunction(node Node) bool {
	return IsFunctionExpression(node) || IsArrowFunction(node)
}

func GetRootDeclaration(node Node) Node {
	for node.Kind() == ast.KindBindingElement {
		node = node.Parent().Parent()
	}
	return node
}

func GetCombinedModifierFlags(node Node) ast.ModifierFlags {
	node = GetRootDeclaration(node)
	flags := node.ModifierFlags()
	if node.Kind() == ast.KindVariableDeclaration {
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == ast.KindVariableDeclarationList {
		flags |= node.ModifierFlags()
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == ast.KindVariableStatement {
		flags |= node.ModifierFlags()
	}
	return flags
}

func GetCombinedNodeFlags(node Node) ast.NodeFlags {
	node = GetRootDeclaration(node)
	flags := node.Flags()
	if node.Kind() == ast.KindVariableDeclaration {
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == ast.KindVariableDeclarationList {
		flags |= node.Flags()
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == ast.KindVariableStatement {
		flags |= node.Flags()
	}
	return flags
}

func IsImportMeta(node Node) bool {
	if node.Kind() == ast.KindMetaProperty {
		return node.AsMetaProperty().KeywordToken() == ast.KindImportKeyword && node.AsMetaProperty().Name().Text() == "meta"
	}
	return false
}

func IsInJSFile(node Node) bool {
	return !node.IsNil() && node.Flags()&ast.NodeFlagsJavaScriptFile != 0
}

func IsLiteralImportTypeNode(node Node) bool {
	return IsImportTypeNode(node) && IsLiteralTypeNode(node.AsImportTypeNode().Argument()) && IsStringLiteral(node.AsImportTypeNode().Argument().AsLiteralTypeNode().Literal())
}

func IsExportsIdentifier(node Node) bool {
	return IsIdentifier(node) && node.Text() == "exports"
}

func IsModuleIdentifier(node Node) bool {
	return IsIdentifier(node) && node.Text() == "module"
}

func IsBindableStaticAccessExpression(node Node, excludeThisKeyword bool) bool {
	return IsPropertyAccessExpression(node) &&
		(!excludeThisKeyword && node.Expression().Kind() == ast.KindThisKeyword || IsIdentifier(node.Name()) && IsBindableStaticNameExpression(node.Expression(), true /*excludeThisKeyword*/)) ||
		IsBindableStaticElementAccessExpression(node, excludeThisKeyword)
}

func IsBindableStaticElementAccessExpression(node Node, excludeThisKeyword bool) bool {
	return IsLiteralLikeElementAccess(node) &&
		((!excludeThisKeyword && node.Expression().Kind() == ast.KindThisKeyword) ||
			IsEntityNameExpression(node.Expression()) ||
			IsBindableStaticAccessExpression(node.Expression(), true /*excludeThisKeyword*/))
}

func IsLiteralLikeElementAccess(node Node) bool {
	return IsElementAccessExpression(node) && IsStringOrNumericLiteralLike(node.AsElementAccessExpression().ArgumentExpression())
}

func IsBindableStaticNameExpression(node Node, excludeThisKeyword bool) bool {
	return IsEntityNameExpression(node) || IsBindableStaticAccessExpression(node, excludeThisKeyword)
}

// Does not handle signed numeric names like `a[+0]` - handling those would require handling prefix unary expressions
// throughout late binding handling as well, which is awkward (but ultimately probably doable if there is demand)
func GetElementOrPropertyAccessName(node Node) Node {
	switch node.Kind() {
	case ast.KindPropertyAccessExpression:
		if name := node.Name(); IsIdentifier(name) {
			return name
		}
		return node.nilNode()
	case ast.KindElementAccessExpression:
		if arg := SkipParentheses(node.AsElementAccessExpression().ArgumentExpression()); IsStringOrNumericLiteralLike(arg) {
			return arg
		}
		return node.nilNode()
	}
	panic("Unhandled case in GetElementOrPropertyAccessName")
}

func GetNameOfDeclaration(declaration Node) Node {
	if declaration.IsNil() {
		return declaration
	}
	nonAssignedName := GetNonAssignedNameOfDeclaration(declaration)
	if !nonAssignedName.IsNil() {
		return nonAssignedName
	}
	if IsFunctionExpression(declaration) || IsArrowFunction(declaration) || IsClassExpression(declaration) {
		return GetAssignedName(declaration)
	}
	return declaration.nilNode()
}

func GetNonAssignedNameOfDeclaration(declaration Node) Node {
	// !!!
	switch declaration.Kind() {
	case ast.KindBinaryExpression, ast.KindCallExpression:
		switch GetAssignmentDeclarationKind(declaration) {
		case ast.JSDeclarationKindProperty, ast.JSDeclarationKindThisProperty, ast.JSDeclarationKindExportsProperty:
			left := declaration.AsBinaryExpression().Left()
			if name := GetElementOrPropertyAccessName(left); !name.IsNil() {
				return name
			}
			return left
		case ast.JSDeclarationKindObjectDefinePropertyValue, ast.JSDeclarationKindObjectDefinePropertyExports:
			return declaration.Arguments().At(1)
		}
		return declaration.nilNode()
	case ast.KindExportAssignment:
		expr := declaration.Expression()
		if IsIdentifier(expr) {
			return expr
		}
		return declaration.nilNode()
	}
	return declaration.Name()
}

func GetAssignedName(node Node) Node {
	parent := node.Parent()
	if !parent.IsNil() {
		switch parent.Kind() {
		case ast.KindPropertyAssignment:
			return parent.AsPropertyAssignment().Name()
		case ast.KindBindingElement:
			return parent.AsBindingElement().Name()
		case ast.KindBinaryExpression:
			if node == parent.AsBinaryExpression().Right() {
				left := parent.AsBinaryExpression().Left()
				switch left.Kind() {
				case ast.KindIdentifier:
					return left
				case ast.KindPropertyAccessExpression:
					return left.AsPropertyAccessExpression().Name()
				case ast.KindElementAccessExpression:
					arg := SkipParentheses(left.AsElementAccessExpression().ArgumentExpression())
					if IsStringOrNumericLiteralLike(arg) {
						return arg
					}
				}
			}
		case ast.KindVariableDeclaration:
			name := parent.AsVariableDeclaration().Name()
			if IsIdentifier(name) {
				return name
			}
		}
	}
	return node.nilNode()
}

func GetAssignmentDeclarationKind(node Node) ast.JSDeclarationKind {
	switch node.Kind() {
	case ast.KindBinaryExpression:
		bin := node.AsBinaryExpression()
		left := bin.Left()
		if bin.OperatorToken().Kind() == ast.KindEqualsToken && IsAccessExpression(left) {
			expression := left.Expression()
			if IsInJSFile(left) {
				if IsModuleExportsAccessExpression(left) && !IsExportsIdentifier(bin.Right()) {
					return ast.JSDeclarationKindModuleExports
				}
				if (IsModuleExportsAccessExpression(expression) || IsExportsIdentifier(expression)) &&
					!GetElementOrPropertyAccessName(left).IsNil() {
					return ast.JSDeclarationKindExportsProperty
				}
				if expression.Kind() == ast.KindThisKeyword {
					return ast.JSDeclarationKindThisProperty
				}
			}
			leftKind := left.Kind()
			if leftKind == ast.KindPropertyAccessExpression && IsEntityNameExpressionEx(expression, IsInJSFile(left)) && IsIdentifier(left.Name()) ||
				leftKind == ast.KindElementAccessExpression && IsEntityNameExpressionEx(expression, IsInJSFile(left)) {
				return ast.JSDeclarationKindProperty
			}
		}
	case ast.KindCallExpression:
		if IsInJSFile(node) && IsBindableObjectDefinePropertyCall(node) {
			entityName := node.Arguments().At(0)
			if IsExportsIdentifier(entityName) || IsModuleExportsAccessExpression(entityName) {
				return ast.JSDeclarationKindObjectDefinePropertyExports
			}
			return ast.JSDeclarationKindObjectDefinePropertyValue
		}
	}
	return ast.JSDeclarationKindNone
}

func IsBindableObjectDefinePropertyCall(node Node) bool {
	if args := node.Arguments(); args.Len() == 3 {
		if expr := node.Expression(); IsPropertyAccessExpression(expr) &&
			IsIdentifier(expr.Expression()) && expr.Expression().AsIdentifier().Text() == "Object" &&
			expr.Name().Text() == "defineProperty" &&
			IsStringOrNumericLiteralLike(args.At(1)) &&
			IsBindableStaticNameExpression(args.At(0) /*excludeThisKeyword*/, true) {
			return true
		}
	}
	return false
}

func HasDynamicName(declaration Node) bool {
	name := GetNameOfDeclaration(declaration)
	return !name.IsNil() && IsDynamicName(name)
}

func IsDynamicName(name Node) bool {
	var expr Node
	switch name.Kind() {
	case ast.KindComputedPropertyName:
		expr = name.Expression()
	case ast.KindElementAccessExpression:
		expr = SkipParentheses(name.AsElementAccessExpression().ArgumentExpression())
	default:
		return false
	}
	return !IsStringOrNumericLiteralLike(expr) && !IsSignedNumericLiteral(expr)
}

func IsEntityNameExpression(node Node) bool {
	return IsEntityNameExpressionEx(node, false /*allowJS*/)
}

func IsEntityNameExpressionEx(node Node, allowJS bool) bool {
	return IsIdentifier(node) ||
		IsPropertyAccessEntityNameExpression(node, allowJS) ||
		allowJS && (node.Kind() == ast.KindThisKeyword || isElementAccessEntityNameExpression(node, allowJS))
}

func IsPropertyAccessEntityNameExpression(node Node, allowJS bool) bool {
	return IsPropertyAccessExpression(node) && IsIdentifier(node.Name()) && IsEntityNameExpressionEx(node.Expression(), allowJS)
}

func isElementAccessEntityNameExpression(node Node, allowJS bool) bool {
	return IsElementAccessExpression(node) && IsStringOrNumericLiteralLike(node.AsElementAccessExpression().ArgumentExpression()) && IsEntityNameExpressionEx(node.Expression(), allowJS)
}

func IsDottedName(node Node) bool {
	switch node.Kind() {
	case ast.KindIdentifier, ast.KindThisKeyword, ast.KindSuperKeyword, ast.KindMetaProperty:
		return true
	case ast.KindPropertyAccessExpression, ast.KindParenthesizedExpression:
		return IsDottedName(node.Expression())
	}
	return false
}

func IsAmbientModule(node Node) bool {
	return IsModuleDeclaration(node) && (node.AsModuleDeclaration().Name().Kind() == ast.KindStringLiteral || IsGlobalScopeAugmentation(node))
}

func IsGlobalScopeAugmentation(node Node) bool {
	return IsModuleDeclaration(node) && node.AsModuleDeclaration().Keyword() == ast.KindGlobalKeyword
}

// IsModuleAugmentationExternal takes the file, because the Pointer version
// reads it off the root node (node.Parent.AsSourceFile()).
func IsModuleAugmentationExternal(file *File, node Node) bool {
	// external module augmentation is a ambient module declaration that is either:
	// - defined in the top level scope and source file is an external module
	// - defined inside ambient module declaration located in the top level scope and source file not an external module
	parent := node.Parent()
	switch parent.Kind() {
	case ast.KindSourceFile:
		return file.IsExternalModule()
	case ast.KindModuleBlock:
		grandParent := parent.Parent()
		return IsAmbientModule(grandParent) && IsSourceFile(grandParent.Parent()) && !file.IsExternalModule()
	}
	return false
}

func GetContainingClass(node Node) Node {
	return FindAncestor(node.Parent(), IsClassLike)
}

func IsPartOfTypeQuery(node Node) bool {
	for node.Kind() == ast.KindQualifiedName || node.Kind() == ast.KindIdentifier {
		node = node.Parent()
	}
	return node.Kind() == ast.KindTypeQuery
}

func IsPartOfParameterDeclaration(node Node) bool {
	return GetRootDeclaration(node).Kind() == ast.KindParameter
}

func IsInTopLevelContext(node Node) bool {
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

func GetThisContainer(node Node, includeArrowFunctions bool, includeClassComputedPropertyName bool) Node {
	for {
		node = node.Parent()
		if node.IsNil() {
			panic("nil parent in getThisContainer")
		}
		switch node.Kind() {
		case ast.KindComputedPropertyName:
			if includeClassComputedPropertyName && IsClassLike(node.Parent().Parent()) {
				return node
			}
			node = node.Parent().Parent()
		case ast.KindDecorator:
			if node.Parent().Kind() == ast.KindParameter && IsClassElement(node.Parent().Parent()) {
				// If the decorator's parent is a ParameterDeclaration, we resolve the this container from
				// the grandparent class declaration.
				node = node.Parent().Parent()
			} else if IsClassElement(node.Parent()) {
				// If the decorator's parent is a class element, we resolve the 'this' container
				// from the parent class declaration.
				node = node.Parent()
			}
		case ast.KindArrowFunction:
			if includeArrowFunctions {
				return node
			}
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindModuleDeclaration, ast.KindClassStaticBlockDeclaration,
			ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor,
			ast.KindGetAccessor, ast.KindSetAccessor, ast.KindCallSignature, ast.KindConstructSignature, ast.KindIndexSignature,
			ast.KindEnumDeclaration, ast.KindSourceFile:
			return node
		}
	}
}

func GetImmediatelyInvokedFunctionExpression(fn Node) Node {
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
	return fn.nilNode()
}

func IsEnumConst(node Node) bool {
	return GetCombinedModifierFlags(node)&ast.ModifierFlagsConst != 0
}

func ExpressionIsAlias(node Node) bool {
	return IsEntityNameExpression(node) || IsClassExpression(node)
}

func IsAnyImportOrReExport(node Node) bool {
	return IsImportNode(node) || IsExportDeclaration(node)
}

func IsImportNode(node Node) bool {
	return IsAnyImportSyntax(node) || NodeKindIs(node, ast.KindJSImportDeclaration)
}

// Checks if the node is a genuine import declation. In particular the re-parsed KindJSImportDeclaration
// is explicitly excluded because the callers of this function are typically not prepared to handle it properly.
// For more permissive check, use IsImportNode.
func IsAnyImportSyntax(node Node) bool {
	return NodeKindIs(node, ast.KindImportDeclaration, ast.KindImportEqualsDeclaration)
}

func IsJsonSourceFile(file *File) bool {
	return file.ScriptKind == core.ScriptKindJSON
}

func GetExternalModuleName(node Node) Node {
	switch node.Kind() {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration:
		return node.ModuleSpecifier()
	case ast.KindImportEqualsDeclaration:
		if node.AsImportEqualsDeclaration().ModuleReference().Kind() == ast.KindExternalModuleReference {
			return node.AsImportEqualsDeclaration().ModuleReference().Expression()
		}
		return node.nilNode()
	case ast.KindImportType:
		return getImportTypeNodeLiteral(node)
	case ast.KindCallExpression:
		if args := node.Arguments(); args.Len() > 0 {
			return args.At(0)
		}
		return node.nilNode()
	case ast.KindModuleDeclaration:
		if IsStringLiteral(node.AsModuleDeclaration().Name()) {
			return node.AsModuleDeclaration().Name()
		}
		return node.nilNode()
	}
	panic("Unhandled case in getExternalModuleName")
}

func getImportTypeNodeLiteral(node Node) Node {
	if IsImportTypeNode(node) {
		importTypeNode := node.AsImportTypeNode()
		if IsLiteralTypeNode(importTypeNode.Argument()) {
			literalTypeNode := importTypeNode.Argument().AsLiteralTypeNode()
			if IsStringLiteral(literalTypeNode.Literal()) {
				return literalTypeNode.Literal()
			}
		}
	}
	return node.nilNode()
}

func IsImportCall(node Node) bool {
	if !IsCallExpression(node) {
		return false
	}
	e := node.Expression()
	return e.Kind() == ast.KindImportKeyword || IsMetaProperty(e) && e.AsMetaProperty().KeywordToken() == ast.KindImportKeyword && e.Text() == "defer"
}

// Push a virtual parent pointer onto `ancestors` and return it.
func pushAncestor(ancestors []Node, parent Node) []Node {
	return append(ancestors, parent)
}

// If a virtual `Parent` exists on the stack, returns the previous stack entry and the virtual `Parent“.
// Otherwise, we return `nil` and the value of `node.Parent`.
func popAncestor(ancestors []Node, node Node) ([]Node, Node) {
	if len(ancestors) == 0 {
		return nil, node.Parent()
	}
	n := len(ancestors) - 1
	return ancestors[:n], ancestors[n]
}

func GetModuleInstanceState(node Node) ast.ModuleInstanceState {
	return getModuleInstanceState(node, nil, nil)
}

func getModuleInstanceState(node Node, ancestors []Node, visited map[NodeRef]ast.ModuleInstanceState) ast.ModuleInstanceState {
	module := node.AsModuleDeclaration()
	if !module.Body().IsNil() {
		return getModuleInstanceStateCached(module.Body(), pushAncestor(ancestors, node), visited)
	} else {
		return ast.ModuleInstanceStateInstantiated
	}
}

func getModuleInstanceStateCached(node Node, ancestors []Node, visited map[NodeRef]ast.ModuleInstanceState) ast.ModuleInstanceState {
	if visited == nil {
		visited = make(map[NodeRef]ast.ModuleInstanceState)
	}
	nodeId := node.Ref()
	if cached, ok := visited[nodeId]; ok {
		if cached != ast.ModuleInstanceStateUnknown {
			return cached
		}
		return ast.ModuleInstanceStateNonInstantiated
	}
	visited[nodeId] = ast.ModuleInstanceStateUnknown
	result := getModuleInstanceStateWorker(node, ancestors, visited)
	visited[nodeId] = result
	return result
}

func getModuleInstanceStateWorker(node Node, ancestors []Node, visited map[NodeRef]ast.ModuleInstanceState) ast.ModuleInstanceState {
	// A module is uninstantiated if it contains only
	switch node.Kind() {
	case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration:
		return ast.ModuleInstanceStateNonInstantiated
	case ast.KindEnumDeclaration:
		if IsEnumConst(node) {
			return ast.ModuleInstanceStateConstEnumOnly
		}
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindImportEqualsDeclaration:
		if !HasSyntacticModifier(node, ast.ModifierFlagsExport) {
			return ast.ModuleInstanceStateNonInstantiated
		}
	case ast.KindExportDeclaration:
		decl := node.AsExportDeclaration()
		exportClause := decl.ExportClause()
		if decl.ModuleSpecifier().IsNil() && !exportClause.IsNil() && exportClause.Kind() == ast.KindNamedExports {
			state := ast.ModuleInstanceStateNonInstantiated
			ancestors = pushAncestor(ancestors, node)
			ancestors = pushAncestor(ancestors, exportClause)
			for _, specifier := range exportClause.Elements().Refs() {
				specifierState := getModuleInstanceStateForAliasTarget(node.s.node(specifier), ancestors, visited)
				if specifierState > state {
					state = specifierState
				}
				if state == ast.ModuleInstanceStateInstantiated {
					return state
				}
			}
			return state
		}
	case ast.KindModuleBlock:
		state := ast.ModuleInstanceStateNonInstantiated
		ancestors = pushAncestor(ancestors, node)
		node.ForEachChild(func(n Node) bool {
			childState := getModuleInstanceStateCached(n, ancestors, visited)
			switch childState {
			case ast.ModuleInstanceStateNonInstantiated:
				return false
			case ast.ModuleInstanceStateConstEnumOnly:
				state = ast.ModuleInstanceStateConstEnumOnly
				return false
			case ast.ModuleInstanceStateInstantiated:
				state = ast.ModuleInstanceStateInstantiated
				return true
			}
			panic("Unhandled case in getModuleInstanceStateWorker")
		})
		return state
	case ast.KindModuleDeclaration:
		return getModuleInstanceState(node, ancestors, visited)
	}
	return ast.ModuleInstanceStateInstantiated
}

func getModuleInstanceStateForAliasTarget(node Node, ancestors []Node, visited map[NodeRef]ast.ModuleInstanceState) ast.ModuleInstanceState {
	name := node.PropertyName()
	if name.IsNil() {
		name = node.Name()
	}
	if name.Kind() != ast.KindIdentifier {
		// Skip for invalid syntax like this: export { "x" }
		return ast.ModuleInstanceStateInstantiated
	}
	for ancestors, p := popAncestor(ancestors, node); !p.IsNil(); ancestors, p = popAncestor(ancestors, p) {
		if IsBlock(p) || IsModuleBlock(p) || IsSourceFile(p) {
			found := ast.ModuleInstanceStateUnknown
			statementsAncestors := pushAncestor(ancestors, p)
			for _, statementRef := range p.Statements().Refs() {
				statement := p.s.node(statementRef)
				if NodeHasName(statement, name) {
					state := getModuleInstanceStateCached(statement, statementsAncestors, visited)
					if found == ast.ModuleInstanceStateUnknown || state > found {
						found = state
					}
					if found == ast.ModuleInstanceStateInstantiated {
						return found
					}
					if statement.Kind() == ast.KindImportEqualsDeclaration {
						// Treat re-exports of import aliases as instantiated since they're ambiguous. This is consistent
						// with `export import x = mod.x` being treated as instantiated:
						//   import x = mod.x;
						//   export { x };
						found = ast.ModuleInstanceStateInstantiated
					}
				}
			}
			if found != ast.ModuleInstanceStateUnknown {
				return found
			}
		}
	}
	// Couldn't locate, assume could refer to a value
	return ast.ModuleInstanceStateInstantiated
}

func NodeHasName(statement Node, id Node) bool {
	name := statement.Name()
	if !name.IsNil() {
		return IsIdentifier(name) && name.Text() == id.Text()
	}
	if IsVariableStatement(statement) {
		for _, d := range statement.AsVariableStatement().DeclarationList().AsVariableDeclarationList().Declarations().Refs() {
			if NodeHasName(statement.s.node(d), id) {
				return true
			}
		}
	}
	return false
}

func ModuleExportNameIsDefault(node Node) bool {
	return node.Text() == ast.InternalSymbolNameDefault
}

// Returns true if the node is a CallExpression to the identifier 'require' with
// exactly one argument (of the form 'require("name")').
// This function does not test if the node is in a JavaScript file or not.
func IsRequireCall(node Node, requireStringLiteralLikeArgument bool) bool {
	if !IsCallExpression(node) {
		return false
	}
	call := node.AsCallExpression()
	expression := call.Expression()
	if !IsIdentifier(expression) || expression.AsIdentifier().Text() != "require" {
		return false
	}
	arguments := call.Arguments()
	if arguments.Len() != 1 {
		return false
	}
	return !requireStringLiteralLikeArgument || IsStringLiteralLike(arguments.At(0))
}

// Of the form: `const x = require("x")` or `const { x } = require("x")` or with `var` or `let`
// The variable must not be exported and must not have a type annotation, even a jsdoc one.
// The initializer must be a call to `require` with a string literal or a string literal-like argument.
func IsVariableDeclarationInitializedToRequire(node Node) bool {
	if node.Kind() == ast.KindBindingElement {
		node = node.Parent().Parent()
	}
	return isVariableDeclarationInitializedWithRequireHelper(node, false /*allowAccessedRequire*/)
}

// allowAccessedRequire is always false here: the Pointer's GetLeftmostAccessExpression path is not ported.
func isVariableDeclarationInitializedWithRequireHelper(node Node, allowAccessedRequire bool) bool {
	if !IsInJSFile(node) {
		return false
	}
	if node.Kind() != ast.KindVariableDeclaration {
		return false
	}
	initializer := node.Initializer()
	if initializer.IsNil() {
		return false
	}
	if allowAccessedRequire {
		panic("store: allowAccessedRequire is not ported")
	}

	return node.Parent().Parent().ModifierFlags()&ast.ModifierFlagsExport == 0 &&
		node.Type().IsNil() &&
		IsRequireCall(initializer, true /*requireStringLiteralLikeArgument*/)
}

func IsModuleExportsAccessExpression(node Node) bool {
	if IsAccessExpression(node) && IsModuleIdentifier(node.Expression()) {
		if name := GetElementOrPropertyAccessName(node); !name.IsNil() {
			return name.Text() == "exports"
		}
	}
	return false
}

func IsJsxOpeningLikeElement(node Node) bool {
	return IsJsxOpeningElement(node) || IsJsxSelfClosingElement(node)
}

func IsExpandoInitializer(declaration Node, initializer Node) bool {
	if initializer.IsNil() {
		return false
	}
	if IsFunctionExpressionOrArrowFunction(initializer) {
		return true
	}
	if IsInJSFile(initializer) {
		return IsClassExpression(initializer) || (IsObjectLiteralExpression(initializer) && initializer.Properties().Len() == 0 && declaration.Type().IsNil())
	}
	return false
}

// IsImplicitlyExportedJSDocDeclaration takes the file, because the Pointer
// version reads it off the root node (node.Parent.AsSourceFile()).
func IsImplicitlyExportedJSDocDeclaration(file *File, node Node) bool {
	if !IsSourceFile(node.Parent()) || !file.IsExternalOrCommonJSModule() {
		return false
	}
	if IsJSTypeAliasDeclaration(node) {
		return true
	}
	// A reparsed ModuleDeclaration synthesized from a JSDoc @typedef/@callback
	// dotted name should also be treated as implicitly exported in modules.
	return IsModuleDeclaration(node) && node.Flags()&ast.NodeFlagsReparsed != 0
}

// Returns true for nodes that are considered executable for the purposes of unreachable code detection.
func IsPotentiallyExecutableNode(node Node) bool {
	kind := node.Kind()
	if ast.KindFirstStatement <= kind && kind <= ast.KindLastStatement {
		if IsVariableStatement(node) {
			declarationList := node.AsVariableStatement().DeclarationList()
			if GetCombinedNodeFlags(declarationList)&ast.NodeFlagsBlockScoped != 0 {
				return true
			}
			for _, d := range declarationList.AsVariableDeclarationList().Declarations().Refs() {
				if !node.s.node(d).Initializer().IsNil() {
					return true
				}
			}
			return false
		}
		return true
	}
	return IsClassDeclaration(node) || IsEnumDeclaration(node) || IsModuleDeclaration(node)
}

func IsAsyncFunction(node Node) bool {
	switch node.Kind() {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration:
		return !node.Body().IsNil() && node.AsteriskToken().IsNil() && HasSyntacticModifier(node, ast.ModifierFlagsAsync)
	}
	return false
}

// IsLocalsContainer is ast.IsLocalsContainer: the kind has a locals slot.
func IsLocalsContainer(node Node) bool {
	_, ok := node.LocalsSlot()
	return ok
}

// The one-line kind predicates that the binder and the reference collector
// call on a node (ast_generated.go's IsXxx), in Kind order.

func IsIdentifier(node Node) bool             { return node.Kind() == ast.KindIdentifier }
func IsPrivateIdentifier(node Node) bool      { return node.Kind() == ast.KindPrivateIdentifier }
func IsNumericLiteral(node Node) bool         { return node.Kind() == ast.KindNumericLiteral }
func IsStringLiteral(node Node) bool          { return node.Kind() == ast.KindStringLiteral }
func IsComputedPropertyName(node Node) bool   { return node.Kind() == ast.KindComputedPropertyName }
func IsParameterDeclaration(node Node) bool   { return node.Kind() == ast.KindParameter }
func IsPropertyDeclaration(node Node) bool    { return node.Kind() == ast.KindPropertyDeclaration }
func IsConstructorDeclaration(node Node) bool { return node.Kind() == ast.KindConstructor }
func IsClassStaticBlockDeclaration(node Node) bool {
	return node.Kind() == ast.KindClassStaticBlockDeclaration
}
func IsTypeQueryNode(node Node) bool           { return node.Kind() == ast.KindTypeQuery }
func IsLiteralTypeNode(node Node) bool         { return node.Kind() == ast.KindLiteralType }
func IsImportTypeNode(node Node) bool          { return node.Kind() == ast.KindImportType }
func IsObjectLiteralExpression(node Node) bool { return node.Kind() == ast.KindObjectLiteralExpression }
func IsPropertyAccessExpression(node Node) bool {
	return node.Kind() == ast.KindPropertyAccessExpression
}
func IsElementAccessExpression(node Node) bool { return node.Kind() == ast.KindElementAccessExpression }
func IsCallExpression(node Node) bool          { return node.Kind() == ast.KindCallExpression }
func IsParenthesizedExpression(node Node) bool { return node.Kind() == ast.KindParenthesizedExpression }
func IsFunctionExpression(node Node) bool      { return node.Kind() == ast.KindFunctionExpression }
func IsArrowFunction(node Node) bool           { return node.Kind() == ast.KindArrowFunction }
func IsTypeOfExpression(node Node) bool        { return node.Kind() == ast.KindTypeOfExpression }
func IsPrefixUnaryExpression(node Node) bool   { return node.Kind() == ast.KindPrefixUnaryExpression }
func IsBinaryExpression(node Node) bool        { return node.Kind() == ast.KindBinaryExpression }
func IsConditionalTypeNode(node Node) bool     { return node.Kind() == ast.KindConditionalType }
func IsClassExpression(node Node) bool         { return node.Kind() == ast.KindClassExpression }
func IsNonNullExpression(node Node) bool       { return node.Kind() == ast.KindNonNullExpression }
func IsMetaProperty(node Node) bool            { return node.Kind() == ast.KindMetaProperty }
func IsBlock(node Node) bool                   { return node.Kind() == ast.KindBlock }
func IsVariableStatement(node Node) bool       { return node.Kind() == ast.KindVariableStatement }
func IsVariableDeclaration(node Node) bool     { return node.Kind() == ast.KindVariableDeclaration }
func IsFunctionDeclaration(node Node) bool     { return node.Kind() == ast.KindFunctionDeclaration }
func IsClassDeclaration(node Node) bool        { return node.Kind() == ast.KindClassDeclaration }
func IsTypeAliasDeclaration(node Node) bool    { return node.Kind() == ast.KindTypeAliasDeclaration }
func IsJSTypeAliasDeclaration(node Node) bool  { return node.Kind() == ast.KindJSTypeAliasDeclaration }
func IsEnumDeclaration(node Node) bool         { return node.Kind() == ast.KindEnumDeclaration }
func IsModuleDeclaration(node Node) bool       { return node.Kind() == ast.KindModuleDeclaration }
func IsModuleBlock(node Node) bool             { return node.Kind() == ast.KindModuleBlock }
func IsImportEqualsDeclaration(node Node) bool { return node.Kind() == ast.KindImportEqualsDeclaration }
func IsImportDeclaration(node Node) bool       { return node.Kind() == ast.KindImportDeclaration }
func IsNamespaceExport(node Node) bool         { return node.Kind() == ast.KindNamespaceExport }
func IsExportDeclaration(node Node) bool       { return node.Kind() == ast.KindExportDeclaration }
func IsExportAssignment(node Node) bool        { return node.Kind() == ast.KindExportAssignment }
func IsExportSpecifier(node Node) bool         { return node.Kind() == ast.KindExportSpecifier }
func IsExternalModuleReference(node Node) bool { return node.Kind() == ast.KindExternalModuleReference }
func IsJsxSelfClosingElement(node Node) bool   { return node.Kind() == ast.KindJsxSelfClosingElement }
func IsJsxOpeningElement(node Node) bool       { return node.Kind() == ast.KindJsxOpeningElement }
func IsJsxFragment(node Node) bool             { return node.Kind() == ast.KindJsxFragment }
func IsJsxNamespacedName(node Node) bool       { return node.Kind() == ast.KindJsxNamespacedName }
func IsSourceFile(node Node) bool              { return node.Kind() == ast.KindSourceFile }

// IsForInOrOfStatement is the kind predicate of ast.IsForInOrOfStatement.
func IsForInOrOfStatement(node Node) bool {
	return node.Kind() == ast.KindForInStatement || node.Kind() == ast.KindForOfStatement
}
