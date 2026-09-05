package checker

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/modulespecifiers"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/pseudochecker"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"maps"
	"slices"
	"strings"
)

type CompositeSymbolIdentity struct {
	isConstructorNode bool
	symbolId          ast.SymbolId
	nodeId            ast.NodeId
}
type TrackedSymbolArgs struct {
	symbol               *ast.Symbol
	enclosingDeclaration ast.Handle
	meaning              ast.SymbolFlags
}
type SerializedTypeEntry struct {
	node           ast.Handle
	truncating     bool
	addedLength    int
	trackedSymbols []*TrackedSymbolArgs
}
type CompositeTypeCacheIdentity struct {
	typeId        TypeId
	flags         nodebuilder.Flags
	internalFlags nodebuilder.InternalFlags
}
type NodeBuilderLinks struct {
	serializedTypes                  map[CompositeTypeCacheIdentity]*SerializedTypeEntry
	fakeScopeForSignatureDeclaration *string
}
type NodeBuilderSymbolLinks struct{ specifierCache module.ModeAwareCache[string] }
type NodeBuilderContext struct {
	host                                  Host
	tracker                               nodebuilder.SymbolTracker
	approximateLength                     int
	maxTruncationLength                   int
	encounteredError                      bool
	truncating                            bool
	reportedDiagnostic                    bool
	flags                                 nodebuilder.Flags
	internalFlags                         nodebuilder.InternalFlags
	depth                                 int
	maxExpansionDepth                     int
	typeStack                             []*Type
	canIncreaseExpansionDepth             bool
	expansionTruncated                    bool
	enclosingDeclaration                  ast.Handle
	enclosingFile                         *ast.SourceFile
	inferTypeParameters                   []*Type
	visitedTypes                          collections.Set[TypeId]
	symbolDepth                           map[CompositeSymbolIdentity]int
	trackedSymbols                        []*TrackedSymbolArgs
	mapper                                *TypeMapper
	reverseMappedStack                    []*ast.Symbol
	enclosingSymbolTypes                  map[ast.SymbolId]*Type
	suppressReportInferenceFallback       bool
	remappedSymbolReferences              map[ast.SymbolId]*ast.Symbol
	typeParameterNames                    collections.CopyOnWriteMap[TypeId, ast.Handle]
	typeParameterNamesByText              collections.CopyOnWriteSet[string]
	typeParameterNamesByTextNextNameCount collections.CopyOnWriteMap[string, int]
	typeParameterSymbolList               collections.CopyOnWriteSet[ast.SymbolId]
}
type NodeBuilderImpl struct {
	f                       ast.HandleFactory
	ch                      *Checker
	e                       *printer.EmitContext
	pc                      *pseudochecker.PseudoChecker
	links                   core.LinkStore[ast.Handle, NodeBuilderLinks]
	symbolLinks             core.LinkStore[*ast.Symbol, NodeBuilderSymbolLinks]
	ctx                     *NodeBuilderContext
	cloneBindingNameVisitor *ast.HandleVisitor
	idToSymbol              map[ast.Handle]*ast.Symbol
}

const (
	defaultMaximumTruncationLength      = 160
	noTruncationMaximumTruncationLength = 1_000_000
)

func newNodeBuilderImpl(ch *Checker, e *printer.EmitContext, idToSymbol map[ast.Handle]*ast.Symbol) *NodeBuilderImpl {
	if idToSymbol == nil {
		idToSymbol = make(map[ast.Handle]*ast.Symbol)
	}
	b := &NodeBuilderImpl{f: e.StoreFactory(), ch: ch, e: e, idToSymbol: idToSymbol, pc: pseudochecker.NewPseudoChecker(ch.strictNullChecks, ch.exactOptionalPropertyTypes)}
	b.cloneBindingNameVisitor = ast.NewHandleVisitor(b.cloneBindingName, e.StoreFactory(), ast.HandleVisitorHooks{})
	return b
}
func (b *NodeBuilderImpl) saveRestoreFlags() func() {
	flags := b.ctx.flags
	internalFlags := b.ctx.internalFlags
	depth := b.ctx.depth
	return func() {
		b.ctx.flags = flags
		b.ctx.internalFlags = internalFlags
		b.ctx.depth = depth
	}
}
func (b *NodeBuilderImpl) checkTruncationLength() bool {
	if b.ctx.truncating {
		return b.ctx.truncating
	}
	var maxLength int
	if b.ctx.flags&nodebuilder.FlagsNoTruncation != 0 {
		maxLength = noTruncationMaximumTruncationLength
	} else if b.ctx.maxTruncationLength > 0 {
		maxLength = b.ctx.maxTruncationLength
	} else {
		maxLength = defaultMaximumTruncationLength
	}
	b.ctx.truncating = b.ctx.approximateLength > maxLength
	return b.ctx.truncating
}

func (b *NodeBuilderImpl) checkTruncationLengthIfExpanding() bool {
	if b.ctx.maxExpansionDepth >= 0 && b.checkTruncationLength() {
		b.ctx.expansionTruncated = true
		return true
	}
	return false
}

func (b *NodeBuilderImpl) isExpandableType(t *Type, isAlias bool) bool {
	if isAlias {
		return !b.ch.IsLibSymbolForHoverVerbosity(t.alias.Symbol())
	}
	if b.ch.IsLibTypeForHoverVerbosity(t) {
		return false
	}
	objectFlags := t.objectFlags
	if t.flags&TypeFlagsEnumLike != 0 || objectFlags&ObjectFlagsReference != 0 || objectFlags&ObjectFlagsClassOrInterface != 0 {
		return true
	}
	if objectFlags&ObjectFlagsAnonymous != 0 && t.symbol != nil && t.symbol.Flags&(ast.SymbolFlagsClass|ast.SymbolFlagsEnum|ast.SymbolFlagsValueModule|ast.SymbolFlagsFunction|ast.SymbolFlagsMethod) != 0 {
		return true
	}
	return false
}

func (b *NodeBuilderImpl) isTypeOnStack(t *Type) bool {
	for i := range len(b.ctx.typeStack) - 1 {
		if b.ctx.typeStack[i] == t {
			return true
		}
	}
	return false
}

func (b *NodeBuilderImpl) shouldExpandType(t *Type, isAlias bool) bool {
	if b.ctx.maxExpansionDepth < 0 {
		return false
	}
	if !b.isExpandableType(t, isAlias) {
		return false
	}
	if b.isTypeOnStack(t) {
		return false
	}
	if b.ctx.depth < b.ctx.maxExpansionDepth {
		return true
	}
	b.ctx.canIncreaseExpansionDepth = true
	return false
}

func (b *NodeBuilderImpl) isActivelyExpanding() bool {
	return b.ctx.maxExpansionDepth > 0 && b.ctx.depth < b.ctx.maxExpansionDepth
}

func (b *NodeBuilderImpl) checkTypeExpandability(t *Type) {
	if b.ctx.maxExpansionDepth < 0 || t == nil || b.ctx.canIncreaseExpansionDepth {
		return
	}
	b.ctx.typeStack = append(b.ctx.typeStack, t)
	defer func() {
		b.ctx.typeStack = b.ctx.typeStack[:len(b.ctx.typeStack)-1]
	}()
	if b.isTypeOnStack(t) {
		return
	}
	if t.alias != nil {
		b.shouldExpandType(t, true)
	}
	if !b.ctx.canIncreaseExpansionDepth {
		b.shouldExpandType(t, false)
	}
	if b.ctx.canIncreaseExpansionDepth {
		return
	}
	if t.objectFlags&ObjectFlagsReference != 0 {
		for _, arg := range b.ch.getTypeArguments(t) {
			b.checkTypeExpandability(arg)
			if b.ctx.canIncreaseExpansionDepth {
				return
			}
		}
	}
}
func (b *NodeBuilderImpl) appendReferenceToType(root ast.Handle, ref ast.Handle) ast.Handle {
	if ast.IsImportTypeNode(root) {
		imprt := root
		ids := getAccessStack(ref)
		qualifier := root.ImportTypeNodeQualifier()
		for _, id := range ids {
			if !qualifier.IsNil() {
				qualifier = b.f.NewQualifiedName(qualifier, id)
			} else {
				qualifier = id
			}
		}
		return b.f.UpdateImportTypeNode(imprt, imprt.IsTypeOf(), imprt.Argument(), imprt.Attributes(), qualifier, ref.TypeArgumentList())
	} else if ast.IsTypeReferenceNode(root) {
		typeRef := root
		if b.ctx.flags&nodebuilder.FlagsUseInstantiationExpressions != 0 && typeRef.TypeArgumentList() != 0 && typeArgumentCount(typeRef) != 0 {
			expr := b.createExpressionWithTypeArguments(b.createAccessExpression(typeRef.TypeName()), typeRef.TypeArgumentList())
			for _, id := range getAccessStack(ref) {
				expr = b.f.NewPropertyAccessExpression(expr, ast.Handle{}, id, ast.NodeFlagsNone)
			}
			return expr
		}
		var typeName ast.Handle = typeRef.TypeName()
		for _, id := range getAccessStack(ref) {
			typeName = b.f.NewQualifiedName(typeName, id)
		}
		return b.f.UpdateTypeReferenceNode(root, typeName, ref.TypeArgumentList())
	}
	expr := b.createAccessExpression(root)
	for _, id := range getAccessStack(ref) {
		expr = b.f.NewPropertyAccessExpression(expr, ast.Handle{}, id, ast.NodeFlagsNone)
	}
	return expr
}
func getAccessStack(ref ast.Handle) []ast.Handle {
	var state ast.Handle = ref.TypeReferenceNodeTypeName()
	ids := []ast.Handle{}
	for !ast.IsIdentifier(state) {
		entity := state
		ids = append([]ast.Handle{entity.Right()}, ids...)
		state = entity.Left()
	}
	ids = append([]ast.Handle{state}, ids...)
	return ids
}
func isClassInstanceSide(c *Checker, t *Type) bool {
	return t.symbol != nil && t.symbol.Flags&ast.SymbolFlagsClass != 0 && (t == c.getDeclaredTypeOfClassOrInterface(t.symbol) || (t.flags&TypeFlagsObject != 0 && t.objectFlags&ObjectFlagsIsClassInstanceClone != 0))
}
func (b *NodeBuilderImpl) createElidedInformationPlaceholder() ast.Handle {
	b.ctx.approximateLength += 3
	if b.ctx.flags&nodebuilder.FlagsNoTruncation == 0 {
		return b.f.NewTypeReferenceNode(b.f.NewIdentifier("..."), 0)
	}
	return b.e.AddSyntheticLeadingComment(b.f.NewKeywordTypeNode(ast.KindAnyKeyword), ast.KindMultiLineCommentTrivia, "elided", false)
}
func (b *NodeBuilderImpl) mapToTypeNodes(list []*Type, isBareList bool) ast.ListRef {
	if len(list) == 0 {
		return 0
	}
	if b.checkTruncationLength() {
		if !isBareList {
			var node ast.Handle
			if b.ctx.flags&nodebuilder.FlagsNoTruncation != 0 {
				node = b.e.AddSyntheticLeadingComment(b.f.NewKeywordTypeNode(ast.KindAnyKeyword), ast.KindMultiLineCommentTrivia, "elided", false)
			} else {
				node = b.f.NewTypeReferenceNode(b.f.NewIdentifier("..."), 0)
			}
			return b.e.StoreFactory().List(core.UndefinedTextRange(), node)
		} else if len(list) > 2 {
			nodes := []ast.Handle{b.typeToTypeNode(list[0]), ast.Handle{}, b.typeToTypeNode(list[len(list)-1])}
			if b.ctx.flags&nodebuilder.FlagsNoTruncation != 0 {
				nodes[1] = b.e.AddSyntheticLeadingComment(b.f.NewKeywordTypeNode(ast.KindAnyKeyword), ast.KindMultiLineCommentTrivia, fmt.Sprintf("... %d more elided ...", len(list)-2), false)
			} else {
				text := fmt.Sprintf("... %d more ...", len(list)-2)
				nodes[1] = b.f.NewTypeReferenceNode(b.f.NewIdentifier(text), 0)
			}
			return b.e.StoreFactory().List(core.UndefinedTextRange(), nodes...)
		}
	}
	mayHaveNameCollisions := b.ctx.flags&nodebuilder.FlagsUseFullyQualifiedType == 0
	type seenName struct {
		t *Type
		i int
	}
	var seenNames *collections.MultiMap[string, seenName]
	if mayHaveNameCollisions {
		seenNames = &collections.MultiMap[string, seenName]{}
	}
	result := make([]ast.Handle, 0, len(list))
	for i, t := range list {
		displayIndex := i + 1
		if b.checkTruncationLength() && (displayIndex+2 < len(list)-1) {
			if b.ctx.flags&nodebuilder.FlagsNoTruncation != 0 {
				result = append(result, b.e.AddSyntheticLeadingComment(b.f.NewKeywordTypeNode(ast.KindAnyKeyword), ast.KindMultiLineCommentTrivia, fmt.Sprintf("... %d more elided ...", len(list)-displayIndex), false))
			} else {
				text := fmt.Sprintf("... %d more ...", len(list)-displayIndex)
				result = append(result, b.f.NewTypeReferenceNode(b.f.NewIdentifier(text), 0))
			}
			typeNode := b.typeToTypeNode(list[len(list)-1])
			if !typeNode.IsNil() {
				result = append(result, typeNode)
			}
			break
		}
		b.ctx.approximateLength += 2
		typeNode := b.typeToTypeNode(t)
		if !typeNode.IsNil() {
			result = append(result, typeNode)
			if seenNames != nil && isIdentifierTypeReference(typeNode) {
				seenNames.Add(typeNode.TypeReferenceNodeTypeName().Text(), seenName{t, len(result) - 1})
			}
		}
	}
	if seenNames != nil {
		restoreFlags := b.saveRestoreFlags()
		b.ctx.flags |= nodebuilder.FlagsUseFullyQualifiedType
		for types := range seenNames.Values() {
			if !arrayIsHomogeneous(types, func(a, b seenName) bool {
				return typesAreSameReference(a.t, b.t)
			}) {
				for _, seen := range types {
					result[seen.i] = b.typeToTypeNode(seen.t)
				}
			}
		}
		restoreFlags()
	}
	return b.e.StoreFactory().List(core.UndefinedTextRange(), result...)
}
func (b *NodeBuilderImpl) serializeTypeName(node ast.Handle, isTypeOf bool, typeArguments ast.ListRef) ast.Handle {
	meaning := ast.SymbolFlagsType
	if isTypeOf {
		meaning = ast.SymbolFlagsValue
	}
	symbol := b.ch.resolveEntityName(node, meaning, true, false, node)
	if symbol == nil {
		return ast.Handle{}
	}
	resolvedSymbol := symbol
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		resolvedSymbol = b.ch.resolveAlias(symbol)
	}
	if b.ch.IsSymbolAccessible(symbol, b.ctx.enclosingDeclaration, meaning, false).Accessibility != printer.SymbolAccessibilityAccessible {
		return ast.Handle{}
	}
	return b.symbolToTypeNode(resolvedSymbol, meaning, typeArguments)
}
func isIdentifierTypeReference(node ast.Handle) bool {
	return ast.IsTypeReferenceNode(node) && ast.IsIdentifier(node.TypeReferenceNodeTypeName())
}
func arrayIsHomogeneous[T any](array []T, comparer func(a, B T) bool) bool {
	if len(array) < 2 {
		return true
	}
	first := array[0]
	for i := 1; i < len(array); i++ {
		target := array[i]
		if !comparer(first, target) {
			return false
		}
	}
	return true
}
func typesAreSameReference(a, b *Type) bool {
	return a == b || a.symbol != nil && a.symbol == b.symbol || a.alias != nil && a.alias == b.alias
}
func (b *NodeBuilderImpl) setCommentRange(node ast.Handle, range_ ast.Handle) {
	if !range_.IsNil() && b.ctx.enclosingFile != nil && b.ctx.enclosingFile == ast.GetSourceFileOfNode(range_) {
		b.e.AssignCommentRange(node, range_)
	}
}
func (b *NodeBuilderImpl) typeNodeIsEquivalentToType(annotatedDeclaration ast.Handle, t *Type, typeFromTypeNode *Type) bool {
	if typeFromTypeNode == t {
		return true
	}
	if annotatedDeclaration.IsNil() {
		return false
	}
	if isOptionalDeclaration(annotatedDeclaration) {
		return b.ch.getTypeWithFacts(t, TypeFactsNEUndefined) == typeFromTypeNode
	}
	return false
}
func (b *NodeBuilderImpl) canReuseExistingJSTypeNode(existing ast.Handle, t *Type) bool {
	return b.ch.getIntendedTypeFromJSDocTypeReference(existing) == nil && b.existingTypeNodeIsNotReferenceOrIsReferenceWithCompatibleTypeArgumentCount(existing, t)
}
func (b *NodeBuilderImpl) tryGetResolvedSymbolFromTypeNode(node ast.Handle) *ast.Symbol {
	if node.IsNil() || node.Parent().IsNil() {
		return nil
	}
	b.ch.getTypeFromTypeNode(node)
	links := b.ch.symbolNodeLinks.TryGet(node)
	if links == nil {
		return nil
	}
	return links.resolvedSymbol
}
func (b *NodeBuilderImpl) existingTypeNodeIsNotReferenceOrIsReferenceWithCompatibleTypeArgumentCount(existing ast.Handle, t *Type) bool {
	if t.objectFlags&ObjectFlagsReference == 0 {
		return true
	}
	if !ast.IsTypeReferenceNode(existing) {
		return true
	}
	symbol := b.tryGetResolvedSymbolFromTypeNode(existing)
	if symbol == nil {
		return true
	}
	existingTarget := b.ch.getDeclaredTypeOfSymbol(symbol)
	if existingTarget == nil || existingTarget != t.AsTypeReference().target {
		return true
	}
	return typeArgumentCount(existing) >= b.ch.getMinTypeArgumentCount(t.AsTypeReference().target.AsInterfaceType().TypeParameters())
}
func (b *NodeBuilderImpl) tryReuseExistingNonParameterTypeNode(existing ast.Handle, t *Type, host ast.Handle, annotationType *Type) ast.Handle {
	if host.IsNil() {
		host = b.ctx.enclosingDeclaration
	}
	if annotationType == nil {
		annotationType = b.getTypeFromTypeNode(existing, true)
	}
	if annotationType != nil && b.typeNodeIsEquivalentToType(host, t, annotationType) && b.canReuseExistingJSTypeNode(existing, t) {
		result := b.tryReuseExistingNodeHelper(existing)
		if !result.IsNil() {
			return result
		}
	}
	return ast.Handle{}
}
func (b *NodeBuilderImpl) getResolvedTypeWithoutAbstractConstructSignatures(t *StructuredType) *Type {
	if len(t.ConstructSignatures()) == 0 {
		return t.AsType()
	}
	if t.objectTypeWithoutAbstractConstructSignatures != nil {
		return t.objectTypeWithoutAbstractConstructSignatures
	}
	constructSignatures := core.Filter(t.ConstructSignatures(), func(signature *Signature) bool {
		return signature.flags&SignatureFlagsAbstract == 0
	})
	if len(constructSignatures) == len(t.ConstructSignatures()) {
		t.objectTypeWithoutAbstractConstructSignatures = t.AsType()
		return t.AsType()
	}
	typeCopy := b.ch.newAnonymousType(t.symbol, t.members, t.CallSignatures(), core.IfElse(len(constructSignatures) > 0, constructSignatures, []*Signature{}), t.indexInfos)
	t.objectTypeWithoutAbstractConstructSignatures = typeCopy
	typeCopy.AsStructuredType().objectTypeWithoutAbstractConstructSignatures = typeCopy
	return typeCopy
}
func (b *NodeBuilderImpl) symbolToNode(symbol *ast.Symbol, meaning ast.SymbolFlags) ast.Handle {
	if b.ctx.internalFlags&nodebuilder.InternalFlagsWriteComputedProps != 0 {
		if symbol.ValueDeclaration != 0 {
			name := ast.GetNameOfDeclaration(ast.NodeOf(symbol.ValueDeclaration))
			if !name.IsNil() && ast.IsComputedPropertyName(name) {
				return name
			}
		}
		if b.ch.valueSymbolLinks.Has(symbol) {
			nameType := b.ch.valueSymbolLinks.Get(symbol).nameType
			if nameType != nil && nameType.flags&(TypeFlagsEnumLiteral|TypeFlagsUniqueESSymbol) != 0 {
				oldEnclosing := b.ctx.enclosingDeclaration
				b.ctx.enclosingDeclaration = ast.NodeOf(nameType.symbol.ValueDeclaration)
				result := b.f.NewComputedPropertyName(b.symbolToExpression(nameType.symbol, meaning))
				b.ctx.enclosingDeclaration = oldEnclosing
				return result
			}
		}
	}
	return b.symbolToExpression(symbol, meaning)
}
func (b *NodeBuilderImpl) symbolToName(symbol *ast.Symbol, meaning ast.SymbolFlags, expectsIdentifier bool) ast.Handle {
	chain := b.lookupSymbolChain(symbol, meaning, false)
	if expectsIdentifier && len(chain) != 1 && !b.ctx.encounteredError && (b.ctx.flags&nodebuilder.FlagsAllowQualifiedNameInPlaceOfIdentifier != 0) {
		b.ctx.encounteredError = true
	}
	return b.createEntityNameFromSymbolChain(chain, len(chain)-1)
}
func (b *NodeBuilderImpl) createEntityNameFromSymbolChain(chain []*ast.Symbol, index int) ast.Handle {
	symbol := chain[index]
	if index == 0 {
		b.ctx.flags |= nodebuilder.FlagsInInitialEntityName
	}
	symbolName := b.getNameOfSymbolAsWritten(symbol)
	if index == 0 {
		b.ctx.flags ^= nodebuilder.FlagsInInitialEntityName
	}
	identifier := b.newIdentifier(symbolName, symbol)
	b.e.AddEmitFlags(identifier, printer.EFNoAsciiEscaping)
	if index > 0 {
		return b.f.NewQualifiedName(b.createEntityNameFromSymbolChain(chain, index-1), identifier)
	}
	return identifier
}

func (b *NodeBuilderImpl) symbolToEntityNameNode(symbol *ast.Symbol) ast.Handle {
	identifier := b.newIdentifier(symbol.Name, symbol)
	if symbol.Parent != nil {
		return b.f.NewQualifiedName(b.symbolToEntityNameNode(symbol.Parent), identifier)
	}
	return identifier
}
func (b *NodeBuilderImpl) symbolToTypeNode(symbol *ast.Symbol, mask ast.SymbolFlags, typeArguments ast.ListRef) ast.Handle {
	chain := b.lookupSymbolChain(symbol, mask, (b.ctx.flags&nodebuilder.FlagsUseAliasDefinedOutsideCurrentScope == 0))
	if len(chain) == 0 {
		return ast.Handle{}
	}
	isTypeOf := mask == ast.SymbolFlagsValue
	if ast.SomeDeclaration(chain[0], hasNonGlobalAugmentationExternalModuleSymbol) {
		var nonRootParts ast.Handle
		if len(chain) > 1 {
			nonRootParts = b.createAccessFromSymbolChain(chain, len(chain)-1, 1, typeArguments)
		}
		typeParameterNodes := typeArguments
		if typeParameterNodes == 0 {
			typeParameterNodes = b.lookupTypeParameterNodes(chain, 0)
		}
		contextFile := ast.GetSourceFileOfNode(b.e.MostOriginal(b.ctx.enclosingDeclaration))
		targetFile := ast.GetSourceFileOfModule(chain[0])
		var specifier string
		var attributes ast.Handle
		if b.ch.compilerOptions.GetModuleResolutionKind() == core.ModuleResolutionKindNode16 || b.ch.compilerOptions.GetModuleResolutionKind() == core.ModuleResolutionKindNodeNext {
			if targetFile != nil && contextFile != nil && b.ch.program.GetEmitModuleFormatOfFile(targetFile) == core.ModuleKindESNext && b.ch.program.GetEmitModuleFormatOfFile(targetFile) != b.ch.program.GetEmitModuleFormatOfFile(contextFile) {
				specifier = b.getSpecifierForModuleSymbol(chain[0], core.ModuleKindESNext)
				attributes = b.f.NewImportAttributes(ast.KindWithKeyword, b.f.NewList([]ast.Handle{b.f.NewImportAttribute(b.newStringLiteral("resolution-mode"), b.newStringLiteral("import"))}), false)
			}
		}
		if len(specifier) == 0 {
			specifier = b.getSpecifierForModuleSymbol(chain[0], core.ResolutionModeNone)
		}
		if (b.ctx.flags&nodebuilder.FlagsAllowNodeModulesRelativePaths == 0) && strings.Contains(specifier, "/node_modules/") {
			oldSpecifier := specifier
			if b.ch.compilerOptions.GetModuleResolutionKind() == core.ModuleResolutionKindNode16 || b.ch.compilerOptions.GetModuleResolutionKind() == core.ModuleResolutionKindNodeNext {
				swappedMode := core.ModuleKindESNext
				if b.ch.program.GetEmitModuleFormatOfFile(contextFile) == core.ModuleKindESNext {
					swappedMode = core.ModuleKindCommonJS
				}
				specifier = b.getSpecifierForModuleSymbol(chain[0], swappedMode)
				if strings.Contains(specifier, "/node_modules/") {
					specifier = oldSpecifier
				} else {
					modeStr := "require"
					if swappedMode == core.ModuleKindESNext {
						modeStr = "import"
					}
					attributes = b.f.NewImportAttributes(ast.KindWithKeyword, b.f.NewList([]ast.Handle{b.f.NewImportAttribute(b.newStringLiteral("resolution-mode"), b.newStringLiteral(modeStr))}), false)
				}
			}
			if attributes.IsNil() {
				b.ctx.encounteredError = true
				b.ctx.tracker.ReportLikelyUnsafeImportRequiredError(oldSpecifier, symbol.Name)
			}
		}
		lit := b.f.NewLiteralTypeNode(b.newStringLiteral(specifier))
		b.ctx.approximateLength += len(specifier) + 10
		if nonRootParts.IsNil() || ast.IsEntityName(nonRootParts) {
			if !nonRootParts.IsNil() {
			}
			return b.f.NewImportTypeNode(isTypeOf, lit, attributes, nonRootParts, typeParameterNodes)
		}
		splitNode := getTopmostIndexedAccessType(nonRootParts)
		qualifier := splitNode.ObjectType().TypeReferenceNodeTypeName()
		return b.f.NewIndexedAccessTypeNode(b.f.NewImportTypeNode(isTypeOf, lit, attributes, qualifier, typeParameterNodes), splitNode.IndexType())
	}
	entityName := b.createAccessFromSymbolChain(chain, len(chain)-1, 0, typeArguments)
	if ast.IsIndexedAccessTypeNode(entityName) {
		return entityName
	}
	if ast.IsEntityName(entityName) {
		if isTypeOf {
			return b.f.NewTypeQueryNode(entityName, 0)
		}
		return b.f.NewTypeReferenceNode(entityName, typeArguments)
	}
	if isTypeOf && ast.IsExpressionWithTypeArguments(entityName) {
		expr := entityName
		return b.f.NewTypeQueryNode(b.f.DeepCloneNode(expr.Expression()), expr.TypeArgumentList())
	}
	return entityName
}
func getTopmostIndexedAccessType(node ast.Handle) ast.Handle {
	if ast.IsIndexedAccessTypeNode(node.ObjectType()) {
		return getTopmostIndexedAccessType(node.ObjectType())
	}
	return node
}
func (b *NodeBuilderImpl) createAccessFromSymbolChain(chain []*ast.Symbol, index int, stopper int, overrideTypeArguments ast.ListRef) ast.Handle {
	typeParameterNodes := overrideTypeArguments
	if index != (len(chain) - 1) {
		typeParameterNodes = b.lookupTypeParameterNodes(chain, index)
	}
	symbol := chain[index]
	var parent *ast.Symbol
	if index > 0 {
		parent = chain[index-1]
	}
	var symbolName string
	if index == 0 {
		b.ctx.flags |= nodebuilder.FlagsInInitialEntityName
		symbolName = b.getNameOfSymbolAsWritten(symbol)
		b.ctx.approximateLength += len(symbolName) + 1
		b.ctx.flags ^= nodebuilder.FlagsInInitialEntityName
	} else {
		if parent != nil {
			exports := b.ch.getExportsOfSymbol(parent)
			if exports != nil {
				res, ok := exports[symbol.Name]
				if symbol.Name != ast.InternalSymbolNameExportEquals && !isLateBoundName(symbol.Name) && ok && res != nil && b.ch.getSymbolIfSameReference(res, symbol) != nil {
					symbolName = symbol.Name
				} else {
					results := make(map[*ast.Symbol]string, 1)
					for name, ex := range exports {
						if b.ch.getSymbolIfSameReference(ex, symbol) != nil && !isLateBoundName(name) && name != ast.InternalSymbolNameExportEquals {
							results[ex] = name
						}
					}
					resultSymbols := slices.Collect(maps.Keys(results))
					if len(resultSymbols) > 0 {
						b.ch.sortSymbols(resultSymbols)
						symbolName = results[resultSymbols[0]]
					}
				}
			}
		}
	}
	if len(symbolName) == 0 {
		var name ast.Handle
		for _, d := range ast.DeclarationNodes(symbol) {
			name = ast.GetNameOfDeclaration(d)
			if !name.IsNil() {
				break
			}
		}
		if !name.IsNil() && ast.IsComputedPropertyName(name) && ast.IsEntityName(name.Expression()) {
			lhs := b.createAccessFromSymbolChain(chain, index-1, stopper, overrideTypeArguments)
			if ast.IsEntityName(lhs) {
				return b.f.NewIndexedAccessTypeNode(b.f.NewParenthesizedTypeNode(b.f.NewTypeQueryNode(lhs, 0)), b.f.NewTypeQueryNode(name.Expression(), 0))
			}
			return lhs
		}
		symbolName = b.getNameOfSymbolAsWritten(symbol)
	}
	b.ctx.approximateLength += len(symbolName) + 1
	if (b.ctx.flags&nodebuilder.FlagsForbidIndexedAccessSymbolReferences == 0) && parent != nil && b.ch.getMembersOfSymbol(parent) != nil && b.ch.getMembersOfSymbol(parent)[symbol.Name] != nil && b.ch.getSymbolIfSameReference(b.ch.getMembersOfSymbol(parent)[symbol.Name], symbol) != nil {
		lhs := b.createAccessFromSymbolChain(chain, index-1, stopper, overrideTypeArguments)
		if ast.IsIndexedAccessTypeNode(lhs) {
			return b.f.NewIndexedAccessTypeNode(lhs, b.f.NewLiteralTypeNode(b.newStringLiteral(symbolName)))
		}
		return b.f.NewIndexedAccessTypeNode(b.f.NewTypeReferenceNode(lhs, typeParameterNodes), b.f.NewLiteralTypeNode(b.newStringLiteral(symbolName)))
	}
	identifier := b.newIdentifier(symbolName, symbol)
	b.e.AddEmitFlags(identifier, printer.EFNoAsciiEscaping)
	if index > stopper {
		lhs := b.createAccessFromSymbolChain(chain, index-1, stopper, overrideTypeArguments)
		if b.ctx.flags&nodebuilder.FlagsUseInstantiationExpressions == 0 || ast.IsEntityName(lhs) && typeParameterNodes == 0 {
			return b.f.NewQualifiedName(lhs, identifier)
		}
		return b.createExpressionWithTypeArguments(b.f.NewPropertyAccessExpression(b.createAccessExpression(lhs), ast.Handle{}, identifier, ast.NodeFlagsNone), typeParameterNodes)
	}
	return identifier
}
func (b *NodeBuilderImpl) symbolToExpression(symbol *ast.Symbol, mask ast.SymbolFlags) ast.Handle {
	chain := b.lookupSymbolChain(symbol, mask, false)
	return b.createExpressionFromSymbolChain(chain, len(chain)-1)
}
func (b *NodeBuilderImpl) createExpressionFromSymbolChain(chain []*ast.Symbol, index int) ast.Handle {
	typeParameterNodes := b.lookupExpressionChainTypeArgumentNodes(chain, index)
	symbol := chain[index]
	if index == 0 {
		b.ctx.flags |= nodebuilder.FlagsInInitialEntityName
	}
	symbolName := b.getNameOfSymbolAsWritten(symbol)
	if index == 0 {
		b.ctx.flags ^= nodebuilder.FlagsInInitialEntityName
	}
	if startsWithSingleOrDoubleQuote(symbolName) && ast.SomeDeclaration(symbol, hasNonGlobalAugmentationExternalModuleSymbol) {
		specifier := b.getSpecifierForModuleSymbol(symbol, core.ResolutionModeNone)
		b.ctx.approximateLength += 2 + len(specifier)
		return b.newStringLiteral(specifier)
	}
	if index == 0 || canUsePropertyAccess(symbolName) {
		identifier := b.newIdentifier(symbolName, symbol)
		b.e.AddEmitFlags(identifier, printer.EFNoAsciiEscaping)
		b.ctx.approximateLength += 1 + len(symbolName)
		if index > 0 {
			result := b.f.NewPropertyAccessExpression(b.createExpressionFromSymbolChain(chain, index-1), ast.Handle{}, identifier, ast.NodeFlagsNone)
			b.e.AddEmitFlags(result, printer.EFNoIndentation)
			return b.createExpressionWithTypeArguments(result, typeParameterNodes)
		}
		return b.createExpressionWithTypeArguments(identifier, typeParameterNodes)
	}
	if startsWithSquareBracket(symbolName) {
		symbolName = symbolName[1 : len(symbolName)-1]
	}
	var expression ast.Handle
	if startsWithSingleOrDoubleQuote(symbolName) && symbol.Flags&ast.SymbolFlagsEnumMember == 0 {
		literalText := stringutil.UnquoteString(symbolName)
		b.ctx.approximateLength += len(literalText) + 2
		expression = b.newStringLiteralEx(literalText, symbolName[0] == '\'')
	} else if jsnum.FromString(symbolName).String() == symbolName {
		b.ctx.approximateLength += len(symbolName)
		expression = b.f.NewNumericLiteral(symbolName, ast.TokenFlagsNone)
	}
	if expression.IsNil() {
		b.ctx.approximateLength += len(symbolName)
		expression = b.newIdentifier(symbolName, symbol)
		b.e.AddEmitFlags(expression, printer.EFNoAsciiEscaping)
	}
	b.ctx.approximateLength += 2
	return b.createExpressionWithTypeArguments(b.f.NewElementAccessExpression(b.createExpressionFromSymbolChain(chain, index-1), ast.Handle{}, expression, ast.NodeFlagsNone), typeParameterNodes)
}
func canUsePropertyAccess(name string) bool {
	if len(name) == 0 {
		return false
	}
	if strings.HasPrefix(name, "#") {
		return len(name) > 1 && scanner.IsIdentifierText(name[1:], core.LanguageVariantStandard)
	}
	return scanner.IsIdentifierText(name, core.LanguageVariantStandard)
}
func startsWithSingleOrDoubleQuote(str string) bool {
	return strings.HasPrefix(str, "'") || strings.HasPrefix(str, "\"")
}
func startsWithSquareBracket(str string) bool {
	return strings.HasPrefix(str, "[")
}
func isDefaultBindingContext(location ast.Handle) bool {
	return location.Kind == ast.KindSourceFile || ast.IsAmbientModule(location)
}
func (b *NodeBuilderImpl) getNameOfSymbolFromNameType(symbol *ast.Symbol) string {
	if b.ch.valueSymbolLinks.Has(symbol) {
		nameType := b.ch.valueSymbolLinks.Get(symbol).nameType
		if nameType == nil {
			return ""
		}
		if nameType.flags&TypeFlagsStringOrNumberLiteral != 0 {
			var name string
			switch v := nameType.AsLiteralType().value.(type) {
			case string:
				name = v
			case jsnum.Number:
				name = v.String()
			}
			if !scanner.IsIdentifierText(name, core.LanguageVariantStandard) && !isNumericLiteralName(name) {
				return b.ch.valueToString(nameType.AsLiteralType().value)
			}
			if isNumericLiteralName(name) && strings.HasPrefix(name, "-") {
				return fmt.Sprintf("[%s]", name)
			}
			return name
		}
		if nameType.flags&TypeFlagsUniqueESSymbol != 0 {
			text := b.getNameOfSymbolAsWritten(nameType.AsUniqueESSymbolType().symbol)
			return fmt.Sprintf("[%s]", text)
		}
	}
	return ""
}

func (b *NodeBuilderImpl) getNameOfSymbolAsWritten(symbol *ast.Symbol) string {
	result, ok := b.ctx.remappedSymbolReferences[ast.GetSymbolId(symbol)]
	if ok {
		symbol = result
	}
	if symbol.Name == ast.InternalSymbolNameDefault && (b.ctx.flags&nodebuilder.FlagsUseAliasDefinedOutsideCurrentScope == 0) && ((b.ctx.flags&nodebuilder.FlagsInInitialEntityName == 0) || len(symbol.Declarations) == 0 || (!b.ctx.enclosingDeclaration.IsNil() && ast.FindAncestor(ast.NodeOf(symbol.Declarations[0]), isDefaultBindingContext) != ast.FindAncestor(b.ctx.enclosingDeclaration, isDefaultBindingContext))) {
		return "default"
	}
	if len(symbol.Declarations) > 0 {
		var name ast.Handle
		for _, d := range ast.DeclarationNodes(symbol) {
			if n := ast.GetNameOfDeclaration(d); !n.IsNil() {
				name = n
				break
			}
		}
		if !name.IsNil() {
			if ast.IsComputedPropertyName(name) && symbol.CheckFlags&ast.CheckFlagsLate == 0 {
				if b.ch.valueSymbolLinks.Has(symbol) && b.ch.valueSymbolLinks.Get(symbol).nameType != nil && b.ch.valueSymbolLinks.Get(symbol).nameType.flags&TypeFlagsStringOrNumberLiteral != 0 {
					result := b.getNameOfSymbolFromNameType(symbol)
					if len(result) > 0 {
						return result
					}
				}
			}
			return scanner.DeclarationNameToString(name)
		}
		declaration := ast.DeclarationNodes(symbol).First()
		if !declaration.Parent().IsNil() && declaration.Parent().Kind == ast.KindVariableDeclaration {
			return scanner.DeclarationNameToString(declaration.Parent().VariableDeclarationName())
		}
		if ast.IsClassExpression(declaration) || ast.IsFunctionExpression(declaration) || ast.IsArrowFunction(declaration) {
			if b.ctx != nil && !b.ctx.encounteredError && b.ctx.flags&nodebuilder.FlagsAllowAnonymousIdentifier == 0 {
				b.ctx.encounteredError = true
			}
			switch declaration.Kind {
			case ast.KindClassExpression:
				return "(Anonymous class)"
			case ast.KindFunctionExpression, ast.KindArrowFunction:
				return "(Anonymous function)"
			}
		}
	}
	name := b.getNameOfSymbolFromNameType(symbol)
	if len(name) > 0 {
		return name
	}
	return ast.EscapeInternalSymbolName(symbol.Name)
}

func (b *NodeBuilderImpl) getTypeParametersOfClassOrInterface(symbol *ast.Symbol) []*Type {
	result := make([]*Type, 0)
	result = append(result, b.ch.getOuterTypeParametersOfClassOrInterface(symbol)...)
	result = append(result, b.ch.getLocalTypeParametersOfClassOrInterfaceOrTypeAlias(symbol)...)
	return result
}
func (b *NodeBuilderImpl) lookupTypeParameterNodes(chain []*ast.Symbol, index int) ast.ListRef {
	debug.Assert(chain != nil && 0 <= index && index < len(chain))
	symbol := chain[index]
	symbolId := ast.GetSymbolId(symbol)
	if b.ctx.typeParameterSymbolList.Has(symbolId) {
		return 0
	}
	b.ctx.typeParameterSymbolList.Add(symbolId)
	if b.ctx.flags&nodebuilder.FlagsWriteTypeParametersInQualifiedName != 0 && index < (len(chain)-1) {
		if typeArgumentNodes := b.lookupInstantiatedTypeArgumentNodes(chain, index); typeArgumentNodes != 0 {
			return typeArgumentNodes
		} else {
			typeParameterNodes := b.typeParametersToTypeParameterDeclarations(symbol)
			if len(typeParameterNodes) > 0 {
				return b.f.NewList(typeParameterNodes)
			}
			return 0
		}
	}
	return 0
}

func (b *NodeBuilderImpl) lookupSymbolChain(symbol *ast.Symbol, meaning ast.SymbolFlags, yieldModuleSymbol bool) []*ast.Symbol {
	b.ctx.tracker.TrackSymbol(symbol, b.ctx.enclosingDeclaration, meaning)
	return b.lookupSymbolChainWorker(symbol, meaning, yieldModuleSymbol)
}
func (b *NodeBuilderImpl) lookupSymbolChainWorker(symbol *ast.Symbol, meaning ast.SymbolFlags, yieldModuleSymbol bool) []*ast.Symbol {
	var chain []*ast.Symbol
	isTypeParameter := symbol.Flags&ast.SymbolFlagsTypeParameter != 0
	if !isTypeParameter && (!b.ctx.enclosingDeclaration.IsNil() || b.ctx.flags&nodebuilder.FlagsUseFullyQualifiedType != 0) && (b.ctx.internalFlags&nodebuilder.InternalFlagsDoNotIncludeSymbolChain == 0) {
		res := b.getSymbolChain(symbol, meaning, true, yieldModuleSymbol)
		chain = res
		debug.Assert(chain != nil)
		debug.Assert(len(chain) > 0)
	} else {
		chain = append(chain, symbol)
	}
	return chain
}

type sortedSymbolNamePair struct {
	sym  *ast.Symbol
	name string
}

func (b *NodeBuilderImpl) getSymbolChain(symbol *ast.Symbol, meaning ast.SymbolFlags, endOfChain bool, yieldModuleSymbol bool) []*ast.Symbol {
	accessibleSymbolChain := b.ch.getAccessibleSymbolChain(symbol, b.ctx.enclosingDeclaration, meaning, b.ctx.flags&nodebuilder.FlagsUseOnlyExternalAliasing != 0)
	qualifierMeaning := meaning
	if len(accessibleSymbolChain) > 1 {
		qualifierMeaning = getQualifiedLeftMeaning(meaning)
	}
	if len(accessibleSymbolChain) == 0 || b.ch.needsQualification(accessibleSymbolChain[0], b.ctx.enclosingDeclaration, qualifierMeaning) {
		root := symbol
		if len(accessibleSymbolChain) > 0 {
			root = accessibleSymbolChain[0]
		}
		parents := b.ch.getContainersOfSymbol(root, b.ctx.enclosingDeclaration, meaning)
		if len(parents) > 0 {
			parentSpecifiers := core.Map(parents, func(symbol *ast.Symbol) sortedSymbolNamePair {
				if ast.SomeDeclaration(symbol, hasNonGlobalAugmentationExternalModuleSymbol) {
					return sortedSymbolNamePair{symbol, b.getSpecifierForModuleSymbol(symbol, core.ResolutionModeNone)}
				}
				return sortedSymbolNamePair{symbol, ""}
			})
			slices.SortStableFunc(parentSpecifiers, b.sortByBestName)
			for _, pair := range parentSpecifiers {
				parent := pair.sym
				parentChain := b.getSymbolChain(parent, getQualifiedLeftMeaning(meaning), false, yieldModuleSymbol)
				if len(parentChain) > 0 {
					if parent.Exports != nil {
						exported, ok := parent.Exports[ast.InternalSymbolNameExportEquals]
						if ok && b.ch.getSymbolIfSameReference(exported, symbol) != nil {
							accessibleSymbolChain = parentChain
							break
						}
					}
					nextSyms := accessibleSymbolChain
					if len(nextSyms) == 0 {
						fallback := b.ch.getAliasForSymbolInContainer(parent, symbol)
						if fallback == nil {
							fallback = symbol
						}
						nextSyms = append(nextSyms, fallback)
					}
					accessibleSymbolChain = append(parentChain, nextSyms...)
					break
				}
			}
		}
	}
	if len(accessibleSymbolChain) > 0 {
		return accessibleSymbolChain
	}
	if endOfChain || (symbol.Flags&(ast.SymbolFlagsTypeLiteral|ast.SymbolFlagsObjectLiteral) == 0) {
		if !endOfChain && !yieldModuleSymbol && ast.SomeDeclaration(symbol, hasNonGlobalAugmentationExternalModuleSymbol) {
			return nil
		}
		return []*ast.Symbol{symbol}
	}
	return nil
}
func (b_ *NodeBuilderImpl) sortByBestName(a sortedSymbolNamePair, b sortedSymbolNamePair) int {
	specifierA := a.name
	specifierB := b.name
	if len(specifierA) > 0 && len(specifierB) > 0 {
		isBRelative := tspath.PathIsRelative(specifierB)
		if tspath.PathIsRelative(specifierA) == isBRelative {
			return modulespecifiers.CountPathComponents(specifierA) - modulespecifiers.CountPathComponents(specifierB)
		}
		if isBRelative {
			return -1
		}
		return 1
	}
	return b_.ch.compareSymbols(a.sym, b.sym)
}
func canHaveModuleSpecifier(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	switch node.Kind {
	case ast.KindVariableDeclaration, ast.KindBindingElement, ast.KindImportDeclaration, ast.KindExportDeclaration, ast.KindImportEqualsDeclaration, ast.KindImportClause, ast.KindNamespaceExport, ast.KindNamespaceImport, ast.KindExportSpecifier, ast.KindImportSpecifier, ast.KindImportType:
		return true
	}
	return false
}
func TryGetModuleSpecifierFromDeclaration(node ast.Handle) ast.Handle {
	res := tryGetModuleSpecifierFromDeclarationWorker(node)
	if res.IsNil() || !ast.IsStringLiteral(res) {
		return ast.Handle{}
	}
	return res
}
func tryGetModuleSpecifierFromDeclarationWorker(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindVariableDeclaration, ast.KindBindingElement:
		requireCall := ast.FindAncestor(node.Initializer(), func(node ast.Handle) bool {
			return ast.IsRequireCall(node, true)
		})
		if requireCall.IsNil() {
			return ast.Handle{}
		}
		return requireCall.ArgumentsSeq().At(0)
	case ast.KindImportDeclaration, ast.KindExportDeclaration, ast.KindJSDocImportTag:
		return node.ModuleSpecifier()
	case ast.KindImportEqualsDeclaration:
		ref := node.ImportEqualsDeclarationModuleReference()
		if ref.Kind != ast.KindExternalModuleReference {
			return ast.Handle{}
		}
		return ref.Expression()
	case ast.KindImportClause:
		if ast.IsImportDeclaration(node.Parent()) {
			return node.Parent().ModuleSpecifier()
		}
		return node.Parent().ModuleSpecifier()
	case ast.KindNamespaceExport:
		return node.Parent().ModuleSpecifier()
	case ast.KindNamespaceImport:
		if ast.IsImportDeclaration(node.Parent().Parent()) {
			return node.Parent().Parent().ModuleSpecifier()
		}
		return node.Parent().Parent().ModuleSpecifier()
	case ast.KindExportSpecifier:
		return node.Parent().Parent().ModuleSpecifier()
	case ast.KindImportSpecifier:
		if ast.IsImportDeclaration(node.Parent().Parent().Parent()) {
			return node.Parent().Parent().Parent().ModuleSpecifier()
		}
		return node.Parent().Parent().Parent().ModuleSpecifier()
	case ast.KindImportType:
		if ast.IsLiteralImportTypeNode(node) {
			return node.ImportTypeNodeArgument().LiteralTypeNodeLiteral()
		}
		return ast.Handle{}
	default:
		debug.AssertNever(node)
		return ast.Handle{}
	}
}
func (b *NodeBuilderImpl) getSpecifierForModuleSymbol(symbol *ast.Symbol, overrideImportMode core.ResolutionMode) string {
	file := ast.GetDeclarationOfKind(symbol, ast.KindSourceFile)
	if file.IsNil() {
		var equivalentSymbol *ast.Symbol
		for _, d := range ast.DeclarationNodes(symbol) {
			if s := b.ch.getFileSymbolIfFileSymbolExportEqualsContainer(d, symbol); s != nil {
				equivalentSymbol = s
				break
			}
		}
		if equivalentSymbol != nil {
			file = ast.GetDeclarationOfKind(equivalentSymbol, ast.KindSourceFile)
		}
	}
	if file.IsNil() {
		if ast.IsAmbientModuleSymbolName(symbol.Name) {
			return stringutil.StripQuotes(symbol.Name)
		}
	}
	if b.ctx.enclosingFile == nil {
		if ast.IsAmbientModuleSymbolName(symbol.Name) {
			return stringutil.StripQuotes(symbol.Name)
		}
		return ast.GetSourceFileOfModule(symbol).FileName()
	}
	enclosingDeclaration := b.e.MostOriginal(b.ctx.enclosingDeclaration)
	var originalModuleSpecifier ast.Handle
	if canHaveModuleSpecifier(enclosingDeclaration) {
		originalModuleSpecifier = TryGetModuleSpecifierFromDeclaration(enclosingDeclaration)
	}
	contextFile := b.ctx.enclosingFile
	resolutionMode := overrideImportMode
	if resolutionMode == core.ResolutionModeNone && !originalModuleSpecifier.IsNil() {
		resolutionMode = b.ch.program.GetModeForUsageLocation(contextFile, originalModuleSpecifier)
	} else if resolutionMode == core.ResolutionModeNone && contextFile != nil {
		resolutionMode = b.ch.program.GetDefaultResolutionModeForFile(contextFile)
	}
	cacheKey := module.ModeAwareCacheKey{Name: string(contextFile.Path()), Mode: resolutionMode}
	links := b.symbolLinks.Get(symbol)
	if links.specifierCache == nil {
		links.specifierCache = make(module.ModeAwareCache[string])
	}
	result, ok := links.specifierCache[cacheKey]
	if ok {
		return result
	}
	host := b.ctx.host
	specifierCompilerOptions := b.ch.compilerOptions
	specifierPref := modulespecifiers.ImportModuleSpecifierPreferenceProjectRelative
	endingPref := modulespecifiers.ImportModuleSpecifierEndingPreferenceNone
	if resolutionMode == core.ResolutionModeESM {
		endingPref = modulespecifiers.ImportModuleSpecifierEndingPreferenceJs
	}
	allSpecifiers := modulespecifiers.GetModuleSpecifiers(symbol, b.ch, specifierCompilerOptions, contextFile, host, modulespecifiers.UserPreferences{ImportModuleSpecifierPreference: specifierPref, ImportModuleSpecifierEnding: endingPref}, modulespecifiers.ModuleSpecifierOptions{OverrideImportMode: overrideImportMode}, false)
	if len(allSpecifiers) == 0 {
		links.specifierCache[cacheKey] = ""
		return ""
	}
	specifier := allSpecifiers[0]
	links.specifierCache[cacheKey] = specifier
	return specifier
}
func (b *NodeBuilderImpl) typeParameterToDeclarationWithConstraint(typeParameter *Type, constraintNode ast.Handle) ast.Handle {
	restoreFlags := b.saveRestoreFlags()
	b.ctx.flags &^= nodebuilder.FlagsWriteTypeParametersInQualifiedName
	modifiers := ast.CreateModifiersFromModifierFlags(b.ch.getTypeParameterModifiers(typeParameter), b.f.NewModifier)
	var modifiersList ast.ListRef
	if len(modifiers) > 0 {
		modifiersList = b.f.NewModifierList(modifiers)
	}
	name := b.typeParameterToName(typeParameter)
	defaultParameter := b.ch.getDefaultFromTypeParameter(typeParameter)
	var defaultParameterDeclarationNode ast.Handle
	if defaultParameter != nil {
		defaultParameterDeclarationNode = b.typeToTypeNode(defaultParameter)
	}
	restoreFlags()
	return b.f.NewTypeParameterDeclaration(modifiersList, name, constraintNode, ast.Handle{}, defaultParameterDeclarationNode)
}

func (b *NodeBuilderImpl) setTextRange(range_ ast.Handle, location ast.Handle) ast.Handle {
	if range_.IsNil() {
		return range_
	}
	if !ast.NodeIsSynthesized(range_) || (range_.Flags()&ast.NodeFlagsSynthesized == 0) || b.ctx.enclosingFile == nil || b.ctx.enclosingFile != ast.GetSourceFileOfNode(b.e.MostOriginal(range_)) {
		original := range_
		range_ = b.f.DeepCloneNode(range_)
		range_.SetLoc(core.NewTextRange(-1, -1))
		if symbol, ok := b.idToSymbol[original]; ok {
			b.idToSymbol[range_] = symbol
		}
	}
	if range_ == location || location.IsNil() {
		return range_
	}
	original := b.e.Original(range_)
	for !original.IsNil() && original != location {
		original = b.e.Original(original)
	}
	if original.IsNil() {
		b.e.SetOriginalEx(range_, location, true)
	}
	if b.ctx.enclosingFile != nil && b.ctx.enclosingFile == ast.GetSourceFileOfNode(b.e.MostOriginal(location)) {
		range_.SetLoc(location.Loc())
		return range_
	} else {
		range_.SetLoc(core.NewTextRange(-1, -1))
	}
	return range_
}
func (b *NodeBuilderImpl) typeParameterShadowsOtherTypeParameterInScope(name string, typeParameter *Type) bool {
	result := b.ch.resolveName(b.ctx.enclosingDeclaration, name, ast.SymbolFlagsType, nil, false, false)
	if result != nil && result.Flags&ast.SymbolFlagsTypeParameter != 0 {
		return result != typeParameter.symbol
	}
	return false
}
func (b *NodeBuilderImpl) typeParameterToName(typeParameter *Type) ast.Handle {
	if b.ctx.flags&nodebuilder.FlagsGenerateNamesForShadowedTypeParams != 0 {
		if cached, ok := b.ctx.typeParameterNames.Get(typeParameter.id); ok {
			return cached
		}
	}
	result := b.symbolToName(typeParameter.symbol, ast.SymbolFlagsType, true)
	if !ast.IsIdentifier(result) {
		return b.f.NewIdentifier("(Missing type parameter)")
	}
	if typeParameter.symbol != nil && len(typeParameter.symbol.Declarations) > 0 {
		decl := ast.DeclarationNodes(typeParameter.symbol).First()
		if !decl.IsNil() && ast.IsTypeParameterDeclaration(decl) {
			result = b.setTextRange(result, decl.Name())
		}
	}
	if b.ctx.flags&nodebuilder.FlagsGenerateNamesForShadowedTypeParams != 0 {
		rawText := result.Text()
		i, _ := b.ctx.typeParameterNamesByTextNextNameCount.Get(rawText)
		text := rawText
		for true {
			if !b.ctx.typeParameterNamesByText.Has(text) && !b.typeParameterShadowsOtherTypeParameterInScope(text, typeParameter) {
				break
			}
			i++
			text = fmt.Sprintf("%s_%d", rawText, i)
		}
		if text != rawText {
			result = b.newIdentifier(text, typeParameter.symbol)
		}
		b.ctx.typeParameterNamesByTextNextNameCount.Set(rawText, i)
		b.ctx.typeParameterNames.Set(typeParameter.id, result)
		b.ctx.typeParameterNamesByText.Add(text)
	}
	return result
}
func (b *NodeBuilderImpl) isMappedTypeHomomorphic(mapped *Type) bool {
	return b.ch.getHomomorphicTypeVariable(mapped) != nil
}
func (b *NodeBuilderImpl) isHomomorphicMappedTypeWithNonHomomorphicInstantiation(mapped *MappedType) bool {
	return mapped.target != nil && !b.isMappedTypeHomomorphic(mapped.AsType()) && b.isMappedTypeHomomorphic(mapped.target)
}
func (b *NodeBuilderImpl) createMappedTypeNodeFromType(t *Type) ast.Handle {
	debug.Assert(t.Flags()&TypeFlagsObject != 0)
	mapped := t.AsMappedType()
	var readonlyToken ast.Handle
	if !mapped.declaration.MappedTypeNodeReadonlyToken().IsNil() {
		readonlyToken = b.f.NewToken(mapped.declaration.MappedTypeNodeReadonlyToken().Kind)
	}
	var questionToken ast.Handle
	if !mapped.declaration.QuestionToken().IsNil() {
		questionToken = b.f.NewToken(mapped.declaration.QuestionToken().Kind)
	}
	var appropriateConstraintTypeNode ast.Handle
	var newTypeVariable ast.Handle
	templateType := b.ch.getTemplateTypeFromMappedType(t)
	typeParameter := b.ch.getTypeParameterFromMappedType(t)
	needsModifierPreservingWrapper := !b.ch.isMappedTypeWithKeyofConstraintDeclaration(t) && b.ch.getModifiersTypeFromMappedType(t).flags&TypeFlagsUnknown == 0 && b.ctx.flags&nodebuilder.FlagsGenerateNamesForShadowedTypeParams != 0 && !(b.ch.getConstraintTypeFromMappedType(t).flags&TypeFlagsTypeParameter != 0 && b.ch.getConstraintOfTypeParameter(b.ch.getConstraintTypeFromMappedType(t)).flags&TypeFlagsIndex != 0)
	if b.ch.isMappedTypeWithKeyofConstraintDeclaration(t) {
		if b.ctx.flags&nodebuilder.FlagsGenerateNamesForShadowedTypeParams != 0 && b.isHomomorphicMappedTypeWithNonHomomorphicInstantiation(mapped) {
			newConstraintParam := b.ch.newTypeParameter(b.ch.newSymbol(ast.SymbolFlagsTypeParameter, "T"))
			name := b.typeParameterToName(newConstraintParam)
			target := t.Target()
			newTypeVariable = b.f.NewTypeReferenceNode(name, 0)
			templateType = b.ch.instantiateType(b.ch.getTemplateTypeFromMappedType(target), newTypeMapper([]*Type{b.ch.getTypeParameterFromMappedType(target), b.ch.getModifiersTypeFromMappedType(target)}, []*Type{typeParameter, newConstraintParam}))
		}
		indexTarget := newTypeVariable
		if indexTarget.IsNil() {
			indexTarget = b.typeToTypeNode(b.ch.getModifiersTypeFromMappedType(t))
		}
		appropriateConstraintTypeNode = b.f.NewTypeOperatorNode(ast.KindKeyOfKeyword, indexTarget)
	} else if needsModifierPreservingWrapper {
		newParam := b.ch.newTypeParameter(b.ch.newSymbol(ast.SymbolFlagsTypeParameter, "T"))
		name := b.typeParameterToName(newParam)
		newTypeVariable = b.f.NewTypeReferenceNode(name, 0)
		appropriateConstraintTypeNode = newTypeVariable
	} else {
		appropriateConstraintTypeNode = b.typeToTypeNode(b.ch.getConstraintTypeFromMappedType(t))
	}
	cleanup := b.enterNewScope(mapped.declaration, nil, []*Type{b.ch.getTypeParameterFromMappedType(t)}, nil, nil)
	typeParameterDeclarationNode := b.typeParameterToDeclarationWithConstraint(typeParameter, appropriateConstraintTypeNode)
	var nameTypeNode ast.Handle
	if !mapped.declaration.NameType().IsNil() {
		nameTypeNode = b.typeToTypeNode(b.ch.getNameTypeFromMappedType(t))
	}
	templateTypeNode := b.typeToTypeNode(b.ch.removeMissingType(templateType, getMappedTypeModifiers(t)&MappedTypeModifiersIncludeOptional != 0))
	cleanup()
	result := b.f.NewMappedTypeNode(readonlyToken, typeParameterDeclarationNode, nameTypeNode, questionToken, templateTypeNode, 0)
	b.ctx.approximateLength += 10
	b.e.AddEmitFlags(result, printer.EFSingleLine)
	if b.ctx.flags&nodebuilder.FlagsGenerateNamesForShadowedTypeParams != 0 && b.isHomomorphicMappedTypeWithNonHomomorphicInstantiation(mapped) {
		rawConstraintTypeFromDeclaration := b.getTypeFromTypeNode(mapped.declaration.MappedTypeNodeTypeParameter().TypeParameterDeclarationConstraint().Type(), false)
		if rawConstraintTypeFromDeclaration != nil {
			rawConstraintTypeFromDeclaration = b.ch.getConstraintOfTypeParameter(rawConstraintTypeFromDeclaration)
		}
		if rawConstraintTypeFromDeclaration == nil {
			rawConstraintTypeFromDeclaration = b.ch.unknownType
		}
		originalConstraint := b.ch.instantiateType(rawConstraintTypeFromDeclaration, mapped.mapper)
		var originalConstraintNode ast.Handle
		if originalConstraint.flags&TypeFlagsUnknown == 0 {
			originalConstraintNode = b.typeToTypeNode(originalConstraint)
		}
		return b.f.NewConditionalTypeNode(b.typeToTypeNode(b.ch.getModifiersTypeFromMappedType(t)), b.f.NewInferTypeNode(b.f.NewTypeParameterDeclaration(0, b.f.DeepCloneNode(newTypeVariable.TypeReferenceNodeTypeName()), originalConstraintNode, ast.Handle{}, ast.Handle{})), result, b.f.NewKeywordTypeNode(ast.KindNeverKeyword))
	} else if needsModifierPreservingWrapper {
		return b.f.NewConditionalTypeNode(b.typeToTypeNode(b.ch.getConstraintTypeFromMappedType(t)), b.f.NewInferTypeNode(b.f.NewTypeParameterDeclaration(0, b.f.DeepCloneNode(newTypeVariable.TypeReferenceNodeTypeName()), b.f.NewTypeOperatorNode(ast.KindKeyOfKeyword, b.typeToTypeNode(b.ch.getModifiersTypeFromMappedType(t))), ast.Handle{}, ast.Handle{})), result, b.f.NewKeywordTypeNode(ast.KindNeverKeyword))
	}
	return result
}
func (b *NodeBuilderImpl) typePredicateToTypePredicateNode(predicate *TypePredicate) ast.Handle {
	var assertsModifier ast.Handle
	if predicate.kind == TypePredicateKindAssertsIdentifier || predicate.kind == TypePredicateKindAssertsThis {
		assertsModifier = b.f.NewToken(ast.KindAssertsKeyword)
	}
	var parameterName ast.Handle
	if predicate.kind == TypePredicateKindIdentifier || predicate.kind == TypePredicateKindAssertsIdentifier {
		parameterName = b.f.NewIdentifier(predicate.parameterName)
		b.e.AddEmitFlags(parameterName, printer.EFNoAsciiEscaping)
	} else {
		parameterName = b.f.NewThisTypeNode()
	}
	var typeNode ast.Handle
	if predicate.t != nil {
		typeNode = b.typeToTypeNode(predicate.t)
	}
	return b.f.NewTypePredicateNode(assertsModifier, parameterName, typeNode)
}
func (b *NodeBuilderImpl) typeToTypeNodeHelperWithPossibleReusableTypeNode(t *Type, typeNode ast.Handle) ast.Handle {
	if t == nil {
		return b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	if !b.isActivelyExpanding() && !typeNode.IsNil() && b.getTypeFromTypeNode(typeNode, false) == t {
		reused := b.tryReuseExistingNodeHelper(typeNode)
		if !reused.IsNil() {
			b.checkTypeExpandability(t)
			return reused
		}
	}
	return b.typeToTypeNode(t)
}
func (b *NodeBuilderImpl) typeParameterToDeclaration(parameter *Type) ast.Handle {
	constraint := b.ch.getConstraintOfTypeParameter(parameter)
	var constraintNode ast.Handle
	if constraint != nil {
		constraintNode = b.typeToTypeNodeHelperWithPossibleReusableTypeNode(constraint, b.ch.getConstraintDeclaration(parameter))
	}
	return b.typeParameterToDeclarationWithConstraint(parameter, constraintNode)
}
func (b *NodeBuilderImpl) symbolToTypeParameterDeclarations(symbol *ast.Symbol) []ast.Handle {
	return b.typeParametersToTypeParameterDeclarations(symbol)
}
func (b *NodeBuilderImpl) typeParametersToTypeParameterDeclarations(symbol *ast.Symbol) []ast.Handle {
	targetSymbol := b.ch.getTargetSymbol(symbol)
	if targetSymbol.Flags&(ast.SymbolFlagsClass|ast.SymbolFlagsInterface|ast.SymbolFlagsAlias) != 0 {
		var results []ast.Handle
		params := b.ch.getLocalTypeParametersOfClassOrInterfaceOrTypeAlias(symbol)
		for _, param := range params {
			results = append(results, b.typeParameterToDeclaration(param))
		}
		return results
	} else if targetSymbol.Flags&ast.SymbolFlagsFunction != 0 {
		var results []ast.Handle
		for _, param := range b.ch.getTypeParametersFromDeclaration(ast.NodeOf(symbol.ValueDeclaration)) {
			results = append(results, b.typeParameterToDeclaration(param))
		}
		return results
	}
	return nil
}
func getEffectiveParameterDeclaration(symbol *ast.Symbol) ast.Handle {
	parameterDeclaration := ast.GetDeclarationOfKind(symbol, ast.KindParameter)
	if !parameterDeclaration.IsNil() {
		return parameterDeclaration
	}
	if symbol.Flags&ast.SymbolFlagsTransient == 0 {
		return ast.GetDeclarationOfKind(symbol, ast.KindJSDocParameterTag)
	}
	return ast.Handle{}
}
func (b *NodeBuilderImpl) symbolToParameterDeclaration(parameterSymbol *ast.Symbol, preserveModifierFlags bool) ast.Handle {
	parameterDeclaration := getEffectiveParameterDeclaration(parameterSymbol)
	parameterType := b.ch.getTypeOfSymbol(parameterSymbol)
	parameterTypeNode := b.serializeTypeForDeclaration(parameterDeclaration, parameterType, parameterSymbol, true)
	var modifiers ast.ListRef
	if b.ctx.flags&nodebuilder.FlagsOmitParameterModifiers == 0 && preserveModifierFlags && !parameterDeclaration.IsNil() && ast.CanHaveModifiers(parameterDeclaration) {
		originals := core.Filter(parameterDeclaration.ModifierNodesSeq().Slice(), ast.IsModifier)
		clones := core.Map(originals, func(node ast.Handle) ast.Handle {
			return b.f.DeepCloneNode(node)
		})
		if len(clones) > 0 {
			modifiers = b.f.NewModifierList(clones)
		}
	}
	isRest := !parameterDeclaration.IsNil() && isRestParameter(parameterDeclaration) || parameterSymbol.CheckFlags&ast.CheckFlagsRestParameter != 0
	var dotDotDotToken ast.Handle
	if isRest {
		dotDotDotToken = b.f.NewToken(ast.KindDotDotDotToken)
	}
	name := b.parameterToParameterDeclarationName(parameterSymbol, parameterDeclaration)
	isOptional := !parameterDeclaration.IsNil() && b.ch.isOptionalParameter(parameterDeclaration) || parameterSymbol.CheckFlags&ast.CheckFlagsOptionalParameter != 0
	var questionToken ast.Handle
	if isOptional {
		questionToken = b.f.NewToken(ast.KindQuestionToken)
	}
	parameterNode := b.f.NewParameterDeclaration(modifiers, dotDotDotToken, name, questionToken, parameterTypeNode, ast.Handle{})
	b.ctx.approximateLength += len(parameterSymbol.Name) + 3
	return parameterNode
}
func (b *NodeBuilderImpl) parameterToParameterDeclarationName(parameterSymbol *ast.Symbol, parameterDeclaration ast.Handle) ast.Handle {
	if parameterDeclaration.IsNil() || parameterDeclaration.Name().IsNil() {
		return b.newIdentifier(parameterSymbol.Name, parameterSymbol)
	}
	name := parameterDeclaration.Name()
	switch name.Kind {
	case ast.KindIdentifier:
		cloned := b.f.DeepCloneNode(name)
		b.e.SetEmitFlags(cloned, printer.EFNoAsciiEscaping)
		b.idToSymbol[cloned] = parameterSymbol
		return cloned
	case ast.KindQualifiedName:
		cloned := b.f.DeepCloneNode(name.QualifiedNameRight())
		b.e.SetEmitFlags(cloned, printer.EFNoAsciiEscaping)
		b.idToSymbol[cloned] = parameterSymbol
		return cloned
	default:
		return b.cloneBindingName(name)
	}
}
func (b *NodeBuilderImpl) cloneBindingName(node ast.Handle) ast.Handle {
	if ast.IsComputedPropertyName(node) && b.ch.isLateBindableName(node) {
		b.trackComputedName(node.Expression(), b.ctx.enclosingDeclaration)
	}
	visited := b.cloneBindingNameVisitor.VisitEachChild(node)
	if ast.IsBindingElement(visited) {
		bindingElement := visited
		visited = b.f.UpdateBindingElement(bindingElement, bindingElement.DotDotDotToken(), bindingElement.PropertyName(), bindingElement.Name(), ast.Handle{})
	}
	if !ast.NodeIsSynthesized(visited) {
		visited = b.f.DeepCloneNode(visited)
	}
	b.e.SetEmitFlags(visited, printer.EFSingleLine|printer.EFNoAsciiEscaping)
	return visited
}
func (b *NodeBuilderImpl) serializeTypeForExpression(expr ast.Handle) ast.Handle {
	t := b.ch.instantiateType(b.ch.getWidenedType(b.ch.getRegularTypeOfExpression(expr)), b.ctx.mapper)
	return b.typeToTypeNode(t)
}
func (b *NodeBuilderImpl) serializeInferredReturnTypeForSignature(signature *Signature, returnType *Type) ast.Handle {
	oldSuppressReportInferenceFallback := b.ctx.suppressReportInferenceFallback
	b.ctx.suppressReportInferenceFallback = true
	typePredicate := b.ch.getTypePredicateOfSignature(signature)
	var returnTypeNode ast.Handle
	if typePredicate != nil {
		var predicate *TypePredicate
		if b.ctx.mapper != nil {
			predicate = b.ch.instantiateTypePredicate(typePredicate, b.ctx.mapper)
		} else {
			predicate = typePredicate
		}
		returnTypeNode = b.typePredicateToTypePredicateNodeHelper(predicate)
	} else {
		returnTypeNode = b.typeToTypeNode(returnType)
	}
	b.ctx.suppressReportInferenceFallback = oldSuppressReportInferenceFallback
	return returnTypeNode
}
func (b *NodeBuilderImpl) typePredicateToTypePredicateNodeHelper(typePredicate *TypePredicate) ast.Handle {
	var assertsModifier ast.Handle
	if typePredicate.kind == TypePredicateKindAssertsThis || typePredicate.kind == TypePredicateKindAssertsIdentifier {
		assertsModifier = b.f.NewToken(ast.KindAssertsKeyword)
	} else {
		assertsModifier = ast.Handle{}
	}
	var parameterName ast.Handle
	if typePredicate.kind == TypePredicateKindIdentifier || typePredicate.kind == TypePredicateKindAssertsIdentifier {
		parameterName = b.newIdentifier(typePredicate.parameterName, nil)
		b.e.SetEmitFlags(parameterName, printer.EFNoAsciiEscaping)
	} else {
		parameterName = b.f.NewThisTypeNode()
	}
	var typeNode ast.Handle
	if typePredicate.t != nil {
		typeNode = b.typeToTypeNode(typePredicate.t)
	}
	return b.f.NewTypePredicateNode(assertsModifier, parameterName, typeNode)
}

type SignatureToSignatureDeclarationOptions struct {
	modifiers     []ast.Handle
	name          ast.Handle
	questionToken ast.Handle
}

func (b *NodeBuilderImpl) signatureToSignatureDeclarationHelper(signature *Signature, kind ast.Kind, options *SignatureToSignatureDeclarationOptions) ast.Handle {
	var typeParameters []ast.Handle
	expandedParams, cleanup := b.enterSignatureScope(signature)
	b.ctx.approximateLength += 3
	if b.ctx.flags&nodebuilder.FlagsWriteTypeArgumentsOfSignature != 0 && signature.target != nil && signature.mapper != nil && len(signature.target.typeParameters) != 0 {
		for _, parameter := range signature.target.typeParameters {
			typeParameters = append(typeParameters, b.typeToTypeNode(b.ch.instantiateType(parameter, signature.mapper)))
		}
	} else {
		for _, parameter := range signature.typeParameters {
			typeParameters = append(typeParameters, b.typeParameterToDeclaration(parameter))
		}
	}
	restoreFlags := b.saveRestoreFlags()
	b.ctx.flags &^= nodebuilder.FlagsSuppressAnyReturnType
	parameters := core.Map(core.IfElse(core.Some(expandedParams, func(p *ast.Symbol) bool {
		return p != expandedParams[len(expandedParams)-1] && p.CheckFlags&ast.CheckFlagsRestParameter != 0
	}), signature.parameters, expandedParams), func(parameter *ast.Symbol) ast.Handle {
		return b.symbolToParameterDeclaration(parameter, kind == ast.KindConstructor)
	})
	var thisParameter ast.Handle
	if b.ctx.flags&nodebuilder.FlagsOmitThisParameter != 0 {
		thisParameter = ast.Handle{}
	} else {
		thisParameter = b.tryGetThisParameterDeclaration(signature)
	}
	if !thisParameter.IsNil() {
		parameters = append([]ast.Handle{thisParameter}, parameters...)
	}
	restoreFlags()
	returnTypeNode := b.serializeReturnTypeForSignature(signature, true)
	var modifiers []ast.Handle
	if options != nil {
		modifiers = options.modifiers
	}
	if (kind == ast.KindConstructorType) && signature.flags&SignatureFlagsAbstract != 0 {
		flags := ast.ModifiersToFlags(modifiers)
		modifiers = ast.CreateModifiersFromModifierFlags(flags|ast.ModifierFlagsAbstract, b.f.NewModifier)
	}
	paramList := b.f.NewList(parameters)
	var typeParamList ast.ListRef
	if len(typeParameters) != 0 {
		typeParamList = b.f.NewList(typeParameters)
	}
	var modifierList ast.ListRef
	if len(modifiers) > 0 {
		modifierList = b.f.NewModifierList(modifiers)
	}
	var name ast.Handle
	if options != nil {
		name = options.name
	}
	if name.IsNil() {
		name = b.f.NewIdentifier("")
	}
	var node ast.Handle
	switch {
	case kind == ast.KindCallSignature:
		node = b.f.NewCallSignatureDeclaration(typeParamList, paramList, returnTypeNode)
	case kind == ast.KindConstructSignature:
		node = b.f.NewConstructSignatureDeclaration(typeParamList, paramList, returnTypeNode)
	case kind == ast.KindMethodSignature:
		var questionToken ast.Handle
		if options != nil {
			questionToken = options.questionToken
		}
		node = b.f.NewMethodSignatureDeclaration(modifierList, name, questionToken, typeParamList, paramList, returnTypeNode)
	case kind == ast.KindMethodDeclaration:
		node = b.f.NewMethodDeclaration(modifierList, ast.Handle{}, name, ast.Handle{}, typeParamList, paramList, returnTypeNode, ast.Handle{}, ast.Handle{})
	case kind == ast.KindConstructor:
		node = b.f.NewConstructorDeclaration(modifierList, 0, paramList, ast.Handle{}, ast.Handle{}, ast.Handle{})
	case kind == ast.KindGetAccessor:
		node = b.f.NewGetAccessorDeclaration(modifierList, name, 0, paramList, returnTypeNode, ast.Handle{}, ast.Handle{})
	case kind == ast.KindSetAccessor:
		node = b.f.NewSetAccessorDeclaration(modifierList, name, 0, paramList, ast.Handle{}, ast.Handle{}, ast.Handle{})
	case kind == ast.KindIndexSignature:
		node = b.f.NewIndexSignatureDeclaration(modifierList, paramList, returnTypeNode)
	case kind == ast.KindFunctionType:
		if returnTypeNode.IsNil() {
			returnTypeNode = b.f.NewTypeReferenceNode(b.f.NewIdentifier(""), 0)
		}
		node = b.f.NewFunctionTypeNode(typeParamList, paramList, returnTypeNode)
	case kind == ast.KindConstructorType:
		if returnTypeNode.IsNil() {
			returnTypeNode = b.f.NewTypeReferenceNode(b.f.NewIdentifier(""), 0)
		}
		node = b.f.NewConstructorTypeNode(modifierList, typeParamList, paramList, returnTypeNode)
	case kind == ast.KindFunctionDeclaration:
		node = b.f.NewFunctionDeclaration(modifierList, ast.Handle{}, name, typeParamList, paramList, returnTypeNode, ast.Handle{}, ast.Handle{})
	case kind == ast.KindFunctionExpression:
		node = b.f.NewFunctionExpression(modifierList, ast.Handle{}, name, typeParamList, paramList, returnTypeNode, ast.Handle{}, b.f.NewBlock(b.f.NewList([]ast.Handle{}), false))
	case kind == ast.KindArrowFunction:
		node = b.f.NewArrowFunction(modifierList, typeParamList, paramList, returnTypeNode, ast.Handle{}, ast.Handle{}, b.f.NewBlock(b.f.NewList([]ast.Handle{}), false))
	default:
		panic("Unhandled kind in signatureToSignatureDeclarationHelper")
	}
	cleanup()
	return node
}
func (c *Checker) getExpandedParameters(sig *Signature, skipUnionExpanding bool) [][]*ast.Symbol {
	if signatureHasRestParameter(sig) {
		restIndex := len(sig.parameters) - 1
		restSymbol := sig.parameters[restIndex]
		restType := c.getTypeOfSymbol(restSymbol)
		getUniqAssociatedNamesFromTupleType := func(t *Type, restSymbol *ast.Symbol) []string {
			names := core.MapIndex(t.Target().AsTupleType().elementInfos, func(info TupleElementInfo, i int) string {
				return c.getTupleElementLabel(info, restSymbol, i)
			})
			if len(names) > 0 {
				duplicates := []int{}
				uniqueNames := make(map[string]bool)
				for i, name := range names {
					_, ok := uniqueNames[name]
					if ok {
						duplicates = append(duplicates, i)
					} else {
						uniqueNames[name] = true
					}
				}
				counters := make(map[string]int)
				for _, i := range duplicates {
					counter, ok := counters[names[i]]
					if !ok {
						counter = 1
					}
					var name string
					for true {
						name = fmt.Sprintf("%s_%d", names[i], counter)
						_, ok := uniqueNames[name]
						if ok {
							counter++
							continue
						} else {
							uniqueNames[name] = true
							break
						}
					}
					names[i] = name
					counters[names[i]] = counter + 1
				}
			}
			return names
		}
		expandSignatureParametersWithTupleMembers := func(restType *Type, restIndex int, restSymbol *ast.Symbol) []*ast.Symbol {
			elementTypes := c.getTypeArguments(restType)
			associatedNames := getUniqAssociatedNamesFromTupleType(restType, restSymbol)
			restParams := core.MapIndex(elementTypes, func(t *Type, i int) *ast.Symbol {
				name := associatedNames[i]
				flags := restType.Target().AsTupleType().elementInfos[i].flags
				var checkFlags ast.CheckFlags
				switch {
				case flags&ElementFlagsVariable != 0:
					checkFlags = ast.CheckFlagsRestParameter
				case flags&ElementFlagsOptional != 0:
					checkFlags = ast.CheckFlagsOptionalParameter
				}
				symbol := c.newSymbolEx(ast.SymbolFlagsFunctionScopedVariable, name, checkFlags)
				links := c.valueSymbolLinks.Get(symbol)
				if flags&ElementFlagsRest != 0 {
					links.resolvedType = c.createArrayType(t)
				} else {
					links.resolvedType = t
				}
				return symbol
			})
			return core.Concatenate(sig.parameters[0:restIndex], restParams)
		}
		if isTupleType(restType) {
			return [][]*ast.Symbol{expandSignatureParametersWithTupleMembers(restType, restIndex, restSymbol)}
		} else if !skipUnionExpanding && restType.flags&TypeFlagsUnion != 0 && core.Every(restType.AsUnionType().types, isTupleType) {
			return core.Map(restType.AsUnionType().types, func(t *Type) []*ast.Symbol {
				return expandSignatureParametersWithTupleMembers(t, restIndex, restSymbol)
			})
		}
	}
	return [][]*ast.Symbol{sig.parameters}
}
func (b *NodeBuilderImpl) tryGetThisParameterDeclaration(signature *Signature) ast.Handle {
	if signature.thisParameter != nil {
		return b.symbolToParameterDeclaration(signature.thisParameter, false)
	}
	if !signature.declaration.IsNil() && ast.IsInJSFile(signature.declaration) {
	}
	return ast.Handle{}
}

func (b *NodeBuilderImpl) serializeReturnTypeForSignature(signature *Signature, tryReuse bool) ast.Handle {
	suppressAny := b.ctx.flags&nodebuilder.FlagsSuppressAnyReturnType != 0
	restoreFlags := b.saveRestoreFlags()
	if suppressAny {
		b.ctx.flags &^= nodebuilder.FlagsSuppressAnyReturnType
	}
	var returnTypeNode ast.Handle
	var returnType *Type
	if !signature.declaration.IsNil() && !ast.NodeIsSynthesized(signature.declaration) {
		symbol := b.ch.getSymbolOfDeclaration(signature.declaration)
		var ok bool
		returnType, ok = b.ctx.enclosingSymbolTypes[ast.GetSymbolId(symbol)]
		if !ok || returnType == nil {
			returnType = b.ch.instantiateType(b.ch.getReturnTypeOfSignature(signature), b.ctx.mapper)
		}
	} else {
		returnType = b.ch.getReturnTypeOfSignature(signature)
	}
	if !(suppressAny && IsTypeAny(returnType)) {
		if !b.isActivelyExpanding() && tryReuse && !b.ctx.enclosingDeclaration.IsNil() && !signature.declaration.IsNil() && !ast.NodeIsSynthesized(signature.declaration) {
			declarationSymbol := b.ch.getSymbolOfDeclaration(signature.declaration)
			restore := b.addSymbolTypeToContext(declarationSymbol, returnType)
			pt := b.pc.GetReturnTypeOfSignature(signature.declaration)
			if b.pseudoTypeEquivalentToType(pt, returnType, false, !b.ctx.suppressReportInferenceFallback) {
				typePredicate := b.ch.getTypePredicateOfSignature(signature)
				if typePredicate != nil && !b.pseudoReturnTypeMatchesPredicate(pt, typePredicate) {
					if !b.ctx.suppressReportInferenceFallback {
						b.ctx.tracker.ReportInferenceFallback(signature.declaration)
					}
					pt = nil
				}
				if pt != nil {
					returnTypeNode = b.pseudoTypeToNodeWithCheckerFallback(pt, returnType)
				}
			}
			restore()
		}
		if returnTypeNode.IsNil() {
			returnTypeNode = b.serializeInferredReturnTypeForSignature(signature, returnType)
		}
	}
	if returnTypeNode.IsNil() && !suppressAny {
		returnTypeNode = b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	restoreFlags()
	return returnTypeNode
}
func (b *NodeBuilderImpl) isTriviallySerializableComputedName(e ast.Handle) bool {
	shapeGood := !e.IsNil() && !e.Name().IsNil() && ast.IsComputedPropertyName(e.Name()) && ast.IsEntityNameExpression(e.Name().Expression())
	if !shapeGood {
		return false
	}
	return b.ch.GetEmitResolver().isEntityNameVisible(e.Name().Expression(), b.ctx.enclosingDeclaration, false).Accessibility == printer.SymbolAccessibilityAccessible
}
func (b *NodeBuilderImpl) indexInfoToObjectComputedNamesOrSignatureDeclaration(indexInfo *IndexInfo, typeNode ast.Handle) []ast.Handle {
	if len(indexInfo.components) > 0 {
		allComponentComputedNamesSerializable := !b.ctx.enclosingDeclaration.IsNil() && core.Every(indexInfo.components, b.isTriviallySerializableComputedName)
		if allComponentComputedNamesSerializable {
			newComponents := core.Filter(indexInfo.components, func(c ast.Handle) bool {
				return !b.ch.hasLateBindableName(c)
			})
			bailed := false
			results := core.Map(newComponents, func(e ast.Handle) ast.Handle {
				name := b.reuseNode(e.Name())
				if !name.IsNil() {
					b.trackComputedName(e.Name().Expression(), b.ctx.enclosingDeclaration)
					var mods ast.ListRef
					if indexInfo.isReadonly {
						mods = b.f.NewModifierList([]ast.Handle{b.f.NewModifier(ast.KindReadonlyKeyword)})
					}
					var postfixToken ast.Handle
					if !e.PostfixToken().IsNil() {
						postfixToken = b.f.DeepCloneNode(e.PostfixToken())
					}
					var currentTypeNode ast.Handle
					if !typeNode.IsNil() {
						currentTypeNode = b.f.DeepCloneNode(typeNode)
					} else {
						currentTypeNode = b.typeToTypeNode(b.ch.getTypeOfSymbol(e.Symbol()))
					}
					sig := b.f.NewPropertySignatureDeclaration(mods, name, postfixToken, currentTypeNode, ast.Handle{})
					sig.SetLoc(e.Loc())
					return sig
				}
				bailed = true
				return ast.Handle{}
			})
			if !bailed {
				return results
			}
		}
	}
	return []ast.Handle{b.indexInfoToIndexSignatureDeclarationHelper(indexInfo, typeNode)}
}
func (b *NodeBuilderImpl) indexInfoToIndexSignatureDeclarationHelper(indexInfo *IndexInfo, typeNode ast.Handle) ast.Handle {
	name := getNameFromIndexInfo(indexInfo)
	indexerTypeNode := b.typeToTypeNode(indexInfo.keyType)
	indexingParameter := b.f.NewParameterDeclaration(0, ast.Handle{}, b.newIdentifier(name, nil), ast.Handle{}, indexerTypeNode, ast.Handle{})
	if typeNode.IsNil() {
		if indexInfo.valueType == nil {
			typeNode = b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
		} else {
			typeNode = b.typeToTypeNode(indexInfo.valueType)
		}
	}
	if indexInfo.valueType == nil && b.ctx.flags&nodebuilder.FlagsAllowEmptyIndexInfoType == 0 {
		b.ctx.encounteredError = true
	}
	b.ctx.approximateLength += len(name) + 4
	var modifiers ast.ListRef
	if indexInfo.isReadonly {
		b.ctx.approximateLength += 9
		modifiers = b.f.NewModifierList([]ast.Handle{b.f.NewModifier(ast.KindReadonlyKeyword)})
	}
	return b.f.NewIndexSignatureDeclaration(modifiers, b.f.NewList([]ast.Handle{indexingParameter}), typeNode)
}
func hasTypeAnnotation(declaration ast.Handle) bool {
	if declaration.IsNil() || declaration.Type().IsNil() {
		return false
	}
	if ast.IsTypeAliasDeclaration(declaration) || ast.IsJSTypeAliasDeclaration(declaration) {
		return false
	}
	return true
}

func (b *NodeBuilderImpl) serializeTypeForDeclaration(declaration ast.Handle, t *Type, symbol *ast.Symbol, tryReuse bool) ast.Handle {
	if declaration.IsNil() {
		if symbol != nil {
			declaration = ast.NodeOf(symbol.ValueDeclaration)
			if declaration.IsNil() {
				declaration = ast.DeclarationNodes(symbol).First()
			}
		}
	}
	if symbol == nil {
		symbol = b.ch.getSymbolOfDeclaration(declaration)
	}
	if t == nil {
		if symbol == nil {
			if ast.IsVariableLike(declaration) {
				t = b.ch.getTypeForVariableLikeDeclaration(declaration, false, CheckModeNormal)
			} else {
				t = b.ch.errorType
			}
		} else {
			t = b.ctx.enclosingSymbolTypes[ast.GetSymbolId(symbol)]
			if t == nil {
				if symbol.Flags&ast.SymbolFlagsAccessor != 0 && declaration.Kind == ast.KindSetAccessor {
					t = b.ch.instantiateType(b.ch.getWriteTypeOfSymbol(symbol), b.ctx.mapper)
				} else if symbol != nil && (symbol.Flags&(ast.SymbolFlagsTypeLiteral|ast.SymbolFlagsSignature) == 0) {
					t = b.ch.instantiateType(b.ch.getWidenedLiteralType(b.ch.getTypeOfSymbol(symbol)), b.ctx.mapper)
				} else {
					t = b.ch.errorType
				}
			}
		}
	}
	requiresAddingUndefined := !declaration.IsNil() && (ast.IsParameterDeclaration(declaration) || ast.IsPropertySignatureDeclaration(declaration) || ast.IsPropertyDeclaration(declaration)) && b.ch.GetEmitResolver().requiresAddingImplicitUndefined(declaration, symbol, b.ctx.enclosingDeclaration)
	addUndefinedForParameter := requiresAddingUndefined && (ast.IsParameterDeclaration(declaration))
	if addUndefinedForParameter {
		t = b.ch.getOptionalType(t, false)
	}
	restoreFlags := b.saveRestoreFlags()
	if t.flags&TypeFlagsUniqueESSymbol != 0 && t.symbol == symbol && (b.ctx.enclosingDeclaration.IsNil() || ast.SomeDeclaration(symbol, func(d ast.Handle) bool {
		return ast.GetSourceFileOfNode(d) == b.ctx.enclosingFile
	})) {
		b.ctx.flags |= nodebuilder.FlagsAllowUniqueESSymbolType
	}
	var result ast.Handle
	var reportedInferenceFallback bool
	if !b.isActivelyExpanding() && tryReuse && !b.ctx.enclosingDeclaration.IsNil() && !declaration.IsNil() && (ast.IsAccessor(declaration) || (ast.HasInferredType(declaration) && !ast.NodeIsSynthesized(declaration) && (t.ObjectFlags()&ObjectFlagsRequiresWidening) == 0)) {
		var remove func()
		if symbol != nil {
			remove = b.addSymbolTypeToContext(symbol, t)
		}
		var pt *pseudochecker.PseudoType
		if ast.IsAccessor(declaration) {
			pt = b.pc.GetTypeOfAccessor(declaration)
		} else {
			pt = b.pc.GetTypeOfDeclaration(declaration)
		}
		if (pt == nil || pt.Kind == pseudochecker.PseudoTypeKindNoResult) && ast.IsBinaryExpression(declaration) && symbol != nil {
			if decl := ast.FindSymbolDeclaration(symbol, hasTypeAnnotation); !decl.IsNil() {
				pt = b.pc.GetTypeOfDeclaration(decl)
			}
		}
		reportErrors := !b.ctx.suppressReportInferenceFallback
		if b.pseudoTypeEquivalentToType(pt, t, !requiresAddingUndefined && (ast.IsParameterDeclaration(declaration) || ast.IsPropertySignatureDeclaration(declaration) || ast.IsPropertyDeclaration(declaration)) && isOptionalDeclaration(declaration), reportErrors) {
			ptt := b.pseudoTypeToType(pt)
			if ptt != nil && requiresAddingUndefined && containsNonMissingUndefinedType(b.ch, t) && !containsNonMissingUndefinedType(b.ch, ptt) {
				pt = pseudochecker.NewPseudoTypeUnion([]*pseudochecker.PseudoType{pt, pseudochecker.PseudoTypeUndefined})
			}
			result = b.pseudoTypeToNodeWithCheckerFallback(pt, t)
		} else {
			reportedInferenceFallback = reportErrors && pt.Kind == pseudochecker.PseudoTypeKindInferred && len(pt.AsPseudoTypeInferred().ErrorNodes) > 0
			shouldAddUndefined := false
			if requiresAddingUndefined {
				if ptt := b.pseudoTypeToType(pt); ptt != nil {
					shouldAddUndefined = !containsNonMissingUndefinedType(b.ch, ptt)
				} else {
					shouldAddUndefined = !pseudochecker.CouldAlreadyReferToUndefinedType(pt)
				}
			}
			if shouldAddUndefined {
				pt = pseudochecker.NewPseudoTypeUnion([]*pseudochecker.PseudoType{pt, pseudochecker.PseudoTypeUndefined})
				if b.pseudoTypeEquivalentToType(pt, t, false, reportErrors) {
					result = b.pseudoTypeToNodeWithCheckerFallback(pt, t)
					reportedInferenceFallback = false
				}
			}
		}
		if remove != nil {
			remove()
		}
	}
	if result.IsNil() {
		if reportedInferenceFallback {
			oldSuppress := b.ctx.suppressReportInferenceFallback
			b.ctx.suppressReportInferenceFallback = true
			result = b.typeToTypeNode(t)
			b.ctx.suppressReportInferenceFallback = oldSuppress
		} else {
			result = b.typeToTypeNode(t)
		}
	}
	restoreFlags()
	if result.IsNil() {
		return b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	return result
}

const MAX_REVERSE_MAPPED_NESTING_INSPECTION_DEPTH = 3

func (b *NodeBuilderImpl) shouldUsePlaceholderForProperty(propertySymbol *ast.Symbol) bool {
	if propertySymbol.CheckFlags&ast.CheckFlagsReverseMapped == 0 {
		return false
	}
	if slices.Contains(b.ctx.reverseMappedStack, propertySymbol) {
		return true
	}
	if len(b.ctx.reverseMappedStack) > 0 {
		last := b.ctx.reverseMappedStack[len(b.ctx.reverseMappedStack)-1]
		if b.ch.ReverseMappedSymbolLinks.Has(last) {
			links := b.ch.ReverseMappedSymbolLinks.TryGet(last)
			propertyType := links.propertyType
			if propertyType != nil && propertyType.objectFlags&ObjectFlagsAnonymous == 0 {
				return true
			}
		}
	}
	if len(b.ctx.reverseMappedStack) < MAX_REVERSE_MAPPED_NESTING_INSPECTION_DEPTH {
		return false
	}
	if !b.ch.ReverseMappedSymbolLinks.Has(propertySymbol) {
		return false
	}
	propertyLinks := b.ch.ReverseMappedSymbolLinks.TryGet(propertySymbol)
	propMappedType := propertyLinks.mappedType
	if propMappedType == nil || propMappedType.symbol == nil {
		return false
	}
	for i := range b.ctx.reverseMappedStack {
		if i > MAX_REVERSE_MAPPED_NESTING_INSPECTION_DEPTH {
			break
		}
		prop := b.ctx.reverseMappedStack[len(b.ctx.reverseMappedStack)-1-i]
		if b.ch.ReverseMappedSymbolLinks.Has(prop) {
			links := b.ch.ReverseMappedSymbolLinks.TryGet(prop)
			mappedType := links.mappedType
			if mappedType != nil && mappedType.symbol == propMappedType.symbol {
				return true
			}
		}
	}
	return false
}
func (b *NodeBuilderImpl) trackComputedName(accessExpression ast.Handle, enclosingDeclaration ast.Handle) {
	firstIdentifier := ast.GetFirstIdentifier(accessExpression)
	name := b.ch.resolveName(enclosingDeclaration, firstIdentifier.Text(), ast.SymbolFlagsValue|ast.SymbolFlagsExportValue, nil, true, false)
	if name != nil {
		b.ctx.tracker.TrackSymbol(name, enclosingDeclaration, ast.SymbolFlagsValue)
	} else {
		fallback := b.ch.resolveName(firstIdentifier, firstIdentifier.Text(), ast.SymbolFlagsValue|ast.SymbolFlagsExportValue, nil, true, false)
		if fallback != nil {
			b.ctx.tracker.TrackSymbol(fallback, enclosingDeclaration, ast.SymbolFlagsValue)
		}
	}
}

type propertyNameNodeKind int

const (
	propertyNameNodeKindIdentifier propertyNameNodeKind = iota
	propertyNameNodeKindNumericLiteral
	propertyNameNodeKindStringLiteral
)

func classifyPropertyName(name string, stringNamed bool, isMethod bool) propertyNameNodeKind {
	if isMethod && name == "new" {
		return propertyNameNodeKindStringLiteral
	}
	if scanner.IsIdentifierText(name, core.LanguageVariantStandard) {
		return propertyNameNodeKindIdentifier
	}
	return core.IfElse(!stringNamed && isNumericLiteralName(name) && jsnum.FromString(name) >= 0, propertyNameNodeKindNumericLiteral, propertyNameNodeKindStringLiteral)
}
func (b *NodeBuilderImpl) createPropertyNameNodeForIdentifierOrLiteral(name string, singleQuote bool, stringNamed bool, isMethod bool, symbol *ast.Symbol) ast.Handle {
	switch classifyPropertyName(name, stringNamed, isMethod) {
	case propertyNameNodeKindIdentifier:
		return b.newIdentifier(name, symbol)
	case propertyNameNodeKindNumericLiteral:
		return b.f.NewNumericLiteral(name, ast.TokenFlagsNone)
	default:
		return b.f.NewStringLiteral(name, core.IfElse(singleQuote, ast.TokenFlagsSingleQuote, ast.TokenFlagsNone))
	}
}
func (b *NodeBuilderImpl) isStringNamed(d ast.Handle) bool {
	name := ast.GetNameOfDeclaration(d)
	if name.IsNil() {
		return false
	}
	if ast.IsComputedPropertyName(name) {
		t := b.ch.checkExpression(name.Expression())
		return t.flags&TypeFlagsStringLike != 0
	}
	if ast.IsElementAccessExpression(name) {
		t := b.ch.checkExpression(name.ElementAccessExpressionArgumentExpression())
		return t.flags&TypeFlagsStringLike != 0
	}
	return ast.IsStringLiteral(name)
}
func (b *NodeBuilderImpl) isSingleQuotedStringNamed(d ast.Handle) bool {
	name := ast.GetNameOfDeclaration(d)
	return !name.IsNil() && ast.IsStringLiteral(name) && name.StringLiteralTokenFlags()&ast.TokenFlagsSingleQuote != 0
}
func (b *NodeBuilderImpl) getPropertyNameNodeForSymbol(symbol *ast.Symbol, enclosingDeclaration ast.Handle) ast.Handle {
	if symbol.ValueDeclaration != 0 {
		declName := ast.NodeOf(symbol.ValueDeclaration).Name()
		if !declName.IsNil() && ast.IsPrivateIdentifier(declName) {
			return b.f.DeepCloneNode(declName)
		}
	}
	stringNamed := len(symbol.Declarations) != 0 && ast.EveryDeclaration(symbol, b.isStringNamed)
	singleQuote := len(symbol.Declarations) != 0 && ast.EveryDeclaration(symbol, b.isSingleQuotedStringNamed)
	isMethod := symbol.Flags&ast.SymbolFlagsMethod != 0
	fromNameType := b.getPropertyNameNodeForSymbolFromNameType(symbol, enclosingDeclaration, singleQuote, stringNamed, isMethod)
	if !fromNameType.IsNil() {
		return fromNameType
	}
	name := symbol.Name
	const privateNamePrefix = ast.InternalSymbolNamePrefix + "#"
	if strings.HasPrefix(name, privateNamePrefix) {
		name = name[len(privateNamePrefix):]
		name = strings.TrimLeftFunc(name, stringutil.IsDigit)
		name = "__#private" + name
	}
	return b.createPropertyNameNodeForIdentifierOrLiteral(name, singleQuote, stringNamed, isMethod, symbol)
}

func (b *NodeBuilderImpl) getPropertyNameNodeForSymbolFromNameType(symbol *ast.Symbol, enclosingDeclaration ast.Handle, singleQuote bool, stringNamed bool, isMethod bool) ast.Handle {
	if !b.ch.valueSymbolLinks.Has(symbol) {
		return ast.Handle{}
	}
	nameType := b.ch.valueSymbolLinks.TryGet(symbol).nameType
	if nameType == nil {
		return ast.Handle{}
	}
	enumEnclosingDeclaration := enclosingDeclaration
	if enumEnclosingDeclaration.IsNil() && b.ctx.enclosingFile != nil {
		enumEnclosingDeclaration = b.ctx.enclosingFile.ParseRoot()
	}
	if nameType.flags&TypeFlagsEnumLiteral != 0 {
		enumSymbol := nameType.symbol.Parent
		if enumSymbol == nil {
			enumSymbol = nameType.symbol
		}
		if !enumEnclosingDeclaration.IsNil() && b.ch.IsSymbolAccessibleByFlags(enumSymbol, enumEnclosingDeclaration, ast.SymbolFlagsValue) {
			saveEnclosingDeclaration := b.ctx.enclosingDeclaration
			b.ctx.enclosingDeclaration = enumEnclosingDeclaration
			result := b.f.NewComputedPropertyName(b.symbolToExpression(nameType.symbol, ast.SymbolFlagsValue))
			b.ctx.enclosingDeclaration = saveEnclosingDeclaration
			return result
		}
	}
	if nameType.flags&TypeFlagsStringOrNumberLiteral != 0 {
		var name string
		switch nameType.AsLiteralType().value.(type) {
		case jsnum.Number:
			name = nameType.AsLiteralType().value.(jsnum.Number).String()
		case string:
			name = nameType.AsLiteralType().value.(string)
		}
		if !scanner.IsIdentifierText(name, core.LanguageVariantStandard) && (stringNamed || !isNumericLiteralName(name)) {
			node := b.f.NewStringLiteral(name, core.IfElse(singleQuote, ast.TokenFlagsSingleQuote, ast.TokenFlagsNone))
			return node
		}
		if isNumericLiteralName(name) && name[0] == '-' {
			return b.f.NewComputedPropertyName(b.f.NewPrefixUnaryExpression(ast.KindMinusToken, b.f.NewNumericLiteral(name[1:], ast.TokenFlagsNone)))
		}
		return b.createPropertyNameNodeForIdentifierOrLiteral(name, singleQuote, stringNamed, isMethod, symbol)
	}
	if nameType.flags&TypeFlagsUniqueESSymbol != 0 {
		return b.f.NewComputedPropertyName(b.symbolToExpression(nameType.AsUniqueESSymbolType().symbol, ast.SymbolFlagsValue))
	}
	return ast.Handle{}
}
func (b *NodeBuilderImpl) addPropertyToElementList(propertySymbol *ast.Symbol, typeElements []ast.Handle) []ast.Handle {
	propertyIsReverseMapped := propertySymbol.CheckFlags&ast.CheckFlagsReverseMapped != 0
	var propertyType *Type
	if b.shouldUsePlaceholderForProperty(propertySymbol) {
		propertyType = b.ch.anyType
	} else {
		propertyType = b.ch.getNonMissingTypeOfSymbol(propertySymbol)
	}
	saveEnclosingDeclaration := b.ctx.enclosingDeclaration
	b.ctx.enclosingDeclaration = ast.Handle{}
	if isLateBoundName(propertySymbol.Name) {
		if len(propertySymbol.Declarations) > 0 {
			decl := ast.DeclarationNodes(propertySymbol).First()
			if b.ch.hasLateBindableName(decl) {
				if ast.IsBinaryExpression(decl) {
					name := ast.GetNameOfDeclaration(decl)
					if !name.IsNil() && ast.IsElementAccessExpression(name) && ast.IsPropertyAccessEntityNameExpression(name.ElementAccessExpressionArgumentExpression(), false) {
						b.trackComputedName(name.ElementAccessExpressionArgumentExpression(), saveEnclosingDeclaration)
					}
				} else {
					b.trackComputedName(decl.Name().Expression(), saveEnclosingDeclaration)
				}
			}
		} else {
			b.ctx.tracker.ReportNonSerializableProperty(b.ch.symbolToString(propertySymbol))
		}
	}
	if propertySymbol.ValueDeclaration != 0 {
		b.ctx.enclosingDeclaration = ast.NodeOf(propertySymbol.ValueDeclaration)
	} else if len(propertySymbol.Declarations) > 0 && !ast.NodeOf(propertySymbol.Declarations[0]).IsNil() {
		b.ctx.enclosingDeclaration = ast.NodeOf(propertySymbol.Declarations[0])
	} else {
		b.ctx.enclosingDeclaration = saveEnclosingDeclaration
	}
	propertyName := b.getPropertyNameNodeForSymbol(propertySymbol, saveEnclosingDeclaration)
	b.ctx.enclosingDeclaration = saveEnclosingDeclaration
	b.ctx.approximateLength += len(ast.SymbolName(propertySymbol)) + 1
	if propertySymbol.Flags&ast.SymbolFlagsAccessor != 0 {
		writeType := b.ch.getWriteTypeOfSymbol(propertySymbol)
		if !b.ch.isErrorType(propertyType) && !b.ch.isErrorType(writeType) {
			propDeclaration := ast.GetDeclarationOfKind(propertySymbol, ast.KindPropertyDeclaration)
			if propertyType != writeType || propertySymbol.Parent != nil && propertySymbol.Parent.Flags&ast.SymbolFlagsClass != 0 && propDeclaration.IsNil() {
				symbolMapper := b.ch.valueSymbolLinks.Get(propertySymbol).mapper
				if getterDeclaration := ast.GetDeclarationOfKind(propertySymbol, ast.KindGetAccessor); !getterDeclaration.IsNil() {
					getterSignature := b.ch.getSignatureFromDeclaration(getterDeclaration)
					if symbolMapper != nil {
						getterSignature = b.ch.instantiateSignature(getterSignature, symbolMapper)
					}
					getter := b.signatureToSignatureDeclarationHelper(getterSignature, ast.KindGetAccessor, &SignatureToSignatureDeclarationOptions{name: propertyName})
					b.setCommentRange(getter, getterDeclaration)
					typeElements = append(typeElements, getter)
				}
				if setterDeclaration := ast.GetDeclarationOfKind(propertySymbol, ast.KindSetAccessor); !setterDeclaration.IsNil() {
					setterSignature := b.ch.getSignatureFromDeclaration(setterDeclaration)
					if symbolMapper != nil {
						setterSignature = b.ch.instantiateSignature(setterSignature, symbolMapper)
					}
					setter := b.signatureToSignatureDeclarationHelper(setterSignature, ast.KindSetAccessor, &SignatureToSignatureDeclarationOptions{name: propertyName})
					b.setCommentRange(setter, setterDeclaration)
					typeElements = append(typeElements, setter)
				}
				return typeElements
			} else if propertySymbol.Parent != nil && propertySymbol.Parent.Flags&ast.SymbolFlagsClass != 0 && !propDeclaration.IsNil() && !propDeclaration.ModifierNodesSeq().Some(func(m ast.Handle) bool {
				return m.Kind == ast.KindAccessorKeyword
			}) {
				fakeGetterSignature := b.ch.newSignature(SignatureFlagsNone, ast.Handle{}, nil, nil, nil, propertyType, nil, 0)
				fakeGetterDeclaration := b.signatureToSignatureDeclarationHelper(fakeGetterSignature, ast.KindGetAccessor, &SignatureToSignatureDeclarationOptions{name: propertyName})
				b.setCommentRange(fakeGetterDeclaration, propDeclaration)
				typeElements = append(typeElements, fakeGetterDeclaration)
				setterParam := b.ch.newSymbol(ast.SymbolFlagsFunctionScopedVariable, "arg")
				b.ch.valueSymbolLinks.Get(setterParam).resolvedType = writeType
				fakeSetterSignature := b.ch.newSignature(SignatureFlagsNone, ast.Handle{}, nil, nil, []*ast.Symbol{setterParam}, b.ch.voidType, nil, 0)
				fakeSetterDeclaration := b.signatureToSignatureDeclarationHelper(fakeSetterSignature, ast.KindSetAccessor, &SignatureToSignatureDeclarationOptions{name: propertyName})
				typeElements = append(typeElements, fakeSetterDeclaration)
				return typeElements
			}
		}
	}
	var optionalToken ast.Handle
	if propertySymbol.Flags&ast.SymbolFlagsOptional != 0 {
		optionalToken = b.f.NewToken(ast.KindQuestionToken)
	} else {
		optionalToken = ast.Handle{}
	}
	if propertySymbol.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsMethod) != 0 && len(b.ch.getPropertiesOfObjectType(propertyType)) == 0 && !b.ch.isReadonlySymbol(propertySymbol) {
		signatures := b.ch.getSignaturesOfType(b.ch.filterType(propertyType, func(t *Type) bool {
			return t.flags&TypeFlagsUndefined == 0
		}), SignatureKindCall)
		for _, signature := range signatures {
			methodDeclaration := b.signatureToSignatureDeclarationHelper(signature, ast.KindMethodSignature, &SignatureToSignatureDeclarationOptions{name: propertyName, questionToken: optionalToken})
			decl := signature.declaration
			if decl.IsNil() {
				decl = ast.NodeOf(propertySymbol.ValueDeclaration)
			}
			b.setCommentRange(methodDeclaration, decl)
			typeElements = append(typeElements, methodDeclaration)
		}
		if len(signatures) != 0 || optionalToken.IsNil() {
			return typeElements
		}
	}
	var propertyTypeNode ast.Handle
	if b.shouldUsePlaceholderForProperty(propertySymbol) {
		propertyTypeNode = b.createElidedInformationPlaceholder()
	} else {
		if propertyIsReverseMapped {
			b.ctx.reverseMappedStack = append(b.ctx.reverseMappedStack, propertySymbol)
		}
		if propertyType != nil {
			propertyTypeNode = b.serializeTypeForDeclaration(ast.Handle{}, propertyType, propertySymbol, true)
		} else {
			propertyTypeNode = b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
		}
		if propertyIsReverseMapped {
			b.ctx.reverseMappedStack = b.ctx.reverseMappedStack[:len(b.ctx.reverseMappedStack)-1]
		}
	}
	var modifiers ast.ListRef
	if b.ch.isReadonlySymbol(propertySymbol) {
		modifiers = b.f.NewModifierList([]ast.Handle{b.f.NewModifier(ast.KindReadonlyKeyword)})
		b.ctx.approximateLength += 9
	}
	propertySignature := b.f.NewPropertySignatureDeclaration(modifiers, propertyName, optionalToken, propertyTypeNode, ast.Handle{})
	b.setCommentRange(propertySignature, ast.NodeOf(propertySymbol.ValueDeclaration))
	typeElements = append(typeElements, propertySignature)
	return typeElements
}
func (b *NodeBuilderImpl) createTypeNodesFromResolvedType(resolvedType *StructuredType) ast.ListRef {
	if b.checkTruncationLength() {
		if b.ctx.flags&nodebuilder.FlagsNoTruncation != 0 {
			elem := b.f.NewNotEmittedTypeElement()
			return b.f.NewList([]ast.Handle{b.e.AddSyntheticTrailingComment(elem, ast.KindMultiLineCommentTrivia, "elided", false)})
		}
		return b.f.NewList([]ast.Handle{b.f.NewPropertySignatureDeclaration(0, b.f.NewIdentifier("..."), ast.Handle{}, ast.Handle{}, ast.Handle{})})
	}
	var typeElements []ast.Handle
	for _, signature := range resolvedType.CallSignatures() {
		typeElements = append(typeElements, b.signatureToSignatureDeclarationHelper(signature, ast.KindCallSignature, nil))
	}
	for _, signature := range resolvedType.ConstructSignatures() {
		if signature.flags&SignatureFlagsAbstract != 0 {
			continue
		}
		typeElements = append(typeElements, b.signatureToSignatureDeclarationHelper(signature, ast.KindConstructSignature, nil))
	}
	for _, info := range resolvedType.indexInfos {
		typeElements = slices.Concat(typeElements, b.indexInfoToObjectComputedNamesOrSignatureDeclaration(info, core.IfElse(resolvedType.objectFlags&ObjectFlagsReverseMapped != 0, b.createElidedInformationPlaceholder(), ast.Handle{})))
	}
	properties := resolvedType.properties
	if len(properties) == 0 {
		return b.f.NewList(typeElements)
	}
	i := 0
	for _, propertySymbol := range properties {
		if isExpanding(b.ctx) && propertySymbol.Flags&ast.SymbolFlagsPrototype != 0 {
			continue
		}
		i++
		if b.ctx.flags&nodebuilder.FlagsWriteClassExpressionAsTypeLiteral != 0 {
			if propertySymbol.Flags&ast.SymbolFlagsPrototype != 0 {
				continue
			}
			if getDeclarationModifierFlagsFromSymbol(propertySymbol)&(ast.ModifierFlagsPrivate|ast.ModifierFlagsProtected) != 0 {
				b.ctx.tracker.ReportPrivateInBaseOfClassExpression(propertySymbol.Name)
			}
			if IsPrivateIdentifierSymbol(propertySymbol) {
				b.ctx.tracker.ReportPrivateInBaseOfClassExpression(ast.SymbolName(propertySymbol))
			}
		}
		if b.checkTruncationLength() && (i+2 < len(properties)-1) {
			if b.ctx.flags&nodebuilder.FlagsNoTruncation != 0 {
				typeElements[len(typeElements)-1] = b.e.AddSyntheticTrailingComment(typeElements[len(typeElements)-1], ast.KindMultiLineCommentTrivia, fmt.Sprintf("... %d more elided ...", len(properties)-i), false)
			} else {
				text := fmt.Sprintf("... %d more ...", len(properties)-i)
				typeElements = append(typeElements, b.f.NewPropertySignatureDeclaration(0, b.f.NewIdentifier(text), ast.Handle{}, ast.Handle{}, ast.Handle{}))
			}
			typeElements = b.addPropertyToElementList(properties[len(properties)-1], typeElements)
			break
		}
		typeElements = b.addPropertyToElementList(propertySymbol, typeElements)
	}
	if len(typeElements) != 0 {
		return b.f.NewList(typeElements)
	} else {
		return 0
	}
}
func (b *NodeBuilderImpl) createTypeNodeFromObjectType(t *Type) ast.Handle {
	if b.ch.isGenericMappedType(t) || (t.objectFlags&ObjectFlagsMapped != 0 && t.AsMappedType().containsError) {
		return b.createMappedTypeNodeFromType(t)
	}
	resolved := b.ch.resolveStructuredTypeMembers(t)
	callSigs := resolved.CallSignatures()
	ctorSigs := resolved.ConstructSignatures()
	if len(resolved.properties) == 0 && len(resolved.indexInfos) == 0 {
		if len(callSigs) == 0 && len(ctorSigs) == 0 {
			b.ctx.approximateLength += 2
			result := b.f.NewTypeLiteralNode(b.f.NewList([]ast.Handle{}))
			b.e.SetEmitFlags(result, printer.EFSingleLine)
			return result
		}
		if len(callSigs) == 1 && len(ctorSigs) == 0 {
			signature := callSigs[0]
			signatureNode := b.signatureToSignatureDeclarationHelper(signature, ast.KindFunctionType, nil)
			return signatureNode
		}
		if len(ctorSigs) == 1 && len(callSigs) == 0 {
			signature := ctorSigs[0]
			signatureNode := b.signatureToSignatureDeclarationHelper(signature, ast.KindConstructorType, nil)
			return signatureNode
		}
	}
	abstractSignatures := core.Filter(ctorSigs, func(signature *Signature) bool {
		return signature.flags&SignatureFlagsAbstract != 0
	})
	if len(abstractSignatures) > 0 {
		types := core.Map(abstractSignatures, func(s *Signature) *Type {
			return b.ch.getOrCreateTypeFromSignature(s)
		})
		typeElementCount := len(callSigs) + (len(ctorSigs) - len(abstractSignatures)) + len(resolved.indexInfos) + core.IfElse(b.ctx.flags&nodebuilder.FlagsWriteClassExpressionAsTypeLiteral != 0, core.CountWhere(resolved.properties, func(p *ast.Symbol) bool {
			return p.Flags&ast.SymbolFlagsPrototype == 0
		}), len(resolved.properties))
		if typeElementCount != 0 {
			types = append(types, b.getResolvedTypeWithoutAbstractConstructSignatures(resolved))
		}
		return b.typeToTypeNode(b.ch.getIntersectionType(types))
	}
	restoreFlags := b.saveRestoreFlags()
	b.ctx.flags |= nodebuilder.FlagsInObjectTypeLiteral
	members := b.createTypeNodesFromResolvedType(resolved)
	restoreFlags()
	typeLiteralNode := b.f.NewTypeLiteralNode(members)
	b.ctx.approximateLength += 2
	b.e.SetEmitFlags(typeLiteralNode, core.IfElse((b.ctx.flags&nodebuilder.FlagsMultilineObjectLiterals != 0), 0, printer.EFSingleLine))
	return typeLiteralNode
}
func getTypeAliasForTypeLiteral(c *Checker, t *Type) *ast.Symbol {
	if t.symbol != nil && t.symbol.Flags&ast.SymbolFlagsTypeLiteral != 0 && t.symbol.Declarations != nil {
		node := ast.WalkUpParenthesizedTypes(ast.NodeOf(t.symbol.Declarations[0]).Parent())
		if ast.IsTypeAliasDeclaration(node) {
			return c.getSymbolOfDeclaration(node)
		}
	}
	return nil
}
func (b *NodeBuilderImpl) shouldWriteTypeOfFunctionSymbol(symbol *ast.Symbol, typeId TypeId) (bool, *ast.Symbol) {
	isStaticMethodSymbol := symbol.Flags&ast.SymbolFlagsMethod != 0 && ast.SomeDeclaration(symbol, func(declaration ast.Handle) bool {
		return ast.IsStatic(declaration) && !b.ch.isLateBindableIndexSignature(ast.GetNameOfDeclaration(declaration))
	})
	isNonLocalFunctionSymbol := false
	isFunctionExpressionSymbol := false
	if symbol.Flags&ast.SymbolFlagsFunction != 0 {
		if symbol.Parent != nil {
			isNonLocalFunctionSymbol = true
		} else {
			for _, declaration := range ast.DeclarationNodes(symbol) {
				if declaration.Parent().Kind == ast.KindSourceFile || declaration.Parent().Kind == ast.KindModuleBlock {
					isNonLocalFunctionSymbol = true
					break
				}
				if ast.IsFunctionExpressionOrArrowFunction(declaration) && ast.IsVariableDeclaration(declaration.Parent()) && ast.IsVariableDeclarationList(declaration.Parent().Parent()) && ast.IsVariableStatement(declaration.Parent().Parent().Parent()) && !declaration.Parent().Parent().Parent().Parent().IsNil() && (declaration.Parent().Parent().Parent().Parent().Kind == ast.KindSourceFile || declaration.Parent().Parent().Parent().Parent().Kind == ast.KindModuleBlock) {
					isNonLocalFunctionSymbol = true
					isFunctionExpressionSymbol = true
					break
				}
			}
		}
	}
	if isStaticMethodSymbol || isNonLocalFunctionSymbol {
		if isFunctionExpressionSymbol && symbol.ValueDeclaration != 0 && !ast.NodeOf(symbol.ValueDeclaration).Parent().IsNil() && ast.NodeOf(symbol.ValueDeclaration).Parent() != b.ctx.enclosingDeclaration {
			symbol = b.ch.getMergedSymbol(ast.NodeOf(symbol.ValueDeclaration).Parent().Symbol())
		}
		return (b.ctx.flags&nodebuilder.FlagsUseTypeOfFunction != 0 || b.ctx.visitedTypes.Has(typeId)) && (b.ctx.flags&nodebuilder.FlagsUseStructuralFallback == 0 || b.ch.IsValueSymbolAccessible(symbol, b.ctx.enclosingDeclaration)), symbol
	}
	return false, symbol
}
func (b *NodeBuilderImpl) createAnonymousTypeNode(t *Type) ast.Handle {
	return b.createAnonymousTypeNodeEx(t, false, false)
}
func (b *NodeBuilderImpl) shouldEmitTypeOfSymbol(forceExpansion bool, forceClassExpansion bool, isInstanceType ast.SymbolFlags, symbol *ast.Symbol, typeId TypeId) (bool, *ast.Symbol) {
	if forceExpansion {
		return false, symbol
	}
	nonFunctionResult := symbol.Flags&ast.SymbolFlagsClass != 0 && !forceClassExpansion && b.ch.getBaseTypeVariableOfClass(symbol) == nil && !(symbol.ValueDeclaration != 0 && ast.IsClassLike(ast.NodeOf(symbol.ValueDeclaration)) && b.ctx.flags&nodebuilder.FlagsWriteClassExpressionAsTypeLiteral != 0 && (!ast.IsClassDeclaration(ast.NodeOf(symbol.ValueDeclaration)) || b.ch.IsSymbolAccessible(symbol, b.ctx.enclosingDeclaration, isInstanceType, false).Accessibility != printer.SymbolAccessibilityAccessible)) || symbol.Flags&(ast.SymbolFlagsEnum|ast.SymbolFlagsValueModule) != 0
	if nonFunctionResult {
		return true, symbol
	}
	return b.shouldWriteTypeOfFunctionSymbol(symbol, typeId)
}
func (b *NodeBuilderImpl) createAnonymousTypeNodeEx(t *Type, forceClassExpansion bool, forceExpansion bool) ast.Handle {
	typeId := t.id
	symbol := t.symbol
	if symbol != nil {
		isInstantiationExpressionType := t.objectFlags&ObjectFlagsInstantiationExpressionType != 0
		if isInstantiationExpressionType {
			instantiationExpressionType := t.AsInstantiationExpressionType()
			existing := instantiationExpressionType.node
			if ast.IsTypeQueryNode(existing) && b.getTypeFromTypeNode(existing, false) == t {
				if b.ctx.visitedTypes.Has(typeId) {
					return b.createElidedInformationPlaceholder()
				}
				b.ctx.visitedTypes.Add(typeId)
				typeNode := b.tryReuseExistingNonParameterTypeNode(existing, t, ast.Handle{}, nil)
				b.ctx.visitedTypes.Delete(typeId)
				if !typeNode.IsNil() {
					return typeNode
				}
			}
			if b.ctx.visitedTypes.Has(typeId) {
				return b.createElidedInformationPlaceholder()
			}
			return b.visitAndTransformType(t, (*NodeBuilderImpl).createTypeNodeFromObjectType)
		}
		var isInstanceType ast.SymbolFlags
		if isClassInstanceSide(b.ch, t) {
			isInstanceType = ast.SymbolFlagsType
		} else {
			isInstanceType = ast.SymbolFlagsValue
		}
		if ok, symbol := b.shouldEmitTypeOfSymbol(forceExpansion, forceClassExpansion, isInstanceType, symbol, typeId); ok {
			if b.shouldExpandType(t, false) {
				b.ctx.depth++
			} else {
				return b.symbolToTypeNode(symbol, isInstanceType, 0)
			}
		}
		if b.ctx.visitedTypes.Has(typeId) {
			typeAlias := getTypeAliasForTypeLiteral(b.ch, t)
			if typeAlias != nil {
				return b.symbolToTypeNode(typeAlias, ast.SymbolFlagsType, 0)
			} else {
				return b.createElidedInformationPlaceholder()
			}
		} else {
			return b.visitAndTransformType(t, (*NodeBuilderImpl).createTypeNodeFromObjectType)
		}
	} else {
		return b.createTypeNodeFromObjectType(t)
	}
}
func (b *NodeBuilderImpl) getTypeFromTypeNode(node ast.Handle, noMappedTypes bool) *Type {
	if node.Parent().IsNil() {
		return b.ch.errorType
	}
	t := b.ch.getTypeFromTypeNode(node)
	if b.ctx.mapper == nil {
		return t
	}
	instantiated := b.ch.instantiateType(t, b.ctx.mapper)
	if noMappedTypes && instantiated != t {
		return nil
	}
	return instantiated
}
func (b *NodeBuilderImpl) typeToTypeNodeOrCircularityElision(t *Type) ast.Handle {
	if t.flags&TypeFlagsUnion != 0 {
		if b.ctx.visitedTypes.Has(t.id) {
			if b.ctx.flags&nodebuilder.FlagsAllowAnonymousIdentifier == 0 {
				b.ctx.encounteredError = true
				b.ctx.tracker.ReportCyclicStructureError()
			}
			return b.createElidedInformationPlaceholder()
		}
		return b.visitAndTransformType(t, (*NodeBuilderImpl).typeToTypeNode)
	}
	return b.typeToTypeNode(t)
}
func (b *NodeBuilderImpl) conditionalTypeToTypeNode(_t *Type) ast.Handle {
	if b.checkTruncationLength() {
		return b.createElidedInformationPlaceholder()
	}
	t := _t.AsConditionalType()
	checkTypeNode := b.typeToTypeNode(t.checkType)
	b.ctx.approximateLength += 15
	if b.ctx.flags&nodebuilder.FlagsGenerateNamesForShadowedTypeParams != 0 && t.root.isDistributive && t.checkType.flags&TypeFlagsTypeParameter == 0 {
		newParam := b.ch.newTypeParameter(b.ch.newSymbol(ast.SymbolFlagsTypeParameter, "T"))
		name := b.typeParameterToName(newParam)
		newTypeVariable := b.f.NewTypeReferenceNode(name, 0)
		b.ctx.approximateLength += 37
		newMapper := prependTypeMapping(t.root.checkType, newParam, t.mapper)
		saveInferTypeParameters := b.ctx.inferTypeParameters
		b.ctx.inferTypeParameters = t.root.inferTypeParameters
		extendsTypeNode := b.typeToTypeNode(b.ch.instantiateType(t.root.extendsType, newMapper))
		b.ctx.inferTypeParameters = saveInferTypeParameters
		trueTypeNode := b.typeToTypeNodeOrCircularityElision(b.ch.instantiateType(b.getTypeFromTypeNode(t.root.node.ConditionalTypeNodeTrueType(), false), newMapper))
		falseTypeNode := b.typeToTypeNodeOrCircularityElision(b.ch.instantiateType(b.getTypeFromTypeNode(t.root.node.ConditionalTypeNodeFalseType(), false), newMapper))
		newId := b.f.DeepCloneNode(newTypeVariable.TypeReferenceNodeTypeName())
		syntheticExtendsNode := b.f.NewInferTypeNode(b.f.NewTypeParameterDeclaration(0, newId, ast.Handle{}, ast.Handle{}, ast.Handle{}))
		innerCheckConditionalNode := b.f.NewConditionalTypeNode(newTypeVariable, extendsTypeNode, trueTypeNode, falseTypeNode)
		syntheticTrueNode := b.f.NewConditionalTypeNode(b.f.NewTypeReferenceNode(b.f.DeepCloneNode(name), 0), b.f.DeepCloneNode(checkTypeNode), innerCheckConditionalNode, b.f.NewKeywordTypeNode(ast.KindNeverKeyword))
		return b.f.NewConditionalTypeNode(checkTypeNode, syntheticExtendsNode, syntheticTrueNode, b.f.NewKeywordTypeNode(ast.KindNeverKeyword))
	}
	saveInferTypeParameters := b.ctx.inferTypeParameters
	b.ctx.inferTypeParameters = t.root.inferTypeParameters
	extendsTypeNode := b.typeToTypeNode(t.extendsType)
	b.ctx.inferTypeParameters = saveInferTypeParameters
	trueTypeNode := b.typeToTypeNodeOrCircularityElision(b.ch.getTrueTypeFromConditionalType(_t))
	falseTypeNode := b.typeToTypeNodeOrCircularityElision(b.ch.getFalseTypeFromConditionalType(_t))
	return b.f.NewConditionalTypeNode(checkTypeNode, extendsTypeNode, trueTypeNode, falseTypeNode)
}
func (b *NodeBuilderImpl) getParentSymbolOfTypeParameter(typeParameter *TypeParameter) *ast.Symbol {
	tp := ast.GetDeclarationOfKind(typeParameter.symbol, ast.KindTypeParameter)
	var host ast.Handle
	host = tp.Parent()
	if host.IsNil() {
		return nil
	}
	return b.ch.getSymbolOfNode(host)
}
func (b *NodeBuilderImpl) typeReferenceToTypeNode(t *Type) ast.Handle {
	var typeArguments []*Type = b.ch.getTypeArguments(t)
	if t.Target() == b.ch.globalArrayType || t.Target() == b.ch.globalReadonlyArrayType {
		if b.ctx.flags&nodebuilder.FlagsWriteArrayAsGenericType != 0 {
			typeArgumentNode := b.typeToTypeNode(typeArguments[0])
			return b.f.NewTypeReferenceNode(b.newIdentifier(core.IfElse(t.Target() == b.ch.globalArrayType, "Array", "ReadonlyArray"), t.Target().symbol), b.f.NewList([]ast.Handle{typeArgumentNode}))
		}
		elementType := b.typeToTypeNode(typeArguments[0])
		arrayType := b.f.NewArrayTypeNode(elementType)
		if t.Target() == b.ch.globalArrayType {
			return arrayType
		} else {
			return b.f.NewTypeOperatorNode(ast.KindReadonlyKeyword, arrayType)
		}
	} else if t.Target().objectFlags&ObjectFlagsTuple != 0 {
		typeArguments = core.SameMapIndex(typeArguments, func(arg *Type, i int) *Type {
			isOptional := false
			if i < len(t.Target().AsTupleType().elementInfos) {
				isOptional = t.Target().AsTupleType().elementInfos[i].flags&ElementFlagsOptional != 0
			}
			return b.ch.removeMissingType(arg, isOptional)
		})
		if len(typeArguments) > 0 {
			arity := b.ch.getTypeReferenceArity(t)
			tupleConstituentNodes := b.mapToTypeNodes(typeArguments[0:arity], false)
			if tupleConstituentNodes != 0 {
				elems := b.e.StoreFactory().Store().ListSlice(tupleConstituentNodes).Slice()
				for i := 0; i < len(elems); i++ {
					flags := t.Target().AsTupleType().elementInfos[i].flags
					labeledElementDeclaration := t.Target().AsTupleType().elementInfos[i].labeledDeclaration
					if !labeledElementDeclaration.IsNil() {
						elems[i] = b.f.NewNamedTupleMember(core.IfElse(flags&ElementFlagsVariable != 0, b.f.NewToken(ast.KindDotDotDotToken), ast.Handle{}), b.newIdentifier(b.ch.getTupleElementLabel(t.Target().AsTupleType().elementInfos[i], nil, i), nil), core.IfElse(flags&ElementFlagsOptional != 0, b.f.NewToken(ast.KindQuestionToken), ast.Handle{}), core.IfElse(flags&ElementFlagsRest != 0, b.f.NewArrayTypeNode(elems[i]), elems[i]))
					} else {
						switch {
						case flags&ElementFlagsVariable != 0:
							elems[i] = b.f.NewRestTypeNode(core.IfElse(flags&ElementFlagsRest != 0, b.f.NewArrayTypeNode(elems[i]), elems[i]))
						case flags&ElementFlagsOptional != 0:
							elems[i] = b.f.NewOptionalTypeNode(elems[i])
						}
					}
				}
				tupleConstituentNodes = b.e.StoreFactory().List(core.UndefinedTextRange(), elems...)
				tupleTypeNode := b.f.NewTupleTypeNode(tupleConstituentNodes)
				b.e.SetEmitFlags(tupleTypeNode, printer.EFSingleLine)
				if t.Target().AsTupleType().readonly {
					return b.f.NewTypeOperatorNode(ast.KindReadonlyKeyword, tupleTypeNode)
				} else {
					return tupleTypeNode
				}
			}
		}
		if b.ctx.encounteredError || (b.ctx.flags&nodebuilder.FlagsAllowEmptyTuple != 0) {
			tupleTypeNode := b.f.NewTupleTypeNode(b.f.NewList([]ast.Handle{}))
			b.e.SetEmitFlags(tupleTypeNode, printer.EFSingleLine)
			if t.Target().AsTupleType().readonly {
				return b.f.NewTypeOperatorNode(ast.KindReadonlyKeyword, tupleTypeNode)
			} else {
				return tupleTypeNode
			}
		}
		b.ctx.encounteredError = true
		return ast.Handle{}
	} else if b.ctx.flags&nodebuilder.FlagsWriteClassExpressionAsTypeLiteral != 0 && t.symbol.ValueDeclaration != 0 && ast.IsClassLike(ast.NodeOf(t.symbol.ValueDeclaration)) && !b.ch.IsValueSymbolAccessible(t.symbol, b.ctx.enclosingDeclaration) {
		return b.createAnonymousTypeNode(t)
	} else {
		outerTypeParameters := t.Target().AsInterfaceType().OuterTypeParameters()
		i := 0
		var resultType ast.Handle
		if outerTypeParameters != nil {
			length := len(outerTypeParameters)
			for i < length {
				start := i
				parent := b.getParentSymbolOfTypeParameter(outerTypeParameters[i].AsTypeParameter())
				for ok := true; ok; ok = i < length && b.getParentSymbolOfTypeParameter(outerTypeParameters[i].AsTypeParameter()) == parent {
					i++
				}
				if !slices.Equal(outerTypeParameters[start:i], typeArguments[start:i]) {
					typeArgumentSlice := b.mapToTypeNodes(typeArguments[start:i], false)
					restoreFlags := b.saveRestoreFlags()
					b.ctx.flags |= nodebuilder.FlagsForbidIndexedAccessSymbolReferences
					ref := b.symbolToTypeNode(parent, ast.SymbolFlagsType, typeArgumentSlice)
					restoreFlags()
					if resultType.IsNil() {
						resultType = ref
					} else {
						resultType = b.appendReferenceToType(resultType, ref)
					}
				}
			}
		}
		var typeArgumentNodes ast.ListRef
		if len(typeArguments) > 0 {
			typeParameterCount := 0
			typeParams := t.Target().AsInterfaceType().TypeParameters()
			if typeParams != nil {
				typeParameterCount = min(len(typeParams), len(typeArguments))
				if b.ch.isReferenceToType(t, b.ch.getGlobalIterableType()) || b.ch.isReferenceToType(t, b.ch.getGlobalIterableIteratorType()) || b.ch.isReferenceToType(t, b.ch.getGlobalAsyncIterableType()) || b.ch.isReferenceToType(t, b.ch.getGlobalAsyncIterableIteratorType()) {
					if t.AsTypeReference().node.IsNil() || !ast.IsTypeReferenceNode(t.AsTypeReference().node) || typeArgumentCount(t.AsTypeReference().node) < typeParameterCount {
						for typeParameterCount > 0 {
							typeArgument := typeArguments[typeParameterCount-1]
							typeParameter := t.Target().AsInterfaceType().TypeParameters()[typeParameterCount-1]
							defaultType := b.ch.getDefaultFromTypeParameter(typeParameter)
							if defaultType == nil || !b.ch.isTypeIdenticalTo(typeArgument, defaultType) {
								break
							}
							typeParameterCount--
						}
					}
				}
			}
			typeArgumentNodes = b.mapToTypeNodes(typeArguments[i:typeParameterCount], false)
		}
		restoreFlags := b.saveRestoreFlags()
		b.ctx.flags |= nodebuilder.FlagsForbidIndexedAccessSymbolReferences
		finalRef := b.symbolToTypeNode(t.symbol, ast.SymbolFlagsType, typeArgumentNodes)
		restoreFlags()
		if resultType.IsNil() {
			return finalRef
		} else {
			return b.appendReferenceToType(resultType, finalRef)
		}
	}
}
func (b *NodeBuilderImpl) visitAndTransformType(t *Type, transform func(b *NodeBuilderImpl, t *Type) ast.Handle) ast.Handle {
	typeId := t.id
	isConstructorObject := t.objectFlags&ObjectFlagsAnonymous != 0 && t.symbol != nil && t.symbol.Flags&ast.SymbolFlagsClass != 0
	var id *CompositeSymbolIdentity
	switch {
	case t.objectFlags&ObjectFlagsReference != 0 && !t.AsTypeReference().node.IsNil():
		id = &CompositeSymbolIdentity{false, 0, t.AsTypeReference().node.NodeId()}
	case t.flags&TypeFlagsConditional != 0:
		id = &CompositeSymbolIdentity{false, 0, t.AsConditionalType().root.node.NodeId()}
	case t.symbol != nil:
		id = &CompositeSymbolIdentity{isConstructorObject, ast.GetSymbolId(t.symbol), 0}
	default:
		id = nil
	}
	key := CompositeTypeCacheIdentity{typeId, b.ctx.flags, b.ctx.internalFlags}
	canUseCache := b.ctx.maxExpansionDepth < 0
	if canUseCache && !b.ctx.enclosingDeclaration.IsNil() && b.links.Has(b.ctx.enclosingDeclaration) {
		links := b.links.Get(b.ctx.enclosingDeclaration)
		cachedResult, ok := links.serializedTypes[key]
		if ok {
			for _, arg := range cachedResult.trackedSymbols {
				b.ctx.tracker.TrackSymbol(arg.symbol, arg.enclosingDeclaration, arg.meaning)
			}
			if cachedResult.truncating {
				b.ctx.truncating = true
			}
			b.ctx.approximateLength += cachedResult.addedLength
			return b.f.DeepCloneNode(cachedResult.node)
		}
	}
	var depth int
	if id != nil {
		depth = b.ctx.symbolDepth[*id]
		if depth > 10 {
			return b.createElidedInformationPlaceholder()
		}
		b.ctx.symbolDepth[*id] = depth + 1
	}
	b.ctx.visitedTypes.Add(typeId)
	prevTrackedSymbols := b.ctx.trackedSymbols
	b.ctx.trackedSymbols = nil
	startLength := b.ctx.approximateLength
	result := transform(b, t)
	addedLength := b.ctx.approximateLength - startLength
	if canUseCache && !b.ctx.reportedDiagnostic && !b.ctx.encounteredError {
		links := b.links.Get(b.ctx.enclosingDeclaration)
		if links.serializedTypes == nil {
			links.serializedTypes = make(map[CompositeTypeCacheIdentity]*SerializedTypeEntry)
		}
		links.serializedTypes[key] = &SerializedTypeEntry{node: result, truncating: b.ctx.truncating, addedLength: addedLength, trackedSymbols: b.ctx.trackedSymbols}
	}
	b.ctx.visitedTypes.Delete(typeId)
	if id != nil {
		b.ctx.symbolDepth[*id] = depth
	}
	b.ctx.trackedSymbols = prevTrackedSymbols
	return result
}
func (b *NodeBuilderImpl) typeToTypeNode(t *Type) ast.Handle {
	if b.ctx.maxExpansionDepth >= 0 && t != nil {
		b.ctx.typeStack = append(b.ctx.typeStack, t)
		defer func() {
			b.ctx.typeStack = b.ctx.typeStack[:len(b.ctx.typeStack)-1]
		}()
	}
	inTypeAlias := b.ctx.flags & nodebuilder.FlagsInTypeAlias
	b.ctx.flags &^= nodebuilder.FlagsInTypeAlias
	if t == nil {
		if b.ctx.flags&nodebuilder.FlagsAllowEmptyUnionOrIntersection == 0 {
			b.ctx.encounteredError = true
			return ast.Handle{}
		}
		b.ctx.approximateLength += 3
		return b.f.NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	if b.ctx.flags&nodebuilder.FlagsNoTypeReduction == 0 {
		t = b.ch.getReducedType(t)
	}
	if t.flags&TypeFlagsAny != 0 {
		if t.alias != nil {
			return t.alias.ToTypeReferenceNode(b)
		}
		if t == b.ch.unresolvedType {
			return b.e.AddSyntheticLeadingComment(b.f.NewKeywordTypeNode(ast.KindAnyKeyword), ast.KindMultiLineCommentTrivia, "unresolved", false)
		}
		b.ctx.approximateLength += 3
		return b.f.NewKeywordTypeNode(core.IfElse(t == b.ch.intrinsicMarkerType, ast.KindIntrinsicKeyword, ast.KindAnyKeyword))
	}
	if t.flags&TypeFlagsUnknown != 0 {
		return b.f.NewKeywordTypeNode(ast.KindUnknownKeyword)
	}
	if t.flags&TypeFlagsString != 0 {
		b.ctx.approximateLength += 6
		return b.f.NewKeywordTypeNode(ast.KindStringKeyword)
	}
	if t.flags&TypeFlagsNumber != 0 {
		b.ctx.approximateLength += 6
		return b.f.NewKeywordTypeNode(ast.KindNumberKeyword)
	}
	if t.flags&TypeFlagsBigInt != 0 {
		b.ctx.approximateLength += 6
		return b.f.NewKeywordTypeNode(ast.KindBigIntKeyword)
	}
	if t.flags&TypeFlagsBoolean != 0 && t.alias == nil {
		b.ctx.approximateLength += 7
		return b.f.NewKeywordTypeNode(ast.KindBooleanKeyword)
	}
	expandingEnum := false
	if t.flags&TypeFlagsEnumLike != 0 {
		if t.symbol.Flags&ast.SymbolFlagsEnumMember != 0 {
			parentSymbol := b.ch.getParentOfSymbol(t.symbol)
			parentName := b.symbolToTypeNode(parentSymbol, ast.SymbolFlagsType, 0)
			if b.ch.getDeclaredTypeOfSymbol(parentSymbol) == t {
				return parentName
			}
			memberName := ast.SymbolName(t.symbol)
			if scanner.IsIdentifierText(memberName, core.LanguageVariantStandard) {
				return b.appendReferenceToType(parentName, b.f.NewTypeReferenceNode(b.f.NewIdentifier(memberName), 0))
			}
			if ast.IsImportTypeNode(parentName) {
				parentName.SetImportTypeNodeIsTypeOf(true)
				return b.f.NewIndexedAccessTypeNode(parentName, b.f.NewLiteralTypeNode(b.newStringLiteral(memberName)))
			} else if ast.IsTypeReferenceNode(parentName) {
				return b.f.NewIndexedAccessTypeNode(b.f.NewTypeQueryNode(parentName.TypeReferenceNodeTypeName(), 0), b.f.NewLiteralTypeNode(b.newStringLiteral(memberName)))
			} else {
				panic("Unhandled type node kind returned from `symbolToTypeNode`.")
			}
		}
		if t.flags&TypeFlagsUnion == 0 || !b.shouldExpandType(t, false) {
			return b.symbolToTypeNode(t.symbol, ast.SymbolFlagsType, 0)
		}
		expandingEnum = true
	}
	if t.flags&TypeFlagsStringLiteral != 0 {
		b.ctx.approximateLength += len(t.AsLiteralType().value.(string)) + 2
		lit := b.newStringLiteral(t.AsLiteralType().value.(string))
		b.e.AddEmitFlags(lit, printer.EFNoAsciiEscaping)
		return b.f.NewLiteralTypeNode(lit)
	}
	if t.flags&TypeFlagsNumberLiteral != 0 {
		value := t.AsLiteralType().value.(jsnum.Number)
		b.ctx.approximateLength += len(value.String())
		if value < 0 {
			return b.f.NewLiteralTypeNode(b.f.NewPrefixUnaryExpression(ast.KindMinusToken, b.f.NewNumericLiteral(value.String()[1:], ast.TokenFlagsNone)))
		} else {
			return b.f.NewLiteralTypeNode(b.f.NewNumericLiteral(value.String(), ast.TokenFlagsNone))
		}
	}
	if t.flags&TypeFlagsBigIntLiteral != 0 {
		b.ctx.approximateLength += len(pseudoBigIntToString(getBigIntLiteralValue(t))) + 1
		return b.f.NewLiteralTypeNode(b.f.NewBigIntLiteral(pseudoBigIntToString(getBigIntLiteralValue(t))+"n", ast.TokenFlagsNone))
	}
	if t.flags&TypeFlagsBooleanLiteral != 0 {
		if t.AsLiteralType().value.(bool) {
			b.ctx.approximateLength += 4
			return b.f.NewLiteralTypeNode(b.f.NewKeywordExpression(ast.KindTrueKeyword))
		} else {
			b.ctx.approximateLength += 5
			return b.f.NewLiteralTypeNode(b.f.NewKeywordExpression(ast.KindFalseKeyword))
		}
	}
	if t.flags&TypeFlagsUniqueESSymbol != 0 {
		if b.ctx.flags&nodebuilder.FlagsAllowUniqueESSymbolType == 0 {
			if b.ch.IsValueSymbolAccessible(t.symbol, b.ctx.enclosingDeclaration) {
				b.ctx.approximateLength += 6
				return b.symbolToTypeNode(t.symbol, ast.SymbolFlagsValue, 0)
			}
			b.ctx.tracker.ReportInaccessibleUniqueSymbolError()
		}
		b.ctx.approximateLength += 13
		return b.f.NewTypeOperatorNode(ast.KindUniqueKeyword, b.f.NewKeywordTypeNode(ast.KindSymbolKeyword))
	}
	if t.flags&TypeFlagsVoid != 0 {
		b.ctx.approximateLength += 4
		return b.f.NewKeywordTypeNode(ast.KindVoidKeyword)
	}
	if t.flags&TypeFlagsUndefined != 0 {
		b.ctx.approximateLength += 9
		return b.f.NewKeywordTypeNode(ast.KindUndefinedKeyword)
	}
	if t.flags&TypeFlagsNull != 0 {
		b.ctx.approximateLength += 4
		return b.f.NewLiteralTypeNode(b.f.NewKeywordExpression(ast.KindNullKeyword))
	}
	if t.flags&TypeFlagsNever != 0 {
		b.ctx.approximateLength += 5
		return b.f.NewKeywordTypeNode(ast.KindNeverKeyword)
	}
	if t.flags&TypeFlagsESSymbol != 0 {
		b.ctx.approximateLength += 6
		return b.f.NewKeywordTypeNode(ast.KindSymbolKeyword)
	}
	if t.flags&TypeFlagsNonPrimitive != 0 {
		b.ctx.approximateLength += 6
		return b.f.NewKeywordTypeNode(ast.KindObjectKeyword)
	}
	if isThisTypeParameter(t) {
		if b.ctx.flags&nodebuilder.FlagsInObjectTypeLiteral != 0 {
			if !b.ctx.encounteredError && b.ctx.flags&nodebuilder.FlagsAllowThisInObjectLiteral == 0 {
				b.ctx.encounteredError = true
			}
			b.ctx.tracker.ReportInaccessibleThisError()
		}
		b.ctx.approximateLength += 4
		return b.f.NewThisTypeNode()
	}
	if inTypeAlias == 0 && t.alias != nil && (b.ctx.flags&nodebuilder.FlagsUseAliasDefinedOutsideCurrentScope != 0 || b.ch.IsTypeSymbolAccessible(t.alias.Symbol(), b.ctx.enclosingDeclaration)) {
		if !b.shouldExpandType(t, true) {
			sym := t.alias.Symbol()
			typeArgumentNodes := b.mapToTypeNodes(t.alias.TypeArguments(), false)
			if isReservedMemberName(sym.Name) && sym.Flags&ast.SymbolFlagsClass == 0 {
				return b.f.NewTypeReferenceNode(b.f.NewIdentifier(""), typeArgumentNodes)
			}
			if typeArgumentNodes != 0 && b.f.Store().ListLen(typeArgumentNodes) == 1 && sym == b.ch.globalArrayType.symbol {
				return b.f.NewArrayTypeNode(b.f.Store().ListAt(typeArgumentNodes, 0))
			}
			return b.symbolToTypeNode(sym, ast.SymbolFlagsType, typeArgumentNodes)
		}
		b.ctx.depth++
		defer func() {
			b.ctx.depth--
		}()
	}
	objectFlags := t.objectFlags
	if objectFlags&ObjectFlagsReference != 0 {
		debug.Assert(t.Flags()&TypeFlagsObject != 0)
		if b.shouldExpandType(t, false) {
			b.ctx.depth++
			result := b.createAnonymousTypeNodeEx(t, true, true)
			b.ctx.depth--
			return result
		}
		if !t.AsTypeReference().node.IsNil() {
			return b.visitAndTransformType(t, (*NodeBuilderImpl).typeReferenceToTypeNode)
		} else {
			return b.typeReferenceToTypeNode(t)
		}
	}
	if t.flags&TypeFlagsTypeParameter != 0 || objectFlags&ObjectFlagsClassOrInterface != 0 {
		if objectFlags&ObjectFlagsClassOrInterface != 0 && b.shouldExpandType(t, false) {
			b.ctx.depth++
			result := b.createAnonymousTypeNodeEx(t, true, true)
			b.ctx.depth--
			return result
		}
		if t.flags&TypeFlagsTypeParameter != 0 && slices.Contains(b.ctx.inferTypeParameters, t) {
			b.ctx.approximateLength += len(ast.SymbolName(t.symbol)) + 6
			var constraintNode ast.Handle
			constraint := b.ch.getConstraintOfTypeParameter(t)
			if constraint != nil {
				inferredConstraint := b.ch.getInferredTypeParameterConstraint(t, true)
				if !(inferredConstraint != nil && b.ch.isTypeIdenticalTo(constraint, inferredConstraint)) {
					b.ctx.approximateLength += 9
					constraintNode = b.typeToTypeNode(constraint)
				}
			}
			return b.f.NewInferTypeNode(b.typeParameterToDeclarationWithConstraint(t, constraintNode))
		}
		if b.ctx.flags&nodebuilder.FlagsGenerateNamesForShadowedTypeParams != 0 && t.flags&TypeFlagsTypeParameter != 0 {
			name := b.typeParameterToName(t)
			b.ctx.approximateLength += len(name.Text())
			return b.f.NewTypeReferenceNode(b.newIdentifier(name.Text(), t.symbol), 0)
		}
		if t.symbol != nil {
			return b.symbolToTypeNode(t.symbol, ast.SymbolFlagsType, 0)
		}
		var name string
		if (t == b.ch.markerSuperTypeForCheck || t == b.ch.markerSubTypeForCheck) && b.ch.varianceTypeParameter != nil && b.ch.varianceTypeParameter.symbol != nil {
			name = core.IfElse(t == b.ch.markerSubTypeForCheck, "sub-", "super-") + ast.SymbolName(b.ch.varianceTypeParameter.symbol)
		} else {
			name = "?"
		}
		return b.f.NewTypeReferenceNode(b.newIdentifier(name, nil), 0)
	}
	if t.flags&TypeFlagsUnion != 0 && t.AsUnionType().origin != nil {
		t = t.AsUnionType().origin
	}
	if t.flags&(TypeFlagsUnion|TypeFlagsIntersection) != 0 {
		var types []*Type
		if t.flags&TypeFlagsUnion != 0 {
			types = b.ch.formatUnionTypes(t.AsUnionType().types, expandingEnum)
		} else {
			types = t.AsIntersectionType().types
		}
		if len(types) == 1 {
			return b.typeToTypeNode(types[0])
		}
		typeNodes := b.mapToTypeNodes(types, true)
		if typeNodes != 0 && b.f.Store().ListLen(typeNodes) > 0 {
			if t.flags&TypeFlagsUnion != 0 {
				return b.f.NewUnionTypeNode(typeNodes)
			} else {
				return b.f.NewIntersectionTypeNode(typeNodes)
			}
		} else {
			if !b.ctx.encounteredError && b.ctx.flags&nodebuilder.FlagsAllowEmptyUnionOrIntersection == 0 {
				b.ctx.encounteredError = true
			}
			return ast.Handle{}
		}
	}
	if objectFlags&(ObjectFlagsAnonymous|ObjectFlagsMapped) != 0 {
		debug.Assert(t.Flags()&TypeFlagsObject != 0)
		return b.createAnonymousTypeNode(t)
	}
	if t.flags&TypeFlagsIndex != 0 {
		indexedType := t.Target()
		b.ctx.approximateLength += 6
		indexTypeNode := b.typeToTypeNode(indexedType)
		return b.f.NewTypeOperatorNode(ast.KindKeyOfKeyword, indexTypeNode)
	}
	if t.flags&TypeFlagsTemplateLiteral != 0 {
		texts := t.AsTemplateLiteralType().texts
		types := t.AsTemplateLiteralType().types
		templateHead := b.f.NewTemplateHead(texts[0], "", ast.TokenFlagsNone)
		b.e.AddEmitFlags(templateHead, printer.EFNoAsciiEscaping)
		templateSpans := b.f.NewList(core.MapIndex(types, func(t *Type, i int) ast.Handle {
			var res ast.Handle
			if i < len(types)-1 {
				res = b.f.NewTemplateMiddle(texts[i+1], "", ast.TokenFlagsNone)
			} else {
				res = b.f.NewTemplateTail(texts[i+1], "", ast.TokenFlagsNone)
			}
			b.e.AddEmitFlags(res, printer.EFNoAsciiEscaping)
			return b.f.NewTemplateLiteralTypeSpan(b.typeToTypeNode(t), res)
		}))
		b.ctx.approximateLength += 2
		return b.f.NewTemplateLiteralTypeNode(templateHead, templateSpans)
	}
	if t.flags&TypeFlagsStringMapping != 0 {
		typeNode := b.typeToTypeNode(t.Target())
		return b.symbolToTypeNode(t.AsStringMappingType().symbol, ast.SymbolFlagsType, b.f.NewList([]ast.Handle{typeNode}))
	}
	if t.flags&TypeFlagsIndexedAccess != 0 {
		objectTypeNode := b.typeToTypeNode(t.AsIndexedAccessType().objectType)
		indexTypeNode := b.typeToTypeNode(t.AsIndexedAccessType().indexType)
		b.ctx.approximateLength += 2
		return b.f.NewIndexedAccessTypeNode(objectTypeNode, indexTypeNode)
	}
	if t.flags&TypeFlagsConditional != 0 {
		return b.visitAndTransformType(t, (*NodeBuilderImpl).conditionalTypeToTypeNode)
	}
	if t.flags&TypeFlagsSubstitution != 0 {
		typeNode := b.typeToTypeNode(t.AsSubstitutionType().baseType)
		if !b.ch.isNoInferType(t) {
			return typeNode
		}
		noInferSymbol := b.ch.getGlobalTypeAliasSymbol("NoInfer", 1, false)
		if noInferSymbol != nil {
			return b.symbolToTypeNode(noInferSymbol, ast.SymbolFlagsType, b.f.NewList([]ast.Handle{typeNode}))
		} else {
			return typeNode
		}
	}
	panic("Should be unreachable.")
}
func (b *NodeBuilderImpl) newStringLiteral(text string) ast.Handle {
	return b.newStringLiteralEx(text, false)
}
func (b *NodeBuilderImpl) newStringLiteralEx(text string, isSingleQuote bool) ast.Handle {
	flags := ast.TokenFlagsNone
	if isSingleQuote || b.ctx.flags&nodebuilder.FlagsUseSingleQuotesForStringLiteralType != 0 {
		flags |= ast.TokenFlagsSingleQuote
	}
	node := b.f.NewStringLiteral(text, flags)
	return node
}
func (t *TypeAlias) ToTypeReferenceNode(b *NodeBuilderImpl) ast.Handle {
	return b.f.NewTypeReferenceNode(b.symbolToEntityNameNode(t.Symbol()), b.mapToTypeNodes(t.TypeArguments(), false))
}
func (b *NodeBuilderImpl) newIdentifier(text string, symbol *ast.Symbol) ast.Handle {
	id := b.f.NewIdentifier(text)
	if symbol != nil {
		b.idToSymbol[id] = symbol
	}
	return id
}
func (b *NodeBuilderImpl) createAccessExpression(node ast.Handle) ast.Handle {
	switch {
	case ast.IsQualifiedName(node):
		return b.f.NewPropertyAccessExpression(b.createAccessExpression(node.QualifiedNameLeft()), ast.Handle{}, b.f.DeepCloneNode(node.QualifiedNameRight()), ast.NodeFlagsNone)
	case ast.IsIdentifier(node), ast.IsPropertyAccessExpression(node), ast.IsExpressionWithTypeArguments(node):
		return b.f.DeepCloneNode(node)
	default:
		panic("unexpected access node kind: " + node.Kind.String())
	}
}
func (b *NodeBuilderImpl) createExpressionWithTypeArguments(expr ast.Handle, typeArguments ast.ListRef) ast.Handle {
	if typeArguments == 0 || expr.Store().ListLen(typeArguments) == 0 {
		return expr
	}
	return b.f.NewExpressionWithTypeArguments(expr, typeArguments)
}
func (b *NodeBuilderImpl) lookupInstantiatedTypeArgumentNodes(chain []*ast.Symbol, index int) ast.ListRef {
	if b.shouldWriteTypeParametersInQualifiedName(chain, index) {
		symbol := chain[index]
		nextSymbol := chain[index+1]
		if nextSymbol.CheckFlags&ast.CheckFlagsInstantiated == 0 {
			return 0
		}
		targetSymbol := symbol
		if symbol.Flags&ast.SymbolFlagsAlias != 0 && !b.ch.canGetTypeParametersOfClassOrInterface(symbol) {
			targetSymbol = b.ch.resolveAlias(symbol)
		}
		if !b.ch.canGetTypeParametersOfClassOrInterface(targetSymbol) {
			return 0
		}
		params := b.getTypeParametersOfClassOrInterface(targetSymbol)
		targetMapper := b.ch.valueSymbolLinks.Get(nextSymbol).mapper
		if targetMapper != nil {
			params = core.Map(params, targetMapper.Map)
		}
		return b.mapToTypeNodes(params, false)
	}
	return 0
}
func (b *NodeBuilderImpl) lookupExpressionChainTypeArgumentNodes(chain []*ast.Symbol, index int) ast.ListRef {
	if b.shouldWriteTypeParametersInQualifiedName(chain, index) {
		symbol := chain[index]
		symbolId := ast.GetSymbolId(symbol)
		if b.ctx.typeParameterSymbolList.Has(symbolId) {
			return 0
		}
		b.ctx.typeParameterSymbolList.Add(symbolId)
		if typeArgumentNodes := b.lookupInstantiatedTypeArgumentNodes(chain, index); typeArgumentNodes != 0 {
			return typeArgumentNodes
		}
		typeParameterNodes := b.typeParametersToTypeParameterDeclarations(symbol)
		if len(typeParameterNodes) > 0 {
			return b.e.StoreFactory().List(core.UndefinedTextRange(), typeParameterNodes...)
		}
	}
	return 0
}
func (b *NodeBuilderImpl) shouldWriteTypeParametersInQualifiedName(chain []*ast.Symbol, index int) bool {
	return b.ctx.flags&nodebuilder.FlagsWriteTypeParametersInQualifiedName != 0 && index < len(chain)-1
}
