package ast

import "sync/atomic"

const valueSlotSubtreeFactsCache = 31

func (h Handle) SubtreeFacts() SubtreeFacts {
	if h.IsNil() {
		return SubtreeFactsNone
	}
	if h.s.frozenAt != 0 && h.id < h.s.frozenAt {
		cached := atomic.LoadUint32(&h.s.subtreeFacts[h.id])
		if SubtreeFacts(cached)&SubtreeFactsComputed != 0 {
			return SubtreeFacts(cached) &^ SubtreeFactsComputed
		}
		facts := h.computeSubtreeFacts()
		atomic.StoreUint32(&h.s.subtreeFacts[h.id], uint32(facts|SubtreeFactsComputed))
		return facts
	}
	cached := h.UintValue(valueSlotSubtreeFactsCache)
	if SubtreeFacts(cached)&SubtreeFactsComputed != 0 {
		return SubtreeFacts(cached) &^ SubtreeFactsComputed
	}
	facts := h.computeSubtreeFacts()
	h.SetUintValue(valueSlotSubtreeFactsCache, uint64(facts|SubtreeFactsComputed))
	return facts
}

func (h Handle) propagateSubtreeFacts() SubtreeFacts {
	if h.IsNil() {
		return SubtreeFactsNone
	}
	if isTypeSyntaxKind(h.Kind()) {
		return SubtreeContainsTypeScript
	}
	facts := h.SubtreeFacts() &^ handleSubtreeExclusion(h.Kind())
	switch h.Kind() {
	case KindMethodDeclaration, KindGetAccessor, KindSetAccessor, KindPropertyDeclaration:
		facts |= propagateHandle(h.Name())
	}
	return facts
}

func propagateHandle(child Handle) SubtreeFacts {
	if child.IsNil() {
		return SubtreeFactsNone
	}
	return child.propagateSubtreeFacts()
}

func propagateHandleList(nodes []Handle, each func(Handle) SubtreeFacts) SubtreeFacts {
	facts := SubtreeFactsNone
	for _, n := range nodes {
		facts |= each(n)
	}
	return facts
}

func propagateObjectBindingElementHandle(child Handle) SubtreeFacts {
	facts := propagateHandle(child)
	if facts&SubtreeContainsRestOrSpread != 0 {
		facts &^= SubtreeContainsRestOrSpread
		facts |= SubtreeContainsObjectRestOrSpread | SubtreeContainsESObjectRestOrSpread
	}
	return facts
}

func propagateArrayBindingElementHandle(child Handle) SubtreeFacts {
	return propagateHandle(child) &^ SubtreeContainsRestOrSpread
}

func propagateChildren(h Handle) SubtreeFacts {
	facts := SubtreeFactsNone
	h.ForEachChild(func(c Handle) bool {
		facts |= propagateHandle(c)
		return false
	})
	return facts
}

func isTypeSyntaxKind(kind Kind) bool {
	switch kind {
	case KindTypeParameter,
		KindInterfaceDeclaration,
		KindTypeAliasDeclaration,
		KindJSTypeAliasDeclaration,
		KindCallSignature,
		KindConstructSignature,
		KindIndexSignature,
		KindMethodSignature,
		KindPropertySignature:
		return true
	}
	return kind >= KindFirstTypeNode && kind <= KindLastTypeNode
}

func handleSubtreeExclusion(kind Kind) SubtreeFacts {
	switch kind {
	case KindArrowFunction:
		return SubtreeExclusionsArrowFunction
	case KindFunctionDeclaration, KindFunctionExpression:
		return SubtreeExclusionsFunction
	case KindConstructor:
		return SubtreeExclusionsConstructor
	case KindMethodDeclaration:
		return SubtreeExclusionsMethod
	case KindGetAccessor, KindSetAccessor:
		return SubtreeExclusionsAccessor
	case KindPropertyDeclaration:
		return SubtreeExclusionsProperty
	case KindClassDeclaration, KindClassExpression:
		return SubtreeExclusionsClass
	case KindModuleDeclaration:
		return SubtreeExclusionsModule
	case KindObjectLiteralExpression:
		return SubtreeExclusionsObjectLiteral
	case KindArrayLiteralExpression:
		return SubtreeExclusionsArrayLiteral
	case KindCallExpression:
		return SubtreeExclusionsCall
	case KindNewExpression:
		return SubtreeExclusionsNew
	case KindVariableDeclarationList:
		return SubtreeExclusionsVariableDeclarationList
	case KindParameter:
		return SubtreeExclusionsParameter
	case KindCatchClause:
		return SubtreeExclusionsCatchClause
	case KindObjectBindingPattern, KindArrayBindingPattern:
		return SubtreeExclusionsBindingPattern
	case KindAsExpression, KindSatisfiesExpression, KindTypeAssertionExpression, KindParenthesizedExpression, KindPartiallyEmittedExpression:
		return SubtreeExclusionsOuterExpression
	case KindPropertyAccessExpression:
		return SubtreeExclusionsPropertyAccess
	case KindElementAccessExpression:
		return SubtreeExclusionsElementAccess
	}
	return SubtreeExclusionsNode
}

func tokenSubtreeFacts(kind Kind) SubtreeFacts {
	switch kind {
	case KindUsingKeyword:
		return SubtreeContainsUsing
	case KindPublicKeyword, KindPrivateKeyword, KindProtectedKeyword, KindReadonlyKeyword,
		KindAbstractKeyword, KindDeclareKeyword, KindConstKeyword, KindAnyKeyword,
		KindNumberKeyword, KindBigIntKeyword, KindNeverKeyword, KindObjectKeyword,
		KindInKeyword, KindOutKeyword, KindOverrideKeyword, KindStringKeyword,
		KindBooleanKeyword, KindSymbolKeyword, KindVoidKeyword, KindUnknownKeyword,
		KindUndefinedKeyword, KindExportKeyword:
		return SubtreeContainsTypeScript
	case KindAccessorKeyword:
		return SubtreeContainsClassFields
	case KindAsyncKeyword:
		return SubtreeContainsAnyAwait
	case KindSuperKeyword:
		return SubtreeContainsLexicalSuper
	case KindThisKeyword:
		return SubtreeContainsLexicalThis
	case KindAsteriskAsteriskToken, KindAsteriskAsteriskEqualsToken:
		return SubtreeContainsExponentiationOperator
	case KindQuestionQuestionToken:
		return SubtreeContainsNullishCoalescing
	case KindQuestionDotToken:
		return SubtreeContainsOptionalChaining
	case KindQuestionQuestionEqualsToken, KindBarBarEqualsToken, KindAmpersandAmpersandEqualsToken:
		return SubtreeContainsLogicalAssignments
	}
	return SubtreeFactsNone
}

func invalidTemplateEscape(h Handle) SubtreeFacts {
	if h.TokenFlags()&TokenFlagsContainsInvalidEscape != 0 {
		return SubtreeContainsInvalidTemplateEscape
	}
	return SubtreeFactsNone
}

func (h Handle) computeSubtreeFacts() SubtreeFacts {
	kind := h.Kind()
	if isTypeSyntaxKind(kind) {
		return SubtreeContainsTypeScript
	}
	switch kind {
	case KindIdentifier:
		return SubtreeContainsIdentifier
	case KindPrivateIdentifier:
		return SubtreeContainsClassFields
	case KindNoSubstitutionTemplateLiteral, KindTemplateHead, KindTemplateMiddle, KindTemplateTail:
		return invalidTemplateEscape(h)
	case KindJsxText, KindJsxTextAllWhiteSpaces:
		return SubtreeContainsJsx
	}
	if IsTokenKind(kind) {
		return tokenSubtreeFacts(kind)
	}
	switch kind {
	case KindDecorator:
		return propagateHandle(h.Expression()) | SubtreeContainsTypeScript | SubtreeContainsDecorators
	case KindForInStatement, KindForOfStatement:
		facts := propagateHandle(h.Initializer()) | propagateHandle(h.Expression()) | propagateHandle(h.ForInOrOfStatementStatement())
		if !h.ForInOrOfStatementAwaitModifier().IsNil() {
			facts |= SubtreeContainsForAwaitOrAsyncGenerator
		}
		return facts
	case KindReturnStatement:
		return propagateHandle(h.Expression()) | SubtreeContainsForAwaitOrAsyncGenerator
	case KindCatchClause:
		decl := h.CatchClauseVariableDeclaration()
		facts := propagateHandle(decl) | propagateHandle(h.CatchClauseBlock())
		if decl.IsNil() {
			facts |= SubtreeContainsMissingCatchClauseVariable
		}
		return facts
	case KindVariableStatement:
		if h.ModifierFlags()&ModifierFlagsAmbient != 0 {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) | propagateHandle(h.VariableStatementDeclarationList())
	case KindVariableDeclaration:
		return propagateHandle(h.Name()) |
			eraseableHandle(h.VariableDeclarationExclamationToken()) |
			eraseableHandle(h.Type()) |
			propagateHandle(h.Initializer())
	case KindVariableDeclarationList:
		facts := propagateHandleList(h.Declarations(), propagateHandle)
		if h.Flags()&NodeFlagsUsing != 0 {
			facts |= SubtreeContainsUsing
		}
		return facts
	case KindObjectBindingPattern:
		return propagateHandleList(h.Elements(), propagateObjectBindingElementHandle)
	case KindArrayBindingPattern:
		return propagateHandleList(h.Elements(), propagateArrayBindingElementHandle)
	case KindParameter:
		name := h.Name()
		if !name.IsNil() && name.Kind() == KindIdentifier && name.Text() == "this" {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(name) |
			eraseableHandle(h.QuestionToken()) |
			eraseableHandle(h.Type()) |
			propagateHandle(h.Initializer())
	case KindBindingElement:
		facts := propagateHandle(h.PropertyName()) | propagateHandle(h.Name()) | propagateHandle(h.Initializer())
		if !h.DotDotDotToken().IsNil() {
			facts |= SubtreeContainsRestOrSpread
		}
		return facts
	case KindFunctionDeclaration:
		if h.Body().IsNil() || h.ModifierFlags()&ModifierFlagsAmbient != 0 {
			return SubtreeContainsTypeScript
		}
		return functionLikeFacts(h, true)
	case KindFunctionExpression:
		return functionLikeFacts(h, true)
	case KindArrowFunction:
		return functionLikeFacts(h, false) | awaitOnly(h.ModifierFlags()&ModifierFlagsAsync != 0)
	case KindClassDeclaration, KindClassExpression:
		if h.ModifierFlags()&ModifierFlagsAmbient != 0 {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(h.Name()) |
			eraseableList(h.TypeParameters()) |
			propagateHandleList(h.ListSlice(h.HeritageClauses()), propagateHandle) |
			propagateHandleList(h.Members(), propagateHandle)
	case KindHeritageClause:
		if h.HeritageClauseToken() == KindImplementsKeyword {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.Types(), propagateHandle)
	case KindEnumMember:
		return propagateHandle(h.Name()) | propagateHandle(h.Initializer()) | SubtreeContainsTypeScript
	case KindEnumDeclaration:
		if h.ModifierFlags()&ModifierFlagsAmbient != 0 {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(h.Name()) |
			propagateHandleList(h.Members(), propagateHandle) |
			SubtreeContainsTypeScript
	case KindModuleDeclaration:
		if h.ModifierFlags()&ModifierFlagsAmbient != 0 {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(h.Name()) |
			propagateHandle(h.Body()) |
			SubtreeContainsTypeScript
	case KindImportEqualsDeclaration:
		ref := h.ImportEqualsDeclarationModuleReference()
		if h.IsTypeOnly() || ref.IsNil() || ref.Kind() != KindExternalModuleReference {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) | propagateHandle(h.Name()) | propagateHandle(ref)
	case KindImportSpecifier, KindExportSpecifier:
		if h.IsTypeOnly() {
			return SubtreeContainsTypeScript
		}
		return propagateHandle(h.PropertyName()) | propagateHandle(h.Name())
	case KindImportClause:
		if h.ImportClausePhaseModifier() == KindTypeKeyword {
			return SubtreeContainsTypeScript
		}
		return propagateHandle(h.Name()) | propagateHandle(h.ImportClauseNamedBindings())
	case KindExportAssignment:
		facts := propagateHandleList(h.ModifierNodes(), propagateHandle) | propagateHandle(h.Type()) | propagateHandle(h.Expression())
		if h.ExportAssignmentIsExportEquals() {
			facts |= SubtreeContainsTypeScript
		}
		return facts
	case KindExportDeclaration:
		facts := propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(h.ExportDeclarationExportClause()) |
			propagateHandle(h.ModuleSpecifier()) |
			propagateHandle(h.ExportDeclarationAttributes())
		if h.IsTypeOnly() {
			facts |= SubtreeContainsTypeScript
		}
		return facts
	case KindNamespaceExportDeclaration:
		return SubtreeContainsTypeScript
	case KindConstructor:
		if h.Body().IsNil() {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			eraseableList(h.TypeParameters()) |
			propagateHandleList(h.Parameters(), propagateHandle) |
			eraseableHandle(h.Type()) |
			eraseableHandle(h.FullSignature()) |
			propagateHandle(h.Body())
	case KindGetAccessor, KindSetAccessor:
		if h.Body().IsNil() {
			return SubtreeContainsTypeScript
		}
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(h.Name()) |
			eraseableList(h.TypeParameters()) |
			propagateHandleList(h.Parameters(), propagateHandle) |
			eraseableHandle(h.Type()) |
			eraseableHandle(h.FullSignature()) |
			propagateHandle(h.Body())
	case KindMethodDeclaration:
		if h.Body().IsNil() {
			return SubtreeContainsTypeScript
		}
		return functionLikeFacts(h, true) | eraseableHandle(h.QuestionToken())
	case KindPropertyDeclaration:
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(h.Name()) |
			eraseableHandle(h.QuestionToken()) |
			eraseableHandle(h.Type()) |
			propagateHandle(h.Initializer()) |
			SubtreeContainsClassFields
	case KindClassStaticBlockDeclaration:
		return propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(h.Body()) |
			SubtreeContainsClassFields
	case KindBinaryExpression:
		op := h.Operator()
		left := h.Left()
		facts := propagateHandleList(h.ModifierNodes(), propagateHandle) |
			propagateHandle(left) |
			propagateHandle(h.Type()) |
			propagateHandle(op) |
			propagateHandle(h.Right())
		if !op.IsNil() && op.Kind() == KindInKeyword && !left.IsNil() && left.Kind() == KindPrivateIdentifier {
			facts |= SubtreeContainsClassFields | SubtreeContainsPrivateIdentifierInExpression
		}
		if !op.IsNil() && op.Kind() == KindEqualsToken && !left.IsNil() &&
			(left.Kind() == KindObjectLiteralExpression || left.Kind() == KindArrayLiteralExpression) &&
			left.SubtreeFacts()&(SubtreeContainsObjectRestOrSpread|SubtreeContainsESObjectRestOrSpread) != 0 {
			facts |= SubtreeContainsObjectRestOrSpread
		}
		return facts
	case KindYieldExpression:
		return propagateHandle(h.Expression()) | SubtreeContainsForAwaitOrAsyncGenerator
	case KindAsExpression, KindSatisfiesExpression, KindNonNullExpression, KindTypeAssertionExpression:
		return propagateHandle(h.Expression()) | SubtreeContainsTypeScript
	case KindPropertyAccessExpression:
		name := h.Name()
		privateName := SubtreeFactsNone
		if name.IsNil() || name.Kind() != KindIdentifier {
			privateName = SubtreeContainsPrivateIdentifierInExpression
		}
		return propagateHandle(h.Expression()) | propagateHandle(h.QuestionDotToken()) | propagateHandle(name) | privateName
	case KindCallExpression:
		facts := propagateHandle(h.Expression()) |
			propagateHandle(h.QuestionDotToken()) |
			eraseableList(h.TypeArguments()) |
			propagateHandleList(h.Arguments(), propagateHandle)
		if h.Expression().Kind() == KindImportKeyword {
			facts |= SubtreeContainsDynamicImport
		}
		return facts
	case KindNewExpression:
		return propagateHandle(h.Expression()) | eraseableList(h.TypeArguments()) | propagateHandleList(h.Arguments(), propagateHandle)
	case KindMetaProperty:
		return propagateHandle(h.Name()) &^ SubtreeContainsIdentifier
	case KindSpreadElement:
		return propagateHandle(h.Expression()) | SubtreeContainsRestOrSpread
	case KindSpreadAssignment:
		return propagateHandle(h.Expression()) | SubtreeContainsESObjectRestOrSpread | SubtreeContainsObjectRestOrSpread
	case KindTaggedTemplateExpression:
		return propagateHandle(h.TaggedTemplateExpressionTag()) |
			propagateHandle(h.QuestionDotToken()) |
			eraseableList(h.TypeArguments()) |
			propagateHandle(h.TaggedTemplateExpressionTemplate())
	case KindPropertyAssignment:
		return propagateHandle(h.Name()) | propagateHandle(h.Type()) | propagateHandle(h.Initializer())
	case KindShorthandPropertyAssignment:
		return propagateHandle(h.Name()) |
			propagateHandle(h.Type()) |
			propagateHandle(h.ShorthandPropertyAssignmentObjectAssignmentInitializer()) |
			SubtreeContainsTypeScript
	case KindAwaitExpression:
		return propagateHandle(h.Expression()) | SubtreeContainsAwait | SubtreeContainsAnyAwait | SubtreeContainsForAwaitOrAsyncGenerator
	case KindExpressionWithTypeArguments:
		return propagateHandle(h.Expression()) | eraseableList(h.TypeArguments())
	case KindJsxElement:
		return propagateHandle(h.JsxElementOpeningElement()) |
			propagateHandleList(h.ListSlice(h.JsxElementChildren()), propagateHandle) |
			propagateHandle(h.JsxElementClosingElement()) |
			SubtreeContainsJsx
	case KindJsxAttributes:
		return propagateHandleList(h.Properties(), propagateHandle) | SubtreeContainsJsx
	case KindJsxNamespacedName:
		return propagateHandle(h.JsxNamespacedNameNamespace()) | propagateHandle(h.Name()) | SubtreeContainsJsx
	case KindJsxOpeningElement, KindJsxSelfClosingElement:
		return propagateHandle(h.TagName()) | eraseableList(h.TypeArguments()) | propagateHandle(jsxAttributesOf(h)) | SubtreeContainsJsx
	case KindJsxFragment:
		return propagateHandleList(h.Children(), propagateHandle) | SubtreeContainsJsx
	case KindJsxOpeningFragment, KindJsxClosingFragment:
		return SubtreeContainsJsx
	case KindJsxAttribute:
		return propagateHandle(h.Name()) | propagateHandle(h.Initializer()) | SubtreeContainsJsx
	case KindJsxSpreadAttribute:
		return propagateHandle(h.Expression()) | SubtreeContainsJsx
	case KindJsxClosingElement:
		return propagateHandle(h.TagName()) | SubtreeContainsJsx
	case KindJsxExpression:
		return propagateHandle(h.Expression()) | SubtreeContainsJsx
	}
	return propagateChildren(h)
}

func jsxAttributesOf(h Handle) Handle {
	switch h.Kind() {
	case KindJsxOpeningElement:
		return h.JsxOpeningElementAttributes()
	case KindJsxSelfClosingElement:
		return h.JsxSelfClosingElementAttributes()
	}
	return Handle{}
}

func eraseableHandle(child Handle) SubtreeFacts {
	if child.IsNil() {
		return SubtreeFactsNone
	}
	return SubtreeContainsTypeScript
}

func eraseableList(nodes []Handle) SubtreeFacts {
	if len(nodes) == 0 {
		return SubtreeFactsNone
	}
	return SubtreeContainsTypeScript
}

func awaitOnly(async bool) SubtreeFacts {
	if async {
		return SubtreeContainsAnyAwait
	}
	return SubtreeFactsNone
}

func functionLikeFacts(h Handle, includeGenerator bool) SubtreeFacts {
	async := h.ModifierFlags()&ModifierFlagsAsync != 0
	generator := includeGenerator && !h.AsteriskToken().IsNil()
	facts := propagateHandleList(h.ModifierNodes(), propagateHandle) |
		propagateHandle(h.AsteriskToken()) |
		propagateHandle(h.Name()) |
		eraseableList(h.TypeParameters()) |
		propagateHandleList(h.Parameters(), propagateHandle) |
		eraseableHandle(h.Type()) |
		eraseableHandle(h.FullSignature()) |
		propagateHandle(h.Body())
	if async && generator {
		facts |= SubtreeContainsForAwaitOrAsyncGenerator
	} else if async {
		facts |= SubtreeContainsAnyAwait
	}
	return facts
}
