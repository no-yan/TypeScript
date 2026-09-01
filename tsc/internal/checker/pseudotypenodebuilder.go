package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/pseudochecker"
)

func (b *NodeBuilderImpl) pseudoTypeToNodeWithCheckerFallback(t *pseudochecker.PseudoType, checkerType *Type) ast.Handle {
	if t.Kind == pseudochecker.PseudoTypeKindInferred {
		if !b.ctx.suppressReportInferenceFallback {
			if errorNodes := t.AsPseudoTypeInferred().ErrorNodes; len(errorNodes) > 0 {
				for _, n := range errorNodes {
					b.ctx.tracker.ReportInferenceFallback(n)
				}
			} else {
				b.ctx.tracker.ReportInferenceFallback(t.AsPseudoTypeInferred().Expression)
			}
		}
		oldSuppress := b.ctx.suppressReportInferenceFallback
		b.ctx.suppressReportInferenceFallback = true
		result := b.typeToTypeNode(checkerType)
		b.ctx.suppressReportInferenceFallback = oldSuppress
		return result
	} else if t.Kind == pseudochecker.PseudoTypeKindDirect {
		existing := t.AsPseudoTypeDirect().TypeNode
		if !b.canReuseExistingJSTypeNode(existing, checkerType) {
			if !b.ctx.suppressReportInferenceFallback {
				b.ctx.tracker.ReportInferenceFallback(existing)
			}
			oldSuppress := b.ctx.suppressReportInferenceFallback
			b.ctx.suppressReportInferenceFallback = true
			result := b.typeToTypeNode(checkerType)
			b.ctx.suppressReportInferenceFallback = oldSuppress
			return result
		}
	}
	return b.pseudoTypeToNode(t)
}

func (b *NodeBuilderImpl) pseudoTypeToNode(t *pseudochecker.PseudoType) ast.Handle {
	debug.Assert(t != nil, "Attempted to serialize nil pseudotype")
	switch t.Kind {
	case pseudochecker.PseudoTypeKindDirect:
		return b.reuseTypeNode(t.AsPseudoTypeDirect().TypeNode)
	case pseudochecker.PseudoTypeKindInferred:
		inferred := t.AsPseudoTypeInferred()
		node := inferred.Expression
		if errorNodes := inferred.ErrorNodes; len(errorNodes) > 0 {
			for _, n := range errorNodes {
				b.ctx.tracker.ReportInferenceFallback(n)
			}
		} else if ast.IsEntityNameExpression(node) && ast.IsDeclaration(node.Parent()) {
			b.ctx.tracker.ReportInferenceFallback(node.Parent())
		} else {
			b.ctx.tracker.ReportInferenceFallback(node)
		}
		if inferred.IsSignatureReturn {
			return b.serializeReturnTypeForSignature(b.ch.getSignatureFromDeclaration(node), false)
		}
		if ast.IsReturnStatement(node.Parent()) {
			enclosing := ast.GetContainingFunction(node)
			if ast.IsAccessor(enclosing) {
				return b.serializeTypeForDeclaration(enclosing, nil, nil, false)
			}
			return b.serializeReturnTypeForSignature(b.ch.getSignatureFromDeclaration(enclosing), false)
		}
		if ast.IsArrowFunction(node.Parent()) && node.Parent().ArrowFunctionBody() == node {
			return b.serializeReturnTypeForSignature(b.ch.getSignatureFromDeclaration(node.Parent()), false)
		}
		if ast.IsDeclaration(node.Parent()) {
			return b.serializeTypeForDeclaration(node.Parent(), nil, nil, false)
		}
		ty := b.ch.getTypeOfExpression(node)
		return b.typeToTypeNode(ty)
	case pseudochecker.PseudoTypeKindNoResult:
		node := t.AsPseudoTypeNoResult().Declaration
		b.ctx.tracker.ReportInferenceFallback(node)
		if ast.IsFunctionLike(node) && !ast.IsAccessor(node) {
			return b.serializeReturnTypeForSignature(b.ch.getSignatureFromDeclaration(node), false)
		}
		return b.serializeTypeForDeclaration(node, nil, nil, false)
	case pseudochecker.PseudoTypeKindMaybeConstLocation:
		d := t.AsPseudoTypeMaybeConstLocation()
		isInConstContext := b.ch.isConstContext(d.Node)
		if !isInConstContext && pseudochecker.IsInConstContext(d.Node) {
			contextualType := b.ch.getContextualType(d.Node, ContextFlagsNone)
			t := b.pseudoTypeToType(d.ConstType)
			if t != nil && b.ch.isLiteralOfContextualType(t, b.ch.instantiateContextualType(contextualType, d.Node, ContextFlagsNone)) {
				isInConstContext = true
			}
		}
		if isInConstContext {
			return b.pseudoTypeToNode(d.ConstType)
		} else {
			return b.pseudoTypeToNode(d.RegularType)
		}
	case pseudochecker.PseudoTypeKindUnion:
		var res []ast.Handle
		var hasElidedType bool
		var hasUndefined bool
		members := t.AsPseudoTypeUnion().Types
		var appendTypeNode func(node ast.Handle)
		appendTypeNode = func(node ast.Handle) {
			if ast.IsUnionTypeNode(node) {
				for _, node := range node.Store().ListSlice(node.UnionTypeNodeTypes()) {
					appendTypeNode(node)
				}
				return
			}
			if node.Kind() == ast.KindUndefinedKeyword {
				if hasUndefined {
					return
				}
				hasUndefined = true
			}
			res = append(res, node)
		}
		for _, m := range members {
			if !b.ch.strictNullChecks {
				if m.Kind == pseudochecker.PseudoTypeKindUndefined || m.Kind == pseudochecker.PseudoTypeKindNull {
					hasElidedType = true
					continue
				}
			}
			appendTypeNode(b.pseudoTypeToNode(m))
		}
		if len(res) == 1 {
			return res[0]
		}
		if len(res) == 0 {
			if hasElidedType {
				return b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
			}
			return b.f.NewKeywordTypeNode(ast.KindNeverKeyword)
		}
		return b.f.NewUnionTypeNode(b.f.NewList(res))
	case pseudochecker.PseudoTypeKindUndefined:
		if !b.ch.strictNullChecks {
			return b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
		}
		return b.f.NewKeywordTypeNode(ast.KindUndefinedKeyword)
	case pseudochecker.PseudoTypeKindNull:
		if !b.ch.strictNullChecks {
			return b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
		}
		return b.f.NewLiteralTypeNode(b.f.NewKeywordExpression(ast.KindNullKeyword))
	case pseudochecker.PseudoTypeKindAny:
		return b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
	case pseudochecker.PseudoTypeKindString:
		return b.f.NewKeywordTypeNode(ast.KindStringKeyword)
	case pseudochecker.PseudoTypeKindNumber:
		return b.f.NewKeywordTypeNode(ast.KindNumberKeyword)
	case pseudochecker.PseudoTypeKindBigInt:
		return b.f.NewKeywordTypeNode(ast.KindBigIntKeyword)
	case pseudochecker.PseudoTypeKindBoolean:
		return b.f.NewKeywordTypeNode(ast.KindBooleanKeyword)
	case pseudochecker.PseudoTypeKindFalse:
		return b.f.NewLiteralTypeNode(b.f.NewKeywordExpression(ast.KindFalseKeyword))
	case pseudochecker.PseudoTypeKindTrue:
		return b.f.NewLiteralTypeNode(b.f.NewKeywordExpression(ast.KindTrueKeyword))
	case pseudochecker.PseudoTypeKindSingleCallSignature:
		d := t.AsPseudoTypeSingleCallSignature()
		signature := b.ch.getSignatureFromDeclaration(d.Signature)
		expandedParams := b.ch.getExpandedParameters(signature, true)[0]
		cleanup := b.enterNewScope(d.Signature, expandedParams, signature.typeParameters, signature.parameters, signature.mapper)
		defer cleanup()
		var typeParams ast.ListRef
		if len(d.TypeParameters) > 0 {
			res := make([]ast.Handle, 0, len(d.TypeParameters))
			for _, tp := range d.TypeParameters {
				res = append(res, b.reuseNode(tp))
			}
			typeParams = b.f.NewList(res)
		}
		params := b.pseudoParametersToNodeList(d.Parameters)
		returnType := b.pseudoTypeToNode(d.ReturnType)
		return b.f.NewFunctionTypeNode(typeParams, params, returnType)
	case pseudochecker.PseudoTypeKindTuple:
		var res []ast.Handle
		elements := t.AsPseudoTypeTuple().Elements
		for _, e := range elements {
			res = append(res, b.pseudoTypeToNode(e))
		}
		result := b.f.NewTupleTypeNode(b.f.NewList(res))
		b.e.AddEmitFlags(result, printer.EFSingleLine)
		return b.f.NewTypeOperatorNode(ast.KindReadonlyKeyword, result)
	case pseudochecker.PseudoTypeKindObjectLiteral:
		elements := t.AsPseudoTypeObjectLiteral().Elements
		if len(elements) == 0 {
			result := b.f.NewTypeLiteralNode(b.f.NewList(nil))
			b.e.AddEmitFlags(result, printer.EFSingleLine)
			return result
		}
		isConst := b.ch.isConstContext(elements[0].Name.Parent().Parent())
		newElements := make([]ast.Handle, 0, len(elements))
		restoreObjectLiteralFlags := b.saveRestoreFlags()
		b.ctx.flags |= nodebuilder.FlagsInObjectTypeLiteral
		for _, e := range elements {
			var modifiers ast.ListRef
			if isConst || (e.Kind == pseudochecker.PseudoObjectElementKindPropertyAssignment && e.AsPseudoPropertyAssignment().Readonly) {
				modifiers = b.f.NewModifierList([]ast.Handle{b.f.NewModifier(ast.KindReadonlyKeyword)})
			}
			var cleanup func()
			if e.Kind != pseudochecker.PseudoObjectElementKindPropertyAssignment {
				signature := b.ch.getSignatureFromDeclaration(e.Signature())
				expandedParams := b.ch.getExpandedParameters(signature, true)[0]
				cleanup = b.enterNewScope(e.Signature(), expandedParams, signature.typeParameters, signature.parameters, signature.mapper)
			}
			var newProp ast.Handle
			switch e.Kind {
			case pseudochecker.PseudoObjectElementKindMethod:
				d := e.AsPseudoObjectMethod()
				var typeParams ast.ListRef
				if len(d.TypeParameters) > 0 {
					res := make([]ast.Handle, 0, len(d.TypeParameters))
					for _, tp := range d.TypeParameters {
						res = append(res, b.reuseNode(tp))
					}
					typeParams = b.f.NewList(res)
				}
				if isConst {
					newProp = b.f.NewPropertySignatureDeclaration(modifiers, b.reuseName(e.Name, false), ast.Handle{}, b.f.NewFunctionTypeNode(typeParams, b.pseudoParametersToNodeList(d.Parameters), b.pseudoTypeToNode(d.ReturnType)), ast.Handle{})
					break
				}
				newProp = b.f.NewMethodSignatureDeclaration(modifiers, b.reuseName(e.Name, true), ast.Handle{}, typeParams, b.pseudoParametersToNodeList(d.Parameters), b.pseudoTypeToNode(d.ReturnType))
			case pseudochecker.PseudoObjectElementKindPropertyAssignment:
				d := e.AsPseudoPropertyAssignment()
				newProp = b.f.NewPropertySignatureDeclaration(modifiers, b.reuseName(e.Name, false), ast.Handle{}, b.pseudoTypeToNode(d.Type), ast.Handle{})
			case pseudochecker.PseudoObjectElementKindSetAccessor:
				d := e.AsPseudoSetAccessor()
				newProp = b.f.NewSetAccessorDeclaration(0, b.reuseName(e.Name, false), 0, b.f.NewList([]ast.Handle{b.pseudoParameterToNode(d.Parameter)}), ast.Handle{}, ast.Handle{}, ast.Handle{})
			case pseudochecker.PseudoObjectElementKindGetAccessor:
				d := e.AsPseudoGetAccessor()
				newProp = b.f.NewGetAccessorDeclaration(0, b.reuseName(e.Name, false), 0, 0, b.pseudoTypeToNode(d.Type), ast.Handle{}, ast.Handle{})
			}
			if b.ctx.enclosingFile == ast.GetSourceFileOfNode(e.Name) {
				b.e.SetCommentRange(newProp, e.Name.Parent().Loc())
			}
			newElements = append(newElements, newProp)
			if cleanup != nil {
				cleanup()
			}
		}
		restoreObjectLiteralFlags()
		result := b.f.NewTypeLiteralNode(b.f.NewList(newElements))
		if b.ctx.flags&nodebuilder.FlagsMultilineObjectLiterals == 0 {
			b.e.AddEmitFlags(result, printer.EFSingleLine)
		}
		return result
	case pseudochecker.PseudoTypeKindStringLiteral, pseudochecker.PseudoTypeKindNumericLiteral, pseudochecker.PseudoTypeKindBigIntLiteral:
		source := t.AsPseudoTypeLiteral().Node
		return b.f.NewLiteralTypeNode(b.reuseNode(source))
	default:
		debug.AssertNever(t.Kind, "Unhandled pseudotype kind in pseudotype node construction")
		return ast.Handle{}
	}
}
func (b *NodeBuilderImpl) pseudoParametersToNodeList(params []*pseudochecker.PseudoParameter) ast.ListRef {
	res := make([]ast.Handle, 0, len(params))
	for _, p := range params {
		res = append(res, b.pseudoParameterToNode(p))
	}
	return b.f.NewList(res)
}
func (b *NodeBuilderImpl) pseudoParameterToNode(p *pseudochecker.PseudoParameter) ast.Handle {
	var dotDotDot ast.Handle
	var questionMark ast.Handle
	if p.Rest {
		dotDotDot = b.f.NewToken(ast.KindDotDotDotToken)
	}
	if p.Optional {
		questionMark = b.f.NewToken(ast.KindQuestionToken)
	}
	parameter := b.f.NewParameterDeclaration(0, dotDotDot, b.parameterToParameterDeclarationName(p.Name.Parent().Symbol(), p.Name.Parent()), questionMark, b.pseudoTypeToNode(p.Type), ast.Handle{})
	if original := p.Name.Parent(); ast.IsParameterDeclaration(original) {
		b.setCommentRange(parameter, original)
	}
	return parameter
}

func (b *NodeBuilderImpl) pseudoTypeEquivalentToType(t *pseudochecker.PseudoType, type_ *Type, isOptionalAnnotated bool, reportErrors bool) bool {
	if type_ != nil && b.ch.isErrorType(type_) {
		return true
	}
	typeFromPseudo := b.pseudoTypeToType(t)
	if typeFromPseudo == type_ {
		return true
	}
	undefinedStripped := type_
	if isOptionalAnnotated {
		undefinedStripped = b.ch.getTypeWithFacts(type_, TypeFactsNEUndefined)
	}
	if typeFromPseudo != nil && type_ != nil {
		if isOptionalAnnotated {
			if undefinedStripped == typeFromPseudo {
				return true
			}
			if typeFromPseudo.flags&TypeFlagsUnion != 0 && undefinedStripped.flags&TypeFlagsUnion != 0 {
				if b.ch.compareTypesIdentical(typeFromPseudo, undefinedStripped) == TernaryTrue {
					return true
				}
			}
		}
		if b.ch.getRegularTypeOfLiteralType(typeFromPseudo) == b.ch.getRegularTypeOfLiteralType(type_) {
			return true
		}
		if typeFromPseudo.flags&TypeFlagsUnion != 0 && type_.flags&TypeFlagsUnion != 0 {
			if b.ch.compareTypesIdentical(typeFromPseudo, type_) == TernaryTrue {
				return true
			}
		}
	}
	switch t.Kind {
	case pseudochecker.PseudoTypeKindInferred:
		if errorNodes := t.AsPseudoTypeInferred().ErrorNodes; len(errorNodes) > 0 {
			if reportErrors {
				for _, n := range errorNodes {
					b.ctx.tracker.ReportInferenceFallback(n)
				}
			}
			return false
		}
		if reportErrors {
			b.ctx.tracker.ReportInferenceFallback(t.AsPseudoTypeInferred().Expression)
		}
		return false
	case pseudochecker.PseudoTypeKindObjectLiteral:
		pt := t.AsPseudoTypeObjectLiteral()
		if type_ == nil {
			return false
		}
		targetProps := b.ch.getPropertiesOfType(undefinedStripped)
		targetDeclCount := 0
		for _, prop := range targetProps {
			targetDeclCount += len(prop.Declarations)
		}
		if len(pt.Elements) != targetDeclCount {
			return false
		}
		for _, e := range pt.Elements {
			var targetProp *ast.Symbol
			elemSymbol := e.Name.Parent().Symbol()
			if elemSymbol != nil {
				targetProp = b.ch.getPropertyOfType(undefinedStripped, elemSymbol.Name)
			}
			if targetProp == nil {
				for _, prop := range targetProps {
					if prop.ValueDeclaration != 0 && ast.NodeOf(prop.ValueDeclaration).Name() == e.Name {
						targetProp = prop
						break
					}
				}
				if targetProp == nil {
					if reportErrors {
						b.ctx.tracker.ReportInferenceFallback(e.Name.Parent())
					}
					return false
				}
			}
			targetIsOptional := targetProp.Flags&ast.SymbolFlagsOptional != 0
			if e.Optional != targetIsOptional {
				if reportErrors {
					b.ctx.tracker.ReportInferenceFallback(e.Name.Parent())
				}
				return false
			}
			propType := b.ch.getTypeOfSymbol(targetProp)
			propType = b.ch.removeMissingType(propType, targetIsOptional)
			switch e.Kind {
			case pseudochecker.PseudoObjectElementKindPropertyAssignment:
				d := e.AsPseudoPropertyAssignment()
				if !b.pseudoTypeEquivalentToType(d.Type, propType, e.Optional, false) {
					if reportErrors {
						if d.Type.Kind == pseudochecker.PseudoTypeKindInferred && len(d.Type.AsPseudoTypeInferred().ErrorNodes) > 0 {
							for _, n := range d.Type.AsPseudoTypeInferred().ErrorNodes {
								b.ctx.tracker.ReportInferenceFallback(n)
							}
						} else if !isStructuralPseudoType(d.Type) {
							b.ctx.tracker.ReportInferenceFallback(e.Name.Parent())
						}
					}
					return false
				}
			case pseudochecker.PseudoObjectElementKindMethod:
				d := e.AsPseudoObjectMethod()
				targetSig := b.ch.getSingleCallSignature(propType)
				if targetSig == nil {
					continue
				}
				paramEq := b.pseudoParametersEquivalentToParameters(d.Parameters, targetSig, reportErrors, e.Name.Parent())
				if !paramEq {
					return false
				}
				targetPredicate := b.ch.getTypePredicateOfSignature(targetSig)
				if targetPredicate != nil {
					if !b.pseudoReturnTypeMatchesPredicate(d.ReturnType, targetPredicate) {
						if reportErrors {
							b.ctx.tracker.ReportInferenceFallback(e.Name.Parent())
						}
						return false
					}
				} else if !b.pseudoTypeEquivalentToType(d.ReturnType, b.ch.getReturnTypeOfSignature(targetSig), false, false) {
					if reportErrors {
						b.ctx.tracker.ReportInferenceFallback(e.Name.Parent())
					}
					return false
				}
			case pseudochecker.PseudoObjectElementKindGetAccessor:
				d := e.AsPseudoGetAccessor()
				if !b.pseudoTypeEquivalentToType(d.Type, propType, false, false) {
					if reportErrors {
						b.ctx.tracker.ReportInferenceFallback(e.Name.Parent())
					}
					return false
				}
			case pseudochecker.PseudoObjectElementKindSetAccessor:
				d := e.AsPseudoSetAccessor()
				writeType := b.ch.getWriteTypeOfSymbol(targetProp)
				if !b.pseudoTypeEquivalentToType(d.Parameter.Type, writeType, false, false) {
					if reportErrors {
						b.ctx.tracker.ReportInferenceFallback(e.Name.Parent())
					}
					return false
				}
			}
		}
		return true
	case pseudochecker.PseudoTypeKindTuple:
		pt := t.AsPseudoTypeTuple()
		if undefinedStripped == nil || !isTupleType(undefinedStripped) {
			return false
		}
		tupleTarget := undefinedStripped.TargetTupleType()
		if tupleTarget.combinedFlags&ElementFlagsNonRequired != 0 {
			return false
		}
		elementTypes := b.ch.getTypeArguments(undefinedStripped)
		if len(pt.Elements) != len(elementTypes) {
			return false
		}
		for i, elem := range pt.Elements {
			if !b.pseudoTypeEquivalentToType(elem, elementTypes[i], false, reportErrors) {
				return false
			}
		}
		return true
	case pseudochecker.PseudoTypeKindSingleCallSignature:
		targetSig := b.ch.getSingleCallSignature(undefinedStripped)
		if targetSig == nil {
			return false
		}
		pt := t.AsPseudoTypeSingleCallSignature()
		if len(targetSig.typeParameters) != len(pt.TypeParameters) {
			if reportErrors {
				b.ctx.tracker.ReportInferenceFallback(pt.Signature)
			}
			return false
		}
		paramEq := b.pseudoParametersEquivalentToParameters(pt.Parameters, targetSig, reportErrors, pt.Signature)
		if !paramEq {
			return false
		}
		targetPredicate := b.ch.getTypePredicateOfSignature(targetSig)
		if targetPredicate != nil {
			if !b.pseudoReturnTypeMatchesPredicate(pt.ReturnType, targetPredicate) {
				if reportErrors {
					b.ctx.tracker.ReportInferenceFallback(pt.Signature)
				}
				return false
			}
		} else if !b.pseudoTypeEquivalentToType(pt.ReturnType, b.ch.getReturnTypeOfSignature(targetSig), false, reportErrors) {
			return false
		}
		return true
	case pseudochecker.PseudoTypeKindNoResult:
		if reportErrors {
			b.ctx.tracker.ReportInferenceFallback(t.AsPseudoTypeNoResult().Declaration)
		}
		return false
	default:
		return false
	}
}
func (b *NodeBuilderImpl) pseudoParametersEquivalentToParameters(params []*pseudochecker.PseudoParameter, targetSig *Signature, reportErrors bool, nonParamErrorLocation ast.Handle) bool {
	if targetSig.thisParameter != nil && len(params) == 0 {
		if reportErrors {
			b.ctx.tracker.ReportInferenceFallback(nonParamErrorLocation)
		}
		return false
	} else if targetSig.thisParameter != nil && ast.IsThisIdentifier(params[0].Name) {
		targetParam := targetSig.thisParameter
		paramType := b.ch.getTypeOfParameter(targetParam)
		if !b.pseudoTypeEquivalentToType(params[0].Type, paramType, params[0].Optional, false) {
			if reportErrors {
				b.ctx.tracker.ReportInferenceFallback(params[0].Name.Parent())
			}
			return false
		}
		params = params[1:]
	} else if targetSig.thisParameter != nil {
		if reportErrors {
			b.ctx.tracker.ReportInferenceFallback(nonParamErrorLocation)
		}
		return false
	}
	if len(targetSig.parameters) != len(params) {
		if reportErrors {
			b.ctx.tracker.ReportInferenceFallback(nonParamErrorLocation)
		}
		return false
	}
	for i, p := range params {
		targetParam := targetSig.parameters[i]
		if p.Optional != b.ch.isOptionalParameter(ast.NodeOf(targetParam.ValueDeclaration)) {
			if reportErrors {
				b.ctx.tracker.ReportInferenceFallback(p.Name.Parent())
			}
			return false
		}
		paramType := b.ch.getTypeOfParameter(targetParam)
		if !b.pseudoTypeEquivalentToType(p.Type, paramType, p.Optional, false) {
			if reportErrors {
				b.ctx.tracker.ReportInferenceFallback(p.Name.Parent())
			}
			return false
		}
	}
	return true
}
func isStructuralPseudoType(t *pseudochecker.PseudoType) bool {
	switch t.Kind {
	case pseudochecker.PseudoTypeKindObjectLiteral, pseudochecker.PseudoTypeKindTuple, pseudochecker.PseudoTypeKindSingleCallSignature:
		return true
	case pseudochecker.PseudoTypeKindMaybeConstLocation:
		d := t.AsPseudoTypeMaybeConstLocation()
		return isStructuralPseudoType(d.ConstType) || isStructuralPseudoType(d.RegularType)
	}
	return false
}

func (b *NodeBuilderImpl) pseudoReturnTypeMatchesPredicate(rt *pseudochecker.PseudoType, predicate *TypePredicate) bool {
	if rt.Kind != pseudochecker.PseudoTypeKindDirect {
		return false
	}
	node := rt.AsPseudoTypeDirect().TypeNode
	if !ast.IsTypePredicateNode(node) {
		return false
	}
	tp := node
	isAsserts := !tp.AssertsModifier().IsNil()
	predicateIsAsserts := predicate.kind == TypePredicateKindAssertsThis || predicate.kind == TypePredicateKindAssertsIdentifier
	if isAsserts != predicateIsAsserts {
		return false
	}
	isThis := ast.IsThisTypeNode(tp.ParameterName())
	predicateIsThis := predicate.kind == TypePredicateKindThis || predicate.kind == TypePredicateKindAssertsThis
	if isThis != predicateIsThis {
		return false
	}
	if !isThis {
		if tp.ParameterName().Text() != predicate.parameterName {
			return false
		}
	}
	if predicate.t != nil {
		if tp.Type().IsNil() {
			return false
		}
		predicateTypeFromNode := b.ch.getTypeFromTypeNode(tp.Type())
		if predicateTypeFromNode != predicate.t {
			if b.ch.compareTypesIdentical(predicateTypeFromNode, predicate.t) != TernaryTrue {
				return false
			}
		}
	} else if !tp.Type().IsNil() {
		return false
	}
	return true
}
func (b *NodeBuilderImpl) pseudoTypeToType(t *pseudochecker.PseudoType) *Type {
	debug.Assert(t != nil, "Attempted to realize nil pseudotype")
	switch t.Kind {
	case pseudochecker.PseudoTypeKindDirect:
		return b.ch.getTypeFromTypeNode(t.AsPseudoTypeDirect().TypeNode)
	case pseudochecker.PseudoTypeKindInferred:
		node := t.AsPseudoTypeInferred().Expression
		if t.AsPseudoTypeInferred().IsSignatureReturn {
			return b.ch.getReturnTypeOfSignature(b.ch.getSignatureFromDeclaration(node))
		}
		ty := b.ch.getWidenedType(b.ch.getRegularTypeOfExpression(node))
		return ty
	case pseudochecker.PseudoTypeKindNoResult:
		return nil
	case pseudochecker.PseudoTypeKindMaybeConstLocation:
		d := t.AsPseudoTypeMaybeConstLocation()
		if b.ch.isConstContext(d.Node) {
			return b.pseudoTypeToType(d.ConstType)
		}
		return b.pseudoTypeToType(d.RegularType)
	case pseudochecker.PseudoTypeKindUnion:
		var res []*Type
		var hasElidedType bool
		members := t.AsPseudoTypeUnion().Types
		for _, m := range members {
			if !b.ch.strictNullChecks {
				if m.Kind == pseudochecker.PseudoTypeKindUndefined || m.Kind == pseudochecker.PseudoTypeKindNull {
					hasElidedType = true
					continue
				}
			}
			t := b.pseudoTypeToType(m)
			if t == nil {
				return nil
			}
			res = append(res, t)
		}
		if len(res) == 1 {
			return res[0]
		}
		if len(res) == 0 {
			if hasElidedType {
				return b.ch.anyType
			}
			return b.ch.neverType
		}
		return b.ch.getUnionType(res)
	case pseudochecker.PseudoTypeKindUndefined:
		return b.ch.undefinedWideningType
	case pseudochecker.PseudoTypeKindNull:
		return b.ch.nullWideningType
	case pseudochecker.PseudoTypeKindAny:
		return b.ch.anyType
	case pseudochecker.PseudoTypeKindString:
		return b.ch.stringType
	case pseudochecker.PseudoTypeKindNumber:
		return b.ch.numberType
	case pseudochecker.PseudoTypeKindBigInt:
		return b.ch.bigintType
	case pseudochecker.PseudoTypeKindBoolean:
		return b.ch.booleanType
	case pseudochecker.PseudoTypeKindFalse:
		return b.ch.falseType
	case pseudochecker.PseudoTypeKindTrue:
		return b.ch.trueType
	case pseudochecker.PseudoTypeKindStringLiteral, pseudochecker.PseudoTypeKindNumericLiteral, pseudochecker.PseudoTypeKindBigIntLiteral:
		source := t.AsPseudoTypeLiteral().Node
		return b.ch.getRegularTypeOfExpression(source)
	case pseudochecker.PseudoTypeKindObjectLiteral, pseudochecker.PseudoTypeKindSingleCallSignature, pseudochecker.PseudoTypeKindTuple:
		return nil
	default:
		debug.Fail("Unhandled pseudochecker.PseudoTypeKind in pseudoTypeToType")
		return nil
	}
}
