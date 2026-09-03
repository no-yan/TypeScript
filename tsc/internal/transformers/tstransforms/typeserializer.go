package tstransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type metadataSerializer struct {
	resolver         printer.EmitResolver
	languageVersion  core.ScriptTarget
	strictNullChecks bool
	f                *printer.NodeFactory
	ec               *printer.EmitContext
	c                metadataSerializerContext
}
type metadataSerializerContext struct {
	currentLexicalScope ast.Handle
	currentNameScope                 ast.Handle
	serializingConditionalTypeBranch bool
}

func newMetadataSerializer(resolver printer.EmitResolver, f *printer.NodeFactory, ec *printer.EmitContext, languageVersion core.ScriptTarget, strictNullChecks bool) *metadataSerializer {
	return &metadataSerializer{resolver: resolver, languageVersion: languageVersion, f: f, ec: ec, strictNullChecks: strictNullChecks}
}
func (s *metadataSerializer) setContext(ctx metadataSerializerContext) {
	s.c = ctx
}
func (s *metadataSerializer) SerializeTypeOfNode(ctx metadataSerializerContext, node ast.Handle, container ast.Handle) ast.Handle {
	oldCtx := s.c
	s.c = ctx
	defer s.setContext(oldCtx)
	return s.serializeTypeOfNode(node, container)
}
func (s *metadataSerializer) SerializeParameterTypesOfNode(ctx metadataSerializerContext, node ast.Handle, container ast.Handle) ast.Handle {
	oldCtx := s.c
	s.c = ctx
	defer s.setContext(oldCtx)
	return s.serializeParameterTypesOfNode(node, container)
}
func (s *metadataSerializer) SerializeReturnTypeOfNode(ctx metadataSerializerContext, node ast.Handle) ast.Handle {
	oldCtx := s.c
	s.c = ctx
	defer s.setContext(oldCtx)
	return s.serializeReturnTypeOfNode(node)
}
func GetSetAccessorValueParameter(node ast.Handle) ast.Handle {
	if !node.IsNil() && len(node.Parameters()) > 0 {
		if len(node.Parameters()) >= 2 && ast.IsThisParameter(node.Parameters()[0]) {
			return node.Parameters()[1]
		}
		return node.Parameters()[0]
	}
	return ast.Handle{}
}

func getSetAccessorTypeAnnotationNode(node ast.Handle) ast.Handle {
	p := GetSetAccessorValueParameter(node)
	if !p.IsNil() && !p.Type().IsNil() {
		return p.Type()
	}
	return ast.Handle{}
}
func getAccessorTypeNode(node ast.Handle, container ast.Handle) ast.Handle {
	accessors := ast.GetAllAccessorDeclarations(container.Members(), node)
	if !accessors.SetAccessor.IsNil() {
		return getSetAccessorTypeAnnotationNode(accessors.SetAccessor)
	}
	if !accessors.GetAccessor.IsNil() {
		return accessors.GetAccessor.Type()
	}
	return ast.Handle{}
}

func (s *metadataSerializer) serializeTypeOfNode(node ast.Handle, container ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindPropertyDeclaration, ast.KindParameter:
		return s.serializeTypeNode(node.Type())
	case ast.KindGetAccessor, ast.KindSetAccessor:
		return s.serializeTypeNode(getAccessorTypeNode(node, container))
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindMethodDeclaration:
		return s.f.NewIdentifier("Function")
	default:
		return s.f.NewVoidZeroExpression()
	}
}

func (s *metadataSerializer) serializeParameterTypesOfNode(node ast.Handle, container ast.Handle) ast.Handle {
	var valueDeclaration ast.Handle
	if ast.IsClassLike(node) {
		valueDeclaration = ast.GetFirstConstructorWithBody(node)
	} else if ast.IsFunctionLike(node) && ast.NodeIsPresent(node.Body()) {
		valueDeclaration = node
	}
	if valueDeclaration.IsNil() {
		return s.f.NewArrayLiteralExpression(s.f.NewList([]ast.Handle{}), false)
	}
	var expressions []ast.Handle
	parameters := getParametersOfDecoratedDeclaration(valueDeclaration, container)
	for i, parameter := range node.Store().ListSlice(parameters) {
		if i == 0 && ast.IsIdentifier(parameter.Name()) && parameter.Name().Text() == "this" {
			continue
		}
		if !parameter.ParameterDeclarationDotDotDotToken().IsNil() {
			expressions = append(expressions, s.serializeTypeNode(ast.GetRestParameterElementType(parameter.Type())))
		} else {
			expressions = append(expressions, s.serializeTypeOfNode(parameter, container))
		}
	}
	return s.f.NewArrayLiteralExpression(s.f.NewList(expressions), false)
}
func getParametersOfDecoratedDeclaration(node ast.Handle, container ast.Handle) ast.ListRef {
	if !container.IsNil() && node.Kind == ast.KindGetAccessor {
		acc := ast.GetAllAccessorDeclarations(container.Members(), node)
		if !acc.SetAccessor.IsNil() {
			return acc.SetAccessor.ParameterList()
		}
	}
	return node.ParameterList()
}

func (s *metadataSerializer) serializeReturnTypeOfNode(node ast.Handle) ast.Handle {
	if ast.IsFunctionLike(node) && !node.Type().IsNil() {
		return s.serializeTypeNode(node.Type())
	} else if ast.IsAsyncFunction(node) {
		return s.f.NewIdentifier("Promise")
	}
	return s.f.NewVoidZeroExpression()
}

func (s *metadataSerializer) serializeTypeNode(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return s.f.NewIdentifier("Object")
	}
	node = ast.SkipTypeParentheses(node)
	switch node.Kind {
	case ast.KindVoidKeyword, ast.KindUndefinedKeyword, ast.KindNeverKeyword:
		return s.f.NewVoidZeroExpression()
	case ast.KindFunctionType, ast.KindConstructorType:
		return s.f.NewIdentifier("Function")
	case ast.KindArrayType, ast.KindTupleType:
		return s.f.NewIdentifier("Array")
	case ast.KindTypePredicate:
		if !node.TypePredicateNodeAssertsModifier().IsNil() {
			return s.f.NewVoidZeroExpression()
		}
		return s.f.NewIdentifier("Boolean")
	case ast.KindBooleanKeyword:
		return s.f.NewIdentifier("Boolean")
	case ast.KindTemplateLiteralType, ast.KindStringKeyword:
		return s.f.NewIdentifier("String")
	case ast.KindObjectKeyword:
		return s.f.NewIdentifier("Object")
	case ast.KindLiteralType:
		return s.serializeLiteralOfLiteralTypeNode(node.LiteralTypeNodeLiteral())
	case ast.KindNumberKeyword:
		return s.f.NewIdentifier("Number")
	case ast.KindBigIntKeyword:
		return s.serializeBigIntConstructor()
	case ast.KindSymbolKeyword:
		return s.f.NewIdentifier("Symbol")
	case ast.KindTypeReference:
		return s.serializeTypeReferenceNode(node)
	case ast.KindIntersectionType:
		return s.serializeUnionOrIntersectionConstituents(node.Store().ListSlice(node.IntersectionTypeNodeTypes()), true)
	case ast.KindUnionType:
		return s.serializeUnionOrIntersectionConstituents(node.Store().ListSlice(node.UnionTypeNodeTypes()), false)
	case ast.KindConditionalType:
		oldState := s.c.serializingConditionalTypeBranch
		s.c.serializingConditionalTypeBranch = true
		defer func() {
			s.c.serializingConditionalTypeBranch = oldState
		}()
		return s.serializeUnionOrIntersectionConstituents([]ast.Handle{node.ConditionalTypeNodeTrueType(), node.ConditionalTypeNodeFalseType()}, false)
	case ast.KindTypeOperator:
		if node.TypeOperatorNodeOperator() == ast.KindReadonlyKeyword {
			return s.serializeTypeNode(node.Type())
		}
	case ast.KindTypeQuery, ast.KindIndexedAccessType, ast.KindMappedType, ast.KindTypeLiteral, ast.KindAnyKeyword, ast.KindUnknownKeyword, ast.KindThisType, ast.KindImportType:
		break
	case ast.KindJSDocAllType, ast.KindJSDocVariadicType:
		break
	case ast.KindJSDocNullableType, ast.KindJSDocNonNullableType, ast.KindJSDocOptionalType:
		return s.serializeTypeNode(node.Type())
	default:
		debug.FailBadSyntaxKind(node)
		return ast.Handle{}
	}
	return s.f.NewIdentifier("Object")
}
func (s *metadataSerializer) serializeUnionOrIntersectionConstituents(types []ast.Handle, isIntersection bool) ast.Handle {
	var serializedType ast.Handle
	for _, typeNode := range types {
		typeNode = ast.SkipTypeParentheses(typeNode)
		if typeNode.Kind == ast.KindNeverKeyword {
			if isIntersection {
				return s.f.NewVoidZeroExpression()
			}
			continue
		}
		if typeNode.Kind == ast.KindUnknownKeyword {
			if !isIntersection {
				return s.f.NewIdentifier("Object")
			}
			continue
		}
		if typeNode.Kind == ast.KindAnyKeyword {
			return s.f.NewIdentifier("Object")
		}
		if !s.strictNullChecks && ((ast.IsLiteralTypeNode(typeNode) && typeNode.LiteralTypeNodeLiteral().Kind == ast.KindNullKeyword) || typeNode.Kind == ast.KindUndefinedKeyword) {
			continue
		}
		serializedConstituent := s.serializeTypeNode(typeNode)
		if ast.IsIdentifier(serializedConstituent) && serializedConstituent.IdentifierText() == "Object" {
			return serializedConstituent
		}
		if !serializedType.IsNil() {
			if !s.equateSerializedTypeNodes(serializedType, serializedConstituent) {
				return s.f.NewIdentifier("Object")
			}
		} else {
			serializedType = serializedConstituent
		}
	}
	if !serializedType.IsNil() {
		return serializedType
	}
	return s.f.NewVoidZeroExpression()
}
func (s *metadataSerializer) serializeLiteralOfLiteralTypeNode(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return s.f.NewIdentifier("String")
	case ast.KindPrefixUnaryExpression:
		operand := node.PrefixUnaryExpressionOperand()
		switch operand.Kind {
		case ast.KindNumericLiteral, ast.KindBigIntLiteral:
			return s.serializeLiteralOfLiteralTypeNode(operand)
		default:
			debug.FailBadSyntaxKind(operand)
		}
	case ast.KindNumericLiteral:
		return s.f.NewIdentifier("Number")
	case ast.KindBigIntLiteral:
		return s.serializeBigIntConstructor()
	case ast.KindTrueKeyword, ast.KindFalseKeyword:
		return s.f.NewIdentifier("Boolean")
	case ast.KindNullKeyword:
		return s.f.NewVoidZeroExpression()
	default:
		debug.FailBadSyntaxKind(node)
		return ast.Handle{}
	}
	return ast.Handle{}
}

func (s *metadataSerializer) serializeTypeReferenceNode(node ast.Handle) ast.Handle {
	serialScope := s.c.currentNameScope
	if serialScope.IsNil() {
		serialScope = s.c.currentLexicalScope
	}
	kind := s.resolver.GetTypeReferenceSerializationKind(s.ec.ParseNode(node.TypeName()), s.ec.ParseNode(serialScope))
	switch kind {
	case printer.TypeReferenceSerializationKindUnknown:
		if s.c.serializingConditionalTypeBranch {
			return s.f.NewIdentifier("Object")
		}
		serialized := s.serializeEntityNameAsExpressionFallback(node.TypeName())
		temp := s.f.NewTempVariable()
		s.ec.AddVariableDeclaration(temp)
		return s.f.NewConditionalExpression(s.f.NewTypeCheck(s.f.NewAssignmentExpression(temp, serialized), "function"), s.f.NewToken(ast.KindQuestionToken), temp, s.f.NewToken(ast.KindColonToken), s.f.NewIdentifier("Object"))
	case printer.TypeReferenceSerializationKindTypeWithConstructSignatureAndValue:
		return s.serializeEntityNameAsExpression(node.TypeName())
	case printer.TypeReferenceSerializationKindVoidNullableOrNeverType:
		return s.f.NewVoidZeroExpression()
	case printer.TypeReferenceSerializationKindBigIntLikeType:
		return s.serializeBigIntConstructor()
	case printer.TypeReferenceSerializationKindBooleanType:
		return s.f.NewIdentifier("Boolean")
	case printer.TypeReferenceSerializationKindNumberLikeType:
		return s.f.NewIdentifier("Number")
	case printer.TypeReferenceSerializationKindStringLikeType:
		return s.f.NewIdentifier("String")
	case printer.TypeReferenceSerializationKindArrayLikeType:
		return s.f.NewIdentifier("Array")
	case printer.TypeReferenceSerializationKindESSymbolType:
		return s.f.NewIdentifier("Symbol")
	case printer.TypeReferenceSerializationKindTypeWithCallSignature:
		return s.f.NewIdentifier("Function")
	case printer.TypeReferenceSerializationKindPromise:
		return s.f.NewIdentifier("Promise")
	case printer.TypeReferenceSerializationKindObjectType:
		return s.f.NewIdentifier("Object")
	default:
		debug.AssertNever(kind, "unknown type reference serialization kind")
		return ast.Handle{}
	}
}
func (s *metadataSerializer) serializeBigIntConstructor() ast.Handle {
	if s.languageVersion >= core.ScriptTargetES2020 {
		return s.f.NewIdentifier("BigInt")
	}
	return s.f.NewConditionalExpression(s.f.NewTypeCheck(s.f.NewIdentifier("BigInt"), "function"), s.f.NewToken(ast.KindQuestionToken), s.f.NewIdentifier("BigInt"), s.f.NewToken(ast.KindColonToken), s.f.NewIdentifier("Object"))
}

func (s *metadataSerializer) serializeEntityNameAsExpression(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindIdentifier:
		name := s.f.DeepCloneNode(node)
		name.SetLoc(node.Loc())
		s.ec.UnsetOriginal(name)
		name.SetParent(s.ec.ParseNode(s.c.currentLexicalScope))
		return name
	case ast.KindQualifiedName:
		return s.serializeQualifiedNameAsExpression(node)
	}
	return ast.Handle{}
}

func (s *metadataSerializer) serializeQualifiedNameAsExpression(node ast.Handle) ast.Handle {
	return s.f.NewPropertyAccessExpression(s.serializeEntityNameAsExpression(node.QualifiedNameLeft()), ast.Handle{}, node.QualifiedNameRight(), ast.NodeFlagsNone)
}

func (s *metadataSerializer) serializeEntityNameAsExpressionFallback(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindIdentifier {
		copied := s.serializeEntityNameAsExpression(node)
		return s.createCheckedValue(copied, copied)
	}
	if node.QualifiedNameLeft().Kind == ast.KindIdentifier {
		return s.createCheckedValue(s.serializeEntityNameAsExpression(node.QualifiedNameLeft()), s.serializeEntityNameAsExpression(node))
	}
	left := s.serializeEntityNameAsExpressionFallback(node.QualifiedNameLeft())
	temp := s.f.NewTempVariable()
	s.ec.AddVariableDeclaration(temp)
	return s.f.NewLogicalANDExpression(s.f.NewLogicalANDExpression(left.BinaryExpressionLeft(), s.f.NewStrictInequalityExpression(s.f.NewAssignmentExpression(temp, left.BinaryExpressionRight()), s.f.NewVoidZeroExpression())), s.f.NewPropertyAccessExpression(temp, ast.Handle{}, node.QualifiedNameRight(), ast.NodeFlagsNone))
}

func (s *metadataSerializer) createCheckedValue(left ast.Handle, right ast.Handle) ast.Handle {
	return s.f.NewLogicalANDExpression(s.f.NewStrictInequalityExpression(s.f.NewTypeOfExpression(left), s.f.NewStringLiteral("undefined", ast.TokenFlagsNone)), right)
}
func (s *metadataSerializer) equateSerializedTypeNodes(left ast.Handle, right ast.Handle) bool {
	if transformers.IsGeneratedIdentifier(s.ec, left) {
		return transformers.IsGeneratedIdentifier(s.ec, right)
	}
	if ast.IsIdentifier(left) {
		return ast.IsIdentifier(right) && left.Text() == right.Text()
	}
	if ast.IsPropertyAccessExpression(left) {
		return ast.IsPropertyAccessExpression(right) && s.equateSerializedTypeNodes(left.Expression(), right.Expression()) && s.equateSerializedTypeNodes(left.Name(), right.Name())
	}
	if ast.IsVoidExpression(left) {
		return ast.IsVoidExpression(right) && ast.IsNumericLiteral(left.Expression()) && ast.IsNumericLiteral(right.Expression()) && left.Expression().Text() == "0" && right.Expression().Text() == "0"
	}
	if ast.IsStringLiteral(left) {
		return ast.IsStringLiteral(right) && left.Text() == right.Text()
	}
	if ast.IsTypeOfExpression(left) {
		return ast.IsTypeOfExpression(right) && s.equateSerializedTypeNodes(left.Expression(), right.Expression())
	}
	if ast.IsParenthesizedExpression(left) {
		return ast.IsParenthesizedExpression(right) && s.equateSerializedTypeNodes(left.Expression(), right.Expression())
	}
	if ast.IsConditionalExpression(left) {
		return ast.IsConditionalExpression(right) && s.equateSerializedTypeNodes(left.ConditionalExpressionCondition(), right.ConditionalExpressionCondition()) && s.equateSerializedTypeNodes(left.ConditionalExpressionWhenTrue(), right.ConditionalExpressionWhenTrue()) && s.equateSerializedTypeNodes(left.ConditionalExpressionWhenFalse(), right.ConditionalExpressionWhenFalse())
	}
	if ast.IsBinaryExpression(left) {
		return ast.IsBinaryExpression(right) && left.BinaryExpressionOperatorToken().Kind == right.BinaryExpressionOperatorToken().Kind && s.equateSerializedTypeNodes(left.BinaryExpressionLeft(), right.BinaryExpressionLeft()) && s.equateSerializedTypeNodes(left.BinaryExpressionRight(), right.BinaryExpressionRight())
	}
	return false
}
