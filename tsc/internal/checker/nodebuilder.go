package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
)

type NodeBuilder struct {
	ctxStack []*// nil for non-hover callers
	// VerbosityContext controls hover-expansion behavior in the node builder.
	// A nil VerbosityContext means no expansion (non-hover callers).
	// Level 0 = default hover (maxExpansionDepth = 0; detects expandability without expanding).
	// Level 1+ = expansion enabled (maxExpansionDepth = Level).
	// 0 = default (no expansion), 1+ = expansion depth
	// 0 = use default
	// output: whether increasing Level would reveal more
	// output: whether output was truncated
	// EmitContext implements NodeBuilderInterface.
	// propagateVerbosityOut copies expansion signals from the context to the VerbosityContext output.
	// Only set to true, never clear — multiple calls share the same VerbosityContext
	// IndexInfoToIndexSignatureDeclaration implements NodeBuilderInterface.
	// SerializeReturnTypeForSignature implements NodeBuilderInterface.
	// SerializeTypeForDeclaration implements NodeBuilderInterface.
	// SerializeTypeForExpression implements NodeBuilderInterface.
	// SignatureToSignatureDeclaration implements NodeBuilderInterface.
	// ExpandSymbolForHover produces declaration nodes for a symbol with verbosity level support.
	// Push the declared type onto the type stack to prevent re-expansion.
	// We push a nil sentinel after the real type so that isTypeOnStack
	// (which skips the last element) still checks declaredType.
	// Simplify declarations by applying original modifiers
	// SymbolToEntityName implements NodeBuilderInterface.
	// SymbolToExpression implements NodeBuilderInterface.
	// SymbolToNode implements NodeBuilderInterface.
	// SymbolToParameterDeclaration implements NodeBuilderInterface.
	// SymbolToTypeParameterDeclarations implements NodeBuilderInterface.
	// TypeParameterToDeclaration implements NodeBuilderInterface.
	// TypePredicateToTypePredicateNode implements NodeBuilderInterface.
	// TypeToTypeNode implements NodeBuilderInterface.
	// var _ NodeBuilderInterface = NewNodeBuilderAPI(nil, nil)
	/*idToSymbol*/ // Allow any allocated nodes to be freed if they're no longer in a cache
	/*idToSymbol*/ NodeBuilderContext
	host      Host
	impl      *NodeBuilderImpl
	verbosity *VerbosityContext
}

type VerbosityContext struct {
	Level                int
	MaxTruncationLength  int
	CanIncreaseVerbosity bool
	Truncated            bool
}

func (b *NodeBuilder) EmitContext() *printer.EmitContext {
	return b.impl.e
}
func (b *NodeBuilder) enterContext(enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) {
	verbosityLevel := -1
	maxTruncationLength := 0
	if b.verbosity != nil {
		verbosityLevel = b.verbosity.Level
		maxTruncationLength = b.verbosity.MaxTruncationLength
	}
	b.ctxStack = append(b.ctxStack, b.impl.ctx)
	b.impl.ctx = &NodeBuilderContext{host: b.host, tracker: tracker, flags: flags, internalFlags: internalFlags, maxExpansionDepth: verbosityLevel, maxTruncationLength: maxTruncationLength, enclosingDeclaration: enclosingDeclaration, enclosingFile: ast.GetSourceFileOfNode(enclosingDeclaration), inferTypeParameters: make([]*Type, 0), symbolDepth: make(map[CompositeSymbolIdentity]int), trackedSymbols: make([]*TrackedSymbolArgs, 0), reverseMappedStack: make([]*ast.Symbol, 0), enclosingSymbolTypes: make(map[ast.SymbolId]*Type), remappedSymbolReferences: make(map[ast.SymbolId]*ast.Symbol)}
	tracker = NewSymbolTrackerImpl(b.impl.ctx, tracker)
	b.impl.ctx.tracker = tracker
}

func (b *NodeBuilder) propagateVerbosityOut() {
	if b.verbosity != nil {
		if b.impl.ctx.canIncreaseExpansionDepth {
			b.verbosity.CanIncreaseVerbosity = true
		}
		if b.impl.ctx.expansionTruncated {
			b.verbosity.Truncated = true
		}
	}
}
func (b *NodeBuilder) popContext() {
	stackSize := len(b.ctxStack)
	if stackSize == 0 {
		b.impl.ctx = nil
	} else {
		b.impl.ctx = b.ctxStack[stackSize-1]
		b.ctxStack = b.ctxStack[:stackSize-1]
	}
}
func (b *NodeBuilder) exitContext(result ast.Handle) ast.Handle {
	b.propagateVerbosityOut()
	b.exitContextCheck()
	defer b.popContext()
	if b.impl.ctx.encounteredError {
		return ast.Handle{}
	}
	return result
}
func (b *NodeBuilder) exitContextSlice(result []ast.Handle) []ast.Handle {
	b.propagateVerbosityOut()
	b.exitContextCheck()
	defer b.popContext()
	if b.impl.ctx.encounteredError {
		return nil
	}
	return result
}
func (b *NodeBuilder) exitContextCheck() {
	if b.impl.ctx.truncating && b.impl.ctx.flags&nodebuilder.FlagsNoTruncation != 0 {
		b.impl.ctx.tracker.ReportTruncationError()
	}
}

func (b *NodeBuilder) IndexInfoToIndexSignatureDeclaration(info *IndexInfo, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.indexInfoToIndexSignatureDeclarationHelper(info, ast.Handle{}))
}

func (b *NodeBuilder) SerializeReturnTypeForSignature(signatureDeclaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	signature := b.impl.ch.getSignatureFromDeclaration(signatureDeclaration)
	_, cleanup := b.impl.enterSignatureScope(signature)
	result := b.impl.serializeReturnTypeForSignature(signature, true)
	cleanup()
	return b.exitContext(result)
}
func (b *NodeBuilder) SerializeTypeParametersForSignature(signatureDeclaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	symbol := b.impl.ch.getSymbolOfDeclaration(signatureDeclaration)
	typeParams := b.SymbolToTypeParameterDeclarations(symbol, enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContextSlice(typeParams)
}

func (b *NodeBuilder) SerializeTypeForDeclaration(declaration ast.Handle, symbol *ast.Symbol, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.serializeTypeForDeclaration(declaration, nil, symbol, true))
}

func (b *NodeBuilder) SerializeTypeForExpression(expr ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.serializeTypeForExpression(expr))
}

func (b *NodeBuilder) SignatureToSignatureDeclaration(signature *Signature, kind ast.Kind, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.signatureToSignatureDeclarationHelper(signature, kind, nil))
}

func (b *NodeBuilder) ExpandSymbolForHover(symbol *ast.Symbol, meaning ast.SymbolFlags) []ast.Handle {
	b.enterContext(ast.Handle{}, nodebuilder.FlagsIgnoreErrors|nodebuilder.FlagsMultilineObjectLiterals|nodebuilder.FlagsUseAliasDefinedOutsideCurrentScope, nodebuilder.InternalFlagsNone, nil)
	declaredType := b.impl.ch.getDeclaredTypeOfSymbol(symbol)
	b.impl.ctx.typeStack = append(b.impl.ctx.typeStack, declaredType)
	b.impl.ctx.typeStack = append(b.impl.ctx.typeStack, nil)
	nodes := b.impl.expandSymbolForHover(symbol)
	b.impl.ctx.typeStack = b.impl.ctx.typeStack[:len(b.impl.ctx.typeStack)-2]
	b.propagateVerbosityOut()
	result := make([]ast.Handle, 0, len(nodes))
	for _, node := range nodes {
		switch node.Kind() {
		case ast.KindClassDeclaration:
			result = append(result, simplifyClassDeclaration(b.impl.f, node, symbol))
		case ast.KindEnumDeclaration:
			result = append(result, simplifyModifiers(b.impl.f, node, ast.IsEnumDeclaration, symbol))
		case ast.KindInterfaceDeclaration:
			if meaning&ast.SymbolFlagsInterface != 0 {
				result = append(result, simplifyModifiers(b.impl.f, node, ast.IsInterfaceDeclaration, symbol))
			}
		case ast.KindModuleDeclaration:
			result = append(result, simplifyModifiers(b.impl.f, node, ast.IsModuleDeclaration, symbol))
		}
	}
	return b.exitContextSlice(result)
}
func simplifyClassDeclaration(f ast.HandleFactory, classDecl ast.Handle, symbol *ast.Symbol) ast.Handle {
	classDeclarations := core.Filter(ast.DeclarationNodes(symbol), ast.IsClassLike)
	var originalClassDecl ast.Handle
	if len(classDeclarations) > 0 {
		originalClassDecl = classDeclarations[0]
	} else {
		originalClassDecl = classDecl
	}
	modifiers := originalClassDecl.ModifierFlags() & ^(ast.ModifierFlagsExport | ast.ModifierFlagsAmbient)
	isAnonymous := ast.IsClassExpression(originalClassDecl)
	if isAnonymous {
		cd := classDecl
		classDecl = f.UpdateClassDeclaration(cd, classDecl.Modifiers(), ast.Handle{}, cd.TypeParameterList(), cd.HeritageClauses(), cd.MemberList())
	}
	return ast.ReplaceHandleModifiers(f, classDecl, f.NewModifierList(ast.CreateModifiersFromModifierFlags(modifiers, f.NewModifier)))
}
func simplifyModifiers(f ast.HandleFactory, newDecl ast.Handle, isDeclKind func(ast.Handle) bool, symbol *ast.Symbol) ast.Handle {
	decls := core.Filter(ast.DeclarationNodes(symbol), isDeclKind)
	var declWithModifiers ast.Handle
	if len(decls) > 0 {
		declWithModifiers = decls[0]
	} else {
		declWithModifiers = newDecl
	}
	modifiers := declWithModifiers.ModifierFlags() & ^(ast.ModifierFlagsExport | ast.ModifierFlagsAmbient)
	return ast.ReplaceHandleModifiers(f, newDecl, f.NewModifierList(ast.CreateModifiersFromModifierFlags(modifiers, f.NewModifier)))
}

func (b *NodeBuilder) SymbolToEntityName(symbol *ast.Symbol, meaning ast.SymbolFlags, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.symbolToName(symbol, meaning, false))
}

func (b *NodeBuilder) SymbolToExpression(symbol *ast.Symbol, meaning ast.SymbolFlags, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.symbolToExpression(symbol, meaning))
}

func (b *NodeBuilder) SymbolToNode(symbol *ast.Symbol, meaning ast.SymbolFlags, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.symbolToNode(symbol, meaning))
}

func (b NodeBuilder) SymbolToParameterDeclaration(symbol *ast.Symbol, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.symbolToParameterDeclaration(symbol, false))
}

func (b *NodeBuilder) SymbolToTypeParameterDeclarations(symbol *ast.Symbol, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContextSlice(b.impl.symbolToTypeParameterDeclarations(symbol))
}

func (b *NodeBuilder) TypeParameterToDeclaration(parameter *Type, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.typeParameterToDeclaration(parameter))
}

func (b *NodeBuilder) TypePredicateToTypePredicateNode(predicate *TypePredicate, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.typePredicateToTypePredicateNode(predicate))
}

func (b *NodeBuilder) TypeToTypeNode(typ *Type, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.typeToTypeNode(typ))
}
func (b *NodeBuilder) TryJSTypeNodeToTypeNode(node ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	b.enterContext(enclosingDeclaration, flags, internalFlags, tracker)
	return b.exitContext(b.impl.tryJSTypeNodeToTypeNode(node))
}
func NewNodeBuilder(ch *Checker, e *printer.EmitContext) *NodeBuilder {
	return NewNodeBuilderEx(ch, e, nil)
}
func NewNodeBuilderEx(ch *Checker, e *printer.EmitContext, idToSymbol map[ast.Handle]*ast.Symbol) *NodeBuilder {
	impl := newNodeBuilderImpl(ch, e, idToSymbol)
	return &NodeBuilder{impl: impl, ctxStack: make([]*NodeBuilderContext, 0, 1), host: ch.program}
}
func (c *Checker) getNodeBuilder() (*NodeBuilder, func()) {
	releaseNodes := func() {
		c.typeToStringNodebuilder.EmitContext().Factory.ReleaseArenas() // no-op on store factory
	}
	if c.typeToStringNodebuilder != nil {
		return c.typeToStringNodebuilder, releaseNodes
	}
	c.typeToStringNodebuilder = c.getNodeBuilderEx(nil)
	return c.typeToStringNodebuilder, releaseNodes
}
func (c *Checker) getNodeBuilderEx(idToSymbol map[ast.Handle]*ast.Symbol) *NodeBuilder {
	b := NewNodeBuilderEx(c, printer.NewEmitContext(), idToSymbol)
	return b
}
