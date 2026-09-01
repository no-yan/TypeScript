package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"strings"
)

func (b *NodeBuilderImpl) reuseNode(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return node
	}
	return b.tryReuseExistingNodeHelper(node)
}
func (b *NodeBuilderImpl) tryJSTypeNodeToTypeNode(node ast.Handle) ast.Handle {
	return b.reuseNode(node)
}
func (b *NodeBuilderImpl) reuseName(node ast.Handle, isMethod bool) ast.Handle {
	res := b.reuseNode(node)
	if res.IsNil() {
		return res
	}
	text, ok := ast.TryGetTextOfPropertyName(res)
	if !ok {
		return res
	}
	kind := classifyPropertyName(text, ast.IsStringLiteral(res), isMethod)
	if ast.IsIdentifier(res) && kind == propertyNameNodeKindIdentifier {
		return res
	}
	if ast.IsStringLiteral(res) && kind == propertyNameNodeKindStringLiteral {
		return res
	}
	var renamed ast.Handle
	switch kind {
	case propertyNameNodeKindIdentifier:
		renamed = b.newIdentifier(text, nil)
	case propertyNameNodeKindStringLiteral:
		renamed = b.f.NewStringLiteral(text, ast.TokenFlagsNone)
	default:
		return res
	}
	b.e.SetOriginal(renamed, res)
	return b.setTextRange(renamed, res)
}
func (b *NodeBuilderImpl) reuseTypeNode(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return node
	}
	r := b.reuseNode(node)
	if !r.IsNil() {
		if b.ctx.maxExpansionDepth >= 0 && !b.ctx.canIncreaseExpansionDepth {
			b.walkNodeForExpandability(node)
		}
		return r
	}
	b.ctx.tracker.ReportInferenceFallback(node)
	t := b.getTypeFromTypeNode(node, false)
	return b.typeToTypeNode(t)
}

func (b *NodeBuilderImpl) walkNodeForExpandability(node ast.Handle) {
	if b.ctx.canIncreaseExpansionDepth || node.IsNil() {
		return
	}
	if ast.IsTypeReferenceNode(node) || ast.IsExpressionWithTypeArguments(node) || ast.IsTypePredicateNode(node) || ast.IsImportTypeNode(node) {
		t := b.getTypeFromTypeNode(node, false)
		if t != nil {
			b.checkTypeExpandability(t)
			if b.ctx.canIncreaseExpansionDepth {
				return
			}
		}
	}
	node.ForEachChild(func(child ast.Handle) bool {
		b.walkNodeForExpandability(child)
		return b.ctx.canIncreaseExpansionDepth
	})
}

type recoveryBoundary struct {
	ctx                  *NodeBuilderContext
	hadError             bool
	deferredReports      []func()
	oldTracker           nodebuilder.SymbolTracker
	oldTrackedSymbols    []*TrackedSymbolArgs
	trackedSymbols       []*TrackedSymbolArgs
	oldEncounteredError  bool
	oldApproximateLength int
}

func (b *recoveryBoundary) markError(f func()) {
	b.hadError = true
	if f != nil {
		b.deferredReports = append(b.deferredReports, f)
	}
}

type originalRecoveryScopeState struct {
	trackedSymbolsTop   int
	unreportedErrorsTop int
	hadError            bool
}

func (b *recoveryBoundary) startRecoveryScope() originalRecoveryScopeState {
	trackedSymbolsTop := len(b.ctx.trackedSymbols)
	unreportedErrorsTop := len(b.deferredReports)
	return originalRecoveryScopeState{trackedSymbolsTop: trackedSymbolsTop, unreportedErrorsTop: unreportedErrorsTop, hadError: b.hadError}
}
func (b *recoveryBoundary) endRecoveryScope(state originalRecoveryScopeState) {
	b.hadError = state.hadError
	b.ctx.trackedSymbols = b.ctx.trackedSymbols[0:state.trackedSymbolsTop]
	b.deferredReports = b.deferredReports[0:state.unreportedErrorsTop]
}

type wrappingTracker struct {
	wrapped nodebuilder.SymbolTracker
	bound   *recoveryBoundary
}

func (w *wrappingTracker) PopErrorFallbackNode() {
	w.wrapped.PopErrorFallbackNode()
}
func (w *wrappingTracker) PushErrorFallbackNode(node ast.Handle) {
	w.wrapped.PushErrorFallbackNode(node)
}
func (w *wrappingTracker) ReportCyclicStructureError() {
	w.bound.markError(w.wrapped.ReportCyclicStructureError)
}
func (w *wrappingTracker) ReportInaccessibleThisError() {
	w.bound.markError(w.wrapped.ReportInaccessibleThisError)
}
func (w *wrappingTracker) ReportInaccessibleUniqueSymbolError() {
	w.bound.markError(w.wrapped.ReportInaccessibleUniqueSymbolError)
}
func (w *wrappingTracker) ReportInferenceFallback(node ast.Handle) {
	w.wrapped.ReportInferenceFallback(node)
}
func (w *wrappingTracker) ReportLikelyUnsafeImportRequiredError(specifier string, symbolName string) {
	w.bound.markError(func() {
		w.wrapped.ReportLikelyUnsafeImportRequiredError(specifier, symbolName)
	})
}
func (w *wrappingTracker) ReportNonSerializableProperty(propertyName string) {
	w.bound.markError(func() {
		w.wrapped.ReportNonSerializableProperty(propertyName)
	})
}
func (w *wrappingTracker) ReportNonlocalAugmentation(containingFile *ast.SourceFile, parentSymbol *ast.Symbol, augmentingSymbol *ast.Symbol) {
	w.wrapped.ReportNonlocalAugmentation(containingFile, parentSymbol, augmentingSymbol)
}
func (w *wrappingTracker) ReportPrivateInBaseOfClassExpression(propertyName string) {
	w.bound.markError(func() {
		w.wrapped.ReportPrivateInBaseOfClassExpression(propertyName)
	})
}
func (w *wrappingTracker) ReportTruncationError() {
	w.wrapped.ReportTruncationError()
}
func (w *wrappingTracker) TrackSymbol(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags) bool {
	w.bound.trackedSymbols = append(w.bound.trackedSymbols, &TrackedSymbolArgs{symbol, enclosingDeclaration, meaning})
	return false
}
func newWrappingTracker(inner nodebuilder.SymbolTracker, bound *recoveryBoundary) *wrappingTracker {
	return &wrappingTracker{wrapped: inner, bound: bound}
}
func (b *NodeBuilderImpl) createRecoveryBoundary() *recoveryBoundary {
	b.ch.checkNotCanceled()
	bound := &recoveryBoundary{ctx: b.ctx, oldTracker: b.ctx.tracker, oldTrackedSymbols: b.ctx.trackedSymbols, oldEncounteredError: b.ctx.encounteredError, oldApproximateLength: b.ctx.approximateLength}
	newTracker := NewSymbolTrackerImpl(b.ctx, newWrappingTracker(b.ctx.tracker, bound))
	b.ctx.tracker = newTracker
	b.ctx.trackedSymbols = nil
	return bound
}
func (b *NodeBuilderImpl) finalizeBoundary(bound *recoveryBoundary) bool {
	b.ctx.tracker = bound.oldTracker
	b.ctx.trackedSymbols = bound.oldTrackedSymbols
	b.ctx.encounteredError = bound.oldEncounteredError
	b.ctx.approximateLength = bound.oldApproximateLength
	for _, f := range bound.deferredReports {
		f()
	}
	if bound.hadError {
		return false
	}
	for _, a := range bound.trackedSymbols {
		b.ctx.tracker.TrackSymbol(a.symbol, a.enclosingDeclaration, a.meaning)
	}
	return true
}
func (b *NodeBuilderImpl) tryReuseExistingNodeHelper(existing ast.Handle) ast.Handle {
	bound := b.createRecoveryBoundary()
	var transformed ast.Handle
	v := getExistingNodeTreeVisitor(b, bound)
	transformed = v.VisitNode(existing)
	if !b.finalizeBoundary(bound) {
		return ast.Handle{}
	}
	b.ctx.approximateLength += existing.Loc().End() - existing.Loc().Pos()
	return transformed
}
func (b *NodeBuilderImpl) getModuleSpecifierOverride(parent ast.Handle, lit ast.Handle) string {
	if b.ctx.enclosingFile != ast.GetSourceFileOfNode(lit) {
		mode := core.ResolutionModeNone
		if !parent.ImportTypeNodeAttributes().IsNil() {
			mode = b.ch.getResolutionModeOverride(parent.ImportTypeNodeAttributes(), false)
		}
		name := lit.Text()
		originalName := name
		nodeSymbol := b.tryGetResolvedSymbolFromTypeNode(parent)
		meaning := ast.SymbolFlagsType
		if parent.ImportTypeNodeIsTypeOf() {
			meaning = ast.SymbolFlagsValue
		}
		var parentSymbol *ast.Symbol
		if nodeSymbol != nil && b.ch.IsSymbolAccessible(nodeSymbol, b.ctx.enclosingDeclaration, meaning, false).Accessibility == printer.SymbolAccessibilityAccessible {
			parentSymbol = b.lookupSymbolChain(nodeSymbol, meaning, true)[0]
		}
		if parentSymbol != nil && IsExternalModuleSymbol(parentSymbol) {
			name = b.getSpecifierForModuleSymbol(parentSymbol, mode)
		} else {
			targetFile := b.ch.getExternalModuleFileFromDeclaration(parent)
			if targetFile != nil {
				name = b.getSpecifierForModuleSymbol(targetFile.Symbol, mode)
			}
		}
		if len(name) > 0 && strings.Contains(name, "/node_modules/") {
			b.ctx.encounteredError = true
			b.ctx.tracker.ReportLikelyUnsafeImportRequiredError(name, "")
		}
		if name != originalName {
			return name
		}
	}
	return ""
}
func (b *NodeBuilderImpl) rewriteModuleSpecifier(parent ast.Handle, lit ast.Handle) ast.Handle {
	newName := b.getModuleSpecifierOverride(parent, lit)
	if len(newName) == 0 {
		return lit
	}
	res := b.f.NewStringLiteral(newName, ast.TokenFlagsNone)
	b.e.SetOriginal(res, lit)
	return res
}
func (b *NodeBuilderImpl) getEnclosingDeclarationIgnoringFakeScope() ast.Handle {
	enc := b.ctx.enclosingDeclaration
	for !enc.IsNil() && b.links.Get(enc).fakeScopeForSignatureDeclaration != nil {
		enc = enc.Parent()
	}
	return enc
}
func getExistingNodeTreeVisitor(b *NodeBuilderImpl, bound *recoveryBoundary) *ast.HandleVisitor {
	var visitor *ast.HandleVisitor
	attachSymbolToLeftmostIdentifier := func(leftmost ast.Handle, node ast.Handle, sym *ast.Symbol) ast.Handle {
		var vis *ast.HandleVisitor
		visitorFunc := func(node ast.Handle) ast.Handle {
			if node == leftmost {
				var type_ *Type
				var name ast.Handle
				if sym != nil {
					type_ = b.ch.getDeclaredTypeOfSymbol(sym)
					if sym.Flags&ast.SymbolFlagsTypeParameter != 0 {
						name = b.typeParameterToName(type_)
					}
				}
				if name.IsNil() {
					name = b.newIdentifier(node.Text(), sym)
				}
				name = b.setTextRange(name, node)
				b.e.AddEmitFlags(name, printer.EFNoAsciiEscaping)
				return name
			}
			return b.setTextRange(vis.VisitEachChild(node), node)
		}
		vis = ast.NewHandleVisitor(visitorFunc, b.e.StoreFactory(), ast.HandleVisitorHooks{})
		return visitorFunc(node)
	}
	trackExistingEntityName := func(node ast.Handle, overrideEnclosing ast.Handle) (bool, ast.Handle, *ast.Symbol) {
		enclosingDeclaration := b.ctx.enclosingDeclaration
		if !overrideEnclosing.IsNil() {
			enclosingDeclaration = overrideEnclosing
		}
		introducesError := false
		leftmost := ast.GetFirstIdentifier(node)
		if ast.IsInJSFile(node) && (ast.IsExportsIdentifier(leftmost) || ast.IsModuleExportsAccessExpression(leftmost.Parent()) || (ast.IsQualifiedName(leftmost.Parent()) && ast.IsModuleIdentifier(leftmost.Parent().QualifiedNameLeft()) && ast.IsExportsIdentifier(leftmost.Parent().QualifiedNameRight()))) {
			introducesError = true
			return introducesError, b.setTextRange(b.f.DeepCloneNode(node), node), nil
		}
		meaning := getMeaningOfEntityNameReference(node)
		var sym *ast.Symbol
		if ast.IsThisIdentifier(leftmost) {
			sym = b.ch.getSymbolOfDeclaration(b.ch.getThisContainer(leftmost, false, false))
			if b.ch.IsSymbolAccessible(sym, leftmost, meaning, false).Accessibility != printer.SymbolAccessibilityAccessible {
				introducesError = true
				b.ctx.tracker.ReportInaccessibleThisError()
			}
			return introducesError, attachSymbolToLeftmostIdentifier(leftmost, node, sym), nil
		}
		sym = b.ch.resolveEntityName(leftmost, meaning, true, true, ast.Handle{})
		if !b.ctx.enclosingDeclaration.IsNil() && !(sym != nil && sym.Flags&ast.SymbolFlagsTypeParameter != 0) {
			sym = b.ch.getExportSymbolOfValueSymbolIfExported(sym)
			symAtLocation := b.ch.resolveEntityName(leftmost, meaning, true, true, b.ctx.enclosingDeclaration)
			if symAtLocation == b.ch.unknownSymbol || (symAtLocation == nil && sym != nil) || (symAtLocation != nil && sym != nil && b.ch.getSymbolIfSameReference(b.ch.getExportSymbolOfValueSymbolIfExported(symAtLocation), sym) == nil) {
				if symAtLocation != b.ch.unknownSymbol {
					b.ctx.tracker.ReportInferenceFallback(node)
				}
				introducesError = true
				return introducesError, b.setTextRange(b.f.DeepCloneNode(node), node), sym
			} else {
				sym = symAtLocation
			}
		}
		if sym != nil {
			if sym.Flags&ast.SymbolFlagsFunctionScopedVariable != 0 && sym.ValueDeclaration != 0 {
				if ast.IsPartOfParameterDeclaration(ast.NodeOf(sym.ValueDeclaration)) || ast.IsJSDocParameterTag(ast.NodeOf(sym.ValueDeclaration)) {
					return introducesError, attachSymbolToLeftmostIdentifier(leftmost, node, sym), nil
				}
			}
			if sym.Flags&ast.SymbolFlagsTypeParameter == 0 && !ast.IsDeclarationName(node) && b.ch.IsSymbolAccessible(sym, enclosingDeclaration, meaning, false).Accessibility != printer.SymbolAccessibilityAccessible {
				b.ctx.tracker.ReportInferenceFallback(node)
				introducesError = true
			} else {
				b.ctx.tracker.TrackSymbol(sym, enclosingDeclaration, meaning)
			}
			return introducesError, attachSymbolToLeftmostIdentifier(leftmost, node, sym), nil
		}
		return introducesError, b.setTextRange(b.f.DeepCloneNode(node), node), nil
	}
	var tryVisitSimpleTypeNode func(node ast.Handle) ast.Handle
	tryVisitIndexedAccess := func(node ast.Handle) ast.Handle {
		resultObjectType := tryVisitSimpleTypeNode(node.IndexedAccessTypeNodeObjectType())
		if resultObjectType.IsNil() {
			return ast.Handle{}
		}
		return b.setTextRange(b.f.UpdateIndexedAccessTypeNode(node, resultObjectType, visitor.VisitNode(node.IndexedAccessTypeNodeIndexType())), node)
	}
	tryVisitKeyOf := func(node ast.Handle) ast.Handle {
		to := node
		t := tryVisitSimpleTypeNode(to.Type())
		if t.IsNil() {
			return ast.Handle{}
		}
		return b.setTextRange(b.f.UpdateTypeOperatorNode(to, to.TypeOperatorNodeOperator(), t), node)
	}
	tryVisitTypeQuery := func(node ast.Handle) ast.Handle {
		introducesError, exprName, _ := trackExistingEntityName(node.TypeQueryNodeExprName(), ast.Handle{})
		if !introducesError {
			return b.setTextRange(b.f.UpdateTypeQueryNode(node, exprName, visitor.VisitNodes(node.TypeQueryNodeTypeArguments())), node)
		}
		serializedName := b.serializeTypeName(node.TypeQueryNodeExprName(), true, visitor.VisitNodes(node.TypeQueryNodeTypeArguments()))
		if !serializedName.IsNil() {
			return b.setTextRange(serializedName, node.TypeQueryNodeExprName())
		}
		return ast.Handle{}
	}
	tryVisitTypeReference := func(node ast.Handle) ast.Handle {
		if ast.IsConstTypeReference(node) {
			return ast.Handle{}
		}
		s := b.tryGetResolvedSymbolFromTypeNode(node)
		if s == nil {
			return ast.Handle{}
		}
		if s.Flags&ast.SymbolFlagsTypeParameter != 0 {
			declaredType := b.ch.getDeclaredTypeOfSymbol(s)
			if b.ctx.mapper != nil && b.ctx.mapper.Map(declaredType) != declaredType {
				return ast.Handle{}
			}
		}
		if !b.canReuseExistingJSTypeNode(node, b.getTypeFromTypeNode(node, false)) {
			return ast.Handle{}
		}
		introducesError, newName, _ := trackExistingEntityName(node.TypeReferenceNodeTypeName(), ast.Handle{})
		if !introducesError {
			typeArguments := visitor.VisitNodes(node.TypeReferenceNodeTypeArguments())
			return b.setTextRange(b.f.UpdateTypeReferenceNode(node, newName, typeArguments), node)
		} else {
			serializedName := b.serializeTypeName(node.TypeReferenceNodeTypeName(), false, visitor.VisitNodes(node.TypeReferenceNodeTypeArguments()))
			if !serializedName.IsNil() {
				return b.setTextRange(serializedName, node.TypeReferenceNodeTypeName())
			}
			return ast.Handle{}
		}
	}
	tryVisitSimpleTypeNode = func(node ast.Handle) ast.Handle {
		innerNode := ast.SkipParentheses(node)
		switch innerNode.Kind() {
		case ast.KindTypeReference:
			return tryVisitTypeReference(innerNode)
		case ast.KindTypeQuery:
			return tryVisitTypeQuery(innerNode)
		case ast.KindIndexedAccessType:
			return tryVisitIndexedAccess(innerNode)
		case ast.KindTypeOperator:
			if innerNode.TypeOperatorNodeOperator() == ast.KindKeyOfKeyword {
				return tryVisitKeyOf(innerNode)
			}
		}
		return visitor.VisitNode(node)
	}
	visitExistingNodeTreeSymbolsWorker := func(node ast.Handle) ast.Handle {
		factory := b.f
		if node.Kind() == ast.KindJSDocTypeExpression {
			return visitor.VisitNode(node.JSDocTypeExpressionType())
		}
		if node.Kind() == ast.KindJSDocAllType {
			return factory.NewKeywordTypeNode(ast.KindAnyKeyword)
		}
		if node.Kind() == ast.KindJSDocNullableType {
			unionMembers := []ast.Handle{visitor.VisitNode(node.JSDocNullableTypeType()), factory.NewLiteralTypeNode(factory.NewKeywordExpression(ast.KindNullKeyword))}
			return factory.NewUnionTypeNode(factory.NewList(unionMembers))
		}
		if node.Kind() == ast.KindJSDocOptionalType {
			unionMembers := []ast.Handle{visitor.VisitNode(node.JSDocOptionalTypeType()), factory.NewKeywordTypeNode(ast.KindUndefinedKeyword)}
			return factory.NewUnionTypeNode(factory.NewList(unionMembers))
		}
		if node.Kind() == ast.KindJSDocNonNullableType {
			return visitor.VisitNode(node.JSDocNonNullableTypeType())
		}
		if node.Kind() == ast.KindJSDocVariadicType {
			return factory.NewArrayTypeNode(visitor.VisitNode(node.JSDocVariadicTypeType()))
		}
		if node.Kind() == ast.KindJSDocTypeLiteral {
			var members []ast.Handle
			for _, t := range node.Store().ListSlice(node.JSDocTypeLiteralJSDocPropertyTags()) {
				if t.Kind() != ast.KindJSDocPropertyTag && t.Kind() != ast.KindJSDocParameterTag {
					continue
				}
				n := t.Name()
				var targetName ast.Handle
				if ast.IsIdentifier(n) {
					targetName = n
				} else {
					targetName = n.QualifiedNameRight()
				}
				name := visitor.VisitNode(targetName)
				shouldBeOptional := t.JSDocParameterOrPropertyTagIsBracketed() || (!t.TypeExpression().IsNil() && t.TypeExpression().Kind() == ast.KindJSDocOptionalType)
				var question ast.Handle
				if shouldBeOptional {
					question = factory.NewToken(ast.KindQuestionToken)
				}
				ty := visitor.VisitNode(t.TypeExpression())
				members = append(members, factory.NewPropertySignatureDeclaration(0, name, question, ty, ast.Handle{}))
			}
			return factory.NewTypeLiteralNode(factory.NewList(members))
		}
		if ast.IsTypeReferenceNode(node) && ast.IsIdentifier(node.TypeReferenceNodeTypeName()) && node.TypeReferenceNodeTypeName().IdentifierText() == "" {
			replacement := factory.NewKeywordTypeNode(ast.KindAnyKeyword)
			b.e.SetOriginal(replacement, node)
			return replacement
		}
		if ast.IsThisTypeNode(node) {
			return node
		}
		if ast.IsTypeParameterDeclaration(node) {
			_, newName, _ := trackExistingEntityName(node.Name(), ast.Handle{})
			return factory.UpdateTypeParameterDeclaration(node, visitor.VisitModifiers(node.Modifiers()), newName, visitor.VisitNode(node.TypeParameterDeclarationConstraint()), visitor.VisitNode(node.TypeParameterDeclarationExpression()), visitor.VisitNode(node.TypeParameterDeclarationDefaultType()))
		}
		if ast.IsIndexedAccessTypeNode(node) {
			result := tryVisitIndexedAccess(node)
			if !result.IsNil() {
				return result
			}
			bound.markError(nil)
			return node
		}
		if ast.IsTypeReferenceNode(node) {
			result := tryVisitTypeReference(node)
			if !result.IsNil() {
				return result
			}
			bound.markError(nil)
			return node
		}
		if ast.IsTypeQueryNode(node) {
			result := tryVisitTypeQuery(node)
			if !result.IsNil() {
				return result
			}
			bound.markError(nil)
			return node
		}
		if ast.IsTypeOperatorNode(node) {
			if node.TypeOperatorNodeOperator() == ast.KindUniqueKeyword && node.TypeOperatorNodeType().Kind() == ast.KindSymbolKeyword {
				nonFakeEnclosing := b.getEnclosingDeclarationIgnoringFakeScope()
				sameScope := ast.FindAncestor(node, func(a ast.Handle) bool {
					return a == nonFakeEnclosing
				})
				if sameScope.IsNil() {
					bound.markError(nil)
					return node
				}
			} else if node.TypeOperatorNodeOperator() == ast.KindKeyOfKeyword {
				result := tryVisitKeyOf(node)
				if !result.IsNil() {
					return result
				}
				bound.markError(nil)
				return node
			}
		}
		if ast.IsLiteralImportTypeNode(node) {
			if !node.ImportTypeNodeAttributes().IsNil() && node.ImportTypeNodeAttributes().ImportAttributesToken() == ast.KindAssertKeyword {
				bound.markError(nil)
				return node
			}
			t := b.getTypeFromTypeNode(node, true)
			if t == nil {
				bound.markError(nil)
				return node
			}
			if ast.IsInJSFile(node) {
			}
			originalSpec := node.ImportTypeNodeArgument().LiteralTypeNodeLiteral()
			specifier := b.rewriteModuleSpecifier(node, originalSpec)
			if originalSpec == specifier {
				specifier = visitor.VisitNode(specifier)
			}
			arg := node.ImportTypeNodeArgument()
			if specifier != originalSpec {
				arg = factory.NewLiteralTypeNode(specifier)
			}
			return factory.UpdateImportTypeNode(node, node.ImportTypeNodeIsTypeOf(), arg, visitor.VisitNode(node.ImportTypeNodeAttributes()), visitor.VisitNode(node.ImportTypeNodeQualifier()), visitor.VisitNodes(node.ImportTypeNodeTypeArguments()))
		}
		if !node.Name().IsNil() && node.Name().Kind() == ast.KindComputedPropertyName && !b.ch.hasLateBindableName(node) {
			if !ast.HasDynamicName(node) {
				return visitor.VisitEachChild(node)
			}
			shouldRemoveDeclaration := !((b.ctx.internalFlags&nodebuilder.InternalFlagsAllowUnresolvedNames != 0) && ast.IsEntityNameExpression(node.Name().ComputedPropertyNameExpression()) && (b.ch.checkComputedPropertyName(node.Name()).flags&TypeFlagsAny != 0))
			if shouldRemoveDeclaration {
				return ast.Handle{}
			}
		}
		if (ast.IsFunctionLike(node) && node.Type().IsNil()) || (ast.IsPropertyDeclaration(node) && node.Type().IsNil() && node.Initializer().IsNil()) || (ast.IsPropertySignatureDeclaration(node) && node.Type().IsNil() && node.Initializer().IsNil()) || (ast.IsParameterDeclaration(node) && node.Type().IsNil() && node.Initializer().IsNil()) {
			visited := visitor.VisitEachChild(node)
			if visited == node {
				visited = b.setTextRange(factory.DeepCloneNode(node), node)
			}
			node = visited
			newType := factory.NewKeywordTypeNode(ast.KindAnyKeyword)
			switch node.Kind() {
			case ast.KindPropertyDeclaration:
				return factory.UpdatePropertyDeclaration(node, node.Modifiers(), node.Name(), node.PostfixToken(), newType, ast.Handle{})
			case ast.KindPropertySignature:
				return factory.UpdatePropertySignatureDeclaration(node, node.Modifiers(), node.Name(), node.PostfixToken(), newType, ast.Handle{})
			case ast.KindParameter:
				return factory.UpdateParameterDeclaration(node, 0, node.ParameterDeclarationDotDotDotToken(), node.Name(), node.ParameterDeclarationQuestionToken(), newType, ast.Handle{})
			case ast.KindMethodSignature:
				return factory.UpdateMethodSignatureDeclaration(node, node.Modifiers(), node.Name(), node.MethodSignatureDeclarationPostfixToken(), node.MethodSignatureDeclarationTypeParameters(), node.MethodSignatureDeclarationParameters(), newType)
			case ast.KindCallSignature:
				return factory.UpdateCallSignatureDeclaration(node, node.CallSignatureDeclarationTypeParameters(), node.CallSignatureDeclarationParameters(), newType)
			case ast.KindJSDocSignature:
				return factory.UpdateJSDocSignature(node, node.JSDocSignatureTypeParameters(), node.JSDocSignatureParameters(), newType)
			case ast.KindConstructSignature:
				return factory.UpdateConstructSignatureDeclaration(node, node.ConstructSignatureDeclarationTypeParameters(), node.ConstructSignatureDeclarationParameters(), newType)
			case ast.KindIndexSignature:
				return factory.UpdateIndexSignatureDeclaration(node, node.Modifiers(), node.IndexSignatureDeclarationParameters(), newType)
			case ast.KindFunctionType:
				return factory.UpdateFunctionTypeNode(node, node.FunctionTypeNodeTypeParameters(), node.FunctionTypeNodeParameters(), newType)
			case ast.KindConstructorType:
				return factory.UpdateConstructorTypeNode(node, node.Modifiers(), node.ConstructorTypeNodeTypeParameters(), node.ConstructorTypeNodeParameters(), newType)
			}
		}
		if ast.IsComputedPropertyName(node) && ast.IsEntityNameExpression(node.ComputedPropertyNameExpression()) {
			introducesError, result, _ := trackExistingEntityName(node.ComputedPropertyNameExpression(), ast.Handle{})
			if !introducesError {
				return factory.UpdateComputedPropertyName(node, result)
			} else {
				bound.markError(nil)
				return visitor.VisitEachChild(node)
			}
		}
		if ast.IsTypePredicateNode(node) {
			var parameterName ast.Handle
			if ast.IsIdentifier(node.TypePredicateNodeParameterName()) {
				introducesError, result, _ := trackExistingEntityName(node.TypePredicateNodeParameterName(), ast.Handle{})
				if introducesError {
					bound.markError(nil)
				}
				parameterName = result
			} else {
				parameterName = factory.DeepCloneNode(node.TypePredicateNodeParameterName())
			}
			return factory.UpdateTypePredicateNode(node, visitor.VisitNode(node.TypePredicateNodeAssertsModifier()), parameterName, visitor.VisitNode(node.TypePredicateNodeType()))
		}
		if ast.IsConditionalTypeNode(node) {
			checkType := visitor.VisitNode(node.ConditionalTypeNodeCheckType())
			dispose := b.enterNewScope(node, nil, b.ch.getInferTypeParameters(node), nil, nil)
			extendsType := visitor.VisitNode(node.ConditionalTypeNodeExtendsType())
			trueType := visitor.VisitNode(node.ConditionalTypeNodeTrueType())
			dispose()
			falseType := visitor.VisitNode(node.ConditionalTypeNodeFalseType())
			return factory.UpdateConditionalTypeNode(node, checkType, extendsType, trueType, falseType)
		}
		if ast.IsTupleTypeNode(node) || (b.ctx.flags&nodebuilder.FlagsMultilineObjectLiterals == 0 && ast.IsTypeLiteralNode(node)) || ast.IsMappedTypeNode(node) {
			res := visitor.VisitEachChild(node)
			if res == node {
				res = factory.DeepCloneNode(res)
				res = b.setTextRange(res, node)
			}
			b.e.AddEmitFlags(res, printer.EFSingleLine)
			return res
		}
		if ast.IsStringLiteralLike(node) {
			c := b.f.DeepCloneNode(node)
			if ast.IsStringLiteral(node) && b.ctx.flags&nodebuilder.FlagsUseSingleQuotesForStringLiteralType != 0 && node.StringLiteralTokenFlags()&ast.TokenFlagsSingleQuote == 0 {
				c.SetStringLiteralTokenFlags(c.StringLiteralTokenFlags() ^ ast.TokenFlagsSingleQuote)
			}
			b.e.AddEmitFlags(c, printer.EFNoAsciiEscaping)
			return c
		}
		return visitor.VisitEachChild(node)
	}
	nonLocalNode := true
	visitor = ast.NewHandleVisitor(func(node ast.Handle) ast.Handle {
		if bound.hadError {
			return node
		}
		recover_ := bound.startRecoveryScope()
		introducesNewScope := ast.IsFunctionLike(node) || ast.IsMappedTypeNode(node)
		var exit func()
		if introducesNewScope {
			var params []*ast.Symbol
			var typeParams []*Type
			if ast.IsFunctionLike(node) {
				sig := b.ch.getSignatureFromDeclaration(node)
				params = sig.parameters
				typeParams = sig.typeParameters
			} else if ast.IsConditionalTypeNode(node) {
				typeParams = b.ch.getInferTypeParameters(node)
			} else if ast.IsMappedTypeNode(node) {
				typeParams = []*Type{b.ch.getDeclaredTypeOfTypeParameter(b.ch.getSymbolOfDeclaration(node.MappedTypeNodeTypeParameter()))}
			}
			exit = b.enterNewScope(node, params, typeParams, nil, nil)
		}
		result := visitExistingNodeTreeSymbolsWorker(node)
		if exit != nil {
			exit()
		}
		if result == node && !ast.NodeIsSynthesized(node) {
			result = b.f.DeepCloneNode(node)
		}
		result = b.setTextRange(result, node)
		if bound.hadError {
			if ast.IsTypeNode(node) && !ast.IsTypePredicateNode(node) {
				bound.endRecoveryScope(recover_)
				t := b.getTypeFromTypeNode(node, false)
				return b.typeToTypeNode(t)
			}
			return b.setTextRange(b.f.DeepCloneNode(node), node)
		}
		return result
	}, b.e.StoreFactory(), ast.HandleVisitorHooks{VisitNodes: func(nodes ast.ListRef, v *ast.HandleVisitor) ast.ListRef {
		res := v.DefaultVisitNodes(nodes)
		if nonLocalNode && res != 0 {
			store := b.e.StoreFactory().Store()
			if res == nodes {
				res = b.e.StoreFactory().List(store.ListLoc(nodes), store.ListSlice(nodes)...)
			}
			res = b.e.StoreFactory().List(core.NewTextRange(-1, -1), store.ListSlice(res)...)
		}
		return res
	}, VisitNode: func(node ast.Handle, v *ast.HandleVisitor) ast.Handle {
		oldNonLocalNode := nonLocalNode
		nonLocalNode = b.ctx.enclosingFile == nil || b.ctx.enclosingFile != ast.GetSourceFileOfNode(b.e.MostOriginal(node))
		res := v.DefaultVisitNode(node)
		nonLocalNode = oldNonLocalNode
		return res
	}})
	return visitor
}
