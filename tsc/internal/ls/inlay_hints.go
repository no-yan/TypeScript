package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/evaluator"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"slices"
	"strings"
	"unicode"
)

func (l *LanguageService) ProvideInlayHint(ctx context.Context, params *lsproto.InlayHintParams) (lsproto.InlayHintResponse, error) {
	userPreferences := l.UserPreferences()
	inlayHintPreferences := userPreferences.InlayHints
	if !isAnyInlayHintEnabled(inlayHintPreferences) {
		return lsproto.InlayHintsOrNull{InlayHints: nil}, nil
	}
	program, file := l.getProgramAndFile(params.TextDocument.Uri)
	quotePreference := lsutil.GetQuotePreference(file, userPreferences)
	mappedRanges := lsconv.FromLSPRangeIntersectingForSourceFile(l.converters, file, params.Range, spanmap.FeatureInlayHints)
	result := make([]*// FunctionDeclaration | MethodDeclaration | GetAccessor | FunctionExpression | ArrowFunction
	/*includeJSDoc*/ // !!! Avoid type node reuse so we collect identifier symbols.
	/*enclosingDeclaration*/ // !!! Avoid type node reuse so we collect identifier symbols.
	/*enclosingDeclaration*/ // node is FunctionDeclaration | ArrowFunction | FunctionExpression | MethodDeclaration | GetAccessor
	/*includeJSDoc*/ // The location is an optional go-to target for the name. Only attach it when the name maps back to a
	// single concrete span in the original text; an approximate or synthesized mapping would point the
	// user somewhere wrong, so it is better to omit the target than to fabricate one.
	lsproto.InlayHint, 0, len(mappedRanges))
	for _, mapped := range mappedRanges {
		projection := mapped.Script
		checker, done := program.GetTypeCheckerForFile(ctx, projection)
		defer done()
		inlayHintState := &inlayHintState{ctx: ctx, span: mapped.Span, preferences: inlayHintPreferences, quotePreference: quotePreference, file: projection, checker: checker, converters: l.converters}
		inlayHintState.visit(projection.ParseRoot())
		result = append(result, inlayHintState.result...)
	}
	return lsproto.InlayHintsOrNull{InlayHints: &result}, nil
}

type inlayHintState struct {
	ctx             context.Context
	span            core.TextRange
	preferences     lsutil.InlayHintsPreferences
	quotePreference lsutil.QuotePreference
	file            *ast.SourceFile
	checker         *checker.Checker
	converters      *lsconv.Converters
	result          []*lsproto.InlayHint
}

func (s *inlayHintState) visit(node ast.Handle) bool {
	if node.IsNil() || node.End()-node.Pos() == 0 || node.Flags()&ast.NodeFlagsReparsed != 0 {
		return false
	}
	switch node.Kind {
	case ast.KindModuleDeclaration, ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindFunctionDeclaration, ast.KindClassExpression, ast.KindFunctionExpression, ast.KindMethodDeclaration, ast.KindArrowFunction:
		if s.ctx.Err() != nil {
			return true
		}
	}
	if !s.span.Intersects(node.Loc()) {
		return false
	}
	if ast.IsTypeNode(node) && !ast.IsExpressionWithTypeArguments(node) {
		return false
	}
	if s.preferences.IncludeInlayVariableTypeHints.IsTrue() && ast.IsVariableDeclaration(node) {
		s.visitVariableLikeDeclaration(node)
	} else if s.preferences.IncludeInlayPropertyDeclarationTypeHints.IsTrue() && ast.IsPropertyDeclaration(node) {
		s.visitVariableLikeDeclaration(node)
	} else if s.preferences.IncludeInlayEnumMemberValueHints.IsTrue() && ast.IsEnumMember(node) {
		s.visitEnumMember(node)
	} else if shouldShowParameterNameHints(s.preferences) && (ast.IsCallExpression(node) || ast.IsNewExpression(node)) {
		s.visitCallOrNewExpression(node)
	} else {
		if s.preferences.IncludeInlayFunctionParameterTypeHints.IsTrue() && ast.IsFunctionLikeDeclaration(node) && ast.HasContextSensitiveParameters(node) {
			s.visitFunctionLikeForParameterType(node)
		}
		if s.preferences.IncludeInlayFunctionLikeReturnTypeHints.IsTrue() && isSignatureSupportingReturnAnnotation(node) {
			s.visitFunctionDeclarationLikeForReturnType(node)
		}
	}
	return node.ForEachChild(s.visit)
}

func (s *inlayHintState) visitFunctionDeclarationLikeForReturnType(decl ast.Handle) {
	if ast.IsArrowFunction(decl) {
		if astnav.FindChildOfKind(decl, ast.KindOpenParenToken, s.file).IsNil() {
			return
		}
	}
	typeAnnotation := decl.Type()
	if !typeAnnotation.IsNil() || decl.Body().IsNil() {
		return
	}
	signature := s.checker.GetSignatureFromDeclaration(decl)
	if signature == nil {
		return
	}
	typePredicate := s.checker.GetTypePredicateOfSignature(signature)
	if typePredicate != nil && typePredicate.Type() != nil {
		hintParts := s.typePredicateToInlayHintParts(typePredicate)
		s.addTypeHints(hintParts, s.getTypeAnnotationPosition(decl))
		return
	}
	returnType := s.checker.GetReturnTypeOfSignature(signature)
	if isModuleReferenceType(returnType) {
		return
	}
	hintParts := s.typeToInlayHintParts(returnType)
	s.addTypeHints(hintParts, s.getTypeAnnotationPosition(decl))
}
func (s *inlayHintState) visitCallOrNewExpression(expr ast.Handle) {
	args := expr.Arguments()
	if len(args) == 0 {
		return
	}
	signature := s.checker.GetResolvedSignature(expr)
	if signature == nil {
		return
	}
	signatureParamPos := 0
	for _, originalArg := range args {
		arg := ast.SkipParentheses(originalArg)
		if shouldShowLiteralParameterNameHintsOnly(s.preferences) && !isHintableLiteral(arg) {
			signatureParamPos++
			continue
		}
		spreadArgs := 0
		if ast.IsSpreadElement(arg) {
			spreadType := s.checker.GetTypeAtLocation(arg.Expression())
			if spreadType.IsTupleType() {
				elementFlags := spreadType.Target().AsTupleType().ElementFlags()
				fixedLength := spreadType.Target().AsTupleType().FixedLength()
				if fixedLength == 0 {
					continue
				}
				firstOptionalIndex := slices.IndexFunc(elementFlags, func(f checker.ElementFlags) bool {
					return f&checker.ElementFlagsRequired == 0
				})
				requiredArgs := core.IfElse(firstOptionalIndex < 0, fixedLength, firstOptionalIndex)
				if requiredArgs > 0 {
					spreadArgs = requiredArgs
				}
			}
		}
		identifierInfo := s.getParameterIdentifierInfoAtPosition(signature, signatureParamPos)
		signatureParamPos = signatureParamPos + core.IfElse(spreadArgs > 0, spreadArgs, 1)
		if identifierInfo == nil {
			return
		}
		parameter := identifierInfo.parameter
		parameterName := identifierInfo.name
		isFirstVariadicArgument := identifierInfo.isRestParameter
		parameterNameNotSameAsArgument := s.preferences.IncludeInlayParameterNameHintsWhenArgumentMatchesName.IsTrue() || !identifierOrAccessExpressionPostfixMatchesParameterName(arg, parameterName)
		if !parameterNameNotSameAsArgument && !isFirstVariadicArgument {
			continue
		}
		if s.leadingCommentsContainsParameterName(arg, parameterName) {
			continue
		}
		s.addParameterHints(parameterName, parameter, astnav.GetStartOfNode(originalArg, s.file, false), isFirstVariadicArgument)
	}
}
func (s *inlayHintState) visitEnumMember(member ast.Handle) {
	if !member.Initializer().IsNil() {
		return
	}
	enumValue := s.checker.GetConstantValue(member)
	if enumValue != nil {
		s.addEnumMemberValueHints(evaluator.AnyToString(enumValue), member.End())
	}
}
func (s *inlayHintState) visitVariableLikeDeclaration(decl ast.Handle) {
	if decl.Initializer().IsNil() && !(ast.IsPropertyDeclaration(decl) && s.checker.GetTypeAtLocation(decl).Flags()&checker.TypeFlagsAny == 0) || ast.IsBindingPattern(decl.Name()) || (ast.IsVariableDeclaration(decl) && !isHintableDeclaration(decl)) {
		return
	}
	typeAnnotation := decl.Type()
	if !typeAnnotation.IsNil() {
		return
	}
	declarationType := s.checker.GetTypeAtLocation(decl)
	if isModuleReferenceType(declarationType) {
		return
	}
	hintParts := s.typeToInlayHintParts(declarationType)
	var hintText string
	if hintParts.String != nil {
		hintText = *hintParts.String
	} else if hintParts.InlayHintLabelParts != nil {
		var b strings.Builder
		for _, part := range *hintParts.InlayHintLabelParts {
			b.WriteString(part.Value)
		}
		hintText = b.String()
	}
	if !s.preferences.IncludeInlayVariableTypeHintsWhenTypeMatchesName.IsTrue() && !ast.IsComputedPropertyName(decl.Name()) && stringutil.EquateStringCaseInsensitive(decl.Name().Text(), hintText) {
		return
	}
	s.addTypeHints(hintParts, decl.Name().End())
}
func (s *inlayHintState) visitFunctionLikeForParameterType(node ast.Handle) {
	signature := s.checker.GetSignatureFromDeclaration(node)
	if signature == nil {
		return
	}
	pos := 0
	for _, param := range node.Parameters() {
		if isHintableDeclaration(param) {
			var symbol *ast.Symbol
			if ast.IsThisParameter(param) {
				symbol = signature.ThisParameter()
			} else {
				symbol = signature.Parameters()[pos]
			}
			s.addParameterTypeHint(param, symbol)
		}
		if ast.IsThisParameter(param) {
			continue
		}
		pos++
	}
}
func (s *inlayHintState) addParameterTypeHint(node ast.Handle, symbol *ast.Symbol) {
	typeAnnotation := node.Type()
	if !typeAnnotation.IsNil() || symbol == nil {
		return
	}
	typeHints := s.getParameterDeclarationTypeHints(symbol)
	if typeHints == nil {
		return
	}
	var pos int
	if !node.QuestionToken().IsNil() {
		pos = node.QuestionToken().End()
	} else {
		pos = node.Name().End()
	}
	s.addTypeHints(*typeHints, pos)
}
func (s *inlayHintState) getParameterDeclarationTypeHints(symbol *ast.Symbol) *lsproto.StringOrInlayHintLabelParts {
	valueDeclaration := ast.NodeOf(symbol.ValueDeclaration)
	if valueDeclaration.IsNil() || !ast.IsParameterDeclaration(valueDeclaration) {
		return nil
	}
	signatureParamType := s.checker.GetTypeOfSymbolAtLocation(symbol, valueDeclaration)
	if isModuleReferenceType(signatureParamType) {
		return nil
	}
	return new(s.typeToInlayHintParts(signatureParamType))
}
func (s *inlayHintState) typeToInlayHintParts(t *checker.Type) lsproto.StringOrInlayHintLabelParts {
	flags := nodebuilder.FlagsIgnoreErrors | nodebuilder.FlagsAllowUniqueESSymbolType | nodebuilder.FlagsUseAliasDefinedOutsideCurrentScope
	idToSymbol := make(map[ast.Handle]*ast.Symbol)
	typeNode := s.checker.TypeToTypeNode(t, ast.Handle{}, flags, idToSymbol)
	debug.Assert(!typeNode.IsNil(), "should always get typenode")
	return lsproto.StringOrInlayHintLabelParts{InlayHintLabelParts: new(s.getInlayHintLabelParts(typeNode, idToSymbol))}
}
func (s *inlayHintState) typePredicateToInlayHintParts(typePredicate *checker.TypePredicate) lsproto.StringOrInlayHintLabelParts {
	flags := nodebuilder.FlagsIgnoreErrors | nodebuilder.FlagsAllowUniqueESSymbolType | nodebuilder.FlagsUseAliasDefinedOutsideCurrentScope
	idToSymbol := make(map[ast.Handle]*ast.Symbol)
	typeNode := s.checker.TypePredicateToTypePredicateNode(typePredicate, ast.Handle{}, flags, idToSymbol)
	debug.Assert(!typeNode.IsNil(), "should always get typePredicateNode")
	return lsproto.StringOrInlayHintLabelParts{InlayHintLabelParts: new(s.getInlayHintLabelParts(typeNode, idToSymbol))}
}
func (s *inlayHintState) addTypeHints(hint lsproto.StringOrInlayHintLabelParts, position int) {
	lspPosition, fidelity := s.converters.ToLSPPositionForFeature(s.file, core.TextPos(position), spanmap.FeatureInlayHints)
	if fidelity.IsNone() {
		return
	}
	if hint.String != nil {
		hint.String = new(": " + *hint.String)
	} else {
		hint.InlayHintLabelParts = new(append([]*lsproto.InlayHintLabelPart{{Value: ": "}}, *hint.InlayHintLabelParts...))
	}
	s.result = append(s.result, &lsproto.InlayHint{Label: hint, Position: lspPosition, Kind: new(lsproto.InlayHintKindType), PaddingLeft: new(true)})
}
func (s *inlayHintState) addEnumMemberValueHints(text string, position int) {
	lspPosition, fidelity := s.converters.ToLSPPositionForFeature(s.file, core.TextPos(position), spanmap.FeatureInlayHints)
	if fidelity.IsNone() {
		return
	}
	s.result = append(s.result, &lsproto.InlayHint{Label: lsproto.StringOrInlayHintLabelParts{String: new("= " + text)}, Position: lspPosition, PaddingLeft: new(true)})
}
func (s *inlayHintState) addParameterHints(text string, parameter ast.Handle, position int, isFirstVariadicArgument bool) {
	lspPosition, fidelity := s.converters.ToLSPPositionForFeature(s.file, core.TextPos(position), spanmap.FeatureInlayHints)
	if fidelity.IsNone() {
		return
	}
	hintText := core.IfElse(isFirstVariadicArgument, "...", "") + text
	displayParts := []*lsproto.InlayHintLabelPart{s.getNodeDisplayPart(hintText, parameter), {Value: ":"}}
	labelParts := lsproto.StringOrInlayHintLabelParts{InlayHintLabelParts: &displayParts}
	s.result = append(s.result, &lsproto.InlayHint{Label: labelParts, Position: lspPosition, Kind: new(lsproto.InlayHintKindParameter), PaddingRight: new(true)})
}
func shouldShowParameterNameHints(preferences lsutil.InlayHintsPreferences) bool {
	return (preferences.IncludeInlayParameterNameHints == lsutil.IncludeInlayParameterNameHintsLiterals || preferences.IncludeInlayParameterNameHints == lsutil.IncludeInlayParameterNameHintsAll)
}
func shouldShowLiteralParameterNameHintsOnly(preferences lsutil.InlayHintsPreferences) bool {
	return preferences.IncludeInlayParameterNameHints == lsutil.IncludeInlayParameterNameHintsLiterals
}

func isSignatureSupportingReturnAnnotation(node ast.Handle) bool {
	return ast.IsArrowFunction(node) || ast.IsFunctionExpression(node) || ast.IsFunctionDeclaration(node) || ast.IsMethodDeclaration(node) || ast.IsGetAccessorDeclaration(node)
}
func isHintableDeclaration(node ast.Handle) bool {
	if (ast.IsPartOfParameterDeclaration(node) || ast.IsVariableDeclaration(node) && ast.IsVarConst(node)) && !node.Initializer().IsNil() {
		initializer := ast.SkipParentheses(node.Initializer())
		return !(isHintableLiteral(initializer) || ast.IsNewExpression(initializer) || ast.IsObjectLiteralExpression(initializer) || ast.IsAssertionExpression(initializer))
	}
	return true
}
func isHintableLiteral(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindPrefixUnaryExpression:
		operand := node.PrefixUnaryExpressionOperand()
		return ast.IsLiteralExpression(operand) || ast.IsIdentifier(operand) && ast.IsInfinityOrNaNString(operand.Text())
	case ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
		return true
	case ast.KindIdentifier:
		name := node.Text()
		return name == "undefined" || ast.IsInfinityOrNaNString(name)
	}
	return ast.IsLiteralExpression(node)
}
func isModuleReferenceType(t *checker.Type) bool {
	symbol := t.Symbol()
	return symbol != nil && symbol.Flags&ast.SymbolFlagsModule != 0
}
func (s *inlayHintState) getInlayHintLabelParts(node ast.Handle, idToSymbol map[ast.Handle]*ast.Symbol) []*lsproto.InlayHintLabelPart {
	var parts []*lsproto.InlayHintLabelPart
	var visitForDisplayParts func(node ast.Handle)
	var visitDisplayPartList func(nodes []ast.Handle, separator string)
	var visitParametersAndTypeParameters func(node ast.Handle)
	visitForDisplayParts = func(node ast.Handle) {
		if node.IsNil() {
			return
		}
		tokenString := scanner.TokenToString(node.Kind)
		if tokenString != "" {
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: tokenString})
			return
		}
		if ast.IsLiteralExpression(node) {
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: s.getLiteralText(node)})
			return
		}
		switch node.Kind {
		case ast.KindIdentifier:
			identifierText := node.Text()
			var name ast.Handle
			if symbol := idToSymbol[node]; symbol != nil && len(symbol.Declarations) != 0 {
				name = ast.GetNameOfDeclaration(ast.NodeOf(symbol.Declarations[0]))
			}
			if !name.IsNil() {
				parts = append(parts, s.getNodeDisplayPart(identifierText, name))
			} else {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: identifierText})
			}
		case ast.KindQualifiedName:
			visitForDisplayParts(node.QualifiedNameLeft())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "."})
			visitForDisplayParts(node.QualifiedNameRight())
		case ast.KindTypePredicate:
			if !node.TypePredicateNodeAssertsModifier().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "asserts "})
			}
			visitForDisplayParts(node.TypePredicateNodeParameterName())
			if !node.Type().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " is "})
				visitForDisplayParts(node.Type())
			}
		case ast.KindTypeReference:
			visitForDisplayParts(node.TypeReferenceNodeTypeName())
			if len(node.TypeArguments()) > 0 {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "<"})
				visitDisplayPartList(node.TypeArguments(), ",")
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ">"})
			}
		case ast.KindTypeParameter:
			if len(node.ModifierNodes()) > 0 {
				visitDisplayPartList(node.ModifierNodes(), "")
			}
			visitForDisplayParts(node.Name())
			if !node.TypeParameterDeclarationConstraint().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " extends "})
				visitForDisplayParts(node.TypeParameterDeclarationConstraint())
			}
			if !node.TypeParameterDeclarationDefaultType().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " = "})
				visitForDisplayParts(node.TypeParameterDeclarationDefaultType())
			}
		case ast.KindParameter:
			if len(node.ModifierNodes()) > 0 {
				visitDisplayPartList(node.ModifierNodes(), " ")
			}
			if !node.ParameterDeclarationDotDotDotToken().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "..."})
			}
			visitForDisplayParts(node.Name())
			if !node.QuestionToken().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "?"})
			}
			if !node.Type().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
				visitForDisplayParts(node.Type())
			}
		case ast.KindConstructorType:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "new "})
			visitParametersAndTypeParameters(node)
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: " => "})
			visitForDisplayParts(node.Type())
		case ast.KindTypeQuery:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "typeof "})
			visitForDisplayParts(node.TypeQueryNodeExprName())
			if len(node.TypeArguments()) > 0 {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "<"})
				visitDisplayPartList(node.TypeArguments(), ", ")
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ">"})
			}
		case ast.KindTypeLiteral:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "{"})
			if len(node.Members()) > 0 {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " "})
				visitDisplayPartList(node.Members(), "; ")
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " "})
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "}"})
		case ast.KindArrayType:
			visitForDisplayParts(node.ArrayTypeNodeElementType())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "[]"})
		case ast.KindTupleType:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "["})
			visitDisplayPartList(node.Elements(), ", ")
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "]"})
		case ast.KindNamedTupleMember:
			if !node.NamedTupleMemberDotDotDotToken().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "..."})
			}
			visitForDisplayParts(node.Name())
			if !node.QuestionToken().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "?"})
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
			visitForDisplayParts(node.Type())
		case ast.KindOptionalType:
			visitForDisplayParts(node.Type())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "?"})
		case ast.KindRestType:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "..."})
			visitForDisplayParts(node.Type())
		case ast.KindUnionType:
			if node.UnionTypeNodeTypes() != 0 {
				visitDisplayPartList(node.Store().ListSlice(node.UnionTypeNodeTypes()), " | ")
			}
		case ast.KindIntersectionType:
			if node.IntersectionTypeNodeTypes() != 0 {
				visitDisplayPartList(node.Store().ListSlice(node.IntersectionTypeNodeTypes()), " & ")
			}
		case ast.KindConditionalType:
			visitForDisplayParts(node.ConditionalTypeNodeCheckType())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: " extends "})
			visitForDisplayParts(node.ConditionalTypeNodeExtendsType())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: " ? "})
			visitForDisplayParts(node.ConditionalTypeNodeTrueType())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: " : "})
			visitForDisplayParts(node.ConditionalTypeNodeFalseType())
		case ast.KindInferType:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "infer "})
			visitForDisplayParts(node.InferTypeNodeTypeParameter())
		case ast.KindParenthesizedType:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "("})
			visitForDisplayParts(node.Type())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: ")"})
		case ast.KindTypeOperator:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: scanner.TokenToString(node.TypeOperatorNodeOperator())})
			visitForDisplayParts(node.Type())
		case ast.KindIndexedAccessType:
			visitForDisplayParts(node.IndexedAccessTypeNodeObjectType())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "["})
			visitForDisplayParts(node.IndexedAccessTypeNodeIndexType())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "]"})
		case ast.KindMappedType:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "{ "})
			if !node.MappedTypeNodeReadonlyToken().IsNil() {
				if node.MappedTypeNodeReadonlyToken().Kind == ast.KindPlusToken {
					parts = append(parts, &lsproto.InlayHintLabelPart{Value: "+"})
				} else if node.MappedTypeNodeReadonlyToken().Kind == ast.KindMinusToken {
					parts = append(parts, &lsproto.InlayHintLabelPart{Value: "-"})
				}
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "readonly "})
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "["})
			visitForDisplayParts(node.MappedTypeNodeTypeParameter())
			if !node.MappedTypeNodeNameType().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " as "})
				visitForDisplayParts(node.MappedTypeNodeNameType())
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "]"})
			if !node.QuestionToken().IsNil() {
				if node.QuestionToken().Kind == ast.KindPlusToken {
					parts = append(parts, &lsproto.InlayHintLabelPart{Value: "+"})
				} else if node.QuestionToken().Kind == ast.KindMinusToken {
					parts = append(parts, &lsproto.InlayHintLabelPart{Value: "-"})
				}
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "?"})
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
			if !node.Type().IsNil() {
				visitForDisplayParts(node.Type())
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "; }"})
		case ast.KindLiteralType:
			visitForDisplayParts(node.LiteralTypeNodeLiteral())
		case ast.KindFunctionType:
			visitParametersAndTypeParameters(node)
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: " => "})
			visitForDisplayParts(node.Type())
		case ast.KindImportType:
			if node.ImportTypeNodeIsTypeOf() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "typeof "})
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "import("})
			visitForDisplayParts(node.ImportTypeNodeArgument())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: ")"})
			if !node.ImportTypeNodeQualifier().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "."})
				visitForDisplayParts(node.ImportTypeNodeQualifier())
			}
			if len(node.TypeArguments()) > 0 {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: "<"})
				visitDisplayPartList(node.TypeArguments(), ", ")
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ">"})
			}
		case ast.KindPropertySignature:
			if len(node.ModifierNodes()) > 0 {
				visitDisplayPartList(node.ModifierNodes(), " ")
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " "})
			}
			visitForDisplayParts(node.Name())
			if !node.PostfixToken().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: scanner.TokenToString(node.PostfixToken().Kind)})
			}
			if !node.Type().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
				visitForDisplayParts(node.Type())
			}
		case ast.KindIndexSignature:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "["})
			visitDisplayPartList(node.Parameters(), ", ")
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "]"})
			if !node.Type().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
				visitForDisplayParts(node.Type())
			}
		case ast.KindMethodSignature:
			if len(node.ModifierNodes()) > 0 {
				visitDisplayPartList(node.ModifierNodes(), " ")
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " "})
			}
			visitForDisplayParts(node.Name())
			if !node.PostfixToken().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: scanner.TokenToString(node.PostfixToken().Kind)})
			}
			visitParametersAndTypeParameters(node)
			if !node.Type().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
				visitForDisplayParts(node.Type())
			}
		case ast.KindCallSignature:
			visitParametersAndTypeParameters(node)
			if !node.Type().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
				visitForDisplayParts(node.Type())
			}
		case ast.KindConstructSignature:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "new "})
			visitParametersAndTypeParameters(node)
			if !node.Type().IsNil() {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: ": "})
				visitForDisplayParts(node.Type())
			}
		case ast.KindArrayBindingPattern:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "["})
			visitDisplayPartList(node.Elements(), ", ")
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "]"})
		case ast.KindObjectBindingPattern:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "{"})
			if len(node.Elements()) > 0 {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " "})
				visitDisplayPartList(node.Elements(), ", ")
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: " "})
			}
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "}"})
		case ast.KindBindingElement:
			visitForDisplayParts(node.Name())
		case ast.KindPrefixUnaryExpression:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: scanner.TokenToString(node.PrefixUnaryExpressionOperator())})
			visitForDisplayParts(node.PrefixUnaryExpressionOperand())
		case ast.KindTemplateLiteralType:
			visitForDisplayParts(node.TemplateLiteralTypeNodeHead())
			for _, span := range node.Store().ListSlice(node.TemplateLiteralTypeNodeTemplateSpans()) {
				visitForDisplayParts(span)
			}
		case ast.KindTemplateHead:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: s.getLiteralText(node)})
		case ast.KindTemplateLiteralTypeSpan:
			visitForDisplayParts(node.Type())
			visitForDisplayParts(node.TemplateLiteralTypeSpanLiteral())
		case ast.KindTemplateMiddle, ast.KindTemplateTail:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: s.getLiteralText(node)})
		case ast.KindThisType:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "this"})
		case ast.KindComputedPropertyName:
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "["})
			visitForDisplayParts(node.Expression())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "]"})
		case ast.KindPropertyAccessExpression:
			visitForDisplayParts(node.Expression())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "."})
			visitForDisplayParts(node.Name())
		case ast.KindElementAccessExpression:
			visitForDisplayParts(node.Expression())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "["})
			visitForDisplayParts(node.ElementAccessExpressionArgumentExpression())
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "]"})
		default:
			debug.FailBadSyntaxKind(node)
		}
	}
	visitDisplayPartList = func(nodes []ast.Handle, separator string) {
		for i, n := range nodes {
			if i > 0 {
				parts = append(parts, &lsproto.InlayHintLabelPart{Value: separator})
			}
			visitForDisplayParts(n)
		}
	}
	visitParametersAndTypeParameters = func(node ast.Handle) {
		if len(node.TypeParameters()) > 0 {
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: "<"})
			visitDisplayPartList(node.TypeParameters(), ", ")
			parts = append(parts, &lsproto.InlayHintLabelPart{Value: ">"})
		}
		parts = append(parts, &lsproto.InlayHintLabelPart{Value: "("})
		visitDisplayPartList(node.Parameters(), ", ")
		parts = append(parts, &lsproto.InlayHintLabelPart{Value: ")"})
	}
	visitForDisplayParts(node)
	return parts
}
func (s *inlayHintState) getNodeDisplayPart(text string, node ast.Handle) *lsproto.InlayHintLabelPart {
	file := ast.GetSourceFileOfNode(node)
	pos := astnav.GetStartOfNode(node, file, false)
	end := node.End()
	part := &lsproto.InlayHintLabelPart{Value: text}
	if lspRange, fidelity := s.converters.ToLSPRangeForFeature(file, core.NewTextRange(pos, end), spanmap.FeatureInlayHints); fidelity.IsSingleSegment() {
		part.Location = &lsproto.Location{Uri: lsconv.FileNameToDocumentURI(file.OriginalFileName()), Range: lspRange}
	}
	return part
}
func (s *inlayHintState) getLiteralText(node ast.Handle) string {
	switch node.Kind {
	case ast.KindStringLiteral:
		if s.quotePreference == lsutil.QuotePreferenceSingle {
			return `'` + printer.EscapeString(node.Text(), printer.QuoteCharSingleQuote) + `'`
		}
		return `"` + printer.EscapeString(node.Text(), printer.QuoteCharDoubleQuote) + `"`
	case ast.KindTemplateHead, ast.KindTemplateMiddle, ast.KindTemplateTail:
		rawText := node.RawText()
		if rawText == "" {
			rawText = printer.EscapeString(node.Text(), printer.QuoteCharBacktick)
		}
		switch node.Kind {
		case ast.KindTemplateHead:
			return "`" + rawText + "${"
		case ast.KindTemplateMiddle:
			return "}" + rawText + "${"
		case ast.KindTemplateTail:
			return "}" + rawText + "`"
		}
	}
	return node.Text()
}

type parameterInfo struct {
	parameter       ast.Handle
	name            string
	isRestParameter bool
}

func (s *inlayHintState) getParameterIdentifierInfoAtPosition(signature *checker.Signature, pos int) *parameterInfo {
	parameters := signature.Parameters()
	paramCount := len(parameters) - core.IfElse(signature.HasRestParameter(), 1, 0)
	if pos < paramCount {
		param := parameters[pos]
		paramId := getParameterDeclarationIdentifier(param)
		if paramId.IsNil() {
			return nil
		}
		return &parameterInfo{parameter: paramId, name: paramId.Text(), isRestParameter: false}
	}
	var restParameter *ast.Symbol
	var restId ast.Handle
	if paramCount < len(parameters) {
		restParameter = parameters[paramCount]
		restId = getParameterDeclarationIdentifier(restParameter)
	}
	if restId.IsNil() {
		return nil
	}
	restType := s.checker.GetTypeOfSymbol(restParameter)
	if restType.IsTupleType() {
		associatedNames := make([]ast.Handle, 0, len(restType.Target().AsTupleType().ElementInfos()))
		for _, elementInfo := range restType.Target().AsTupleType().ElementInfos() {
			labeledElement := elementInfo.LabeledDeclaration()
			associatedNames = append(associatedNames, labeledElement)
		}
		index := pos - paramCount
		if index < len(associatedNames) {
			associatedName := associatedNames[index]
			if !associatedName.IsNil() {
				debug.Assert(ast.IsIdentifier(associatedName.Name()))
				var isRestTupleElement bool
				if ast.IsNamedTupleMember(associatedName) {
					isRestTupleElement = !associatedName.NamedTupleMemberDotDotDotToken().IsNil()
				} else {
					isRestTupleElement = !associatedName.ParameterDeclarationDotDotDotToken().IsNil()
				}
				return &parameterInfo{parameter: associatedName.Name(), name: associatedName.Name().Text(), isRestParameter: isRestTupleElement}
			}
		}
		return nil
	}
	if pos == paramCount {
		return &parameterInfo{parameter: restId, name: restParameter.Name, isRestParameter: true}
	}
	return nil
}
func getParameterDeclarationIdentifier(symbol *ast.Symbol) ast.Handle {
	if symbol.ValueDeclaration != 0 && ast.IsParameterDeclaration(ast.NodeOf(symbol.ValueDeclaration)) && ast.IsIdentifier(ast.NodeOf(symbol.ValueDeclaration).Name()) {
		return ast.NodeOf(symbol.ValueDeclaration).Name()
	}
	return ast.Handle{}
}
func identifierOrAccessExpressionPostfixMatchesParameterName(expr ast.Handle, parameterName string) bool {
	if ast.IsIdentifier(expr) {
		return expr.Text() == parameterName
	}
	if ast.IsPropertyAccessExpression(expr) {
		return expr.Name().Text() == parameterName
	}
	return false
}
func (s *inlayHintState) leadingCommentsContainsParameterName(node ast.Handle, name string) bool {
	if !scanner.IsIdentifierText(name, s.file.LanguageVariant) {
		return false
	}
	ranges := getLeadingCommentRangesOfNode(node, s.file)
	fileText := s.file.Text()
	for r := range ranges {
		commentText := strings.TrimFunc(fileText[r.Pos():r.End()], func(r rune) bool {
			return unicode.IsSpace(r) || r == '/' || r == '*'
		})
		if commentText == name {
			return true
		}
	}
	return false
}
func (s *inlayHintState) getTypeAnnotationPosition(decl ast.Handle) int {
	closeParenToken := astnav.FindChildOfKind(decl, ast.KindCloseParenToken, s.file)
	if !closeParenToken.IsNil() {
		return closeParenToken.End()
	}
	return decl.Store().ListLoc(decl.ParameterList()).End()
}
func isAnyInlayHintEnabled(preferences lsutil.InlayHintsPreferences) bool {
	return preferences.IncludeInlayParameterNameHints != lsutil.IncludeInlayParameterNameHintsNone || preferences.IncludeInlayFunctionParameterTypeHints.IsTrue() || preferences.IncludeInlayVariableTypeHints.IsTrue() || preferences.IncludeInlayPropertyDeclarationTypeHints.IsTrue() || preferences.IncludeInlayFunctionLikeReturnTypeHints.IsTrue() || preferences.IncludeInlayEnumMemberValueHints.IsTrue()
}
