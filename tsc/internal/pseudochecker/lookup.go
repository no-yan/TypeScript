package pseudochecker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"slices"
)

func (ch *PseudoChecker) GetReturnTypeOfSignature(signatureNode ast.Handle) *PseudoType {
	switch signatureNode.Kind {
	case ast.KindGetAccessor:
		return ch.GetTypeOfAccessor(signatureNode)
	case ast.KindMethodDeclaration, ast.KindFunctionDeclaration, ast.KindConstructor, ast.KindMethodSignature, ast.KindCallSignature, ast.KindConstructSignature, ast.KindSetAccessor, ast.KindIndexSignature, ast.KindFunctionType, ast.KindConstructorType, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindJSDocSignature:
		return ch.createReturnFromSignature(signatureNode)
	default:
		debug.FailBadSyntaxKind(signatureNode, "Node needs to be an inferrable node")
		return nil
	}
}
func (ch *PseudoChecker) GetTypeOfAccessor(accessor ast.Handle) *PseudoType {
	return ch.typeFromAccessor(accessor)
}
func (ch *PseudoChecker) GetTypeOfExpression(node ast.Handle) *PseudoType {
	return ch.typeFromExpression(node)
}
func (ch *PseudoChecker) GetTypeOfDeclaration(node ast.Handle) *PseudoType {
	switch node.Kind {
	case ast.KindParameter:
		return ch.typeFromParameter(node)
	case ast.KindVariableDeclaration:
		return ch.typeFromVariable(node)
	case ast.KindPropertySignature, ast.KindPropertyDeclaration, ast.KindJSDocPropertyTag:
		return ch.typeFromProperty(node)
	case ast.KindBindingElement:
		return NewPseudoTypeNoResult(node)
	case ast.KindExportAssignment:
		return ch.typeFromExpression(node.ExportAssignmentExpression())
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression, ast.KindBinaryExpression:
		return ch.typeFromExpandoProperty(node)
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment:
		return ch.typeFromPropertyAssignment(node)
	case ast.KindCallExpression:
		switch ast.GetAssignmentDeclarationKind(node) {
		case ast.JSDeclarationKindObjectDefinePropertyValue:
			{
			}
		case ast.JSDeclarationKindObjectDefinePropertyExports:
			{
			}
		}
		return NewPseudoTypeNoResult(node)
	default:
		debug.FailBadSyntaxKind(node, "node needs to be an inferrable node")
		return nil
	}
}
func (ch *PseudoChecker) typeFromPropertyAssignment(node ast.Handle) *PseudoType {
	annotation := node.Type()
	if !annotation.IsNil() {
		return NewPseudoTypeDirect(annotation)
	}
	if node.Kind == ast.KindPropertyAssignment {
		init := node.Initializer()
		if !init.IsNil() {
			expr := ch.typeFromExpression(init)
			if expr != nil && (expr.Kind != PseudoTypeKindInferred || len(expr.AsPseudoTypeInferred().ErrorNodes) > 0) {
				return expr
			}
		}
	}
	return NewPseudoTypeNoResult(node)
}

func (ch *PseudoChecker) typeFromExpandoProperty(node ast.Handle) *PseudoType {
	declaredType := node.Type()
	if !declaredType.IsNil() {
		return NewPseudoTypeDirect(declaredType)
	}
	return NewPseudoTypeNoResult(node)
}
func (ch *PseudoChecker) typeFromProperty(node ast.Handle) *PseudoType {
	t := node.Type()
	if !t.IsNil() {
		return NewPseudoTypeDirect(t)
	}
	if ast.IsPropertyDeclaration(node) {
		init := node.Initializer()
		if !init.IsNil() && !isContextuallyTyped(node) {
			if ast.HasModifier(node, ast.ModifierFlagsReadonly) && ast.IsTemplateExpression(init) {
				return NewPseudoTypeNoResult(node)
			}
			expr := ch.typeFromExpression(init)
			if expr != nil && (expr.Kind != PseudoTypeKindInferred || len(expr.AsPseudoTypeInferred().ErrorNodes) > 0) {
				if expr.Kind != PseudoTypeKindDirect && !node.PropertyDeclarationPostfixToken().IsNil() && node.PropertyDeclarationPostfixToken().Kind == ast.KindQuestionToken {
					return addUndefinedIfDefinitelyRequired(expr)
				}
				return expr
			}
		}
	}
	return NewPseudoTypeNoResult(node)
}
func (ch *PseudoChecker) typeFromVariable(declaration ast.Handle) *PseudoType {
	t := declaration.Type()
	if !t.IsNil() {
		return NewPseudoTypeDirect(t)
	}
	init := declaration.Initializer()
	if !init.IsNil() && declaration.Symbol() != nil && (len(declaration.Symbol().Declarations) == 1 || core.CountWhere(ast.DeclarationNodes(declaration.Symbol()), ast.IsVariableDeclaration) == 1) {
		if !isContextuallyTyped(declaration) {
			if ast.IsVarConst(declaration) && ast.IsTemplateExpression(init) {
				return NewPseudoTypeNoResult(declaration)
			}
			expr := ch.typeFromExpression(init)
			if expr != nil && (expr.Kind != PseudoTypeKindInferred || len(expr.AsPseudoTypeInferred().ErrorNodes) > 0) {
				return expr
			}
		}
	}
	return NewPseudoTypeNoResult(declaration)
}
func (ch *PseudoChecker) typeFromAccessor(accessor ast.Handle) *PseudoType {
	accessorDeclarations := ast.GetAllAccessorDeclarationsForDeclaration(accessor, ast.DeclarationNodes(accessor.Symbol()))
	accessorType := ch.getTypeAnnotationFromAllAccessorDeclarations(accessor, accessorDeclarations)
	if !accessorType.IsNil() && !ast.IsTypePredicateNode(accessorType) {
		return NewPseudoTypeDirect(accessorType)
	}
	if !accessorDeclarations.GetAccessor.IsNil() {
		res := ch.createReturnFromSignature(accessorDeclarations.GetAccessor)
		if res.Kind == PseudoTypeKindInferred && len(res.AsPseudoTypeInferred().ErrorNodes) == 0 {
			errorNodes := []ast.Handle{accessorDeclarations.GetAccessor}
			if !accessorDeclarations.SetAccessor.IsNil() {
				errorNodes = append(errorNodes, accessorDeclarations.SetAccessor)
			}
			res = NewPseudoTypeInferredWithErrors(res.AsPseudoTypeInferred().Expression, res.AsPseudoTypeInferred().IsSignatureReturn, errorNodes)
		}
		return res
	}
	return NewPseudoTypeNoResult(accessor)
}
func (ch *PseudoChecker) getTypeAnnotationFromAllAccessorDeclarations(node ast.Handle, accessors ast.AllAccessorDeclarations) ast.Handle {
	accessorType := ch.getTypeAnnotationFromAccessor(node)
	if accessorType.IsNil() && node != accessors.FirstAccessor {
		accessorType = ch.getTypeAnnotationFromAccessor(accessors.FirstAccessor)
	}
	if accessorType.IsNil() && !accessors.SecondAccessor.IsNil() && node != accessors.SecondAccessor {
		accessorType = ch.getTypeAnnotationFromAccessor(accessors.SecondAccessor)
	}
	return accessorType
}
func (ch *PseudoChecker) getTypeAnnotationFromAccessor(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	if node.Kind == ast.KindGetAccessor {
		return node.GetAccessorDeclarationType()
	}
	set := node
	if len(set.Parameters()) < 1 {
		return ast.Handle{}
	}
	p := set.Parameters()[0]
	if !ast.IsParameterDeclaration(p) {
		return ast.Handle{}
	}
	return p.ParameterDeclarationType()
}
func isValueSignatureDeclaration(node ast.Handle) bool {
	return ast.IsFunctionExpression(node) || ast.IsArrowFunction(node) || ast.IsMethodDeclaration(node) || ast.IsAccessor(node) || ast.IsFunctionDeclaration(node) || ast.IsConstructorDeclaration(node)
}

func (ch *PseudoChecker) createReturnFromSignature(fn ast.Handle) *PseudoType {
	if ast.IsFunctionLike(fn) {
		if r := fn.Type(); !r.IsNil() {
			return NewPseudoTypeDirect(r)
		}
	}
	if isValueSignatureDeclaration(fn) {
		return ch.typeFromSingleReturnExpression(fn)
	}
	return NewPseudoTypeNoResult(fn)
}
func (ch *PseudoChecker) typeFromSingleReturnExpression(fn ast.Handle) *PseudoType {
	var candidateExpr ast.Handle
	if !fn.IsNil() && !ast.NodeIsMissing(fn.Body()) {
		flags := ast.GetFunctionFlags(fn)
		if flags&ast.FunctionFlagsAsyncGenerator != 0 {
			return NewPseudoTypeInferred(fn, true)
		}
		body := fn.Body()
		if ast.IsBlock(body) {
			ast.ForEachReturnStatement(body, func(stmt ast.Handle) bool {
				if stmt.Parent() != body {
					candidateExpr = ast.Handle{}
					return true
				}
				if candidateExpr.IsNil() {
					candidateExpr = stmt.ReturnStatementExpression()
				} else {
					candidateExpr = ast.Handle{}
					return true
				}
				return false
			})
		} else {
			candidateExpr = body
		}
	}
	if !candidateExpr.IsNil() {
		if isContextuallyTyped(candidateExpr) {
			var t ast.Handle
			if candidateExpr.Kind == ast.KindTypeAssertionExpression {
				t = candidateExpr.TypeAssertionType()
			} else if candidateExpr.Kind == ast.KindAsExpression {
				t = candidateExpr.AsExpressionType()
			}
			if !t.IsNil() && !ast.IsConstTypeReference(t) {
				return NewPseudoTypeDirect(t)
			}
		} else {
			return ch.typeFromExpression(candidateExpr)
		}
	}
	return NewPseudoTypeInferred(fn, true)
}

func (ch *PseudoChecker) typeFromExpression(node ast.Handle) *PseudoType {
	switch node.Kind {
	case ast.KindOmittedExpression:
		return PseudoTypeUndefined
	case ast.KindParenthesizedExpression:
		return ch.typeFromExpression(node.ParenthesizedExpressionExpression())
	case ast.KindIdentifier:
		if node.IdentifierText() == "undefined" {
			return PseudoTypeUndefined
		}
	case ast.KindNullKeyword:
		return PseudoTypeNull
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return ch.typeFromFunctionLikeExpression(node)
	case ast.KindTypeAssertionExpression:
		return ch.typeFromTypeAssertion(node.TypeAssertionExpression(), node.TypeAssertionType())
	case ast.KindAsExpression:
		return ch.typeFromTypeAssertion(node.AsExpressionExpression(), node.AsExpressionType())
	case ast.KindPrefixUnaryExpression:
		if ast.IsPrimitiveLiteralValue(node, true) {
			return ch.typeFromPrimitiveLiteralPrefix(node)
		}
	case ast.KindArrayLiteralExpression:
		return ch.typeFromArrayLiteral(node)
	case ast.KindObjectLiteralExpression:
		return ch.typeFromObjectLiteral(node)
	case ast.KindClassExpression:
		return NewPseudoTypeInferredWithErrors(node, false, []ast.Handle{node})
	case ast.KindTemplateExpression:
		if IsInConstContext(node) {
			return NewPseudoTypeInferred(node, false)
		}
		return NewPseudoTypeMaybeConstLocation(node, NewPseudoTypeInferred(node, false), PseudoTypeString)
	case ast.KindNumericLiteral:
		return NewPseudoTypeMaybeConstLocation(node, NewPseudoTypeNumericLiteral(node), PseudoTypeNumber)
	case ast.KindNoSubstitutionTemplateLiteral:
		return NewPseudoTypeMaybeConstLocation(node, NewPseudoTypeStringLiteral(node), PseudoTypeString)
	case ast.KindStringLiteral:
		return NewPseudoTypeMaybeConstLocation(node, NewPseudoTypeStringLiteral(node), PseudoTypeString)
	case ast.KindBigIntLiteral:
		return NewPseudoTypeMaybeConstLocation(node, NewPseudoTypeBigIntLiteral(node), PseudoTypeBigInt)
	case ast.KindTrueKeyword:
		return NewPseudoTypeMaybeConstLocation(node, PseudoTypeTrue, PseudoTypeBoolean)
	case ast.KindFalseKeyword:
		return NewPseudoTypeMaybeConstLocation(node, PseudoTypeFalse, PseudoTypeBoolean)
	}
	return NewPseudoTypeInferred(node, false)
}
func (ch *PseudoChecker) typeFromObjectLiteral(node ast.Handle) *PseudoType {
	if errorNodes := ch.canGetTypeFromObjectLiteral(node); errorNodes != nil {
		return NewPseudoTypeInferredWithErrors(node, false, errorNodes)
	}
	if len(node.Properties()) == 0 {
		return NewPseudoTypeObjectLiteral(nil)
	}
	results := make([]*PseudoObjectElement, 0, len(node.Properties()))
	for _, e := range node.Properties() {
		switch e.Kind {
		case ast.KindMethodDeclaration:
			optional := !e.MethodDeclarationPostfixToken().IsNil() && e.MethodDeclarationPostfixToken().Kind == ast.KindQuestionToken
			if !e.FullSignature().IsNil() {
				results = append(results, NewPseudoPropertyAssignment(false, e.Name(), optional, NewPseudoTypeDirect(e.FullSignature())))
			} else {
				results = append(results, NewPseudoObjectMethod(e, e.Name(), optional, ch.cloneTypeParameters(e.TypeParameters()), ch.cloneParameters(e.Parameters()), ch.createReturnFromSignature(e)))
			}
		case ast.KindPropertyAssignment:
			results = append(results, NewPseudoPropertyAssignment(false, e.Name(), !e.PropertyAssignmentPostfixToken().IsNil() && e.PropertyAssignmentPostfixToken().Kind == ast.KindQuestionToken, ch.typeFromExpression(e.Initializer())))
		case ast.KindSetAccessor, ast.KindGetAccessor:
			member := ch.getAccessorMember(e, e.Name())
			if member != nil {
				results = append(results, member)
			}
		}
	}
	return NewPseudoTypeObjectLiteral(results)
}

func (ch *PseudoChecker) getAccessorMember(accessor ast.Handle, name ast.Handle) *PseudoObjectElement {
	allAccessors := ast.GetAllAccessorDeclarationsForDeclaration(accessor, ast.DeclarationNodes(accessor.Symbol()))
	if !allAccessors.GetAccessor.IsNil() && !allAccessors.GetAccessor.Type().IsNil() && !allAccessors.SetAccessor.IsNil() && len(allAccessors.SetAccessor.Parameters()) > 0 && !allAccessors.SetAccessor.Parameters()[0].ParameterDeclarationType().IsNil() {
		if ast.IsGetAccessorDeclaration(accessor) {
			return NewPseudoGetAccessor(accessor, name, false, ch.typeFromAccessor(accessor))
		} else {
			return NewPseudoSetAccessor(accessor, name, false, ch.cloneParameters(accessor.Parameters())[0])
		}
	}
	if accessor == allAccessors.FirstAccessor {
		accessorType := ch.typeFromAccessor(accessor)
		readonly := ast.IsGetAccessorDeclaration(accessor) && allAccessors.SecondAccessor.IsNil()
		return NewPseudoPropertyAssignment(readonly, name, false, accessorType)
	}
	return nil
}

func (ch *PseudoChecker) canGetTypeFromObjectLiteral(node ast.Handle) []ast.Handle {
	if len(node.Properties()) == 0 {
		return nil
	}
	var errorNodes []ast.Handle
	for _, e := range node.Properties() {
		if e.Flags()&ast.NodeFlagsThisNodeHasError != 0 {
			errorNodes = append(errorNodes, e)
			continue
		}
		if e.Kind == ast.KindShorthandPropertyAssignment || e.Kind == ast.KindSpreadAssignment {
			errorNodes = append(errorNodes, e)
			continue
		}
		if e.Name().Flags()&ast.NodeFlagsThisNodeHasError != 0 {
			errorNodes = append(errorNodes, e.Name())
			continue
		}
		if e.Name().Kind == ast.KindPrivateIdentifier {
			errorNodes = append(errorNodes, e)
			continue
		}
		if e.Name().Kind == ast.KindComputedPropertyName {
			expression := e.Name().Expression()
			if !ast.IsPrimitiveLiteralValue(expression, false) {
				errorNodes = append(errorNodes, e.Name())
			}
		}
	}
	return errorNodes
}
func (ch *PseudoChecker) typeFromArrayLiteral(node ast.Handle) *PseudoType {
	if errorNodes := ch.canGetTypeFromArrayLiteral(node); errorNodes != nil {
		return NewPseudoTypeInferredWithErrors(node, false, errorNodes)
	}
	if IsInConstContext(node) && isContextuallyTyped(node) {
		return NewPseudoTypeInferred(node, false)
	}
	results := make([]*PseudoType, 0, len(node.Elements()))
	for _, e := range node.Elements() {
		results = append(results, ch.typeFromExpression(e))
	}
	return NewPseudoTypeTuple(results)
}

func (ch *PseudoChecker) canGetTypeFromArrayLiteral(node ast.Handle) []ast.Handle {
	if !IsInConstContext(node) {
		return []ast.Handle{node}
	}
	for _, e := range node.Elements() {
		if e.Kind == ast.KindSpreadElement {
			return []ast.Handle{e}
		}
	}
	return nil
}

func isConstContextPropagatingKind(kind ast.Kind) bool {
	switch kind {
	case ast.KindArrayLiteralExpression, ast.KindObjectLiteralExpression, ast.KindParenthesizedExpression, ast.KindSpreadElement, ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindTemplateSpan, ast.KindPrefixUnaryExpression:
		return true
	}
	return false
}

func IsInConstContext(node ast.Handle) bool {
	maybeAssertion := ast.FindAncestor(node.Parent(), func(n ast.Handle) bool {
		return ast.IsAssertionExpression(n) || !isConstContextPropagatingKind(n.Kind)
	})
	return ast.IsConstAssertion(maybeAssertion)
}
func (ch *PseudoChecker) typeFromPrimitiveLiteralPrefix(node ast.Handle) *PseudoType {
	expr := node
	if node.PrefixUnaryExpressionOperator() == ast.KindPlusToken {
		expr = node.PrefixUnaryExpressionOperand()
	}
	inner := node.PrefixUnaryExpressionOperand()
	if inner.Kind == ast.KindBigIntLiteral {
		return NewPseudoTypeMaybeConstLocation(node, NewPseudoTypeBigIntLiteral(expr), PseudoTypeBigInt)
	}
	if inner.Kind == ast.KindNumericLiteral {
		return NewPseudoTypeMaybeConstLocation(node, NewPseudoTypeNumericLiteral(expr), PseudoTypeNumber)
	}
	debug.FailBadSyntaxKind(inner)
	return nil
}
func (ch *PseudoChecker) typeFromTypeAssertion(expression ast.Handle, typeNode ast.Handle) *PseudoType {
	if ast.IsConstTypeReference(typeNode) {
		return ch.typeFromExpression(expression)
	}
	return NewPseudoTypeDirect(typeNode)
}
func (ch *PseudoChecker) typeFromFunctionLikeExpression(node ast.Handle) *PseudoType {
	if !node.FullSignature().IsNil() {
		return NewPseudoTypeDirect(node.FullSignature())
	}
	returnType := ch.createReturnFromSignature(node)
	typeParameters := ch.cloneTypeParameters(node.TypeParameters())
	parameters := ch.cloneParameters(node.Parameters())
	return NewPseudoTypeSingleCallSignature(node, parameters, typeParameters, returnType)
}
func (ch *PseudoChecker) cloneTypeParameters(nodes []ast.Handle) []ast.Handle {
	if len(nodes) == 0 {
		return nil
	}
	return append([]ast.Handle{}, nodes...)
}
func isUndefinedPseudoType(t *PseudoType) bool {
	return t.Kind == PseudoTypeKindUndefined || (t.Kind == PseudoTypeKindMaybeConstLocation && isUndefinedPseudoType(t.AsPseudoTypeMaybeConstLocation().ConstType))
}
func typeNodeCouldReferToUndefined(node ast.Handle) bool {
	for node.Kind == ast.KindParenthesizedType {
		node = node.ParenthesizedTypeNodeType()
	}
	switch node.Kind {
	case ast.KindTypeReference, ast.KindIndexedAccessType, ast.KindTypeQuery, ast.KindOptionalType, ast.KindRestType, ast.KindImportType:
		return true
	case ast.KindIntersectionType:
		return core.Some(node.Store().ListSlice(node.IntersectionTypeNodeTypes()), typeNodeCouldReferToUndefined)
	case ast.KindUnionType:
		return core.Some(node.Store().ListSlice(node.UnionTypeNodeTypes()), typeNodeCouldReferToUndefined)
	case ast.KindConditionalType:
		return true
	case ast.KindTypeOperator:
		return true
	case ast.KindTypePredicate:
		return true
	case ast.KindUndefinedKeyword:
		return true
	default:
		return false
	}
}

func CouldAlreadyReferToUndefinedType(t *PseudoType) bool {
	if t.Kind == PseudoTypeKindNoResult || t.Kind == PseudoTypeKindInferred || isUndefinedPseudoType(t) {
		return true
	}
	if t.Kind == PseudoTypeKindMaybeConstLocation {
		mc := t.AsPseudoTypeMaybeConstLocation()
		return CouldAlreadyReferToUndefinedType(mc.RegularType)
	}
	if t.Kind == PseudoTypeKindDirect {
		node := t.AsPseudoTypeDirect().TypeNode
		return typeNodeCouldReferToUndefined(node)
	}
	if t.Kind == PseudoTypeKindUnion {
		return core.Some(t.AsPseudoTypeUnion().Types, CouldAlreadyReferToUndefinedType)
	}
	return false
}
func isOptionalInitializedOrRestParameter(node ast.Handle) bool {
	p := node
	if !p.DotDotDotToken().IsNil() || !p.Initializer().IsNil() || !p.QuestionToken().IsNil() {
		return true
	}
	return false
}

func lastRequiredParamIndex(params []ast.Handle) int {
	for i := len(params) - 1; i >= 0; i-- {
		if !isOptionalInitializedOrRestParameter(params[i]) {
			return i + 1
		}
	}
	return 0
}
func addUndefinedIfDefinitelyRequired(expr *PseudoType) *PseudoType {
	if CouldAlreadyReferToUndefinedType(expr) {
		return expr
	}
	return NewPseudoTypeUnion([]*PseudoType{expr, PseudoTypeUndefined})
}
func (ch *PseudoChecker) typeFromParameter(node ast.Handle) *PseudoType {
	parent := node.Parent()
	if parent.Kind == ast.KindSetAccessor {
		return ch.GetTypeOfAccessor(parent)
	}
	if node.Initializer().IsNil() {
		if !node.Type().IsNil() {
			return NewPseudoTypeDirect(node.Type())
		}
		return NewPseudoTypeNoResult(node)
	}
	p := parent.Parameters()
	selfIdx := slices.Index(p, node)
	lastRequired := lastRequiredParamIndex(p)
	return ch.typeFromParameterWorker(node, selfIdx, lastRequired)
}
func (ch *PseudoChecker) typeFromParameterWorker(node ast.Handle, selfIdx int, lastRequired int) *PseudoType {
	parent := node.Parent()
	if parent.Kind == ast.KindSetAccessor {
		return ch.GetTypeOfAccessor(parent)
	}
	hasRequiredAfter := selfIdx < lastRequired-1
	declaredType := node.Type()
	if !declaredType.IsNil() {
		result := NewPseudoTypeDirect(declaredType)
		if ch.strictNullChecks && !node.Initializer().IsNil() && hasRequiredAfter {
			return addUndefinedIfDefinitelyRequired(result)
		}
		return result
	}
	if !node.Initializer().IsNil() && ast.IsIdentifier(node.Name()) && !isContextuallyTyped(node) {
		expr := ch.typeFromExpression(node.Initializer())
		if expr != nil && (expr.Kind == PseudoTypeKindInferred && len(expr.AsPseudoTypeInferred().ErrorNodes) == 0) {
			expr = NewPseudoTypeInferredWithErrors(expr.AsPseudoTypeInferred().Expression, false, []ast.Handle{node})
		}
		if !ch.strictNullChecks {
			return expr
		}
		if !hasRequiredAfter {
			return expr
		}
		return addUndefinedIfDefinitelyRequired(expr)
	}
	return NewPseudoTypeNoResult(node)
}
func (ch *PseudoChecker) cloneParameters(nodes []ast.Handle) []*PseudoParameter {
	if len(nodes) == 0 {
		return nil
	}
	lastRequired := lastRequiredParamIndex(nodes)
	result := make([]*PseudoParameter, 0, len(nodes))
	for i, e := range nodes {
		p := e
		optional := !p.QuestionToken().IsNil()
		if !optional && !p.Initializer().IsNil() {
			optional = i >= lastRequired-1
		}
		result = append(result, NewPseudoParameter(!p.DotDotDotToken().IsNil(), e.Name(), optional, ch.typeFromParameterWorker(p, i, lastRequired)))
	}
	return result
}
func isContextuallyTyped(node ast.Handle) bool {
	return !ast.FindAncestor(node.Parent(), func(n ast.Handle) bool {
		if ast.IsCallExpression(n) {
			return true
		}
		if ast.IsSatisfiesExpression(n) {
			return true
		}
		if (ast.IsVariableParameterOrProperty(n) || ast.IsAssertionExpression(n)) && !n.Type().IsNil() && !ast.IsConstAssertion(n) {
			return true
		}
		return ast.IsJsxElement(n) || ast.IsJsxExpression(n)
	}).IsNil()
}
