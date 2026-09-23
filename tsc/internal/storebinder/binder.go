package storebinder

import (
	"slices"
	"strconv"
	"sync"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

type ContainerFlags int32

const (
	// The current node is not a container, and no container manipulation should happen before
	// recursing into it.
	ContainerFlagsNone ContainerFlags = 0
	// The current node is a container.  It should be set as the current container (and block-
	// container) before recursing into it.  The current node does not have locals.  Examples:
	//
	//      Classes, ObjectLiterals, TypeLiterals, Interfaces...
	ContainerFlagsIsContainer ContainerFlags = 1 << 0
	// The current node is a block-scoped-container.  It should be set as the current block-
	// container before recursing into it.  Examples:
	//
	//      Blocks (when not parented by functions), Catch clauses, For/For-in/For-of statements...
	ContainerFlagsIsBlockScopedContainer ContainerFlags = 1 << 1
	// The current node is the container of a control flow path. The current control flow should
	// be saved and restored, and a new control flow initialized within the container.
	ContainerFlagsIsControlFlowContainer                           ContainerFlags = 1 << 2
	ContainerFlagsIsFunctionLike                                   ContainerFlags = 1 << 3
	ContainerFlagsIsFunctionExpression                             ContainerFlags = 1 << 4
	ContainerFlagsHasLocals                                        ContainerFlags = 1 << 5
	ContainerFlagsIsInterface                                      ContainerFlags = 1 << 6
	ContainerFlagsIsObjectLiteralOrClassExpressionMethodOrAccessor ContainerFlags = 1 << 7
	ContainerFlagsIsThisContainer                                  ContainerFlags = 1 << 8
	ContainerFlagsPropagatesThisKeyword                            ContainerFlags = 1 << 9
)

type ExpandoAssignmentInfo struct {
	node                store.Node
	container           store.Node
	blockScopeContainer store.Node
}

type Binder struct {
	file            *store.File
	s               *store.Store
	bound           *store.Bound
	bindFunc        store.Visitor
	unreachableFlow store.FlowRef

	container               store.Node
	thisContainer           store.Node
	blockScopeContainer     store.Node
	lastContainer           store.Node
	currentFlow             store.FlowRef
	currentBreakTarget      store.FlowRef
	currentContinueTarget   store.FlowRef
	currentReturnTarget     store.FlowRef
	currentTrueTarget       store.FlowRef
	currentFalseTarget      store.FlowRef
	currentExceptionTarget  store.FlowRef
	preSwitchCaseFlow       store.FlowRef
	activeLabelList         *ActiveLabel
	emitFlags               ast.NodeFlags
	seenThisKeyword         bool
	hasExplicitReturn       bool
	hasFlowEffects          bool
	inAssignmentPattern     bool
	seenParseError          bool
	symbolCount             int
	notConstEnumOnlyModules collections.Set[*store.Symbol]
	symbolArena             core.Arena[store.Symbol]
	singleDeclarationsArena core.Arena[store.Ref]
	expandoAssignments      []ExpandoAssignmentInfo
	scratch                 *store.BoundScratch // kept by putBinder
}

type ActiveLabel struct {
	next           *ActiveLabel
	breakTarget    store.FlowRef
	continueTarget store.FlowRef
	name           string
	referenced     bool
}

func (label *ActiveLabel) BreakTarget() store.FlowRef    { return label.breakTarget }
func (label *ActiveLabel) ContinueTarget() store.FlowRef { return label.continueTarget }

func BindSourceFile(file *store.File) {
	// This is constructed this way to make the compiler "out-line" the function,
	// avoiding most work in the common case where the file has already been bound.
	if !file.IsBound() {
		bindSourceFile(file)
	}
}

var binderPool = sync.Pool{
	New: func() any {
		b := &Binder{}
		b.bindFunc = b.bind // Allocate closure once
		return b
	},
}

func getBinder() *Binder {
	return binderPool.Get().(*Binder)
}

func putBinder(b *Binder) {
	*b = Binder{bindFunc: b.bindFunc, scratch: b.scratch}
	binderPool.Put(b)
}

func bindSourceFile(file *store.File) {
	file.BindOnce(func() {
		b := getBinder()
		defer putBinder(b)
		b.file = file
		b.s = file.Store
		// The scratch goes back to the Binder only after Compact: if the bind
		// panics, file.Bound still aliases it and it must not be reused.
		scratch := b.scratch
		b.scratch = nil
		if scratch == nil {
			scratch = &store.BoundScratch{}
		}
		b.bound = store.NewBoundIn(scratch)
		file.Bound = b.bound
		// The container locals start as the nil node, not the zero Node,
		// so that IsNil reads a header.
		b.container = b.nilNode()
		b.thisContainer = b.nilNode()
		b.blockScopeContainer = b.nilNode()
		b.lastContainer = b.nilNode()
		b.unreachableFlow = b.newFlowNode(ast.FlowFlagsUnreachable)
		b.bind(file.Root())
		b.bindDeferredExpandoAssignments()
		b.bound.SymbolCount = b.symbolCount
		b.bound.Compact(scratch)
		b.scratch = scratch
		b.s.Seal()
	})
}

// nilNode is the sentinel node: what a nil *ast.Node is in the Store.
func (b *Binder) nilNode() store.Node {
	return b.s.Node(store.NoNodeRef)
}

// symbolOf resolves the symbol in the node's header (node.Symbol() in the Pointer AST).
func (b *Binder) symbolOf(node store.Node) *store.Symbol {
	return b.bound.SymbolOf(node.Symbol())
}

func (b *Binder) newSymbol(flags ast.SymbolFlags, name string) *store.Symbol {
	b.symbolCount++
	result := b.symbolArena.New()
	result.Flags = flags
	result.Name = name
	b.bound.AddSymbol(result)
	return result
}

/**
 * Declares a Symbol for the node and adds it to symbols. Reports errors for conflicting identifier names.
 * @param symbolTable - The symbol table which node will be added to.
 * @param parent - node's parent declaration.
 * @param node - The declaration to be added to the symbol table
 * @param includes - The SymbolFlags that node has in addition to its declaration type (eg: export, ambient, etc.)
 * @param excludes - The flags which node cannot be declared alongside in a symbol table. Used to report forbidden declarations.
 */
func (b *Binder) declareSymbol(symbolTable store.SymbolTable, parent *store.Symbol, node store.Node, includes ast.SymbolFlags, excludes ast.SymbolFlags) *store.Symbol {
	return b.declareSymbolEx(symbolTable, parent, node, includes, excludes, false /*isReplaceableByMethod*/, false /*isComputedName*/)
}

func (b *Binder) declareSymbolEx(symbolTable store.SymbolTable, parent *store.Symbol, node store.Node, includes ast.SymbolFlags, excludes ast.SymbolFlags, isReplaceableByMethod bool, isComputedName bool) *store.Symbol {
	debug.Assert(isComputedName || !store.HasDynamicName(node))
	isDefaultExport := store.HasSyntacticModifier(node, ast.ModifierFlagsDefault) || store.IsExportSpecifier(node) && store.ModuleExportNameIsDefault(node.AsExportSpecifier().Name())
	// The exported symbol for an export default function/class node is always named "default"
	var name string
	switch {
	case isComputedName:
		name = ast.InternalSymbolNameComputed
	case isDefaultExport && parent != nil:
		name = ast.InternalSymbolNameDefault
	default:
		name = b.getDeclarationName(node)
	}
	var symbol *store.Symbol
	if name == ast.InternalSymbolNameMissing {
		symbol = b.newSymbol(ast.SymbolFlagsNone, ast.InternalSymbolNameMissing)
	} else {
		// Check and see if the symbol table already has a symbol with this name.  If not,
		// create a new symbol with this name and add it to the table.  Note that we don't
		// give the new symbol any flags *yet*.  This ensures that it will not conflict
		// with the 'excludes' flags we pass in.
		//
		// If we do get an existing symbol, see if it conflicts with the new symbol we're
		// creating.  For example, a 'var' symbol and a 'class' symbol will conflict within
		// the same symbol table.  If we have a conflict, report the issue on each
		// declaration we have for this symbol, and then create a new symbol for this
		// declaration.
		//
		// Note that when properties declared in Javascript constructors
		// (marked by isReplaceableByMethod) conflict with another symbol, the property loses.
		// Always. This allows the common Javascript pattern of overwriting a prototype method
		// with an bound instance method of the same type: `this.method = this.method.bind(this)`
		//
		// If we created a new symbol, either because we didn't have a symbol with this name
		// in the symbol table, or we conflicted with an existing symbol, then just add this
		// node as the sole declaration of the new symbol.
		//
		// Otherwise, we'll be merging into a compatible existing symbol (for example when
		// you have multiple 'vars' with the same name in the same container).  In this case
		// just add this node into the declarations list of the symbol.
		symbol = symbolTable[name]
		if symbol == nil {
			symbol = b.newSymbol(ast.SymbolFlagsNone, name)
			symbolTable[name] = symbol
			if isReplaceableByMethod {
				symbol.Flags |= ast.SymbolFlagsReplaceableByMethod
			}
		} else if isReplaceableByMethod && symbol.Flags&ast.SymbolFlagsReplaceableByMethod == 0 {
			// A symbol already exists, so don't add this as a declaration.
			return symbol
		} else if symbol.Flags&excludes != 0 {
			if symbol.Flags&ast.SymbolFlagsReplaceableByMethod != 0 {
				// Javascript constructor-declared symbols can be discarded in favor of
				// prototype symbols like methods.
				symbol = b.newSymbol(ast.SymbolFlagsNone, name)
				symbolTable[name] = symbol
			} else if !(includes&ast.SymbolFlagsVariable != 0 && symbol.Flags&ast.SymbolFlagsAssignment != 0 ||
				includes&ast.SymbolFlagsAssignment != 0 && symbol.Flags&ast.SymbolFlagsVariable != 0) {
				// Assignment declarations are allowed to merge with variables, no matter what other flags they have.
				// Report errors every position with duplicate declaration
				// Report errors on previous encountered declarations
				var message *diagnostics.Message
				if symbol.Flags&ast.SymbolFlagsBlockScopedVariable != 0 {
					message = diagnostics.Cannot_redeclare_block_scoped_variable_0
				} else {
					message = diagnostics.Duplicate_identifier_0
				}
				messageNeedsName := true
				if symbol.Flags&ast.SymbolFlagsEnum != 0 || includes&ast.SymbolFlagsEnum != 0 {
					message = diagnostics.Enum_declarations_can_only_merge_with_namespace_or_other_enum_declarations
					messageNeedsName = false
				}
				multipleDefaultExports := false
				if len(symbol.Declarations) != 0 {
					// If the current node is a default export of some sort, then check if
					// there are any other default exports that we need to error on.
					// We'll know whether we have other default exports depending on if `symbol` already has a declaration list set.
					if isDefaultExport {
						message = diagnostics.A_module_cannot_have_multiple_default_exports
						messageNeedsName = false
						multipleDefaultExports = true
					} else {
						// This is to properly report an error in the case "export default { }" is after export default of class declaration or function declaration.
						// Error on multiple export default in the following case:
						// 1. multiple export default of class declaration or function declaration by checking NodeFlags.Default
						// 2. multiple export default of export assignment. This one doesn't have NodeFlags.Default on (as export default doesn't considered as modifiers)
						if len(symbol.Declarations) != 0 && store.IsExportAssignment(node) && !node.AsExportAssignment().IsExportEquals() {
							message = diagnostics.A_module_cannot_have_multiple_default_exports
							messageNeedsName = false
							multipleDefaultExports = true
						}
					}
				}
				var declarationName store.Node = store.GetNameOfDeclaration(node)
				if declarationName.IsNil() {
					declarationName = node
				}
				var diag *ast.Diagnostic
				if messageNeedsName {
					diag = b.createDiagnosticForNode(declarationName, message, b.getDisplayName(node))
				} else {
					diag = b.createDiagnosticForNode(declarationName, message)
				}
				if store.IsTypeAliasDeclaration(node) && store.NodeIsMissing(node.Type()) && store.HasSyntacticModifier(node, ast.ModifierFlagsExport) && symbol.Flags&(ast.SymbolFlagsAlias|ast.SymbolFlagsType|ast.SymbolFlagsNamespace) != 0 {
					// export type T; - may have meant export type { T }?
					diag.AddRelatedInfo(b.createDiagnosticForNode(node, diagnostics.Did_you_mean_0, "export type { "+node.AsTypeAliasDeclaration().Name().Text()+" }"))
				}
				for index, declarationRef := range symbol.Declarations {
					declaration := b.s.Node(declarationRef.Id())
					var decl store.Node = store.GetNameOfDeclaration(declaration)
					if decl.IsNil() {
						decl = declaration
					}
					var d *ast.Diagnostic
					if messageNeedsName {
						d = b.createDiagnosticForNode(decl, message, b.getDisplayName(declaration))
					} else {
						d = b.createDiagnosticForNode(decl, message)
					}
					if multipleDefaultExports {
						d.AddRelatedInfo(b.createDiagnosticForNode(declarationName, core.IfElse(index == 0, diagnostics.Another_export_default_is_here, diagnostics.X_and_here)))
					}
					b.addDiagnostic(d)
					if multipleDefaultExports {
						diag.AddRelatedInfo(b.createDiagnosticForNode(decl, diagnostics.The_first_export_default_is_here))
					}
				}
				b.addDiagnostic(diag)
				// When get or set accessor conflicts with a non-accessor or an accessor of a different kind, we mark
				// the symbol as a full accessor such that all subsequent declarations are considered conflicting. This
				// for example ensures that a get accessor followed by a non-accessor followed by a set accessor with the
				// same name are all marked as duplicates.
				if symbol.Flags&ast.SymbolFlagsAccessor != 0 && symbol.Flags&ast.SymbolFlagsAccessor != includes&ast.SymbolFlagsAccessor {
					symbol.Flags |= ast.SymbolFlagsAccessor
				}
				symbol = b.newSymbol(ast.SymbolFlagsNone, name)
			}
		}
	}
	b.addDeclarationToSymbol(symbol, node, includes)
	if symbol.Parent == nil {
		symbol.Parent = parent
	} else if symbol.Parent != parent {
		panic("Existing symbol parent should match new one")
	}
	return symbol
}

// Should not be called on a declaration with a computed property name,
// unless it is a well known Symbol.
func (b *Binder) getDeclarationName(node store.Node) string {
	if store.IsExportAssignment(node) {
		return core.IfElse(node.AsExportAssignment().IsExportEquals(), ast.InternalSymbolNameExportEquals, ast.InternalSymbolNameDefault)
	}
	name := store.GetNameOfDeclaration(node)
	if !name.IsNil() {
		if store.IsAmbientModule(node) {
			moduleName := name.Text()
			if store.IsGlobalScopeAugmentation(node) {
				return ast.InternalSymbolNameGlobal
			}
			if pattern := core.TryParsePattern(moduleName); pattern.IsValid() && pattern.StarIndex >= 0 {
				if attributes := node.AsModuleDeclaration().Attributes(); !attributes.IsNil() {
					// The Pointer binder uses the process-wide node id; here the
					// id is the file and node index, so that two files' same-named
					// patterns stay apart when the checker merges globals.
					ref := attributes.FileRef()
					return ast.InternalSymbolNamePrefix + "\"" + moduleName + "\"pattern@" + strconv.FormatUint(uint64(ref.File()), 10) + ":" + strconv.FormatUint(uint64(ref.Id()), 10)
				}
			}
			return "\"" + moduleName + "\""
		}
		if store.IsPrivateIdentifier(name) {
			// containingClass exists because private names only allowed inside classes
			containingClass := store.GetContainingClass(node)
			if containingClass.IsNil() {
				// we can get here in cases where there is already a parse error.
				return ast.InternalSymbolNameMissing
			}
			return GetSymbolNameForPrivateIdentifier(b.symbolOf(containingClass), name.Text())
		}
		if store.IsPropertyNameLiteral(name) || store.IsJsxNamespacedName(name) {
			return name.Text()
		}
		if store.IsComputedPropertyName(name) {
			nameExpression := name.Expression()
			// treat computed property names where expression is string/numeric literal as just string/numeric literal
			if store.IsStringOrNumericLiteralLike(nameExpression) {
				return nameExpression.Text()
			}
			if store.IsSignedNumericLiteral(nameExpression) {
				unaryExpression := nameExpression.AsPrefixUnaryExpression()
				return scanner.TokenToString(unaryExpression.Operator()) + unaryExpression.Operand().Text()
			}
			panic("Only computed properties with literal names have declaration names")
		}
		return ast.InternalSymbolNameMissing
	}
	switch node.Kind() {
	case ast.KindConstructor:
		return ast.InternalSymbolNameConstructor
	case ast.KindFunctionType, ast.KindCallSignature:
		return ast.InternalSymbolNameCall
	case ast.KindConstructorType, ast.KindConstructSignature:
		return ast.InternalSymbolNameNew
	case ast.KindIndexSignature:
		return ast.InternalSymbolNameIndex
	case ast.KindExportDeclaration:
		return ast.InternalSymbolNameExportStar
	case ast.KindSourceFile, ast.KindBinaryExpression:
		return ast.InternalSymbolNameExportEquals
	}
	return ast.InternalSymbolNameMissing
}

func (b *Binder) getDisplayName(node store.Node) string {
	nameNode := node.Name()
	if !nameNode.IsNil() {
		return declarationNameToString(b.file, nameNode)
	}
	name := b.getDeclarationName(node)
	if name != ast.InternalSymbolNameMissing {
		return name
	}
	return "(Missing)"
}

func GetSymbolNameForPrivateIdentifier(containingClassSymbol *store.Symbol, description string) string {
	return ast.InternalSymbolNamePrefix + "#" + strconv.Itoa(int(containingClassSymbol.Id())) + "@" + description
}

func (b *Binder) declareModuleMember(node store.Node, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *store.Symbol {
	container := b.container
	hasExportModifier := store.GetCombinedModifierFlags(node)&ast.ModifierFlagsExport != 0 || store.IsImplicitlyExportedJSDocDeclaration(b.file, node)
	if symbolFlags&ast.SymbolFlagsAlias != 0 {
		if node.Kind() == ast.KindExportSpecifier || (node.Kind() == ast.KindImportEqualsDeclaration && hasExportModifier) {
			return b.declareSymbol(store.GetExports(b.symbolOf(container)), b.symbolOf(container), node, symbolFlags, symbolExcludes)
		}
		return b.declareSymbol(store.GetLocals(b.bound, container), nil /*parent*/, node, symbolFlags, symbolExcludes)
	}
	// Exported module members are given 2 symbols: A local symbol that is classified with an ExportValue flag,
	// and an associated export symbol with all the correct flags set on it. There are 2 main reasons:
	//
	//   1. We treat locals and exports of the same name as mutually exclusive within a container.
	//      That means the binder will issue a Duplicate Identifier error if you mix locals and exports
	//      with the same name in the same container.
	//      TODO: Make this a more specific error and decouple it from the exclusion logic.
	//   2. When we checkIdentifier in the checker, we set its resolved symbol to the local symbol,
	//      but return the export symbol (by calling getExportSymbolOfValueSymbolIfExported). That way
	//      when the emitter comes back to it, it knows not to qualify the name if it was found in a containing scope.
	//
	// NOTE: Nested ambient modules always should go to to 'locals' table to prevent their automatic merge
	//       during global merging in the checker. Why? The only case when ambient module is permitted inside another module is module augmentation
	//       and this case is specially handled. Module augmentations should only be merged with original module definition
	//       and should never be merged directly with other augmentation, and the latter case would be possible if automatic merge is allowed.
	if !store.IsAmbientModule(node) && (hasExportModifier || container.Flags()&ast.NodeFlagsExportContext != 0) {
		if !store.IsLocalsContainer(container) || (store.HasSyntacticModifier(node, ast.ModifierFlagsDefault) && b.getDeclarationName(node) == ast.InternalSymbolNameMissing) {
			return b.declareSymbol(store.GetExports(b.symbolOf(container)), b.symbolOf(container), node, symbolFlags, symbolExcludes)
			// No local symbol for an unnamed default!
		}
		exportKind := ast.SymbolFlagsNone
		if symbolFlags&ast.SymbolFlagsValue != 0 {
			exportKind = ast.SymbolFlagsExportValue
		}
		local := b.declareSymbol(store.GetLocals(b.bound, container), nil /*parent*/, node, exportKind, symbolExcludes)
		local.ExportSymbol = b.declareSymbol(store.GetExports(b.symbolOf(container)), b.symbolOf(container), node, symbolFlags, symbolExcludes)
		node.SetLocalSymbol(local.Id())
		return local
	}
	return b.declareSymbol(store.GetLocals(b.bound, container), nil /*parent*/, node, symbolFlags, symbolExcludes)
}

func (b *Binder) declareClassMember(node store.Node, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *store.Symbol {
	if store.IsStatic(node) {
		return b.declareSymbol(store.GetExports(b.symbolOf(b.container)), b.symbolOf(b.container), node, symbolFlags, symbolExcludes)
	}
	return b.declareSymbol(store.GetMembers(b.symbolOf(b.container)), b.symbolOf(b.container), node, symbolFlags, symbolExcludes)
}

func (b *Binder) declareSourceFileMember(node store.Node, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *store.Symbol {
	if b.file.IsExternalModule() {
		return b.declareModuleMember(node, symbolFlags, symbolExcludes)
	}
	return b.declareSymbol(store.GetLocals(b.bound, b.file.Root()), nil /*parent*/, node, symbolFlags, symbolExcludes)
}

func (b *Binder) declareSymbolAndAddToSymbolTable(node store.Node, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *store.Symbol {
	switch b.container.Kind() {
	case ast.KindModuleDeclaration:
		return b.declareModuleMember(node, symbolFlags, symbolExcludes)
	case ast.KindSourceFile:
		return b.declareSourceFileMember(node, symbolFlags, symbolExcludes)
	case ast.KindClassExpression, ast.KindClassDeclaration:
		return b.declareClassMember(node, symbolFlags, symbolExcludes)
	case ast.KindEnumDeclaration:
		return b.declareSymbol(store.GetExports(b.symbolOf(b.container)), b.symbolOf(b.container), node, symbolFlags, symbolExcludes)
	case ast.KindTypeLiteral, ast.KindObjectLiteralExpression, ast.KindInterfaceDeclaration, ast.KindJsxAttributes:
		return b.declareSymbol(store.GetMembers(b.symbolOf(b.container)), b.symbolOf(b.container), node, symbolFlags, symbolExcludes)
	case ast.KindFunctionType, ast.KindConstructorType, ast.KindCallSignature, ast.KindConstructSignature,
		ast.KindIndexSignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor,
		ast.KindSetAccessor, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindClassStaticBlockDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindMappedType:
		return b.declareSymbol(store.GetLocals(b.bound, b.container), nil /*parent*/, node, symbolFlags, symbolExcludes)
	}
	panic("Unhandled case in declareSymbolAndAddToSymbolTable")
}

func (b *Binder) newFlowNode(flags ast.FlowFlags) store.FlowRef {
	return b.bound.NewFlow(flags, store.NoNodeRef, 0)
}

// node is a NodeRef: the Ref of an AST node, or the FlowData index of a
// SwitchClause or ReduceLabel flow.
func (b *Binder) newFlowNodeEx(flags ast.FlowFlags, node store.NodeRef, antecedent store.FlowRef) store.FlowRef {
	return b.bound.NewFlow(flags, node, antecedent)
}

func (b *Binder) createLoopLabel() store.FlowRef {
	return b.newFlowNode(ast.FlowFlagsLoopLabel)
}

func (b *Binder) createBranchLabel() store.FlowRef {
	return b.newFlowNode(ast.FlowFlagsBranchLabel)
}

func (b *Binder) createReduceLabel(target store.FlowRef, antecedents store.FlowListRef, antecedent store.FlowRef) store.FlowRef {
	return b.newFlowNodeEx(ast.FlowFlagsReduceLabel, store.NodeRef(b.newFlowData(uint32(target), uint32(antecedents), 0)), antecedent)
}

func (b *Binder) createFlowCondition(flags ast.FlowFlags, antecedent store.FlowRef, expression store.Node) store.FlowRef {
	if b.bound.Flow(antecedent).Flags&ast.FlowFlagsUnreachable != 0 {
		return antecedent
	}
	if expression.IsNil() {
		if flags&ast.FlowFlagsTrueCondition != 0 {
			return antecedent
		}
		return b.unreachableFlow
	}
	if (expression.Kind() == ast.KindTrueKeyword && flags&ast.FlowFlagsFalseCondition != 0 || expression.Kind() == ast.KindFalseKeyword && flags&ast.FlowFlagsTrueCondition != 0) && !store.IsExpressionOfOptionalChainRoot(expression) && !store.IsNullishCoalesce(expression.Parent()) {
		return b.unreachableFlow
	}
	if !isNarrowingExpression(expression) {
		return antecedent
	}
	b.setFlowNodeReferenced(antecedent)
	return b.newFlowNodeEx(flags, expression.Ref(), antecedent)
}

func (b *Binder) createFlowMutation(flags ast.FlowFlags, antecedent store.FlowRef, node store.Node) store.FlowRef {
	b.setFlowNodeReferenced(antecedent)
	b.hasFlowEffects = true
	result := b.newFlowNodeEx(flags, node.Ref(), antecedent)
	if b.currentExceptionTarget != 0 {
		b.addAntecedent(b.currentExceptionTarget, result)
	}
	return result
}

func (b *Binder) createFlowSwitchClause(antecedent store.FlowRef, switchStatement store.Node, clauseStart int, clauseEnd int) store.FlowRef {
	b.setFlowNodeReferenced(antecedent)
	return b.newFlowNodeEx(ast.FlowFlagsSwitchClause, store.NodeRef(b.newFlowData(uint32(switchStatement.Ref()), uint32(clauseStart), uint32(clauseEnd))), antecedent)
}

func (b *Binder) createFlowCall(antecedent store.FlowRef, node store.Node) store.FlowRef {
	b.setFlowNodeReferenced(antecedent)
	b.hasFlowEffects = true
	return b.newFlowNodeEx(ast.FlowFlagsCall, node.Ref(), antecedent)
}

func (b *Binder) newFlowList(head store.FlowRef, tail store.FlowListRef) store.FlowListRef {
	return b.bound.NewFlowList(head, tail)
}

func (b *Binder) combineFlowLists(head store.FlowListRef, tail store.FlowListRef) store.FlowListRef {
	if head == 0 {
		return tail
	}
	// Read head before the recursion appends to the list slab.
	flow, next := b.bound.FlowList(head).Flow, b.bound.FlowList(head).Next
	return b.newFlowList(flow, b.combineFlowLists(next, tail))
}

func (b *Binder) newSingleDeclaration(declaration store.Ref) []store.Ref {
	return b.singleDeclarationsArena.NewSlice1(declaration)
}

// newFlowData is what ast.NewFlowSwitchClauseData and NewFlowReduceLabelData
// are here: the payload of a synthetic flow node, referenced by index.
func (b *Binder) newFlowData(a, bb, c uint32) uint32 {
	return b.bound.NewFlowData(a, bb, c)
}

func (b *Binder) setFlowNodeReferenced(flow store.FlowRef) {
	// On first reference we set the Referenced flag, thereafter we set the Shared flag
	f := b.bound.Flow(flow)
	if f.Flags&ast.FlowFlagsReferenced == 0 {
		f.Flags |= ast.FlowFlagsReferenced
	} else {
		f.Flags |= ast.FlowFlagsShared
	}
}

func (b *Binder) addAntecedent(label store.FlowRef, antecedent store.FlowRef) {
	if b.bound.Flow(antecedent).Flags&ast.FlowFlagsUnreachable != 0 {
		return
	}
	// If antecedent isn't already on the Antecedents list, add it to the end of the list
	var last store.FlowListRef
	for list := b.bound.Flow(label).Antecedents; list != 0; list = b.bound.FlowList(list).Next {
		if b.bound.FlowList(list).Flow == antecedent {
			return
		}
		last = list
	}
	// The new list cell is made before the slab pointer is taken: the append may move the slab.
	cell := b.newFlowList(antecedent, 0)
	if last == 0 {
		b.bound.Flow(label).Antecedents = cell
	} else {
		b.bound.FlowList(last).Next = cell
	}
	b.setFlowNodeReferenced(antecedent)
}

func (b *Binder) finishFlowLabel(label store.FlowRef) store.FlowRef {
	antecedents := b.bound.Flow(label).Antecedents
	if antecedents == 0 {
		return b.unreachableFlow
	}
	if b.bound.FlowList(antecedents).Next == 0 {
		return b.bound.FlowList(antecedents).Flow
	}
	return label
}

func (b *Binder) bind(node store.Node) bool {
	if node.IsNil() {
		return false
	}
	// Even though in the AST the jsdoc @typedef node belongs to the current node,
	// its symbol might be in the same scope with the current node's symbol. Consider:
	//
	//     /** @typedef {string | number} MyType */
	//     function foo();
	//
	// Here the current node is "foo", which is a container, but the scope of "MyType" should
	// not be inside "foo". Therefore we always bind @typedef before bind the parent node,
	// and skip binding this tag later when binding all the other jsdoc tags.

	// First we bind declaration nodes to a symbol if possible. We'll both create a symbol
	// and then potentially add the symbol to an appropriate symbol table. Possible
	// destination symbol tables are:
	//
	//  1) The 'exports' table of the current container's symbol.
	//  2) The 'members' table of the current container's symbol.
	//  3) The 'locals' table of the current container.
	//
	// However, not all symbols will end up in any of these tables. 'Anonymous' symbols
	// (like TypeLiterals for example) will not be put in any table.
	kind := node.Kind()
	switch kind {
	case ast.KindIdentifier:
		node.SetFlow(b.currentFlow)
		b.checkContextualIdentifier(node)
	case ast.KindThisKeyword, ast.KindSuperKeyword:
		if kind == ast.KindThisKeyword {
			b.seenThisKeyword = true
		}
		node.SetFlow(b.currentFlow)
	case ast.KindQualifiedName:
		if b.currentFlow != 0 && store.IsPartOfTypeQuery(node) {
			node.SetFlow(b.currentFlow)
		}
	case ast.KindMetaProperty:
		node.SetFlow(b.currentFlow)
	case ast.KindPrivateIdentifier:
		b.checkPrivateIdentifier(node)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		if b.currentFlow != 0 && isNarrowableReference(node) {
			setFlowNode(node, b.currentFlow)
		}
	case ast.KindBinaryExpression:
		switch store.GetAssignmentDeclarationKind(node) {
		case ast.JSDeclarationKindModuleExports:
			b.bindModuleExportsAssignment(node)
		case ast.JSDeclarationKindExportsProperty:
			b.bindExportsOrObjectDefineProperty(node)
		case ast.JSDeclarationKindProperty:
			b.bindExpandoPropertyAssignment(node)
		case ast.JSDeclarationKindThisProperty:
			b.bindThisPropertyAssignment(node)
		}
		b.checkStrictModeBinaryExpression(node)
	case ast.KindCatchClause:
		b.checkStrictModeCatchClause(node)
	case ast.KindDeleteExpression:
		b.checkStrictModeDeleteExpression(node)
	case ast.KindPostfixUnaryExpression:
		b.checkStrictModePostfixUnaryExpression(node)
	case ast.KindPrefixUnaryExpression:
		b.checkStrictModePrefixUnaryExpression(node)
	case ast.KindWithStatement:
		b.checkStrictModeWithStatement(node)
	case ast.KindLabeledStatement:
		b.checkStrictModeLabeledStatement(node)
	case ast.KindThisType:
		b.seenThisKeyword = true
	case ast.KindTypeParameter:
		b.bindTypeParameter(node)
	case ast.KindParameter:
		b.bindParameter(node)
	case ast.KindVariableDeclaration:
		b.bindVariableDeclarationOrBindingElement(node)
	case ast.KindBindingElement:
		node.SetFlow(b.currentFlow)
		b.bindVariableDeclarationOrBindingElement(node)
	case ast.KindPropertyDeclaration, ast.KindPropertySignature:
		b.bindPropertyWorker(node)
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment:
		b.bindPropertyOrMethodOrAccessor(node, ast.SymbolFlagsProperty, ast.SymbolFlagsPropertyExcludes)
	case ast.KindEnumMember:
		b.bindPropertyOrMethodOrAccessor(node, ast.SymbolFlagsEnumMember, ast.SymbolFlagsEnumMemberExcludes)
	case ast.KindCallSignature, ast.KindConstructSignature, ast.KindIndexSignature:
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsSignature, ast.SymbolFlagsNone)
	case ast.KindMethodDeclaration, ast.KindMethodSignature:
		b.bindPropertyOrMethodOrAccessor(node, ast.SymbolFlagsMethod|getOptionalSymbolFlagForNode(node), core.IfElse(store.IsObjectLiteralMethod(node), ast.SymbolFlagsValue, ast.SymbolFlagsMethodExcludes))
	case ast.KindFunctionDeclaration:
		b.bindFunctionDeclaration(node)
	case ast.KindConstructor:
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsConstructor, ast.SymbolFlagsNone)
	case ast.KindGetAccessor:
		b.bindPropertyOrMethodOrAccessor(node, ast.SymbolFlagsGetAccessor, ast.SymbolFlagsGetAccessorExcludes)
	case ast.KindSetAccessor:
		b.bindPropertyOrMethodOrAccessor(node, ast.SymbolFlagsSetAccessor, ast.SymbolFlagsSetAccessorExcludes)
	case ast.KindFunctionType, ast.KindConstructorType:
		b.bindFunctionOrConstructorType(node)
	case ast.KindTypeLiteral, ast.KindMappedType:
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsTypeLiteral, ast.InternalSymbolNameType)
	case ast.KindObjectLiteralExpression:
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsObjectLiteral, ast.InternalSymbolNameObject)
	case ast.KindFunctionExpression, ast.KindArrowFunction:
		b.bindFunctionExpression(node)
	case ast.KindClassExpression, ast.KindClassDeclaration:
		b.bindClassLikeDeclaration(node)
	case ast.KindInterfaceDeclaration:
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsInterface, ast.SymbolFlagsInterfaceExcludes)
	case ast.KindCallExpression:
		switch store.GetAssignmentDeclarationKind(node) {
		case ast.JSDeclarationKindObjectDefinePropertyValue:
			b.bindExpandoPropertyAssignment(node)
		case ast.JSDeclarationKindObjectDefinePropertyExports:
			b.bindExportsOrObjectDefineProperty(node)
		}
		if store.IsInJSFile(node) {
			b.bindCallExpression(node)
		}
	case ast.KindTypeAliasDeclaration:
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
	case ast.KindJSTypeAliasDeclaration:
		// Top-level JSTypeAliasDeclaration nodes are processed in bindContainer
		if !store.IsSourceFile(b.blockScopeContainer) {
			b.bindBlockScopedDeclaration(node, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
		}
	case ast.KindEnumDeclaration:
		b.bindEnumDeclaration(node)
	case ast.KindModuleDeclaration:
		b.bindModuleDeclaration(node)
	case ast.KindImportEqualsDeclaration, ast.KindNamespaceImport, ast.KindImportSpecifier, ast.KindExportSpecifier:
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	case ast.KindNamespaceExportDeclaration:
		b.bindNamespaceExportDeclaration(node)
	case ast.KindImportClause:
		b.bindImportClause(node)
	case ast.KindExportDeclaration:
		b.bindExportDeclaration(node)
	case ast.KindExportAssignment:
		b.bindExportAssignment(node)
	case ast.KindSourceFile:
		b.bindSourceFileIfExternalModule()
	case ast.KindJsxAttributes:
		b.bindJsxAttributes(node)
	case ast.KindJsxAttribute:
		b.bindJsxAttribute(node, ast.SymbolFlagsProperty, ast.SymbolFlagsPropertyExcludes)
	}
	// Then we recurse into the children of the node to bind them as well. For certain
	// symbols we do specialized work when we recurse. For example, we'll keep track of
	// the current 'container' node when it changes. This helps us know which symbol table
	// a local should go into for example. Since terminal nodes are known not to have
	// children, as an optimization we don't process those.
	thisNodeOrAnySubnodesHasError := node.Flags()&ast.NodeFlagsThisNodeHasError != 0
	if kind > ast.KindLastToken {
		saveSeenParseError := b.seenParseError
		b.seenParseError = false
		containerFlags := GetContainerFlags(node)
		if containerFlags == ContainerFlagsNone {
			b.bindChildren(node)
		} else {
			b.bindContainer(node, containerFlags)
		}
		if b.seenParseError {
			thisNodeOrAnySubnodesHasError = true
		}
		b.seenParseError = saveSeenParseError
	}
	if thisNodeOrAnySubnodesHasError {
		node.AddFlags(ast.NodeFlagsThisNodeOrAnySubNodesHasError)
		b.seenParseError = true
	}
	return false
}

func (b *Binder) bindPropertyWorker(node store.Node) {
	isAutoAccessor := store.IsAutoAccessorPropertyDeclaration(node)
	includes := core.IfElse(isAutoAccessor, ast.SymbolFlagsAccessor, ast.SymbolFlagsProperty)
	excludes := core.IfElse(isAutoAccessor, ast.SymbolFlagsAccessorExcludes, ast.SymbolFlagsPropertyExcludes)
	b.bindPropertyOrMethodOrAccessor(node, includes|getOptionalSymbolFlagForNode(node), excludes)
}

func (b *Binder) bindSourceFileIfExternalModule() {
	b.setExportContextFlag(b.file.Root())
	if b.file.IsExternalOrCommonJSModule() {
		b.bindSourceFileAsExternalModule()
	} else if store.IsJsonSourceFile(b.file) {
		b.bindSourceFileAsExternalModule()
		// Create symbol equivalent for the module.exports = {}
		root := b.file.Root()
		originalSymbol := root.Symbol()
		b.declareSymbol(store.GetSymbolTable(&b.symbolOf(root).Exports), b.symbolOf(root), root, ast.SymbolFlagsProperty, ast.SymbolFlagsAll)
		root.SetSymbol(originalSymbol)
	}
}

func (b *Binder) bindSourceFileAsExternalModule() {
	b.bindAnonymousDeclaration(b.file.Root(), ast.SymbolFlagsValueModule, "\""+tspath.RemoveFileExtension(b.file.FileName)+"\"")
}

func (b *Binder) bindModuleDeclaration(node store.Node) {
	b.setExportContextFlag(node)
	if store.IsAmbientModule(node) {
		if store.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
			b.errorOnFirstToken(node, diagnostics.X_export_modifier_cannot_be_applied_to_ambient_modules_and_module_augmentations_since_they_are_always_visible)
		}
		if store.IsModuleAugmentationExternal(b.file, node) {
			b.declareModuleSymbol(node)
		} else {
			name := node.AsModuleDeclaration().Name()
			symbol := b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsValueModule, ast.SymbolFlagsValueModuleExcludes)

			if store.IsStringLiteral(name) {
				attributes := node.AsModuleDeclaration().Attributes()
				nameText := name.Text()
				pattern := core.TryParsePattern(nameText)
				if !pattern.IsValid() {
					// An invalid pattern - must have multiple wildcards.
					b.errorOnFirstToken(name, diagnostics.Pattern_0_can_have_at_most_one_Asterisk_character, nameText)
				} else if pattern.StarIndex >= 0 {
					b.bound.PatternAmbientModules = append(b.bound.PatternAmbientModules, store.PatternAmbientModule{Pattern: pattern, Symbol: symbol})
				} else if !attributes.IsNil() {
					b.errorOnNode(name, diagnostics.An_ambient_module_declaration_with_import_attributes_must_use_a_pattern_name_with_an_Asterisk_character)
				}
			}
		}
	} else {
		state := b.declareModuleSymbol(node)
		if state != ast.ModuleInstanceStateNonInstantiated {
			symbol := b.symbolOf(node)
			// if module was already merged with some function, class or non-const enum, treat it as non-const-enum-only
			constEnumOnlyModule := (symbol.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsClass|ast.SymbolFlagsRegularEnum) == 0) &&
				// Current must be `const enum` only
				state == ast.ModuleInstanceStateConstEnumOnly &&
				// Can't have been set to 'false' in a previous merged symbol. ('undefined' OK)
				!b.notConstEnumOnlyModules.Has(symbol)
			if constEnumOnlyModule {
				symbol.Flags |= ast.SymbolFlagsConstEnumOnlyModule
			} else {
				symbol.Flags &^= ast.SymbolFlagsConstEnumOnlyModule
				b.notConstEnumOnlyModules.Add(symbol)
			}
		}
	}
}

func (b *Binder) declareModuleSymbol(node store.Node) ast.ModuleInstanceState {
	state := store.GetModuleInstanceState(node)
	instantiated := state != ast.ModuleInstanceStateNonInstantiated
	b.declareSymbolAndAddToSymbolTable(node, core.IfElse(instantiated, ast.SymbolFlagsValueModule, ast.SymbolFlagsNamespaceModule), core.IfElse(instantiated, ast.SymbolFlagsValueModuleExcludes, ast.SymbolFlagsNamespaceModuleExcludes))
	return state
}

func (b *Binder) bindNamespaceExportDeclaration(node store.Node) {
	if !node.Modifiers().IsNil() {
		b.errorOnNode(node, diagnostics.Modifiers_cannot_appear_here)
	}
	switch {
	case !store.IsSourceFile(node.Parent()):
		b.errorOnNode(node, diagnostics.Global_module_exports_may_only_appear_at_top_level)
	case !b.file.IsExternalModule():
		b.errorOnNode(node, diagnostics.Global_module_exports_may_only_appear_in_module_files)
	case !b.file.IsDeclarationFile:
		b.errorOnNode(node, diagnostics.Global_module_exports_may_only_appear_in_declaration_files)
	default:
		b.declareSymbol(store.GetSymbolTable(&b.bound.GlobalExports), b.file.Symbol(), node, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	}
}

func (b *Binder) bindImportClause(node store.Node) {
	if !node.AsImportClause().Name().IsNil() {
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	}
}

func (b *Binder) bindExportDeclaration(node store.Node) {
	decl := node.AsExportDeclaration()
	exportClause := decl.ExportClause()
	if b.symbolOf(b.container) == nil {
		// Export * in some sort of block construct
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsExportStar, b.getDeclarationName(node))
	} else if exportClause.IsNil() {
		// All export * declarations are collected in an __export symbol
		b.declareSymbol(store.GetExports(b.symbolOf(b.container)), b.symbolOf(b.container), node, ast.SymbolFlagsExportStar, ast.SymbolFlagsNone)
	} else if store.IsNamespaceExport(exportClause) {
		b.declareSymbol(store.GetExports(b.symbolOf(b.container)), b.symbolOf(b.container), exportClause, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	}
}

func (b *Binder) bindExportAssignment(node store.Node) {
	container := b.container
	if b.symbolOf(container) == nil && store.IsExportAssignment(node) {
		// Incorrect export assignment in some sort of block construct
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsValue, b.getDeclarationName(node))
	} else {
		// If there is an `export default x;` alias declaration, can't `export default` anything else.
		// (In contrast, you can still have `export default function f() {}` and `export default interface I {}`.)
		flags := core.IfElse(store.ExpressionIsAlias(node.Expression()), ast.SymbolFlagsAlias, ast.SymbolFlagsProperty)
		symbol := b.declareSymbol(store.GetExports(b.symbolOf(container)), b.symbolOf(container), node, flags, ast.SymbolFlagsAll)
		if node.AsExportAssignment().IsExportEquals() {
			// Ensure export assignments have a ValueDeclaration set.
			SetValueDeclaration(b.s, symbol, node)
		}
	}
}

func (b *Binder) bindJsxAttributes(node store.Node) {
	b.bindAnonymousDeclaration(node, ast.SymbolFlagsObjectLiteral, ast.InternalSymbolNameJSXAttributes)
}

func (b *Binder) bindJsxAttribute(node store.Node, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	b.declareSymbolAndAddToSymbolTable(node, symbolFlags, symbolExcludes)
}

func (b *Binder) setExportContextFlag(node store.Node) {
	// A declaration source file or ambient module declaration that contains no export declarations (but possibly regular
	// declarations with export modifiers) is an export context in which declarations are implicitly exported.
	if node.Flags()&ast.NodeFlagsAmbient != 0 && !b.hasExportDeclarations(node) {
		node.AddFlags(ast.NodeFlagsExportContext)
	} else {
		node.ClearFlags(ast.NodeFlagsExportContext)
	}
}

func (b *Binder) hasExportDeclarations(node store.Node) bool {
	var statements []store.NodeRef
	switch node.Kind() {
	case ast.KindSourceFile:
		statements = node.Statements().Refs()
	case ast.KindModuleDeclaration:
		body := node.Body()
		if !body.IsNil() && store.IsModuleBlock(body) {
			statements = body.Statements().Refs()
		}
	}
	for _, ref := range statements {
		s := b.s.Node(ref)
		if store.IsExportDeclaration(s) || store.IsExportAssignment(s) {
			return true
		}
	}
	return false
}

func (b *Binder) bindFunctionExpression(node store.Node) {
	if !b.file.IsDeclarationFile && node.Flags()&ast.NodeFlagsAmbient == 0 && store.IsAsyncFunction(node) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	setFlowNode(node, b.currentFlow)
	bindingName := ast.InternalSymbolNameFunction
	if store.IsFunctionExpression(node) {
		if name := node.AsFunctionExpression().Name(); !name.IsNil() {
			b.checkStrictModeFunctionName(node)
			bindingName = name.Text()
		}
	}
	b.bindAnonymousDeclaration(node, ast.SymbolFlagsFunction, bindingName)
}

func (b *Binder) bindCallExpression(node store.Node) {
	// We're only inspecting call expressions to detect CommonJS modules, so we can skip
	// this check if we've already seen the module indicator
	if b.bound.CommonJSModuleIndicator == store.NoNodeRef && store.IsRequireCall(node, false /*requireStringLiteralLikeArgument*/) {
		b.setCommonJSModuleIndicator(node)
	}
}

func (b *Binder) setCommonJSModuleIndicator(node store.Node) bool {
	if b.file.ExternalModuleIndicator != store.NoNodeRef && b.file.ExternalModuleIndicator != b.file.Root().Ref() {
		return false
	}
	if b.bound.CommonJSModuleIndicator == store.NoNodeRef {
		b.bound.CommonJSModuleIndicator = node.Ref()
		if b.file.ExternalModuleIndicator == store.NoNodeRef {
			b.bindSourceFileAsExternalModule()
		}
	}
	return true
}

func (b *Binder) bindClassLikeDeclaration(node store.Node) {
	name := node.Name()
	switch node.Kind() {
	case ast.KindClassDeclaration:
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsClass, ast.SymbolFlagsClassExcludes)
	case ast.KindClassExpression:
		nameText := ast.InternalSymbolNameClass
		if !name.IsNil() {
			nameText = name.Text()
		}
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsClass, nameText)
	}
	symbol := b.symbolOf(node)
	// TypeScript 1.0 spec (April 2014): 8.4
	// Every class automatically contains a static property member named 'prototype', the
	// type of which is an instantiation of the class type with type Any supplied as a type
	// argument for each type parameter. It is an error to explicitly declare a static
	// property member with the name 'prototype'.
	//
	// Note: we check for this here because this class may be merging into a module.  The
	// module might have an exported variable called 'prototype'.  We can't allow that as
	// that would clash with the built-in 'prototype' for the class.
	prototypeSymbol := b.newSymbol(ast.SymbolFlagsProperty|ast.SymbolFlagsPrototype, "prototype")
	symbolExport := store.GetExports(symbol)[prototypeSymbol.Name]
	if symbolExport != nil {
		b.errorOnNode(b.s.Node(symbolExport.Declarations[0].Id()), diagnostics.Duplicate_identifier_0, store.SymbolName(b.s, prototypeSymbol))
	}
	store.GetExports(symbol)[prototypeSymbol.Name] = prototypeSymbol
	prototypeSymbol.Parent = symbol
}

func (b *Binder) bindPropertyOrMethodOrAccessor(node store.Node, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	if !b.file.IsDeclarationFile && node.Flags()&ast.NodeFlagsAmbient == 0 && store.IsAsyncFunction(node) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	if b.currentFlow != 0 && store.IsObjectLiteralOrClassExpressionMethodOrAccessor(node) {
		setFlowNode(node, b.currentFlow)
	}
	if store.HasDynamicName(node) {
		b.bindAnonymousDeclaration(node, symbolFlags, ast.InternalSymbolNameComputed)
	} else {
		b.declareSymbolAndAddToSymbolTable(node, symbolFlags, symbolExcludes)
	}
}

func (b *Binder) bindFunctionOrConstructorType(node store.Node) {
	// For a given function symbol "<...>(...) => T" we want to generate a symbol identical
	// to the one we would get for: { <...>(...): T }
	//
	// We do that by making an anonymous type literal symbol, and then setting the function
	// symbol as its sole member. To the rest of the system, this symbol will be indistinguishable
	// from an actual type literal symbol you would have gotten had you used the long form.
	symbol := b.newSymbol(ast.SymbolFlagsSignature, b.getDeclarationName(node))
	b.addDeclarationToSymbol(symbol, node, ast.SymbolFlagsSignature)
	typeLiteralSymbol := b.newSymbol(ast.SymbolFlagsTypeLiteral, ast.InternalSymbolNameType)
	b.addDeclarationToSymbol(typeLiteralSymbol, node, ast.SymbolFlagsTypeLiteral)
	typeLiteralSymbol.Members = make(store.SymbolTable)
	typeLiteralSymbol.Members[symbol.Name] = symbol
}

func (b *Binder) addLateBoundAssignmentDeclarationToSymbol(node store.Node, symbol *store.Symbol) {
	exports := store.GetExports(symbol)
	assignmentSymbol := exports[ast.InternalSymbolNameAssignmentDeclaration]
	if assignmentSymbol == nil {
		assignmentSymbol = b.newSymbol(ast.SymbolFlagsNone, ast.InternalSymbolNameAssignmentDeclaration)
		exports[ast.InternalSymbolNameAssignmentDeclaration] = assignmentSymbol
	}
	assignmentSymbol.Declarations = append(assignmentSymbol.Declarations, node.FileRef())
}

func (b *Binder) bindModuleExportsAssignment(node store.Node) {
	if b.setCommonJSModuleIndicator(node) {
		container := b.file.Root()
		flags := core.IfElse(store.ExpressionIsAlias(node.AsBinaryExpression().Right()), ast.SymbolFlagsAlias, ast.SymbolFlagsProperty)
		symbol := b.declareSymbol(store.GetExports(b.symbolOf(container)), b.symbolOf(container), node, flags, 0)
		SetValueDeclaration(b.s, symbol, node)
	}
}

func (b *Binder) bindExpandoPropertyAssignment(node store.Node) {
	b.expandoAssignments = append(b.expandoAssignments, ExpandoAssignmentInfo{
		node:                node,
		container:           b.container,
		blockScopeContainer: b.blockScopeContainer,
	})
}

func (b *Binder) bindDeferredExpandoAssignments() {
	for _, info := range b.expandoAssignments {
		b.container = info.container
		b.blockScopeContainer = info.blockScopeContainer
		b.bindDeferredExpandoAssignment(info.node)
	}
}

// If the given module symbol has an export= symbol, promote exports with a type or namespace meaning
// from the module symbol onto the export= symbol and, if any such exports exist, mark the export=
// symbol as a namespace module.
func (b *Binder) bindCommonJSTypeExports(moduleSymbol *store.Symbol) {
	moduleExports := moduleSymbol.Exports
	if exportEquals := moduleExports[ast.InternalSymbolNameExportEquals]; exportEquals != nil {
		for _, symbol := range moduleExports {
			if symbol.Name != ast.InternalSymbolNameExportEquals && symbol.Flags&(ast.SymbolFlagsType|ast.SymbolFlagsNamespace) != 0 {
				store.GetExports(exportEquals)[symbol.Name] = symbol
				exportEquals.Flags |= ast.SymbolFlagsNamespaceModule
			}
		}
	}
}

func (b *Binder) bindDeferredExpandoAssignment(node store.Node) {
	parent := getParentOfPropertyAssignment(node)
	symbol := b.lookupEntity(parent, b.blockScopeContainer)
	if symbol == nil {
		symbol = b.lookupEntity(parent, b.container)
	}
	if symbol = b.getInitializerSymbol(symbol); symbol != nil {
		if store.HasDynamicName(node) {
			b.bindAnonymousDeclaration(node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.InternalSymbolNameComputed)
			b.addLateBoundAssignmentDeclarationToSymbol(node, symbol)
		} else {
			// We declare expandos only when there are no non-expando declarations for that name.
			exports := store.GetExports(symbol)
			if existing := exports[b.getDeclarationName(node)]; existing == nil || existing.Flags&ast.SymbolFlagsAssignment != 0 {
				b.declareSymbol(exports, symbol, node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.SymbolFlagsPropertyExcludes)
			}
		}
	}
}

func getParentOfPropertyAssignment(node store.Node) store.Node {
	switch node.Kind() {
	case ast.KindBinaryExpression:
		return node.AsBinaryExpression().Left().Expression()
	case ast.KindCallExpression:
		return node.Arguments().At(0)
	}
	panic("Unhandled case in getParentOfPropertyAssignment")
}

func (b *Binder) bindExportsOrObjectDefineProperty(node store.Node) {
	if b.setCommonJSModuleIndicator(node) {
		container := b.file.Root()
		flags := core.IfElse(store.IsBinaryExpression(node) && store.ExpressionIsAlias(node.AsBinaryExpression().Right()), ast.SymbolFlagsAlias, ast.SymbolFlagsFunctionScopedVariable)
		b.declareSymbol(store.GetExports(b.symbolOf(container)), b.symbolOf(container), node, flags, ast.SymbolFlagsFunctionScopedVariableExcludes)
	}
}

// getInitializerSymbol is a method here: the ValueDeclaration is a Ref that the Store resolves.
func (b *Binder) getInitializerSymbol(symbol *store.Symbol) *store.Symbol {
	if symbol == nil || symbol.ValueDeclaration == (store.Ref{}) {
		return nil
	}
	declaration := b.s.Node(symbol.ValueDeclaration.Id())
	// For an assignment 'fn.xxx = ...', where 'fn' is a previously declared function or a previously
	// declared const variable initialized with a function expression or arrow function, we add expando
	// property declarations to the function's symbol. This also applies to class expressions in JS files,
	// and empty object literals in JS files when the declaration doesn't have a type annotation.
	switch {
	case store.IsFunctionDeclaration(declaration) || store.IsInJSFile(declaration) && store.IsClassDeclaration(declaration):
		return symbol
	case store.IsVariableDeclaration(declaration) &&
		(declaration.Parent().Flags()&ast.NodeFlagsConst != 0 || store.IsInJSFile(declaration)):
		initializer := declaration.Initializer()
		if store.IsExpandoInitializer(declaration, initializer) {
			return b.symbolOf(initializer)
		}
	case store.IsBinaryExpression(declaration) && store.IsInJSFile(declaration):
		initializer := declaration.AsBinaryExpression().Right()
		if store.IsExpandoInitializer(declaration, initializer) {
			return b.symbolOf(initializer)
		}
	}
	return nil
}

func (b *Binder) bindThisPropertyAssignment(node store.Node) {
	if !store.IsInJSFile(node) {
		return
	}
	bin := node.AsBinaryExpression()
	left := bin.Left()
	if store.IsPropertyAccessExpression(left) && store.IsPrivateIdentifier(left.AsPropertyAccessExpression().Name()) ||
		b.thisContainer.IsNil() {
		return
	}
	if classSymbol, symbolTable := b.getThisClassAndSymbolTable(); symbolTable != nil {
		if store.HasDynamicName(node) {
			b.declareSymbolEx(symbolTable, classSymbol, node, ast.SymbolFlagsProperty, ast.SymbolFlagsNone, true /*isReplaceableByMethod*/, true /*isComputedName*/)
			b.addLateBoundAssignmentDeclarationToSymbol(node, classSymbol)
		} else {
			b.declareSymbolEx(symbolTable, classSymbol, node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.SymbolFlagsNone, true /*isReplaceableByMethod*/, false /*isComputedName*/)
		}
	} else if kind := b.thisContainer.Kind(); kind != ast.KindFunctionDeclaration && kind != ast.KindFunctionExpression {
		// !!! constructor functions
		panic("Unhandled case in bindThisPropertyAssignment: " + kind.String())
	}
}

func (b *Binder) getThisClassAndSymbolTable() (classSymbol *store.Symbol, symbolTable store.SymbolTable) {
	if b.thisContainer.IsNil() {
		return nil, nil
	}
	switch b.thisContainer.Kind() {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		// !!! constructor functions
	case ast.KindConstructor, ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindClassStaticBlockDeclaration:
		// this.property assignment in class member -- bind to the containing class
		classSymbol = b.symbolOf(b.thisContainer.Parent())
		if store.IsStatic(b.thisContainer) {
			symbolTable = store.GetExports(classSymbol)
		} else {
			symbolTable = store.GetMembers(classSymbol)
		}
	}
	return classSymbol, symbolTable
}

func (b *Binder) bindEnumDeclaration(node store.Node) {
	if store.IsEnumConst(node) {
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsConstEnum, ast.SymbolFlagsConstEnumExcludes)
	} else {
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsRegularEnum, ast.SymbolFlagsRegularEnumExcludes)
	}
}

func (b *Binder) bindVariableDeclarationOrBindingElement(node store.Node) {
	name := node.Name()
	b.checkStrictModeEvalOrArguments(node, name)
	if !name.IsNil() && !store.IsBindingPattern(name) {
		switch {
		case store.IsVariableDeclarationInitializedToRequire(node):
			b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
		case store.IsBlockOrCatchScoped(node):
			b.bindBlockScopedDeclaration(node, ast.SymbolFlagsBlockScopedVariable, ast.SymbolFlagsBlockScopedVariableExcludes)
		case store.IsPartOfParameterDeclaration(node):
			// It is safe to walk up parent chain to find whether the node is a destructuring parameter declaration
			// because its parent chain has already been set up, since parents are set before descending into children.
			//
			// If node is a binding element in parameter declaration, we need to use ParameterExcludes.
			// Using ParameterExcludes flag allows the compiler to report an error on duplicate identifiers in Parameter Declaration
			// For example:
			//      function foo([a,a]) {} // Duplicate Identifier error
			//      function bar(a,a) {}   // Duplicate Identifier error, parameter declaration in this case is handled in bindParameter
			//                             // which correctly set excluded symbols
			b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsParameterExcludes)
		default:
			b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsFunctionScopedVariableExcludes)
		}
	}
}

func (b *Binder) bindParameter(node store.Node) {
	decl := node.AsParameterDeclaration()
	name := decl.Name()
	parent := node.Parent()
	if node.Flags()&ast.NodeFlagsAmbient == 0 {
		// It is a SyntaxError if the identifier eval or arguments appears within a FormalParameterList of a
		// strict mode FunctionLikeDeclaration or FunctionExpression(13.1)
		b.checkStrictModeEvalOrArguments(node, name)
	}
	if store.IsBindingPattern(name) {
		index := slices.Index(parent.Parameters().Refs(), node.Ref())
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsFunctionScopedVariable, "__"+strconv.Itoa(index))
	} else {
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsParameterExcludes)
	}
	// If this is a property-parameter, then also declare the property symbol into the
	// containing class.
	if store.IsParameterPropertyDeclaration(node, parent) {
		classDeclaration := parent.Parent()
		flags := ast.SymbolFlagsProperty | core.IfElse(!decl.QuestionToken().IsNil(), ast.SymbolFlagsOptional, ast.SymbolFlagsNone)
		b.declareSymbol(store.GetMembers(b.symbolOf(classDeclaration)), b.symbolOf(classDeclaration), node, flags, ast.SymbolFlagsPropertyExcludes)
	}
}

func (b *Binder) bindFunctionDeclaration(node store.Node) {
	if !b.file.IsDeclarationFile && node.Flags()&ast.NodeFlagsAmbient == 0 && store.IsAsyncFunction(node) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	b.checkStrictModeFunctionName(node)
	b.bindBlockScopedDeclaration(node, ast.SymbolFlagsFunction, ast.SymbolFlagsFunctionExcludes)
}

func (b *Binder) getInferTypeContainer(node store.Node) store.Node {
	extendsType := store.FindAncestor(node, func(n store.Node) bool {
		parent := n.Parent()
		return !parent.IsNil() && store.IsConditionalTypeNode(parent) && parent.AsConditionalTypeNode().ExtendsType() == n
	})
	if !extendsType.IsNil() {
		return extendsType.Parent()
	}
	return b.nilNode()
}

func (b *Binder) bindAnonymousDeclaration(node store.Node, symbolFlags ast.SymbolFlags, name string) {
	symbol := b.newSymbol(symbolFlags, name)
	if symbolFlags&(ast.SymbolFlagsEnumMember|ast.SymbolFlagsClassMember) != 0 {
		symbol.Parent = b.symbolOf(b.container)
	}
	b.addDeclarationToSymbol(symbol, node, symbolFlags)
}

func (b *Binder) bindBlockScopedDeclaration(node store.Node, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	switch b.blockScopeContainer.Kind() {
	case ast.KindModuleDeclaration:
		b.declareModuleMember(node, symbolFlags, symbolExcludes)
	case ast.KindSourceFile:
		if b.file.IsExternalOrCommonJSModule() {
			b.declareModuleMember(node, symbolFlags, symbolExcludes)
			break
		}
		fallthrough
	default:
		b.declareSymbol(store.GetLocals(b.bound, b.blockScopeContainer), nil /*parent*/, node, symbolFlags, symbolExcludes)
	}
}

func (b *Binder) bindTypeParameter(node store.Node) {
	if node.Parent().Kind() == ast.KindInferType {
		container := b.getInferTypeContainer(node.Parent())
		if !container.IsNil() {
			b.declareSymbol(store.GetLocals(b.bound, container), nil /*parent*/, node, ast.SymbolFlagsTypeParameter, ast.SymbolFlagsTypeParameterExcludes)
		} else {
			b.bindAnonymousDeclaration(node, ast.SymbolFlagsTypeParameter, b.getDeclarationName(node))
		}
	} else {
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsTypeParameter, ast.SymbolFlagsTypeParameterExcludes)
	}
}

func (b *Binder) lookupEntity(node store.Node, container store.Node) *store.Symbol {
	if store.IsIdentifier(node) {
		return b.lookupName(node.Text(), container)
	}
	expression := node.Expression()
	if expression.Kind() == ast.KindThisKeyword {
		if _, symbolTable := b.getThisClassAndSymbolTable(); symbolTable != nil {
			if name := store.GetElementOrPropertyAccessName(node); !name.IsNil() {
				return symbolTable[name.Text()]
			}
		}
		return nil
	}
	if symbol := b.getInitializerSymbol(b.lookupEntity(expression, container)); symbol != nil && symbol.Exports != nil {
		if name := store.GetElementOrPropertyAccessName(node); !name.IsNil() {
			return symbol.Exports[name.Text()]
		}
	}
	return nil
}

func (b *Binder) lookupName(name string, container store.Node) *store.Symbol {
	if localsSlot, ok := container.LocalsSlot(); ok {
		if local := b.bound.Locals(localsSlot)[name]; local != nil {
			return core.OrElse(local.ExportSymbol, local)
		}
	}
	if symbol := b.symbolOf(container); symbol != nil {
		return symbol.Exports[name]
	}
	return nil
}

// The binder visits every node in the syntax tree so it is a convenient place to perform a single localized
// check for reserved words used as identifiers in strict mode code, as well as `yield` or `await` in
// [Yield] or [Await] contexts, respectively.
func (b *Binder) checkContextualIdentifier(node store.Node) {
	// Report error only if there are no parse errors in file
	if len(b.file.Diagnostics()) == 0 && node.Flags()&ast.NodeFlagsAmbient == 0 && node.Flags()&ast.NodeFlagsJSDoc == 0 && !store.IsIdentifierName(node) {
		// strict mode identifiers
		originalKeywordKind := scanner.GetIdentifierToken(node.Text())
		if originalKeywordKind == ast.KindIdentifier {
			return
		}
		if originalKeywordKind >= ast.KindFirstFutureReservedWord && originalKeywordKind <= ast.KindLastFutureReservedWord {
			b.errorOnNode(node, b.getStrictModeIdentifierMessage(node), declarationNameToString(b.file, node))
		} else if originalKeywordKind == ast.KindAwaitKeyword {
			if b.file.IsExternalModule() && store.IsInTopLevelContext(node) {
				b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_at_the_top_level_of_a_module, declarationNameToString(b.file, node))
			} else if node.Flags()&ast.NodeFlagsAwaitContext != 0 {
				b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, declarationNameToString(b.file, node))
			}
		} else if originalKeywordKind == ast.KindYieldKeyword && node.Flags()&ast.NodeFlagsYieldContext != 0 {
			b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, declarationNameToString(b.file, node))
		}
	}
}

func (b *Binder) checkPrivateIdentifier(node store.Node) {
	if node.Text() == "#constructor" {
		// Report error only if there are no parse errors in file
		if len(b.file.Diagnostics()) == 0 {
			b.errorOnNode(node, diagnostics.X_constructor_is_a_reserved_word, declarationNameToString(b.file, node))
		}
	}
}

func (b *Binder) getStrictModeIdentifierMessage(node store.Node) *diagnostics.Message {
	// Provide specialized messages to help the user understand why we think they're in
	// strict mode.
	if !store.GetContainingClass(node).IsNil() {
		return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode_Class_definitions_are_automatically_in_strict_mode
	}
	if b.file.ExternalModuleIndicator != store.NoNodeRef {
		return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode
}

// Should be called only on prologue directives (store.IsPrologueDirective(node) should be true)
func isUseStrictPrologueDirective(sourceFile *store.File, node store.Node) bool {
	nodeText := getSourceTextOfNodeFromSourceFile(sourceFile, node.Expression(), false /*includeTrivia*/)
	// Note: the node text must be exactly "use strict" or 'use strict'.  It is not ok for the
	// string to contain unicode escapes (as per ES5).
	return nodeText == "\"use strict\"" || nodeText == "'use strict'"
}

func FindUseStrictPrologue(sourceFile *store.File, statements []store.NodeRef) store.Node {
	for _, ref := range statements {
		statement := sourceFile.Store.Node(ref)
		if store.IsPrologueDirective(statement) {
			if isUseStrictPrologueDirective(sourceFile, statement) {
				return statement
			}
		} else {
			return sourceFile.Store.Node(store.NoNodeRef)
		}
	}

	return sourceFile.Store.Node(store.NoNodeRef)
}

func (b *Binder) checkStrictModeFunctionName(node store.Node) {
	if node.Flags()&ast.NodeFlagsAmbient == 0 {
		// It is a SyntaxError if the identifier eval or arguments appears within a FormalParameterList of a strict mode FunctionDeclaration or FunctionExpression (13.1))
		b.checkStrictModeEvalOrArguments(node, node.Name())
	}
}

func (b *Binder) getStrictModeBlockScopeFunctionDeclarationMessage(node store.Node) *diagnostics.Message {
	// Provide specialized messages to help the user understand why we think they're in strict mode.
	if !store.GetContainingClass(node).IsNil() {
		return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5_Class_definitions_are_automatically_in_strict_mode
	}
	if b.file.ExternalModuleIndicator != store.NoNodeRef {
		return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5
}

func (b *Binder) checkStrictModeBinaryExpression(node store.Node) {
	expr := node.AsBinaryExpression()
	left := expr.Left()
	if store.IsLeftHandSideExpression(left) && ast.IsAssignmentOperator(expr.OperatorToken().Kind()) {
		// ECMA 262 (Annex C) The identifier eval or arguments may not appear as the LeftHandSideExpression of an
		// Assignment operator(11.13) or of a PostfixExpression(11.3)
		b.checkStrictModeEvalOrArguments(node, left)
	}
}

func (b *Binder) checkStrictModeCatchClause(node store.Node) {
	// It is a SyntaxError if a TryStatement with a Catch occurs within strict code and the Identifier of the
	// Catch production is eval or arguments
	clause := node.AsCatchClause()
	variableDeclaration := clause.VariableDeclaration()
	if !variableDeclaration.IsNil() {
		b.checkStrictModeEvalOrArguments(node, variableDeclaration.AsVariableDeclaration().Name())
	}
}

func (b *Binder) checkStrictModeDeleteExpression(node store.Node) {
	// Grammar checking
	expr := node.AsDeleteExpression()
	expression := expr.Expression()
	if expression.Kind() == ast.KindIdentifier {
		// When a delete operator occurs within strict mode code, a SyntaxError is thrown if its
		// UnaryExpression is a direct reference to a variable, function argument, or function name
		b.errorOnNode(expression, diagnostics.X_delete_cannot_be_called_on_an_identifier_in_strict_mode)
	}
}

func (b *Binder) checkStrictModePostfixUnaryExpression(node store.Node) {
	// Grammar checking
	// The identifier eval or arguments may not appear as the LeftHandSideExpression of an
	// Assignment operator(11.13) or of a PostfixExpression(11.3) or as the UnaryExpression
	// operated upon by a Prefix Increment(11.4.4) or a Prefix Decrement(11.4.5) operator.
	b.checkStrictModeEvalOrArguments(node, node.AsPostfixUnaryExpression().Operand())
}

func (b *Binder) checkStrictModePrefixUnaryExpression(node store.Node) {
	// Grammar checking
	expr := node.AsPrefixUnaryExpression()
	operator := expr.Operator()
	if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
		b.checkStrictModeEvalOrArguments(node, expr.Operand())
	}
}

func (b *Binder) checkStrictModeWithStatement(node store.Node) {
	// Grammar checking for withStatement
	b.errorOnFirstToken(node, diagnostics.X_with_statements_are_not_allowed_in_strict_mode)
}

func (b *Binder) checkStrictModeLabeledStatement(node store.Node) {
	// Grammar checking for labeledStatement
	data := node.AsLabeledStatement()
	statement := data.Statement()
	if store.IsDeclarationStatement(statement) || store.IsVariableStatement(statement) {
		b.errorOnFirstToken(data.Label(), diagnostics.A_label_is_not_allowed_here)
	}
}

func isEvalOrArgumentsIdentifier(node store.Node) bool {
	if store.IsIdentifier(node) {
		text := node.Text()
		return text == "eval" || text == "arguments"
	}
	return false
}

func (b *Binder) checkStrictModeEvalOrArguments(contextNode store.Node, name store.Node) {
	if !name.IsNil() && isEvalOrArgumentsIdentifier(name) {
		// We check first if the name is inside class declaration or class expression; if so give explicit message
		// otherwise report generic error message.
		b.errorOnNode(name, b.getStrictModeEvalOrArgumentsMessage(contextNode), name.Text())
	}
}

func (b *Binder) getStrictModeEvalOrArgumentsMessage(node store.Node) *diagnostics.Message {
	// Provide specialized messages to help the user understand why we think they're in strict mode
	if !store.GetContainingClass(node).IsNil() {
		return diagnostics.Code_contained_in_a_class_is_evaluated_in_JavaScript_s_strict_mode_which_does_not_allow_this_use_of_0_For_more_information_see_https_Colon_Slash_Slashdeveloper_mozilla_org_Slashen_US_Slashdocs_SlashWeb_SlashJavaScript_SlashReference_SlashStrict_mode
	}
	if b.file.ExternalModuleIndicator != store.NoNodeRef {
		return diagnostics.Invalid_use_of_0_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Invalid_use_of_0_in_strict_mode
}

// All container nodes are kept on a linked list in declaration order. This list is used by
// the getLocalNameOfContainer function in the type checker to validate that the local name
// used for a container is unique.
func (b *Binder) bindContainer(node store.Node, containerFlags ContainerFlags) {
	// Before we recurse into a node's children, we first save the existing parent, container
	// and block-container.  Then after we pop out of processing the children, we restore
	// these saved values.
	saveContainer := b.container
	saveThisContainer := b.thisContainer
	savedBlockScopeContainer := b.blockScopeContainer
	kind := node.Kind()
	// Depending on what kind of node this is, we may have to adjust the current container
	// and block-container.   If the current node is a container, then it is automatically
	// considered the current block-container as well.  Also, for containers that we know
	// may contain locals, we eagerly initialize the .locals field. We do this because
	// it's highly likely that the .locals will be needed to place some child in (for example,
	// a parameter, or variable declaration).
	//
	// However, we do not proactively create the .locals for block-containers because it's
	// totally normal and common for block-containers to never actually have a block-scoped
	// variable in them.  We don't want to end up allocating an object for every 'block' we
	// run into when most of them won't be necessary.
	//
	// Finally, if this is a block-container, then we clear out any existing .locals object
	// it may contain within it.  This happens in incremental scenarios.  Because we can be
	// reusing a node from a previous compilation, that node may have had 'locals' created
	// for it.  We must clear this so we don't accidentally move any stale data forward from
	// a previous compilation.
	if containerFlags&ContainerFlagsIsContainer != 0 {
		b.container = node
		b.blockScopeContainer = node
		if containerFlags&ContainerFlagsHasLocals != 0 {
			// localsContainer := node
			// localsContainer.LocalsContainerData().locals = make(SymbolTable)
			b.addToContainerChain(node)
		}
	} else if containerFlags&ContainerFlagsIsBlockScopedContainer != 0 {
		b.blockScopeContainer = node
		b.addToContainerChain(node)
	}
	if containerFlags&ContainerFlagsIsThisContainer != 0 {
		b.thisContainer = node
	}
	if containerFlags&ContainerFlagsIsControlFlowContainer != 0 {
		saveCurrentFlow := b.currentFlow
		saveBreakTarget := b.currentBreakTarget
		saveContinueTarget := b.currentContinueTarget
		saveReturnTarget := b.currentReturnTarget
		saveExceptionTarget := b.currentExceptionTarget
		saveActiveLabelList := b.activeLabelList
		saveHasExplicitReturn := b.hasExplicitReturn
		saveSeenThisKeyword := b.seenThisKeyword
		isImmediatelyInvoked := (containerFlags&ContainerFlagsIsFunctionExpression != 0 &&
			!store.HasSyntacticModifier(node, ast.ModifierFlagsAsync) &&
			!isGeneratorFunctionExpression(node) &&
			!store.GetImmediatelyInvokedFunctionExpression(node).IsNil()) || kind == ast.KindClassStaticBlockDeclaration
		// A non-async, non-generator IIFE is considered part of the containing control flow. Return statements behave
		// similarly to break statements that exit to a label just past the statement body.
		if !isImmediatelyInvoked {
			flowStart := b.newFlowNode(ast.FlowFlagsStart)
			b.currentFlow = flowStart
			if containerFlags&(ContainerFlagsIsFunctionExpression|ContainerFlagsIsObjectLiteralOrClassExpressionMethodOrAccessor) != 0 {
				b.bound.Flow(flowStart).Node = node.Ref()
			}
		}
		// We create a return control flow graph for IIFEs and constructors. For constructors
		// we use the return control flow graph in strict property initialization checks.
		if isImmediatelyInvoked || kind == ast.KindConstructor {
			b.currentReturnTarget = b.newFlowNode(ast.FlowFlagsBranchLabel)
		} else {
			b.currentReturnTarget = 0
		}
		b.currentExceptionTarget = 0
		b.currentBreakTarget = 0
		b.currentContinueTarget = 0
		b.activeLabelList = nil
		b.hasExplicitReturn = false
		b.seenThisKeyword = false
		b.bindChildren(node)
		// Reset flags (for incremental scenarios)
		node.ClearFlags(ast.NodeFlagsReachabilityAndEmitFlags | ast.NodeFlagsContainsThis)
		if b.bound.Flow(b.currentFlow).Flags&ast.FlowFlagsUnreachable == 0 && containerFlags&ContainerFlagsIsFunctionLike != 0 {
			// The Body role is nil for a kind without BodyBase (the signatures), like BodyData is.
			body := node.Body()
			if store.NodeIsPresent(body) {
				node.AddFlags(ast.NodeFlagsHasImplicitReturn)
				if b.hasExplicitReturn {
					node.AddFlags(ast.NodeFlagsHasExplicitReturn)
				}
				node.SetEndFlowNode(b.currentFlow)
			}
		}
		if b.seenThisKeyword {
			node.AddFlags(ast.NodeFlagsContainsThis)
		}
		if kind == ast.KindSourceFile {
			node.AddFlags(b.emitFlags)
		}
		if b.currentReturnTarget != 0 {
			b.addAntecedent(b.currentReturnTarget, b.currentFlow)
			b.currentFlow = b.finishFlowLabel(b.currentReturnTarget)
			if kind == ast.KindConstructor || kind == ast.KindClassStaticBlockDeclaration {
				setReturnFlowNode(node, b.currentFlow)
			}
		}
		if !isImmediatelyInvoked {
			b.currentFlow = saveCurrentFlow
		}
		b.currentBreakTarget = saveBreakTarget
		b.currentContinueTarget = saveContinueTarget
		b.currentReturnTarget = saveReturnTarget
		b.currentExceptionTarget = saveExceptionTarget
		b.activeLabelList = saveActiveLabelList
		b.hasExplicitReturn = saveHasExplicitReturn
		if containerFlags&ContainerFlagsPropagatesThisKeyword != 0 {
			b.seenThisKeyword = saveSeenThisKeyword || b.seenThisKeyword
		} else {
			b.seenThisKeyword = saveSeenThisKeyword
		}
	} else if containerFlags&ContainerFlagsIsInterface != 0 {
		saveSeenThisKeyword := b.seenThisKeyword
		b.seenThisKeyword = false
		b.bindChildren(node)
		// ContainsThis cannot overlap with HasExtendedUnicodeEscape on Identifier
		if b.seenThisKeyword {
			node.AddFlags(ast.NodeFlagsContainsThis)
		} else {
			node.ClearFlags(ast.NodeFlagsContainsThis)
		}
		b.seenThisKeyword = saveSeenThisKeyword
	} else {
		b.bindChildren(node)
	}
	if kind == ast.KindSourceFile && store.IsInJSFile(node) {
		// Binding of top-level JSTypeAliasDeclaration nodes is deferred to ensure CommonJS module
		// indicators, if any, are processed first.
		for _, ref := range node.Statements().Refs() {
			statement := b.s.Node(ref)
			if store.IsJSTypeAliasDeclaration(statement) {
				b.bindBlockScopedDeclaration(statement, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
			}
		}
		if b.bound.CommonJSModuleIndicator != store.NoNodeRef {
			b.declareCommonJSVariable("module")
			b.declareCommonJSVariable("exports")
		}
	}
	if kind == ast.KindSourceFile && b.file.IsExternalOrCommonJSModule() || store.IsAmbientModule(node) {
		b.bindCommonJSTypeExports(b.symbolOf(node))
	}
	b.container = saveContainer
	b.thisContainer = saveThisContainer
	b.blockScopeContainer = savedBlockScopeContainer
}

func (b *Binder) declareCommonJSVariable(name string) {
	locals := store.GetLocals(b.bound, b.file.Root())
	if locals[name] == nil {
		symbol := b.newSymbol(ast.SymbolFlagsFunctionScopedVariable|ast.SymbolFlagsModuleExports, name)
		symbol.Declarations = b.newSingleDeclaration(b.file.Root().FileRef())
		symbol.ValueDeclaration = symbol.Declarations[0]
		if name == "module" {
			exportsProperty := b.newSymbol(ast.SymbolFlagsModuleExports|ast.SymbolFlagsProperty, "exports")
			exportsProperty.Declarations = symbol.Declarations
			exportsProperty.ValueDeclaration = symbol.ValueDeclaration
			exportsProperty.Parent = symbol
			symbol.Members = make(store.SymbolTable, 1)
			symbol.Members["exports"] = exportsProperty
		}
		locals[name] = symbol
	}
}

func (b *Binder) bindChildren(node store.Node) {
	saveInAssignmentPattern := b.inAssignmentPattern
	// Most nodes aren't valid in an assignment pattern, so we clear the value here
	// and set it before we descend into nodes that could actually be part of an assignment pattern.
	b.inAssignmentPattern = false

	if b.currentFlow == b.unreachableFlow {
		// Every header has a flow word; a kind without FlowNodeBase has 0 there already.
		node.SetFlow(0)
		if store.IsPotentiallyExecutableNode(node) {
			node.AddFlags(ast.NodeFlagsUnreachable)
		}
		b.bindEachChild(node)
		b.inAssignmentPattern = saveInAssignmentPattern
		return
	}

	kind := node.Kind()
	if ast.KindFirstStatement <= kind && kind <= ast.KindLastStatement {
		// Every statement kind embeds FlowNodeBase.
		node.SetFlow(b.currentFlow)
	}

	switch kind {
	case ast.KindWhileStatement:
		b.bindWhileStatement(node)
	case ast.KindDoStatement:
		b.bindDoStatement(node)
	case ast.KindForStatement:
		b.bindForStatement(node)
	case ast.KindForInStatement, ast.KindForOfStatement:
		b.bindForInOrForOfStatement(node)
	case ast.KindIfStatement:
		b.bindIfStatement(node)
	case ast.KindReturnStatement:
		b.bindReturnStatement(node)
	case ast.KindThrowStatement:
		b.bindThrowStatement(node)
	case ast.KindBreakStatement:
		b.bindBreakStatement(node)
	case ast.KindContinueStatement:
		b.bindContinueStatement(node)
	case ast.KindTryStatement:
		b.bindTryStatement(node)
	case ast.KindSwitchStatement:
		b.bindSwitchStatement(node)
	case ast.KindCaseBlock:
		b.bindCaseBlock(node)
	case ast.KindCaseClause, ast.KindDefaultClause:
		b.bindCaseOrDefaultClause(node)
	case ast.KindExpressionStatement:
		b.bindExpressionStatement(node)
	case ast.KindLabeledStatement:
		b.bindLabeledStatement(node)
	case ast.KindPrefixUnaryExpression:
		b.bindPrefixUnaryExpressionFlow(node)
	case ast.KindPostfixUnaryExpression:
		b.bindPostfixUnaryExpressionFlow(node)
	case ast.KindBinaryExpression:
		if store.IsDestructuringAssignment(node) {
			// Carry over whether we are in an assignment pattern to
			// binary expressions that could actually be an initializer
			b.inAssignmentPattern = saveInAssignmentPattern
			b.bindDestructuringAssignmentFlow(node)
			return
		}
		b.bindBinaryExpressionFlow(node)
	case ast.KindDeleteExpression:
		b.bindDeleteExpressionFlow(node)
	case ast.KindConditionalExpression:
		b.bindConditionalExpressionFlow(node)
	case ast.KindVariableDeclaration:
		b.bindVariableDeclarationFlow(node)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		b.bindAccessExpressionFlow(node)
	case ast.KindCallExpression:
		b.bindCallExpressionFlow(node)
	case ast.KindNonNullExpression:
		b.bindNonNullExpressionFlow(node)
	case ast.KindSourceFile:
		sourceFile := node.AsSourceFile()
		b.bindEachStatementFunctionsFirst(sourceFile.Statements())
		b.bind(sourceFile.EndOfFileToken())
	case ast.KindBlock, ast.KindModuleBlock:
		b.bindEachStatementFunctionsFirst(node.Statements())
	case ast.KindBindingElement:
		b.bindBindingElementFlow(node)
	case ast.KindParameter:
		b.bindParameterFlow(node)
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression, ast.KindPropertyAssignment, ast.KindSpreadElement:
		b.inAssignmentPattern = saveInAssignmentPattern
		b.bindEachChild(node)
	default:
		b.bindEachChild(node)
	}
	b.inAssignmentPattern = saveInAssignmentPattern
}

func (b *Binder) bindEachChild(node store.Node) {
	node.ForEachChild(b.bindFunc)
}

func (b *Binder) bindEach(nodes []store.NodeRef) {
	for _, node := range nodes {
		b.bind(b.s.Node(node))
	}
}

func (b *Binder) bindNodeList(nodeList store.List) {
	if !nodeList.IsNil() {
		b.bindEach(nodeList.Refs())
	}
}

func (b *Binder) bindModifiers(modifiers store.List) {
	if !modifiers.IsNil() {
		b.bindEach(modifiers.Refs())
	}
}

func (b *Binder) bindEachStatementFunctionsFirst(statements store.List) {
	for _, ref := range statements.Refs() {
		node := b.s.Node(ref)
		if node.Kind() == ast.KindFunctionDeclaration {
			b.bind(node)
		}
	}
	for _, ref := range statements.Refs() {
		node := b.s.Node(ref)
		if node.Kind() != ast.KindFunctionDeclaration {
			b.bind(node)
		}
	}
}

func (b *Binder) setContinueTarget(node store.Node, target store.FlowRef) store.FlowRef {
	label := b.activeLabelList
	for parent := node.Parent(); label != nil && parent.Kind() == ast.KindLabeledStatement; parent = parent.Parent() {
		label.continueTarget = target
		label = label.next
	}
	return target
}

func (b *Binder) doWithConditionalBranches(action func(b *Binder, value store.Node) bool, value store.Node, trueTarget store.FlowRef, falseTarget store.FlowRef) {
	savedTrueTarget := b.currentTrueTarget
	savedFalseTarget := b.currentFalseTarget
	b.currentTrueTarget = trueTarget
	b.currentFalseTarget = falseTarget
	action(b, value)
	b.currentTrueTarget = savedTrueTarget
	b.currentFalseTarget = savedFalseTarget
}

func (b *Binder) bindCondition(node store.Node, trueTarget store.FlowRef, falseTarget store.FlowRef) {
	b.doWithConditionalBranches((*Binder).bind, node, trueTarget, falseTarget)
	if node.IsNil() || !isLogicalAssignmentExpression(node) && !store.IsLogicalExpression(node) && !(store.IsOptionalChain(node) && store.IsOutermostOptionalChain(node)) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}

func (b *Binder) bindIterativeStatement(node store.Node, breakTarget store.FlowRef, continueTarget store.FlowRef) {
	saveBreakTarget := b.currentBreakTarget
	saveContinueTarget := b.currentContinueTarget
	b.currentBreakTarget = breakTarget
	b.currentContinueTarget = continueTarget
	b.bind(node)
	b.currentBreakTarget = saveBreakTarget
	b.currentContinueTarget = saveContinueTarget
}

func isLogicalAssignmentExpression(node store.Node) bool {
	return store.IsLogicalOrCoalescingAssignmentExpression(store.SkipParentheses(node))
}

func (b *Binder) bindAssignmentTargetFlow(node store.Node) {
	switch node.Kind() {
	case ast.KindArrayLiteralExpression:
		for _, ref := range node.Elements().Refs() {
			e := b.s.Node(ref)
			if e.Kind() == ast.KindSpreadElement {
				b.bindAssignmentTargetFlow(e.Expression())
			} else {
				b.bindDestructuringTargetFlow(e)
			}
		}
	case ast.KindObjectLiteralExpression:
		for _, ref := range node.Properties().Refs() {
			p := b.s.Node(ref)
			switch p.Kind() {
			case ast.KindPropertyAssignment:
				b.bindDestructuringTargetFlow(p.Initializer())
			case ast.KindShorthandPropertyAssignment:
				b.bindAssignmentTargetFlow(p.AsShorthandPropertyAssignment().Name())
			case ast.KindSpreadAssignment:
				b.bindAssignmentTargetFlow(p.Expression())
			}
		}
	default:
		if isNarrowableReference(node) {
			b.currentFlow = b.createFlowMutation(ast.FlowFlagsAssignment, b.currentFlow, node)
		}
	}
}

func (b *Binder) bindDestructuringTargetFlow(node store.Node) {
	if store.IsBinaryExpression(node) && node.AsBinaryExpression().OperatorToken().Kind() == ast.KindEqualsToken {
		b.bindAssignmentTargetFlow(node.AsBinaryExpression().Left())
	} else {
		b.bindAssignmentTargetFlow(node)
	}
}

func (b *Binder) bindWhileStatement(node store.Node) {
	stmt := node.AsWhileStatement()
	preWhileLabel := b.setContinueTarget(node, b.createLoopLabel())
	preBodyLabel := b.createBranchLabel()
	postWhileLabel := b.createBranchLabel()
	b.addAntecedent(preWhileLabel, b.currentFlow)
	b.currentFlow = preWhileLabel
	b.bindCondition(stmt.Expression(), preBodyLabel, postWhileLabel)
	b.currentFlow = b.finishFlowLabel(preBodyLabel)
	b.bindIterativeStatement(stmt.Statement(), postWhileLabel, preWhileLabel)
	b.addAntecedent(preWhileLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postWhileLabel)
}

func (b *Binder) bindDoStatement(node store.Node) {
	stmt := node.AsDoStatement()
	preDoLabel := b.createLoopLabel()
	preConditionLabel := b.setContinueTarget(node, b.createBranchLabel())
	postDoLabel := b.createBranchLabel()
	b.addAntecedent(preDoLabel, b.currentFlow)
	b.currentFlow = preDoLabel
	b.bindIterativeStatement(stmt.Statement(), postDoLabel, preConditionLabel)
	b.addAntecedent(preConditionLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(preConditionLabel)
	b.bindCondition(stmt.Expression(), preDoLabel, postDoLabel)
	b.currentFlow = b.finishFlowLabel(postDoLabel)
}

func (b *Binder) bindForStatement(node store.Node) {
	stmt := node.AsForStatement()
	b.bind(stmt.Initializer())
	if b.currentFlow == b.unreachableFlow {
		// Unlike while/do, the for-loop initializer is bound inside this function before the loop's
		// flow graph is constructed. If it makes flow unreachable (e.g. a throwing IIFE), addAntecedent
		// will filter out the unreachable entry to preLoopLabel, leaving only the back-edge from the
		// incrementor. This creates a cycle with no exit that crashes isReachableFlowNodeWorker.
		// Bail out early and just bind the remaining children with unreachable flow.
		b.bind(stmt.Condition())
		b.bind(stmt.Statement())
		b.bind(stmt.Incrementor())
		return
	}
	preLoopLabel := b.setContinueTarget(node, b.createLoopLabel())
	preBodyLabel := b.createBranchLabel()
	preIncrementorLabel := b.createBranchLabel()
	postLoopLabel := b.createBranchLabel()
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = preLoopLabel
	b.bindCondition(stmt.Condition(), preBodyLabel, postLoopLabel)
	b.currentFlow = b.finishFlowLabel(preBodyLabel)
	b.bindIterativeStatement(stmt.Statement(), postLoopLabel, preIncrementorLabel)
	b.addAntecedent(preIncrementorLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(preIncrementorLabel)
	b.bind(stmt.Incrementor())
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postLoopLabel)
}

func (b *Binder) bindForInOrForOfStatement(node store.Node) {
	stmt := node.AsForInOrOfStatement()
	b.bind(stmt.Expression())
	if b.currentFlow == b.unreachableFlow {
		// Like the for-loop initializer, the for-in/for-of expression is bound before the loop's
		// flow graph is constructed. If it makes flow unreachable (e.g. a throwing IIFE), addAntecedent
		// will filter out the unreachable entry to preLoopLabel, leaving only the back-edge from the
		// loop body. This creates a cycle with no exit that crashes isReachableFlowNodeWorker.
		// Bail out early and just bind the remaining children with unreachable flow.
		b.bind(stmt.Initializer())
		b.bind(stmt.Statement())
		return
	}
	preLoopLabel := b.setContinueTarget(node, b.createLoopLabel())
	postLoopLabel := b.createBranchLabel()
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = preLoopLabel
	if node.Kind() == ast.KindForOfStatement {
		b.bind(stmt.AwaitModifier())
	}
	b.addAntecedent(postLoopLabel, b.currentFlow)
	initializer := stmt.Initializer()
	b.bind(initializer)
	if initializer.Kind() != ast.KindVariableDeclarationList {
		b.bindAssignmentTargetFlow(initializer)
	}
	b.bindIterativeStatement(stmt.Statement(), postLoopLabel, preLoopLabel)
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postLoopLabel)
}

func (b *Binder) bindIfStatement(node store.Node) {
	stmt := node.AsIfStatement()
	thenLabel := b.createBranchLabel()
	elseLabel := b.createBranchLabel()
	postIfLabel := b.createBranchLabel()
	b.bindCondition(stmt.Expression(), thenLabel, elseLabel)
	b.currentFlow = b.finishFlowLabel(thenLabel)
	b.bind(stmt.ThenStatement())
	b.addAntecedent(postIfLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(elseLabel)
	b.bind(stmt.ElseStatement())
	b.addAntecedent(postIfLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postIfLabel)
}

func (b *Binder) bindReturnStatement(node store.Node) {
	b.bind(node.Expression())
	if b.currentReturnTarget != 0 {
		b.addAntecedent(b.currentReturnTarget, b.currentFlow)
	}
	b.currentFlow = b.unreachableFlow
	b.hasExplicitReturn = true
	b.hasFlowEffects = true
}

func (b *Binder) bindThrowStatement(node store.Node) {
	b.bind(node.Expression())
	b.currentFlow = b.unreachableFlow
	b.hasFlowEffects = true
}

func (b *Binder) bindBreakStatement(node store.Node) {
	b.bindBreakOrContinueStatement(node.Label(), b.currentBreakTarget, (*ActiveLabel).BreakTarget)
}

func (b *Binder) bindContinueStatement(node store.Node) {
	b.bindBreakOrContinueStatement(node.Label(), b.currentContinueTarget, (*ActiveLabel).ContinueTarget)
}

func (b *Binder) bindBreakOrContinueStatement(label store.Node, currentTarget store.FlowRef, getTarget func(*ActiveLabel) store.FlowRef) {
	b.bind(label)
	if !label.IsNil() {
		activeLabel := b.findActiveLabel(label.Text())
		if activeLabel != nil {
			activeLabel.referenced = true
			b.bindBreakOrContinueFlow(getTarget(activeLabel))
		}
	} else {
		b.bindBreakOrContinueFlow(currentTarget)
	}
}

func (b *Binder) findActiveLabel(name string) *ActiveLabel {
	for label := b.activeLabelList; label != nil; label = label.next {
		if label.name == name {
			return label
		}
	}
	return nil
}

func (b *Binder) bindBreakOrContinueFlow(flowLabel store.FlowRef) {
	if flowLabel != 0 {
		b.addAntecedent(flowLabel, b.currentFlow)
		b.currentFlow = b.unreachableFlow
		b.hasFlowEffects = true
	}
}

func (b *Binder) bindTryStatement(node store.Node) {
	// We conservatively assume that *any* code in the try block can cause an exception, but we only need
	// to track code that causes mutations (because only mutations widen the possible control flow type of
	// a variable). The exceptionLabel is the target label for control flows that result from exceptions.
	// We add all mutation flow nodes as antecedents of this label such that we can analyze them as possible
	// antecedents of the start of catch or finally blocks. Furthermore, we add the current control flow to
	// represent exceptions that occur before any mutations.
	stmt := node.AsTryStatement()
	catchClause, finallyBlock := stmt.CatchClause(), stmt.FinallyBlock()
	saveReturnTarget := b.currentReturnTarget
	saveExceptionTarget := b.currentExceptionTarget
	normalExitLabel := b.createBranchLabel()
	returnLabel := b.createBranchLabel()
	exceptionLabel := b.createBranchLabel()
	if !finallyBlock.IsNil() {
		b.currentReturnTarget = returnLabel
	}
	b.addAntecedent(exceptionLabel, b.currentFlow)
	b.currentExceptionTarget = exceptionLabel
	b.bind(stmt.TryBlock())
	b.addAntecedent(normalExitLabel, b.currentFlow)
	if !catchClause.IsNil() {
		// Start of catch clause is the target of exceptions from try block.
		b.currentFlow = b.finishFlowLabel(exceptionLabel)
		// The currentExceptionTarget now represents control flows from exceptions in the catch clause.
		// Effectively, in a try-catch-finally, if an exception occurs in the try block, the catch block
		// acts like a second try block.
		exceptionLabel = b.createBranchLabel()
		b.addAntecedent(exceptionLabel, b.currentFlow)
		b.currentExceptionTarget = exceptionLabel
		b.bind(catchClause)
		b.addAntecedent(normalExitLabel, b.currentFlow)
	}
	b.currentReturnTarget = saveReturnTarget
	b.currentExceptionTarget = saveExceptionTarget
	if !finallyBlock.IsNil() {
		// Possible ways control can reach the finally block:
		// 1) Normal completion of try block of a try-finally or try-catch-finally
		// 2) Normal completion of catch block (following exception in try block) of a try-catch-finally
		// 3) Return in try or catch block of a try-finally or try-catch-finally
		// 4) Exception in try block of a try-finally
		// 5) Exception in catch block of a try-catch-finally
		// When analyzing a control flow graph that starts inside a finally block we want to consider all
		// five possibilities above. However, when analyzing a control flow graph that starts outside (past)
		// the finally block, we only want to consider the first two (if we're past a finally block then it
		// must have completed normally). Likewise, when analyzing a control flow graph from return statements
		// in try or catch blocks in an IIFE, we only want to consider the third. To make this possible, we
		// inject a ReduceLabel node into the control flow graph. This node contains an alternate reduced
		// set of antecedents for the pre-finally label. As control flow analysis passes by a ReduceLabel
		// node, the pre-finally label is temporarily switched to the reduced antecedent set.
		finallyLabel := b.createBranchLabel()
		antecedents := b.combineFlowLists(b.bound.Flow(normalExitLabel).Antecedents, b.combineFlowLists(b.bound.Flow(exceptionLabel).Antecedents, b.bound.Flow(returnLabel).Antecedents))
		b.bound.Flow(finallyLabel).Antecedents = antecedents
		b.currentFlow = finallyLabel
		b.bind(finallyBlock)
		if b.bound.Flow(b.currentFlow).Flags&ast.FlowFlagsUnreachable != 0 {
			// If the end of the finally block is unreachable, the end of the entire try statement is unreachable.
			b.currentFlow = b.unreachableFlow
		} else {
			// If we have an IIFE return target and return statements in the try or catch blocks, add a control
			// flow that goes back through the finally block and back through only the return statements.
			if b.currentReturnTarget != 0 && b.bound.Flow(returnLabel).Antecedents != 0 {
				b.addAntecedent(b.currentReturnTarget, b.createReduceLabel(finallyLabel, b.bound.Flow(returnLabel).Antecedents, b.currentFlow))
			}
			// If we have an outer exception target (i.e. a containing try-finally or try-catch-finally), add a
			// control flow that goes back through the finally block and back through each possible exception source.
			if b.currentExceptionTarget != 0 && b.bound.Flow(exceptionLabel).Antecedents != 0 {
				b.addAntecedent(b.currentExceptionTarget, b.createReduceLabel(finallyLabel, b.bound.Flow(exceptionLabel).Antecedents, b.currentFlow))
			}
			// If the end of the finally block is reachable, but the end of the try and catch blocks are not,
			// convert the current flow to unreachable. For example, 'try { return 1; } finally { ... }' should
			// result in an unreachable current control flow.
			if b.bound.Flow(normalExitLabel).Antecedents != 0 {
				b.currentFlow = b.createReduceLabel(finallyLabel, b.bound.Flow(normalExitLabel).Antecedents, b.currentFlow)
			} else {
				b.currentFlow = b.unreachableFlow
			}
		}
	} else {
		b.currentFlow = b.finishFlowLabel(normalExitLabel)
	}
}

func (b *Binder) bindSwitchStatement(node store.Node) {
	stmt := node.AsSwitchStatement()
	postSwitchLabel := b.createBranchLabel()
	b.bind(stmt.Expression())
	saveBreakTarget := b.currentBreakTarget
	savePreSwitchCaseFlow := b.preSwitchCaseFlow
	b.currentBreakTarget = postSwitchLabel
	b.preSwitchCaseFlow = b.currentFlow
	caseBlock := stmt.CaseBlock()
	b.bind(caseBlock)
	b.addAntecedent(postSwitchLabel, b.currentFlow)
	hasDefault := false
	for _, c := range caseBlock.AsCaseBlock().Clauses().Refs() {
		if b.s.Node(c).Kind() == ast.KindDefaultClause {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		b.addAntecedent(postSwitchLabel, b.createFlowSwitchClause(b.preSwitchCaseFlow, node, 0, 0))
	}
	b.currentBreakTarget = saveBreakTarget
	b.preSwitchCaseFlow = savePreSwitchCaseFlow
	b.currentFlow = b.finishFlowLabel(postSwitchLabel)
}

func (b *Binder) bindCaseBlock(node store.Node) {
	switchStatement := node.Parent()
	clauses := node.AsCaseBlock().Clauses().Refs()
	switchExpression := switchStatement.Expression()
	isNarrowingSwitch := switchExpression.Kind() == ast.KindTrueKeyword || isNarrowingExpression(switchExpression)
	var fallthroughFlow store.FlowRef = b.unreachableFlow
	for i := 0; i < len(clauses); i++ {
		clauseStart := i
		for b.s.Node(clauses[i]).Statements().Len() == 0 && i+1 < len(clauses) {
			if fallthroughFlow == b.unreachableFlow {
				b.currentFlow = b.preSwitchCaseFlow
			}
			b.bind(b.s.Node(clauses[i]))
			i++
		}
		preCaseLabel := b.createBranchLabel()
		preCaseFlow := b.preSwitchCaseFlow
		if isNarrowingSwitch {
			preCaseFlow = b.createFlowSwitchClause(b.preSwitchCaseFlow, switchStatement, clauseStart, i+1)
		}
		b.addAntecedent(preCaseLabel, preCaseFlow)
		b.addAntecedent(preCaseLabel, fallthroughFlow)
		b.currentFlow = b.finishFlowLabel(preCaseLabel)
		clause := b.s.Node(clauses[i])
		b.bind(clause)
		fallthroughFlow = b.currentFlow
		if b.bound.Flow(b.currentFlow).Flags&ast.FlowFlagsUnreachable == 0 && i != len(clauses)-1 {
			clause.AsCaseOrDefaultClause().SetFallthroughFlowNode(b.currentFlow)
		}
	}
}

func (b *Binder) bindCaseOrDefaultClause(node store.Node) {
	clause := node.AsCaseOrDefaultClause()
	expression := clause.Expression()
	if !expression.IsNil() {
		saveCurrentFlow := b.currentFlow
		b.currentFlow = b.preSwitchCaseFlow
		b.bind(expression)
		b.currentFlow = saveCurrentFlow
	}
	b.bindEach(clause.Statements().Refs())
}

func (b *Binder) bindExpressionStatement(node store.Node) {
	stmt := node.AsExpressionStatement()
	expression := stmt.Expression()
	b.bind(expression)
	b.maybeBindExpressionFlowIfCall(expression)
}

func (b *Binder) maybeBindExpressionFlowIfCall(node store.Node) {
	// A top level or comma expression call expression with a dotted function name and at least one argument
	// is potentially an assertion and is therefore included in the control flow.
	if store.IsCallExpression(node) {
		expression := node.Expression()
		if expression.Kind() != ast.KindSuperKeyword && store.IsDottedName(expression) {
			b.currentFlow = b.createFlowCall(b.currentFlow, node)
		}
	}
}

func (b *Binder) bindLabeledStatement(node store.Node) {
	stmt := node.AsLabeledStatement()
	label := stmt.Label()
	postStatementLabel := b.createBranchLabel()
	b.activeLabelList = &ActiveLabel{
		next:           b.activeLabelList,
		name:           label.Text(),
		breakTarget:    postStatementLabel,
		continueTarget: 0,
		referenced:     false,
	}
	b.bind(label)
	b.bind(stmt.Statement())
	if !b.activeLabelList.referenced {
		// Mark the label as unused; the checker will decide whether to report it
		label.AddFlags(ast.NodeFlagsUnreachable)
	}
	b.activeLabelList = b.activeLabelList.next
	b.addAntecedent(postStatementLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postStatementLabel)
}

func (b *Binder) bindPrefixUnaryExpressionFlow(node store.Node) {
	expr := node.AsPrefixUnaryExpression()
	operator := expr.Operator()
	if operator == ast.KindExclamationToken {
		saveTrueTarget := b.currentTrueTarget
		b.currentTrueTarget = b.currentFalseTarget
		b.currentFalseTarget = saveTrueTarget
		b.bindEachChild(node)
		b.currentFalseTarget = b.currentTrueTarget
		b.currentTrueTarget = saveTrueTarget
	} else {
		b.bindEachChild(node)
		if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
			b.bindAssignmentTargetFlow(expr.Operand())
		}
	}
}

func (b *Binder) bindPostfixUnaryExpressionFlow(node store.Node) {
	expr := node.AsPostfixUnaryExpression()
	b.bindEachChild(node)
	operator := expr.Operator()
	if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
		b.bindAssignmentTargetFlow(expr.Operand())
	}
}

func (b *Binder) bindDestructuringAssignmentFlow(node store.Node) {
	expr := node.AsBinaryExpression()
	left, typeNode, operatorToken, right := expr.Left(), expr.Type(), expr.OperatorToken(), expr.Right()
	if b.inAssignmentPattern {
		b.inAssignmentPattern = false
		b.bind(operatorToken)
		b.bind(right)
		b.inAssignmentPattern = true
		b.bind(left)
		b.bind(typeNode)
	} else {
		b.inAssignmentPattern = true
		b.bind(left)
		b.bind(typeNode)
		b.inAssignmentPattern = false
		b.bind(operatorToken)
		b.bind(right)
	}
	b.bindAssignmentTargetFlow(left)
}

func (b *Binder) bindBinaryExpressionFlow(node store.Node) {
	expr := node.AsBinaryExpression()
	operator := expr.OperatorToken().Kind()
	if ast.IsLogicalOrCoalescingBinaryOperator(operator) || ast.IsLogicalOrCoalescingAssignmentOperator(operator) {
		if isTopLevelLogicalExpression(node) {
			postExpressionLabel := b.createBranchLabel()
			saveCurrentFlow := b.currentFlow
			saveHasFlowEffects := b.hasFlowEffects
			b.hasFlowEffects = false
			b.bindLogicalLikeExpression(node, postExpressionLabel, postExpressionLabel)
			if b.hasFlowEffects {
				b.currentFlow = b.finishFlowLabel(postExpressionLabel)
			} else {
				b.currentFlow = saveCurrentFlow
			}
			b.hasFlowEffects = b.hasFlowEffects || saveHasFlowEffects
		} else {
			b.bindLogicalLikeExpression(node, b.currentTrueTarget, b.currentFalseTarget)
		}
	} else {
		left, right := expr.Left(), expr.Right()
		b.bind(left)
		b.bind(expr.Type())
		if operator == ast.KindCommaToken {
			b.maybeBindExpressionFlowIfCall(left)
		}
		b.bind(expr.OperatorToken())
		b.bind(right)
		if operator == ast.KindCommaToken {
			b.maybeBindExpressionFlowIfCall(right)
		}
		if ast.IsAssignmentOperator(operator) && !store.IsAssignmentTarget(node) {
			b.bindAssignmentTargetFlow(left)
			if operator == ast.KindEqualsToken && left.Kind() == ast.KindElementAccessExpression {
				elementAccess := left.AsElementAccessExpression()
				if isNarrowableOperand(elementAccess.Expression()) {
					b.currentFlow = b.createFlowMutation(ast.FlowFlagsArrayMutation, b.currentFlow, node)
				}
			}
		}
	}
}

func (b *Binder) bindLogicalLikeExpression(node store.Node, trueTarget store.FlowRef, falseTarget store.FlowRef) {
	expr := node.AsBinaryExpression()
	left, operatorToken, right := expr.Left(), expr.OperatorToken(), expr.Right()
	operator := operatorToken.Kind()
	preRightLabel := b.createBranchLabel()
	if operator == ast.KindAmpersandAmpersandToken || operator == ast.KindAmpersandAmpersandEqualsToken {
		b.bindCondition(left, preRightLabel, falseTarget)
	} else {
		b.bindCondition(left, trueTarget, preRightLabel)
	}
	b.currentFlow = b.finishFlowLabel(preRightLabel)
	b.bind(operatorToken)
	if ast.IsLogicalOrCoalescingAssignmentOperator(operator) {
		b.doWithConditionalBranches((*Binder).bind, right, trueTarget, falseTarget)
		b.bindAssignmentTargetFlow(left)
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	} else {
		b.bindCondition(right, trueTarget, falseTarget)
	}
}

func (b *Binder) bindDeleteExpressionFlow(node store.Node) {
	expr := node.AsDeleteExpression()
	b.bindEachChild(node)
	expression := expr.Expression()
	if expression.Kind() == ast.KindPropertyAccessExpression {
		b.bindAssignmentTargetFlow(expression)
	}
}

func (b *Binder) bindConditionalExpressionFlow(node store.Node) {
	expr := node.AsConditionalExpression()
	trueLabel := b.createBranchLabel()
	falseLabel := b.createBranchLabel()
	postExpressionLabel := b.createBranchLabel()
	saveCurrentFlow := b.currentFlow
	saveHasFlowEffects := b.hasFlowEffects
	b.hasFlowEffects = false
	b.bindCondition(expr.Condition(), trueLabel, falseLabel)
	b.currentFlow = b.finishFlowLabel(trueLabel)
	b.bind(expr.QuestionToken())
	b.bind(expr.WhenTrue())
	b.addAntecedent(postExpressionLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(falseLabel)
	b.bind(expr.ColonToken())
	b.bind(expr.WhenFalse())
	b.addAntecedent(postExpressionLabel, b.currentFlow)
	if b.hasFlowEffects {
		b.currentFlow = b.finishFlowLabel(postExpressionLabel)
	} else {
		b.currentFlow = saveCurrentFlow
	}
	b.hasFlowEffects = b.hasFlowEffects || saveHasFlowEffects
}

func (b *Binder) bindVariableDeclarationFlow(node store.Node) {
	b.bindEachChild(node)
	if !node.Initializer().IsNil() || store.IsForInOrOfStatement(node.Parent().Parent()) {
		b.bindInitializedVariableFlow(node)
	}
}

func (b *Binder) bindInitializedVariableFlow(node store.Node) {
	name := b.nilNode()
	switch node.Kind() {
	case ast.KindVariableDeclaration:
		name = node.AsVariableDeclaration().Name()
	case ast.KindBindingElement:
		name = node.AsBindingElement().Name()
	}
	if !name.IsNil() && store.IsBindingPattern(name) {
		for _, ref := range name.Elements().Refs() {
			b.bindInitializedVariableFlow(b.s.Node(ref))
		}
	} else {
		b.currentFlow = b.createFlowMutation(ast.FlowFlagsAssignment, b.currentFlow, node)
	}
}

func (b *Binder) bindAccessExpressionFlow(node store.Node) {
	if store.IsOptionalChain(node) {
		b.bindOptionalChainFlow(node)
	} else {
		b.bindEachChild(node)
	}
}

func (b *Binder) bindOptionalChainFlow(node store.Node) {
	if isTopLevelLogicalExpression(node) {
		postExpressionLabel := b.createBranchLabel()
		saveCurrentFlow := b.currentFlow
		saveHasFlowEffects := b.hasFlowEffects
		b.bindOptionalChain(node, postExpressionLabel, postExpressionLabel)
		if b.hasFlowEffects {
			b.currentFlow = b.finishFlowLabel(postExpressionLabel)
		} else {
			b.currentFlow = saveCurrentFlow
		}
		b.hasFlowEffects = b.hasFlowEffects || saveHasFlowEffects
	} else {
		b.bindOptionalChain(node, b.currentTrueTarget, b.currentFalseTarget)
	}
}

func (b *Binder) bindOptionalChain(node store.Node, trueTarget store.FlowRef, falseTarget store.FlowRef) {
	// For an optional chain, we emulate the behavior of a logical expression:
	//
	// a?.b         -> a && a.b
	// a?.b.c       -> a && a.b.c
	// a?.b?.c      -> a && a.b && a.b.c
	// a?.[x = 1]   -> a && a[x = 1]
	//
	// To do this we descend through the chain until we reach the root of a chain (the expression with a `?.`)
	// and build it's CFA graph as if it were the first condition (`a && ...`). Then we bind the rest
	// of the node as part of the "true" branch, and continue to do so as we ascend back up to the outermost
	// chain node. We then treat the entire node as the right side of the expression.
	var preChainLabel store.FlowRef
	if store.IsOptionalChainRoot(node) {
		preChainLabel = b.createBranchLabel()
	}
	b.bindOptionalExpression(node.Expression(), core.IfElse(preChainLabel != 0, preChainLabel, trueTarget), falseTarget)
	if preChainLabel != 0 {
		b.currentFlow = b.finishFlowLabel(preChainLabel)
	}
	b.doWithConditionalBranches((*Binder).bindOptionalChainRest, node, trueTarget, falseTarget)
	if store.IsOutermostOptionalChain(node) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}

func (b *Binder) bindOptionalExpression(node store.Node, trueTarget store.FlowRef, falseTarget store.FlowRef) {
	b.doWithConditionalBranches((*Binder).bind, node, trueTarget, falseTarget)
	if !store.IsOptionalChain(node) || store.IsOutermostOptionalChain(node) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}

func (b *Binder) bindOptionalChainRest(node store.Node) bool {
	switch node.Kind() {
	case ast.KindPropertyAccessExpression:
		b.bind(node.QuestionDotToken())
		b.bind(node.Name())
	case ast.KindElementAccessExpression:
		b.bind(node.QuestionDotToken())
		b.bind(node.AsElementAccessExpression().ArgumentExpression())
	case ast.KindCallExpression:
		b.bind(node.QuestionDotToken())
		b.bindNodeList(node.TypeArguments())
		b.bindEach(node.Arguments().Refs())
	}
	return false
}

func (b *Binder) bindCallExpressionFlow(node store.Node) {
	call := node.AsCallExpression()
	expression := call.Expression()
	if store.IsOptionalChain(node) {
		b.bindOptionalChainFlow(node)
	} else {
		// If the target of the call expression is a function expression or arrow function we have
		// an immediately invoked function expression (IIFE). Initialize the flowNode property to
		// the current control flow (which includes evaluation of the IIFE arguments).
		expr := store.SkipParentheses(expression)
		exprKind := expr.Kind()
		if exprKind == ast.KindFunctionExpression || exprKind == ast.KindArrowFunction {
			b.bindNodeList(call.TypeArguments())
			b.bindEach(call.Arguments().Refs())
			b.bind(expression)
		} else {
			b.bindEachChild(node)
			if expression.Kind() == ast.KindSuperKeyword {
				b.currentFlow = b.createFlowCall(b.currentFlow, node)
			}
		}
	}
	if store.IsPropertyAccessExpression(expression) {
		access := expression.AsPropertyAccessExpression()
		name := access.Name()
		if store.IsIdentifier(name) && isNarrowableOperand(access.Expression()) && store.IsPushOrUnshiftIdentifier(name) {
			b.currentFlow = b.createFlowMutation(ast.FlowFlagsArrayMutation, b.currentFlow, node)
		}
	}
}

func (b *Binder) bindNonNullExpressionFlow(node store.Node) {
	if store.IsOptionalChain(node) {
		b.bindOptionalChainFlow(node)
	} else {
		b.bindEachChild(node)
	}
}

func (b *Binder) bindBindingElementFlow(node store.Node) {
	// When evaluating a binding pattern, the initializer is evaluated before the binding pattern, per:
	// - https://tc39.es/ecma262/#sec-destructuring-binding-patterns-runtime-semantics-iteratorbindinginitialization
	//   - `BindingElement: BindingPattern Initializer?`
	// - https://tc39.es/ecma262/#sec-runtime-semantics-keyedbindinginitialization
	//   - `BindingElement: BindingPattern Initializer?`
	elem := node.AsBindingElement()
	b.bind(elem.DotDotDotToken())
	b.bind(elem.PropertyName())
	b.bindInitializer(elem.Initializer())
	b.bind(elem.Name())
}

func (b *Binder) bindParameterFlow(node store.Node) {
	param := node.AsParameterDeclaration()
	b.bindModifiers(param.Modifiers())
	b.bind(param.DotDotDotToken())
	b.bind(param.QuestionToken())
	b.bind(param.Type())
	b.bindInitializer(param.Initializer())
	b.bind(param.Name())
}

// a BindingElement/Parameter does not have side effects if initializers are not evaluated and used. (see GH#49759)
func (b *Binder) bindInitializer(node store.Node) {
	if node.IsNil() {
		return
	}
	entryFlow := b.currentFlow
	b.bind(node)
	if entryFlow == b.unreachableFlow || entryFlow == b.currentFlow {
		return
	}
	exitFlow := b.createBranchLabel()
	b.addAntecedent(exitFlow, entryFlow)
	b.addAntecedent(exitFlow, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(exitFlow)
}

// Every header has a flow word; the callers are guarded by kind, like the
// Pointer's FlowNodeData check.
func setFlowNode(node store.Node, flowNode store.FlowRef) {
	node.SetFlow(flowNode)
}

// The kind switch of the Pointer version is the table lookup of the role setter.
func setReturnFlowNode(node store.Node, returnFlowNode store.FlowRef) {
	node.SetReturnFlowNode(returnFlowNode)
}

func isGeneratorFunctionExpression(node store.Node) bool {
	return store.IsFunctionExpression(node) && !node.AsFunctionExpression().AsteriskToken().IsNil()
}

// The NextContainer chain is the containers column of Bound, in the order the
// containers are added.
func (b *Binder) addToContainerChain(next store.Node) {
	if !b.lastContainer.IsNil() {
		b.bound.AddContainer(next.Ref())
	}
	b.lastContainer = next
}

func (b *Binder) addDeclarationToSymbol(symbol *store.Symbol, node store.Node, symbolFlags ast.SymbolFlags) {
	symbol.Flags |= symbolFlags
	node.SetSymbol(symbol.Id())
	if symbol.Declarations == nil {
		symbol.Declarations = b.newSingleDeclaration(node.FileRef())
	} else {
		symbol.Declarations = core.AppendIfUnique(symbol.Declarations, node.FileRef())
	}
	// On merge of const enum module with class or function, reset const enum only flag (namespaces will already recalculate)
	if symbol.Flags&ast.SymbolFlagsConstEnumOnlyModule != 0 && symbol.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsClass|ast.SymbolFlagsRegularEnum) != 0 {
		symbol.Flags &^= ast.SymbolFlagsConstEnumOnlyModule
		b.notConstEnumOnlyModules.Add(symbol)
	}
	if symbolFlags&ast.SymbolFlagsValue != 0 {
		SetValueDeclaration(b.s, symbol, node)
	}
}

// SetValueDeclaration takes the Store that resolves the symbol's ValueDeclaration Ref (one file in 7c).
func SetValueDeclaration(s *store.Store, symbol *store.Symbol, node store.Node) {
	valueDeclarationRef := symbol.ValueDeclaration
	if valueDeclarationRef == (store.Ref{}) {
		symbol.ValueDeclaration = node.FileRef()
		return
	}
	valueDeclaration := s.Node(valueDeclarationRef.Id())
	if isAssignmentDeclaration(valueDeclaration) && !isAssignmentDeclaration(node) ||
		valueDeclaration.Kind() != node.Kind() && isEffectiveModuleDeclaration(valueDeclaration) {
		// Non-assignment declarations take precedence over assignment declarations and
		// non-namespace declarations take precedence over namespace declarations.
		symbol.ValueDeclaration = node.FileRef()
	}
}

/**
 * Declares a Symbol for the node and adds it to symbols. Reports errors for conflicting identifier names.
 * @param symbolTable - The symbol table which node will be added to.
 * @param parent - node's parent declaration.
 * @param node - The declaration to be added to the symbol table
 * @param includes - The SymbolFlags that node has in addition to its declaration type (eg: export, ambient, etc.)
 * @param excludes - The flags which node cannot be declared alongside in a symbol table. Used to report forbidden declarations.
 */

func GetContainerFlags(node store.Node) ContainerFlags {
	switch node.Kind() {
	case ast.KindClassExpression, ast.KindClassDeclaration, ast.KindEnumDeclaration, ast.KindObjectLiteralExpression, ast.KindTypeLiteral,
		ast.KindJsxAttributes:
		return ContainerFlagsIsContainer
	case ast.KindInterfaceDeclaration:
		return ContainerFlagsIsContainer | ContainerFlagsIsInterface
	case ast.KindModuleDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindMappedType, ast.KindIndexSignature:
		return ContainerFlagsIsContainer | ContainerFlagsHasLocals
	case ast.KindSourceFile:
		return ContainerFlagsIsContainer | ContainerFlagsIsControlFlowContainer | ContainerFlagsHasLocals
	case ast.KindGetAccessor, ast.KindSetAccessor, ast.KindMethodDeclaration:
		if store.IsObjectLiteralOrClassExpressionMethodOrAccessor(node) {
			return ContainerFlagsIsContainer | ContainerFlagsIsControlFlowContainer | ContainerFlagsHasLocals | ContainerFlagsIsFunctionLike | ContainerFlagsIsObjectLiteralOrClassExpressionMethodOrAccessor | ContainerFlagsIsThisContainer
		}
		fallthrough
	case ast.KindConstructor, ast.KindFunctionDeclaration, ast.KindClassStaticBlockDeclaration:
		return ContainerFlagsIsContainer | ContainerFlagsIsControlFlowContainer | ContainerFlagsHasLocals | ContainerFlagsIsFunctionLike | ContainerFlagsIsThisContainer
	case ast.KindMethodSignature, ast.KindCallSignature, ast.KindFunctionType, ast.KindConstructSignature, ast.KindConstructorType:
		return ContainerFlagsIsContainer | ContainerFlagsIsControlFlowContainer | ContainerFlagsHasLocals | ContainerFlagsIsFunctionLike | ContainerFlagsPropagatesThisKeyword
	case ast.KindFunctionExpression:
		return ContainerFlagsIsContainer | ContainerFlagsIsControlFlowContainer | ContainerFlagsHasLocals | ContainerFlagsIsFunctionLike | ContainerFlagsIsFunctionExpression | ContainerFlagsIsThisContainer
	case ast.KindArrowFunction:
		return ContainerFlagsIsContainer | ContainerFlagsIsControlFlowContainer | ContainerFlagsHasLocals | ContainerFlagsIsFunctionLike | ContainerFlagsIsFunctionExpression | ContainerFlagsPropagatesThisKeyword
	case ast.KindModuleBlock:
		return ContainerFlagsIsControlFlowContainer
	case ast.KindPropertyDeclaration:
		if !node.Initializer().IsNil() {
			return ContainerFlagsIsControlFlowContainer | ContainerFlagsIsThisContainer
		} else {
			return ContainerFlagsNone
		}
	case ast.KindCatchClause, ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindCaseBlock:
		return ContainerFlagsIsBlockScopedContainer | ContainerFlagsHasLocals
	case ast.KindBlock:
		parent := node.Parent()
		if store.IsFunctionLike(parent) || store.IsClassStaticBlockDeclaration(parent) {
			return ContainerFlagsNone
		} else {
			return ContainerFlagsIsBlockScopedContainer | ContainerFlagsHasLocals
		}
	}
	return ContainerFlagsNone
}

func isNarrowingExpression(expr store.Node) bool {
	switch expr.Kind() {
	case ast.KindIdentifier, ast.KindThisKeyword:
		return true
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		return containsNarrowableReference(expr)
	case ast.KindCallExpression:
		return hasNarrowableArgument(expr)
	case ast.KindParenthesizedExpression, ast.KindNonNullExpression, ast.KindTypeOfExpression:
		return isNarrowingExpression(expr.Expression())
	case ast.KindBinaryExpression:
		return isNarrowingBinaryExpression(expr.AsBinaryExpression())
	case ast.KindPrefixUnaryExpression:
		return expr.AsPrefixUnaryExpression().Operator() == ast.KindExclamationToken && isNarrowingExpression(expr.AsPrefixUnaryExpression().Operand())
	}
	return false
}

func containsNarrowableReference(expr store.Node) bool {
	if isNarrowableReference(expr) {
		return true
	}
	if expr.Flags()&ast.NodeFlagsOptionalChain != 0 {
		switch expr.Kind() {
		case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression, ast.KindCallExpression, ast.KindNonNullExpression:
			return containsNarrowableReference(expr.Expression())
		}
	}
	return false
}

func isNarrowableReference(node store.Node) bool {
	switch node.Kind() {
	case ast.KindIdentifier, ast.KindThisKeyword, ast.KindSuperKeyword, ast.KindMetaProperty:
		return true
	case ast.KindPropertyAccessExpression, ast.KindParenthesizedExpression, ast.KindNonNullExpression:
		return isNarrowableReference(node.Expression())
	case ast.KindElementAccessExpression:
		expr := node.AsElementAccessExpression()
		return store.IsStringOrNumericLiteralLike(expr.ArgumentExpression()) ||
			store.IsEntityNameExpression(expr.ArgumentExpression()) && isNarrowableReference(expr.Expression())
	case ast.KindBinaryExpression:
		expr := node.AsBinaryExpression()
		return expr.OperatorToken().Kind() == ast.KindCommaToken && isNarrowableReference(expr.Right()) ||
			ast.IsAssignmentOperator(expr.OperatorToken().Kind()) && store.IsLeftHandSideExpression(expr.Left())
	}
	return false
}

func hasNarrowableArgument(expr store.Node) bool {
	call := expr.AsCallExpression()
	arguments := call.Arguments()
	for i := range arguments.Len() { //nolint:modernize
		if containsNarrowableReference(arguments.At(i)) {
			return true
		}
	}
	expression := call.Expression()
	if store.IsPropertyAccessExpression(expression) {
		if containsNarrowableReference(expression.Expression()) {
			return true
		}
	}
	return false
}

func isNarrowingBinaryExpression(expr store.BinaryExpression) bool {
	switch expr.OperatorToken().Kind() {
	case ast.KindEqualsToken, ast.KindBarBarEqualsToken, ast.KindAmpersandAmpersandEqualsToken, ast.KindQuestionQuestionEqualsToken:
		return containsNarrowableReference(expr.Left())
	case ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken, ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken:
		left := store.SkipParentheses(expr.Left())
		right := store.SkipParentheses(expr.Right())
		return isNarrowableOperand(left) || isNarrowableOperand(right) ||
			isNarrowingTypeOfOperands(right, left) || isNarrowingTypeOfOperands(left, right) ||
			(store.IsBooleanLiteral(right) && isNarrowingExpression(left) || store.IsBooleanLiteral(left) && isNarrowingExpression(right))
	case ast.KindInstanceOfKeyword:
		return isNarrowableOperand(expr.Left())
	case ast.KindInKeyword:
		return isNarrowingExpression(expr.Right())
	case ast.KindCommaToken:
		return isNarrowingExpression(expr.Right())
	}
	return false
}

func isNarrowableOperand(expr store.Node) bool {
	switch expr.Kind() {
	case ast.KindParenthesizedExpression:
		return isNarrowableOperand(expr.Expression())
	case ast.KindBinaryExpression:
		binary := expr.AsBinaryExpression()
		switch binary.OperatorToken().Kind() {
		case ast.KindEqualsToken:
			return isNarrowableOperand(binary.Left())
		case ast.KindCommaToken:
			return isNarrowableOperand(binary.Right())
		}
	}
	return containsNarrowableReference(expr)
}

func isNarrowingTypeOfOperands(expr1 store.Node, expr2 store.Node) bool {
	return store.IsTypeOfExpression(expr1) && isNarrowableOperand(expr1.Expression()) && store.IsStringLiteralLike(expr2)
}

func (b *Binder) errorOnNode(node store.Node, message *diagnostics.Message, args ...any) {
	b.addDiagnostic(b.createDiagnosticForNode(node, message, args...))
}

func (b *Binder) errorOnFirstToken(node store.Node, message *diagnostics.Message, args ...any) {
	span := getRangeOfTokenAtPosition(b.file, int(node.Pos()))
	b.addDiagnostic(ast.NewDiagnostic(nil, span, message, args...))
}

// Inside the binder, we may create a diagnostic for an as-yet unbound node (with potentially no parent pointers, implying no accessible source file)
// If so, the node _must_ be in the current file (as that's the only way anything could have traversed to it to yield it as the error node)
// This version of `createDiagnosticForNode` uses the binder's context to account for this, and always yields correct diagnostics even in these situations.
// The diagnostic's file is nil (7c): the program attaches it.
func (b *Binder) createDiagnosticForNode(node store.Node, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	return ast.NewDiagnostic(nil, getErrorRangeForNode(b.file, node), message, args...)
}

func (b *Binder) addDiagnostic(diagnostic *ast.Diagnostic) {
	b.bound.AddDiagnostic(diagnostic)
}

func isSignedNumericLiteral(node store.Node) bool {
	if node.Kind() == ast.KindPrefixUnaryExpression {
		node := node.AsPrefixUnaryExpression()
		return (node.Operator() == ast.KindPlusToken || node.Operator() == ast.KindMinusToken) && store.IsNumericLiteral(node.Operand())
	}
	return false
}

func getOptionalSymbolFlagForNode(node store.Node) ast.SymbolFlags {
	postfixToken := node.PostfixToken()
	return core.IfElse(!postfixToken.IsNil() && postfixToken.Kind() == ast.KindQuestionToken, ast.SymbolFlagsOptional, ast.SymbolFlagsNone)
}

// isFunctionSymbol takes the Store that resolves the symbol's ValueDeclaration Ref.
func isFunctionSymbol(s *store.Store, symbol *store.Symbol) bool {
	if symbol.ValueDeclaration != (store.Ref{}) {
		d := s.Node(symbol.ValueDeclaration.Id())
		if store.IsFunctionDeclaration(d) {
			return true
		}
		if store.IsVariableDeclaration(d) {
			varDecl := d.AsVariableDeclaration()
			if !varDecl.Initializer().IsNil() {
				return store.IsFunctionLike(varDecl.Initializer())
			}
		}
	}
	return false
}

func isStatementCondition(node store.Node) bool {
	parent := node.Parent()
	switch parent.Kind() {
	case ast.KindIfStatement, ast.KindWhileStatement, ast.KindDoStatement:
		return parent.Expression() == node
	case ast.KindForStatement:
		return parent.AsForStatement().Condition() == node
	case ast.KindConditionalExpression:
		return parent.AsConditionalExpression().Condition() == node
	}
	return false
}

func isTopLevelLogicalExpression(node store.Node) bool {
	parent := node.Parent()
	for store.IsParenthesizedExpression(parent) || store.IsPrefixUnaryExpression(parent) && parent.AsPrefixUnaryExpression().Operator() == ast.KindExclamationToken {
		node = parent
		parent = node.Parent()
	}
	return !isStatementCondition(node) && !store.IsLogicalExpression(parent) && !(store.IsOptionalChain(parent) && parent.Expression() == node)
}

func isAssignmentDeclaration(decl store.Node) bool {
	return store.IsBinaryExpression(decl) || store.IsAccessExpression(decl) || store.IsIdentifier(decl) || store.IsCallExpression(decl)
}

func isEffectiveModuleDeclaration(node store.Node) bool {
	return store.IsModuleDeclaration(node) || store.IsIdentifier(node)
}
