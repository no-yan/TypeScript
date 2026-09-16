package binder

import (
	"strconv"
	"sync"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
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
	node                ast.NodeRef
	container           ast.NodeRef
	blockScopeContainer ast.NodeRef
}

type Binder struct {
	file            *ast.SourceFile
	store           *ast.Store
	unreachableFlow *ast.FlowNode

	container               ast.NodeRef
	thisContainer           ast.NodeRef
	blockScopeContainer     ast.NodeRef
	lastContainer           ast.NodeRef
	currentFlow             *ast.FlowNode
	currentBreakTarget      *ast.FlowLabel
	currentContinueTarget   *ast.FlowLabel
	currentReturnTarget     *ast.FlowLabel
	currentTrueTarget       *ast.FlowLabel
	currentFalseTarget      *ast.FlowLabel
	currentExceptionTarget  *ast.FlowLabel
	preSwitchCaseFlow       *ast.FlowNode
	activeLabelList         *ActiveLabel
	emitFlags               ast.NodeFlags
	seenThisKeyword         bool
	hasExplicitReturn       bool
	hasFlowEffects          bool
	inAssignmentPattern     bool
	seenParseError          bool
	symbolCount             int
	notConstEnumOnlyModules collections.Set[*ast.Symbol]
	symbolArena             core.Arena[ast.Symbol]
	flowListArena           core.Arena[ast.FlowList]
	singleDeclarationsArena core.Arena[ast.Handle]
	expandoAssignments      []ExpandoAssignmentInfo
}

type ActiveLabel struct {
	next           *ActiveLabel
	breakTarget    *ast.FlowLabel
	continueTarget *ast.FlowLabel
	name           string
	referenced     bool
}

func (label *ActiveLabel) BreakTarget() *ast.FlowNode    { return label.breakTarget }
func (label *ActiveLabel) ContinueTarget() *ast.FlowNode { return label.continueTarget }

func BindSourceFile(file *ast.SourceFile) {
	// This is constructed this way to make the compiler "out-line" the function,
	// avoiding most work in the common case where the file has already been bound.
	if !file.IsBound() {
		bindSourceFile(file)
	}
}

var binderPool = sync.Pool{
	New: func() any {
		return &Binder{}
	},
}

func getBinder() *Binder {
	return binderPool.Get().(*Binder)
}

func putBinder(b *Binder) {
	*b = Binder{}
	binderPool.Put(b)
}

func bindSourceFile(file *ast.SourceFile) {
	file.BindOnce(func() {
		b := getBinder()
		defer putBinder(b)
		store, root := file.ParseTreeRef()
		b.file = file
		b.store = store
		ast.RegisterFile(file)
		if store != nil {
			store.PrepareBindTables()
		}
		b.unreachableFlow = b.newFlowNode(ast.FlowFlagsUnreachable)
		b.bind(root)
		b.bindDeferredExpandoAssignments()
		file.SymbolCount = b.symbolCount
	})
}

func (b *Binder) newSymbol(flags ast.SymbolFlags, name string) *ast.Symbol {
	b.symbolCount++
	result := b.symbolArena.New()
	result.Flags = flags
	result.Name = name
	return result
}

func (b *Binder) getLocals(ref ast.NodeRef) ast.SymbolTable {
	if ref == ast.NoNodeRef {
		return nil
	}
	if locals := b.store.Locals(ref); locals != nil {
		return locals
	}
	locals := make(ast.SymbolTable)
	b.store.SetLocals(ref, locals)
	return locals
}

/**
 * Declares a Symbol for the node and adds it to symbols. Reports errors for conflicting identifier names.
 * @param symbolTable - The symbol table which node will be added to.
 * @param parent - node's parent declaration.
 * @param node - The declaration to be added to the symbol table
 * @param includes - The SymbolFlags that node has in addition to its declaration type (eg: export, ambient, etc.)
 * @param excludes - The flags which node cannot be declared alongside in a symbol table. Used to report forbidden declarations.
 */
func (b *Binder) declareSymbol(symbolTable ast.SymbolTable, parent *ast.Symbol, node ast.NodeRef, includes ast.SymbolFlags, excludes ast.SymbolFlags) *ast.Symbol {
	return b.declareSymbolEx(symbolTable, parent, node, includes, excludes, false /*isReplaceableByMethod*/, false /*isComputedName*/)
}

func (b *Binder) declareSymbolEx(symbolTable ast.SymbolTable, parent *ast.Symbol, node ast.NodeRef, includes ast.SymbolFlags, excludes ast.SymbolFlags, isReplaceableByMethod bool, isComputedName bool) *ast.Symbol {
	kind := b.store.KindAt(node)
	debug.Assert(isComputedName || !ast.HasDynamicName(b.store.At(node)))
	isDefaultExport := ast.HasSyntacticModifier(b.store.At(node), ast.ModifierFlagsDefault) || kind == ast.KindExportSpecifier && b.store.TextAt(b.store.AccessExportSpecifier(node).Name) == ast.InternalSymbolNameDefault
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
	var symbol *ast.Symbol
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
						if len(symbol.Declarations) != 0 && kind == ast.KindExportAssignment && !b.isExportEqualsRefGenerated(node, kind) {
							message = diagnostics.A_module_cannot_have_multiple_default_exports
							messageNeedsName = false
							multipleDefaultExports = true
						}
					}
				}
				declarationName := ast.GetNameOfDeclaration(b.store.At(node))
				if declarationName.IsNil() {
					declarationName = b.store.At(node)
				}
				debug.Assert(declarationName.Store() == b.store)
				var diag *ast.Diagnostic
				if messageNeedsName {
					diag = b.createDiagnosticForNode(declarationName.Ref(), message, b.getDisplayName(node))
				} else {
					diag = b.createDiagnosticForNode(declarationName.Ref(), message)
				}
				if kind == ast.KindTypeAliasDeclaration {
					declaration := b.store.AccessTypeAliasDeclaration(node)
					loc := b.store.LocAt(declaration.Type)
					missingType := declaration.Type == ast.NoNodeRef || loc.Pos() == loc.End() && loc.Pos() >= 0 && b.store.KindAt(declaration.Type) != ast.KindEndOfFile
					if missingType && ast.HasSyntacticModifier(b.store.At(node), ast.ModifierFlagsExport) && symbol.Flags&(ast.SymbolFlagsAlias|ast.SymbolFlagsType|ast.SymbolFlagsNamespace) != 0 {
						// export type T; - may have meant export type { T }?
						diag.AddRelatedInfo(b.createDiagnosticForNode(node, diagnostics.Did_you_mean_0, "export type { "+b.store.TextAt(declaration.Name)+" }"))
					}
				}
				for index, declaration := range symbol.Declarations {
					debug.Assert(declaration.Store() == b.store)
					decl := ast.GetNameOfDeclaration(declaration)
					if decl.IsNil() {
						decl = declaration
					}
					debug.Assert(decl.Store() == b.store)
					var d *ast.Diagnostic
					if messageNeedsName {
						d = b.createDiagnosticForNode(decl.Ref(), message, b.getDisplayName(declaration.Ref()))
					} else {
						d = b.createDiagnosticForNode(decl.Ref(), message)
					}
					if multipleDefaultExports {
						d.AddRelatedInfo(b.createDiagnosticForNode(declarationName.Ref(), core.IfElse(index == 0, diagnostics.Another_export_default_is_here, diagnostics.X_and_here)))
					}
					b.addDiagnostic(d)
					if multipleDefaultExports {
						diag.AddRelatedInfo(b.createDiagnosticForNode(decl.Ref(), diagnostics.The_first_export_default_is_here))
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
func (b *Binder) getDeclarationName(node ast.NodeRef) string {
	kind := b.store.KindAt(node)
	if kind == ast.KindExportAssignment {
		return core.IfElse(b.isExportEqualsRefGenerated(node, kind), ast.InternalSymbolNameExportEquals, ast.InternalSymbolNameDefault)
	}
	name := ast.GetNameOfDeclaration(b.store.At(node))
	if !name.IsNil() {
		if ast.IsAmbientModule(b.store.At(node)) {
			moduleName := name.Text()
			if ast.IsGlobalScopeAugmentation(b.store.At(node)) {
				return ast.InternalSymbolNameGlobal
			}
			// The current Store parser has no ambient-module attributes field.

			return "\"" + moduleName + "\""
		}
		if name.Kind == ast.KindPrivateIdentifier {
			// containingClass exists because private names only allowed inside classes
			containingClass := ast.GetContainingClass(b.store.At(node))
			if containingClass.IsNil() {
				// we can get here in cases where there is already a parse error.
				return ast.InternalSymbolNameMissing
			}
			return GetSymbolNameForPrivateIdentifier(containingClass.Symbol(), name.Text())
		}
		if name.Kind == ast.KindIdentifier || name.Kind == ast.KindStringLiteral || name.Kind == ast.KindNoSubstitutionTemplateLiteral || name.Kind == ast.KindNumericLiteral || name.Kind == ast.KindJsxNamespacedName {
			return name.Text()
		}
		if name.Kind == ast.KindComputedPropertyName {
			nameExpression := name.Expression()
			// treat computed property names where expression is string/numeric literal as just string/numeric literal
			if nameExpression.Kind == ast.KindStringLiteral || nameExpression.Kind == ast.KindNoSubstitutionTemplateLiteral || nameExpression.Kind == ast.KindNumericLiteral {
				return nameExpression.Text()
			}
			if ast.IsSignedNumericLiteral(nameExpression) {
				return scanner.TokenToString(nameExpression.PrefixUnaryExpressionOperator()) + nameExpression.PrefixUnaryExpressionOperand().Text()
			}
			panic("Only computed properties with literal names have declaration names")
		}
		return ast.InternalSymbolNameMissing
	}
	switch kind {
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

func (b *Binder) getDisplayName(node ast.NodeRef) string {
	nameNode := b.nameRefGenerated(node, b.store.KindAt(node))
	if nameNode != ast.NoNodeRef {
		return scanner.DeclarationNameToString(b.store.At(nameNode))
	}
	name := b.getDeclarationName(node)
	if name != ast.InternalSymbolNameMissing {
		return name
	}
	return "(Missing)"
}

func GetSymbolNameForPrivateIdentifier(containingClassSymbol *ast.Symbol, description string) string {
	return ast.InternalSymbolNamePrefix + "#" + strconv.Itoa(int(ast.GetSymbolId(containingClassSymbol))) + "@" + description
}

func (b *Binder) declareModuleMember(node ast.NodeRef, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	container := b.container
	kind := b.store.KindAt(node)
	hasExportModifier := ast.GetCombinedModifierFlags(b.store.At(node))&ast.ModifierFlagsExport != 0 || ast.IsImplicitlyExportedJSDocDeclaration(b.store.At(node))
	if symbolFlags&ast.SymbolFlagsAlias != 0 {
		if kind == ast.KindExportSpecifier || (kind == ast.KindImportEqualsDeclaration && hasExportModifier) {
			return b.declareSymbol(ast.GetExports(b.store.Symbol(container)), b.store.Symbol(container), node, symbolFlags, symbolExcludes)
		}
		return b.declareSymbol(b.getLocals(container), nil /*parent*/, node, symbolFlags, symbolExcludes)
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
	if !ast.IsAmbientModule(b.store.At(node)) && (hasExportModifier || b.store.FlagsAt(container)&ast.NodeFlagsExportContext != 0) {
		if !ast.IsLocalsContainerKind(b.store.KindAt(container)) || (ast.HasSyntacticModifier(b.store.At(node), ast.ModifierFlagsDefault) && b.getDeclarationName(node) == ast.InternalSymbolNameMissing) {
			return b.declareSymbol(ast.GetExports(b.store.Symbol(container)), b.store.Symbol(container), node, symbolFlags, symbolExcludes)
			// No local symbol for an unnamed default!
		}
		exportKind := ast.SymbolFlagsNone
		if symbolFlags&ast.SymbolFlagsValue != 0 {
			exportKind = ast.SymbolFlagsExportValue
		}
		local := b.declareSymbol(b.getLocals(container), nil /*parent*/, node, exportKind, symbolExcludes)
		local.ExportSymbol = b.declareSymbol(ast.GetExports(b.store.Symbol(container)), b.store.Symbol(container), node, symbolFlags, symbolExcludes)
		b.store.SetLocalSymbol(node, local)
		return local
	}
	return b.declareSymbol(b.getLocals(container), nil /*parent*/, node, symbolFlags, symbolExcludes)
}

func (b *Binder) declareClassMember(node ast.NodeRef, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	if ast.IsStatic(b.store.At(node)) {
		return b.declareSymbol(ast.GetExports(b.store.Symbol(b.container)), b.store.Symbol(b.container), node, symbolFlags, symbolExcludes)
	}
	return b.declareSymbol(ast.GetMembers(b.store.Symbol(b.container)), b.store.Symbol(b.container), node, symbolFlags, symbolExcludes)
}

func (b *Binder) declareSourceFileMember(node ast.NodeRef, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	if ast.IsExternalModule(b.file) {
		return b.declareModuleMember(node, symbolFlags, symbolExcludes)
	}
	_, root := b.file.ParseTreeRef()
	return b.declareSymbol(b.getLocals(root), nil /*parent*/, node, symbolFlags, symbolExcludes)
}

func (b *Binder) declareSymbolAndAddToSymbolTable(node ast.NodeRef, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	switch b.store.KindAt(b.container) {
	case ast.KindModuleDeclaration:
		return b.declareModuleMember(node, symbolFlags, symbolExcludes)
	case ast.KindSourceFile:
		return b.declareSourceFileMember(node, symbolFlags, symbolExcludes)
	case ast.KindClassExpression, ast.KindClassDeclaration:
		return b.declareClassMember(node, symbolFlags, symbolExcludes)
	case ast.KindEnumDeclaration:
		return b.declareSymbol(ast.GetExports(b.store.Symbol(b.container)), b.store.Symbol(b.container), node, symbolFlags, symbolExcludes)
	case ast.KindTypeLiteral, ast.KindObjectLiteralExpression, ast.KindInterfaceDeclaration, ast.KindJsxAttributes:
		return b.declareSymbol(ast.GetMembers(b.store.Symbol(b.container)), b.store.Symbol(b.container), node, symbolFlags, symbolExcludes)
	case ast.KindFunctionType, ast.KindConstructorType, ast.KindCallSignature, ast.KindConstructSignature,
		ast.KindIndexSignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor,
		ast.KindSetAccessor, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindClassStaticBlockDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindMappedType:
		return b.declareSymbol(b.getLocals(b.container), nil /*parent*/, node, symbolFlags, symbolExcludes)
	}
	panic("Unhandled case in declareSymbolAndAddToSymbolTable")
}
func (b *Binder) newFlowNode(flags ast.FlowFlags) *ast.FlowNode {
	return b.store.NewFlow(flags)
}

func (b *Binder) newFlowNodeEx(flags ast.FlowFlags, node ast.NodeRef, antecedent *ast.FlowNode) *ast.FlowNode {
	result := b.newFlowNode(flags)
	result.Node = b.store.At(node)
	result.Antecedent = antecedent
	return result
}

func (b *Binder) newFlowData(flags ast.FlowFlags, data *ast.Node, antecedent *ast.FlowNode) *ast.FlowNode {
	result := b.newFlowNode(flags)
	result.Data = data
	result.Antecedent = antecedent
	return result
}

func (b *Binder) createLoopLabel() *ast.FlowLabel {
	return b.newFlowNode(ast.FlowFlagsLoopLabel)
}

func (b *Binder) createBranchLabel() *ast.FlowLabel {
	return b.newFlowNode(ast.FlowFlagsBranchLabel)
}

func (b *Binder) createReduceLabel(target *ast.FlowLabel, antecedents *ast.FlowList, antecedent *ast.FlowNode) *ast.FlowNode {
	return b.newFlowData(ast.FlowFlagsReduceLabel, ast.NewFlowReduceLabelData(target, antecedents), antecedent)
}

func (b *Binder) createFlowCondition(flags ast.FlowFlags, antecedent *ast.FlowNode, expression ast.NodeRef) *ast.FlowNode {
	if antecedent.Flags&ast.FlowFlagsUnreachable != 0 {
		return antecedent
	}
	if expression == ast.NoNodeRef {
		if flags&ast.FlowFlagsTrueCondition != 0 {
			return antecedent
		}
		return b.unreachableFlow
	}
	if (b.store.KindAt(expression) == ast.KindTrueKeyword && flags&ast.FlowFlagsFalseCondition != 0 || b.store.KindAt(expression) == ast.KindFalseKeyword && flags&ast.FlowFlagsTrueCondition != 0) && !ast.IsExpressionOfOptionalChainRoot(b.store.At(expression)) && !ast.IsNullishCoalesce(b.store.At(b.store.ParentRef(expression))) {
		return b.unreachableFlow
	}
	if !isNarrowingExpression(b.store.At(expression)) {
		return antecedent
	}
	setFlowNodeReferenced(antecedent)
	return b.newFlowNodeEx(flags, expression, antecedent)
}

func (b *Binder) createFlowMutation(flags ast.FlowFlags, antecedent *ast.FlowNode, node ast.NodeRef) *ast.FlowNode {
	setFlowNodeReferenced(antecedent)
	b.hasFlowEffects = true
	result := b.newFlowNodeEx(flags, node, antecedent)
	if b.currentExceptionTarget != nil {
		b.addAntecedent(b.currentExceptionTarget, result)
	}
	return result
}

func (b *Binder) createFlowSwitchClause(antecedent *ast.FlowNode, switchStatement ast.NodeRef, clauseStart int, clauseEnd int) *ast.FlowNode {
	setFlowNodeReferenced(antecedent)
	return b.newFlowData(ast.FlowFlagsSwitchClause, ast.NewFlowSwitchClauseData(b.store.At(switchStatement), clauseStart, clauseEnd), antecedent)
}

func (b *Binder) createFlowCall(antecedent *ast.FlowNode, node ast.NodeRef) *ast.FlowNode {
	setFlowNodeReferenced(antecedent)
	b.hasFlowEffects = true
	return b.newFlowNodeEx(ast.FlowFlagsCall, node, antecedent)
}

func (b *Binder) newFlowList(head *ast.FlowNode, tail *ast.FlowList) *ast.FlowList {
	result := b.flowListArena.New()
	result.Flow = head
	result.Next = tail
	return result
}

func (b *Binder) combineFlowLists(head *ast.FlowList, tail *ast.FlowList) *ast.FlowList {
	if head == nil {
		return tail
	}
	return b.newFlowList(head.Flow, b.combineFlowLists(head.Next, tail))
}

func (b *Binder) newSingleDeclaration(declaration ast.Handle) []ast.Handle {
	return b.singleDeclarationsArena.NewSlice1(declaration)
}

func setFlowNodeReferenced(flow *ast.FlowNode) {
	// On first reference we set the Referenced flag, thereafter we set the Shared flag
	if flow.Flags&ast.FlowFlagsReferenced == 0 {
		flow.Flags |= ast.FlowFlagsReferenced
	} else {
		flow.Flags |= ast.FlowFlagsShared
	}
}

func (b *Binder) addAntecedent(label *ast.FlowLabel, antecedent *ast.FlowNode) {
	if antecedent.Flags&ast.FlowFlagsUnreachable != 0 {
		return
	}
	// If antecedent isn't already on the Antecedents list, add it to the end of the list
	var last *ast.FlowList
	for list := label.Antecedents; list != nil; list = list.Next {
		if list.Flow == antecedent {
			return
		}
		last = list
	}
	if last == nil {
		label.Antecedents = b.newFlowList(antecedent, nil)
	} else {
		last.Next = b.newFlowList(antecedent, nil)
	}
	setFlowNodeReferenced(antecedent)
}

func (b *Binder) finishFlowLabel(label *ast.FlowLabel) *ast.FlowNode {
	if label.Antecedents == nil {
		return b.unreachableFlow
	}
	if label.Antecedents.Next == nil {
		return label.Antecedents.Flow
	}
	return label
}

func (b *Binder) bind(node ast.NodeRef) bool {
	if node == ast.NoNodeRef {
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
	kind := b.store.KindAt(node)
	switch kind {
	case ast.KindIdentifier:
		b.store.SetFlow(node, b.currentFlow)
		b.checkContextualIdentifier(node)
	case ast.KindThisKeyword, ast.KindSuperKeyword:
		if kind == ast.KindThisKeyword {
			b.seenThisKeyword = true
		}
		b.store.SetFlow(node, b.currentFlow)
	case ast.KindQualifiedName:
		if b.currentFlow != nil && ast.IsPartOfTypeQuery(b.store.At(node)) {
			b.store.SetFlow(node, b.currentFlow)
		}
	case ast.KindMetaProperty:
		b.store.SetFlow(node, b.currentFlow)
	case ast.KindPrivateIdentifier:
		b.checkPrivateIdentifier(node)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		if b.currentFlow != nil && isNarrowableReference(b.store.At(node)) {
			b.store.SetFlow(node, b.currentFlow)
		}
	case ast.KindBinaryExpression:
		switch ast.GetAssignmentDeclarationKind(b.store.At(node)) {
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
		b.store.SetFlow(node, b.currentFlow)
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
		b.bindPropertyOrMethodOrAccessor(node, ast.SymbolFlagsMethod|getOptionalSymbolFlagForNode(b.store.At(node)), core.IfElse(ast.IsObjectLiteralMethod(b.store.At(node)), ast.SymbolFlagsValue, ast.SymbolFlagsMethodExcludes))
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
		switch ast.GetAssignmentDeclarationKind(b.store.At(node)) {
		case ast.JSDeclarationKindObjectDefinePropertyValue:
			b.bindExpandoPropertyAssignment(node)
		case ast.JSDeclarationKindObjectDefinePropertyExports:
			b.bindExportsOrObjectDefineProperty(node)
		}
		if b.store.FlagsAt(node)&ast.NodeFlagsJavaScriptFile != 0 {
			b.bindCallExpression(node)
		}
	case ast.KindTypeAliasDeclaration:
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
	case ast.KindJSTypeAliasDeclaration:
		// Top-level JSTypeAliasDeclaration nodes are processed in bindContainer
		if b.store.KindAt(b.blockScopeContainer) != ast.KindSourceFile {
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
	thisNodeOrAnySubnodesHasError := b.store.FlagsAt(node)&ast.NodeFlagsThisNodeHasError != 0
	if kind > ast.KindLastToken {
		saveSeenParseError := b.seenParseError
		b.seenParseError = false
		containerFlags := GetContainerFlags(b.store.At(node))
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
		b.store.SetFlagsAt(node, b.store.FlagsAt(node)|ast.NodeFlagsThisNodeOrAnySubNodesHasError)
		b.seenParseError = true
	}
	return false
}

func (b *Binder) bindPropertyWorker(node ast.NodeRef) {
	isAutoAccessor := ast.IsAutoAccessorPropertyDeclaration(b.store.At(node))
	includes := core.IfElse(isAutoAccessor, ast.SymbolFlagsAccessor, ast.SymbolFlagsProperty)
	excludes := core.IfElse(isAutoAccessor, ast.SymbolFlagsAccessorExcludes, ast.SymbolFlagsPropertyExcludes)
	b.bindPropertyOrMethodOrAccessor(node, includes|getOptionalSymbolFlagForNode(b.store.At(node)), excludes)
}

func (b *Binder) bindSourceFileIfExternalModule() {
	_, root := b.file.ParseTreeRef()
	b.setExportContextFlag(root)
	if ast.IsExternalOrCommonJSModule(b.file) {
		b.bindSourceFileAsExternalModule()
	} else if ast.IsJsonSourceFile(b.file) {
		b.bindSourceFileAsExternalModule()
		// Create symbol equivalent for the module.exports = {}
		originalSymbol := b.file.Symbol
		b.declareSymbol(ast.GetSymbolTable(&b.file.Symbol.Exports), b.file.Symbol, root, ast.SymbolFlagsProperty, ast.SymbolFlagsAll)
		b.store.SetSymbol(root, originalSymbol)
		b.file.Symbol = originalSymbol
	}
}

func (b *Binder) bindSourceFileAsExternalModule() {
	_, root := b.file.ParseTreeRef()
	b.bindAnonymousDeclaration(root, ast.SymbolFlagsValueModule, "\""+tspath.RemoveFileExtension(b.file.FileName())+"\"")
}

func (b *Binder) bindModuleDeclaration(node ast.NodeRef) {
	b.setExportContextFlag(node)
	if ast.IsAmbientModule(b.store.At(node)) {
		if ast.HasSyntacticModifier(b.store.At(node), ast.ModifierFlagsExport) {
			b.errorOnFirstToken(node, diagnostics.X_export_modifier_cannot_be_applied_to_ambient_modules_and_module_augmentations_since_they_are_always_visible)
		}
		if ast.IsModuleAugmentationExternal(b.store.At(node)) {
			b.declareModuleSymbol(node)
		} else {
			module := b.store.AccessModuleDeclaration(node)
			name := module.Name
			symbol := b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsValueModule, ast.SymbolFlagsValueModuleExcludes)

			if b.store.KindAt(name) == ast.KindStringLiteral {
				pattern := core.TryParsePattern(b.store.TextAt(name))
				if !pattern.IsValid() {
					// An invalid pattern - must have multiple wildcards.
					b.errorOnFirstToken(name, diagnostics.Pattern_0_can_have_at_most_one_Asterisk_character, b.store.TextAt(name))
				} else if pattern.StarIndex >= 0 {
					b.file.PatternAmbientModules = append(b.file.PatternAmbientModules, &ast.PatternAmbientModule{Pattern: pattern, Symbol: symbol})
				}
			}
		}
	} else {
		state := b.declareModuleSymbol(node)
		if state != ast.ModuleInstanceStateNonInstantiated {
			symbol := b.store.Symbol(node)
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

func (b *Binder) declareModuleSymbol(node ast.NodeRef) ast.ModuleInstanceState {
	state := ast.GetModuleInstanceState(b.store.At(node))
	instantiated := state != ast.ModuleInstanceStateNonInstantiated
	b.declareSymbolAndAddToSymbolTable(node, core.IfElse(instantiated, ast.SymbolFlagsValueModule, ast.SymbolFlagsNamespaceModule), core.IfElse(instantiated, ast.SymbolFlagsValueModuleExcludes, ast.SymbolFlagsNamespaceModuleExcludes))
	return state
}

func (b *Binder) bindNamespaceExportDeclaration(node ast.NodeRef) {
	if b.modifiersRefGenerated(node, b.store.KindAt(node)) != ast.NoListRef {
		b.errorOnNode(node, diagnostics.Modifiers_cannot_appear_here)
	}
	switch {
	case b.store.KindAt(b.store.ParentRef(node)) != ast.KindSourceFile:
		b.errorOnNode(node, diagnostics.Global_module_exports_may_only_appear_at_top_level)
	case !ast.IsExternalModule(b.file):
		b.errorOnNode(node, diagnostics.Global_module_exports_may_only_appear_in_module_files)
	case !b.file.IsDeclarationFile:
		b.errorOnNode(node, diagnostics.Global_module_exports_may_only_appear_in_declaration_files)
	default:
		b.declareSymbol(ast.GetSymbolTable(&b.file.GlobalExports), b.file.Symbol, node, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	}
}

func (b *Binder) bindImportClause(node ast.NodeRef) {
	if b.store.AccessImportClause(node).Name != ast.NoNodeRef {
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	}
}

func (b *Binder) bindExportDeclaration(node ast.NodeRef) {
	decl := b.store.AccessExportDeclaration(node)
	if b.store.Symbol(b.container) == nil {
		// Export * in some sort of block construct
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsExportStar, b.getDeclarationName(node))
	} else if decl.ExportClause == ast.NoNodeRef {
		// All export * declarations are collected in an __export symbol
		b.declareSymbol(ast.GetExports(b.store.Symbol(b.container)), b.store.Symbol(b.container), node, ast.SymbolFlagsExportStar, ast.SymbolFlagsNone)
	} else if b.store.KindAt(decl.ExportClause) == ast.KindNamespaceExport {
		b.declareSymbol(ast.GetExports(b.store.Symbol(b.container)), b.store.Symbol(b.container), decl.ExportClause, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	}
}

func (b *Binder) bindExportAssignment(node ast.NodeRef) {
	container := b.container
	if b.store.Symbol(container) == nil && b.store.KindAt(node) == ast.KindExportAssignment {
		// Incorrect export assignment in some sort of block construct
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsValue, b.getDeclarationName(node))
	} else {
		// If there is an `export default x;` alias declaration, can't `export default` anything else.
		// (In contrast, you can still have `export default function f() {}` and `export default interface I {}`.)
		flags := core.IfElse(ast.ExpressionIsAlias(b.store.At(b.store.AccessExportAssignment(node).Expression)), ast.SymbolFlagsAlias, ast.SymbolFlagsProperty)
		symbol := b.declareSymbol(ast.GetExports(b.store.Symbol(container)), b.store.Symbol(container), node, flags, ast.SymbolFlagsAll)
		if b.isExportEqualsRefGenerated(node, b.store.KindAt(node)) {
			// Ensure export assignments have a ValueDeclaration set.
			SetValueDeclaration(symbol, b.store.At(node))
		}
	}
}

func (b *Binder) bindJsxAttributes(node ast.NodeRef) {
	b.bindAnonymousDeclaration(node, ast.SymbolFlagsObjectLiteral, ast.InternalSymbolNameJSXAttributes)
}

func (b *Binder) bindJsxAttribute(node ast.NodeRef, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	b.declareSymbolAndAddToSymbolTable(node, symbolFlags, symbolExcludes)
}

func (b *Binder) setExportContextFlag(node ast.NodeRef) {
	// A declaration source file or ambient module declaration that contains no export declarations (but possibly regular
	// declarations with export modifiers) is an export context in which declarations are implicitly exported.
	flags := b.store.FlagsAt(node)
	if flags&ast.NodeFlagsAmbient != 0 && !b.hasExportDeclarations(node) {
		b.store.SetFlagsAt(node, flags|ast.NodeFlagsExportContext)
	} else {
		b.store.SetFlagsAt(node, flags&^ast.NodeFlagsExportContext)
	}
}

func (b *Binder) hasExportDeclarations(node ast.NodeRef) bool {
	var statements ast.ListRef
	switch b.store.KindAt(node) {
	case ast.KindSourceFile:
		statements = b.store.AccessSourceFile(node).Statements
	case ast.KindModuleDeclaration:
		body := b.store.AccessModuleDeclaration(node).Body
		if body != ast.NoNodeRef && b.store.KindAt(body) == ast.KindModuleBlock {
			statements = b.store.AccessModuleBlock(body).Statements
		}
	}
	for i, count := 0, b.store.ListLen(statements); i < count; i++ {
		s := b.store.ListElem(statements, i)
		if kind := b.store.KindAt(s); kind == ast.KindExportDeclaration || kind == ast.KindExportAssignment {
			return true
		}
	}
	return false
}

func (b *Binder) bindFunctionExpression(node ast.NodeRef) {
	if !b.file.IsDeclarationFile && b.store.FlagsAt(node)&ast.NodeFlagsAmbient == 0 && ast.IsAsyncFunction(b.store.At(node)) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	b.store.SetFlow(node, b.currentFlow)
	bindingName := ast.InternalSymbolNameFunction
	if b.store.KindAt(node) == ast.KindFunctionExpression && b.store.AccessFunctionExpression(node).Name != ast.NoNodeRef {
		b.checkStrictModeFunctionName(node)
		bindingName = b.store.TextAt(b.store.AccessFunctionExpression(node).Name)
	}
	b.bindAnonymousDeclaration(node, ast.SymbolFlagsFunction, bindingName)
}

func (b *Binder) bindCallExpression(node ast.NodeRef) {
	// We're only inspecting call expressions to detect CommonJS modules, so we can skip
	// this check if we've already seen the module indicator
	if b.file.CommonJSModuleIndicator.IsNil() && ast.IsRequireCall(b.store.At(node), false /*requireStringLiteralLikeArgument*/) {
		b.setCommonJSModuleIndicator(node)
	}
}

func (b *Binder) setCommonJSModuleIndicator(node ast.NodeRef) bool {
	_, root := b.file.ParseTreeRef()
	if !b.file.ExternalModuleIndicator.IsNil() && b.file.ExternalModuleIndicator != b.store.At(root) {
		return false
	}
	if b.file.CommonJSModuleIndicator.IsNil() {
		b.file.CommonJSModuleIndicator = b.store.At(node)
		if b.file.ExternalModuleIndicator.IsNil() {
			b.bindSourceFileAsExternalModule()
		}
	}
	return true
}

func (b *Binder) bindClassLikeDeclaration(node ast.NodeRef) {
	kind := b.store.KindAt(node)
	name := b.nameRefGenerated(node, kind)
	switch kind {
	case ast.KindClassDeclaration:
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsClass, ast.SymbolFlagsClassExcludes)
	case ast.KindClassExpression:
		nameText := ast.InternalSymbolNameClass
		if name != ast.NoNodeRef {
			nameText = b.store.TextAt(name)
		}
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsClass, nameText)
	}
	symbol := b.store.Symbol(node)
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
	symbolExport := ast.GetExports(symbol)[prototypeSymbol.Name]
	if symbolExport != nil {
		declaration := symbolExport.Declarations[0]
		debug.Assert(declaration.Store() == b.store)
		b.errorOnNode(declaration.Ref(), diagnostics.Duplicate_identifier_0, ast.SymbolName(prototypeSymbol))
	}
	ast.GetExports(symbol)[prototypeSymbol.Name] = prototypeSymbol
	prototypeSymbol.Parent = symbol
}

func (b *Binder) bindPropertyOrMethodOrAccessor(node ast.NodeRef, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	if !b.file.IsDeclarationFile && b.store.FlagsAt(node)&ast.NodeFlagsAmbient == 0 && ast.IsAsyncFunction(b.store.At(node)) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	if b.currentFlow != nil && ast.IsObjectLiteralOrClassExpressionMethodOrAccessor(b.store.At(node)) {
		b.store.SetFlow(node, b.currentFlow)
	}
	if ast.HasDynamicName(b.store.At(node)) {
		b.bindAnonymousDeclaration(node, symbolFlags, ast.InternalSymbolNameComputed)
	} else {
		b.declareSymbolAndAddToSymbolTable(node, symbolFlags, symbolExcludes)
	}
}

func (b *Binder) bindFunctionOrConstructorType(node ast.NodeRef) {
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
	typeLiteralSymbol.Members = make(ast.SymbolTable)
	typeLiteralSymbol.Members[symbol.Name] = symbol
}

func (b *Binder) addLateBoundAssignmentDeclarationToSymbol(node ast.NodeRef, symbol *ast.Symbol) {
	exports := ast.GetExports(symbol)
	assignmentSymbol := exports[ast.InternalSymbolNameAssignmentDeclaration]
	if assignmentSymbol == nil {
		assignmentSymbol = b.newSymbol(ast.SymbolFlagsNone, ast.InternalSymbolNameAssignmentDeclaration)
		exports[ast.InternalSymbolNameAssignmentDeclaration] = assignmentSymbol
	}
	assignmentSymbol.Declarations = append(assignmentSymbol.Declarations, b.store.At(node))
}

func (b *Binder) bindModuleExportsAssignment(node ast.NodeRef) {
	if b.setCommonJSModuleIndicator(node) {
		_, container := b.file.ParseTreeRef()
		flags := core.IfElse(ast.ExpressionIsAlias(b.store.At(b.store.AccessBinaryExpression(node).Right)), ast.SymbolFlagsAlias, ast.SymbolFlagsProperty)
		symbol := b.declareSymbol(ast.GetExports(b.store.Symbol(container)), b.store.Symbol(container), node, flags, 0)
		SetValueDeclaration(symbol, b.store.At(node))
	}
}

func (b *Binder) bindExpandoPropertyAssignment(node ast.NodeRef) {
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
func (b *Binder) bindCommonJSTypeExports(moduleSymbol *ast.Symbol) {
	moduleExports := moduleSymbol.Exports
	if exportEquals := moduleExports[ast.InternalSymbolNameExportEquals]; exportEquals != nil {
		for _, symbol := range moduleExports {
			if symbol.Name != ast.InternalSymbolNameExportEquals && symbol.Flags&(ast.SymbolFlagsType|ast.SymbolFlagsNamespace) != 0 {
				ast.GetExports(exportEquals)[symbol.Name] = symbol
				exportEquals.Flags |= ast.SymbolFlagsNamespaceModule
			}
		}
	}
}

func (b *Binder) bindDeferredExpandoAssignment(node ast.NodeRef) {
	parentHandle := getParentOfPropertyAssignment(b.store.At(node))
	debug.Assert(parentHandle.IsNil() || parentHandle.Store() == b.store)
	parent := parentHandle.Ref()
	symbol := b.lookupEntity(parent, b.blockScopeContainer)
	if symbol == nil {
		symbol = b.lookupEntity(parent, b.container)
	}
	if symbol = getInitializerSymbol(symbol); symbol != nil {
		if ast.HasDynamicName(b.store.At(node)) {
			b.bindAnonymousDeclaration(node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.InternalSymbolNameComputed)
			b.addLateBoundAssignmentDeclarationToSymbol(node, symbol)
		} else {
			// We declare expandos only when there are no non-expando declarations for that name.
			exports := ast.GetExports(symbol)
			if existing := exports[b.getDeclarationName(node)]; existing == nil || existing.Flags&ast.SymbolFlagsAssignment != 0 {
				b.declareSymbol(exports, symbol, node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.SymbolFlagsPropertyExcludes)
			}
		}
	}
}

func getParentOfPropertyAssignment(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindBinaryExpression:
		return node.BinaryExpressionLeft().Expression()
	case ast.KindCallExpression:
		return node.Store().ListAt(node.CallExpressionArguments(), 0)
	}
	panic("Unhandled case in getParentOfPropertyAssignment")
}

func (b *Binder) bindExportsOrObjectDefineProperty(node ast.NodeRef) {
	if b.setCommonJSModuleIndicator(node) {
		_, container := b.file.ParseTreeRef()
		flags := core.IfElse(b.store.KindAt(node) == ast.KindBinaryExpression && ast.ExpressionIsAlias(b.store.At(b.store.AccessBinaryExpression(node).Right)), ast.SymbolFlagsAlias, ast.SymbolFlagsFunctionScopedVariable)
		b.declareSymbol(ast.GetExports(b.store.Symbol(container)), b.store.Symbol(container), node, flags, ast.SymbolFlagsFunctionScopedVariableExcludes)
	}
}

func getInitializerSymbol(symbol *ast.Symbol) *ast.Symbol {
	if symbol == nil || symbol.ValueDeclaration.IsNil() {
		return nil
	}
	declaration := symbol.ValueDeclaration
	// For an assignment 'fn.xxx = ...', where 'fn' is a previously declared function or a previously
	// declared const variable initialized with a function expression or arrow function, we add expando
	// property declarations to the function's symbol. This also applies to class expressions in JS files,
	// and empty object literals in JS files when the declaration doesn't have a type annotation.
	switch {
	case ast.IsFunctionDeclaration(declaration) || ast.IsInJSFile(declaration) && ast.IsClassDeclaration(declaration):
		return symbol
	case ast.IsVariableDeclaration(declaration) &&
		(declaration.Parent().Flags()&ast.NodeFlagsConst != 0 || ast.IsInJSFile(declaration)):
		initializer := declaration.Initializer()
		if ast.IsExpandoInitializer(declaration, initializer) {
			return initializer.Symbol()
		}
	case ast.IsBinaryExpression(declaration) && ast.IsInJSFile(declaration):
		initializer := declaration.BinaryExpressionRight()
		if ast.IsExpandoInitializer(declaration, initializer) {
			return initializer.Symbol()
		}
	}
	return nil
}

func (b *Binder) bindThisPropertyAssignment(node ast.NodeRef) {
	if b.store.FlagsAt(node)&ast.NodeFlagsJavaScriptFile == 0 {
		return
	}
	bin := b.store.AccessBinaryExpression(node)
	if b.store.KindAt(bin.Left) == ast.KindPropertyAccessExpression && b.store.KindAt(b.store.AccessPropertyAccessExpression(bin.Left).Name) == ast.KindPrivateIdentifier ||
		b.thisContainer == ast.NoNodeRef {
		return
	}
	if classSymbol, symbolTable := b.getThisClassAndSymbolTable(); symbolTable != nil {
		if ast.HasDynamicName(b.store.At(node)) {
			b.declareSymbolEx(symbolTable, classSymbol, node, ast.SymbolFlagsProperty, ast.SymbolFlagsNone, true /*isReplaceableByMethod*/, true /*isComputedName*/)
			b.addLateBoundAssignmentDeclarationToSymbol(node, classSymbol)
		} else {
			b.declareSymbolEx(symbolTable, classSymbol, node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.SymbolFlagsNone, true /*isReplaceableByMethod*/, false /*isComputedName*/)
		}
	} else if b.store.KindAt(b.thisContainer) != ast.KindFunctionDeclaration && b.store.KindAt(b.thisContainer) != ast.KindFunctionExpression {
		// !!! constructor functions
		panic("Unhandled case in bindThisPropertyAssignment: " + b.store.KindAt(b.thisContainer).String())
	}
}

func (b *Binder) getThisClassAndSymbolTable() (classSymbol *ast.Symbol, symbolTable ast.SymbolTable) {
	if b.thisContainer == ast.NoNodeRef {
		return nil, nil
	}
	kind := b.store.KindAt(b.thisContainer)
	switch kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		// !!! constructor functions
	case ast.KindConstructor, ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindClassStaticBlockDeclaration:
		// this.property assignment in class member -- bind to the containing class
		classSymbol = b.store.Symbol(b.store.ParentRef(b.thisContainer))
		if ast.IsStatic(b.store.At(b.thisContainer)) {
			symbolTable = ast.GetExports(classSymbol)
		} else {
			symbolTable = ast.GetMembers(classSymbol)
		}
	}
	return classSymbol, symbolTable
}

func (b *Binder) bindEnumDeclaration(node ast.NodeRef) {
	if ast.IsEnumConst(b.store.At(node)) {
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsConstEnum, ast.SymbolFlagsConstEnumExcludes)
	} else {
		b.bindBlockScopedDeclaration(node, ast.SymbolFlagsRegularEnum, ast.SymbolFlagsRegularEnumExcludes)
	}
}

func (b *Binder) bindVariableDeclarationOrBindingElement(node ast.NodeRef) {
	name := b.nameRefGenerated(node, b.store.KindAt(node))
	b.checkStrictModeEvalOrArguments(node, name)
	if name != ast.NoNodeRef && b.store.KindAt(name) != ast.KindObjectBindingPattern && b.store.KindAt(name) != ast.KindArrayBindingPattern {
		switch {
		case ast.IsVariableDeclarationInitializedToRequire(b.store.At(node)):
			b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
		case ast.IsBlockOrCatchScoped(b.store.At(node)):
			b.bindBlockScopedDeclaration(node, ast.SymbolFlagsBlockScopedVariable, ast.SymbolFlagsBlockScopedVariableExcludes)
		case ast.IsPartOfParameterDeclaration(b.store.At(node)):
			// Parent links are set before descending, so this query sees the complete parameter chain.
			b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsParameterExcludes)
		default:
			b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsFunctionScopedVariableExcludes)
		}
	}
}
func (b *Binder) bindParameter(node ast.NodeRef) {
	parameter := b.store.AccessParameter(node)
	if b.store.FlagsAt(node)&ast.NodeFlagsAmbient == 0 {
		// It is a SyntaxError if eval or arguments appears in a strict-mode parameter list.
		b.checkStrictModeEvalOrArguments(node, parameter.Name)
	}
	if b.store.KindAt(parameter.Name) == ast.KindObjectBindingPattern || b.store.KindAt(parameter.Name) == ast.KindArrayBindingPattern {
		parent := b.store.ParentRef(node)
		parameters := b.parametersRefGenerated(parent, b.store.KindAt(parent))
		index := -1
		for i, count := 0, b.store.ListLen(parameters); i < count; i++ {
			if b.store.ListElem(parameters, i) == node {
				index = i
				break
			}
		}
		b.bindAnonymousDeclaration(node, ast.SymbolFlagsFunctionScopedVariable, "__"+strconv.Itoa(index))
	} else {
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsParameterExcludes)
	}
	// If this is a property-parameter, also declare the property symbol in the containing class.
	parent := b.store.ParentRef(node)
	if b.store.KindAt(parent) == ast.KindConstructor && b.store.FlagsAt(node)&ast.NodeFlagsHasParameterPropertyModifier != 0 {
		classDeclaration := b.store.ParentRef(parent)
		flags := ast.SymbolFlagsProperty | core.IfElse(parameter.Question != ast.NoNodeRef, ast.SymbolFlagsOptional, ast.SymbolFlagsNone)
		classSymbol := b.store.Symbol(classDeclaration)
		b.declareSymbol(ast.GetMembers(classSymbol), classSymbol, node, flags, ast.SymbolFlagsPropertyExcludes)
	}
}
func (b *Binder) bindFunctionDeclaration(node ast.NodeRef) {
	if !b.file.IsDeclarationFile && b.store.FlagsAt(node)&ast.NodeFlagsAmbient == 0 && ast.IsAsyncFunction(b.store.At(node)) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	b.checkStrictModeFunctionName(node)
	b.bindBlockScopedDeclaration(node, ast.SymbolFlagsFunction, ast.SymbolFlagsFunctionExcludes)
}

func (b *Binder) getInferTypeContainer(node ast.NodeRef) ast.NodeRef {
	extendsType := ast.FindAncestor(b.store.At(node), func(n ast.Handle) bool {
		parent := n.Parent()
		return !parent.IsNil() && parent.Kind == ast.KindConditionalType && parent.ConditionalTypeNodeExtendsType() == n
	})
	if extendsType.IsNil() {
		return ast.NoNodeRef
	}
	parent := extendsType.Parent()
	if parent.IsNil() {
		return ast.NoNodeRef
	}
	debug.Assert(parent.Store() == b.store)
	return parent.Ref()
}
func (b *Binder) bindAnonymousDeclaration(node ast.NodeRef, symbolFlags ast.SymbolFlags, name string) {
	symbol := b.newSymbol(symbolFlags, name)
	if symbolFlags&(ast.SymbolFlagsEnumMember|ast.SymbolFlagsClassMember) != 0 {
		symbol.Parent = b.store.Symbol(b.container)
	}
	b.addDeclarationToSymbol(symbol, node, symbolFlags)
}

func (b *Binder) bindBlockScopedDeclaration(node ast.NodeRef, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	switch b.store.KindAt(b.blockScopeContainer) {
	case ast.KindModuleDeclaration:
		b.declareModuleMember(node, symbolFlags, symbolExcludes)
	case ast.KindSourceFile:
		if ast.IsExternalOrCommonJSModule(b.file) {
			b.declareModuleMember(node, symbolFlags, symbolExcludes)
			break
		}
		fallthrough
	default:
		b.declareSymbol(b.getLocals(b.blockScopeContainer), nil /*parent*/, node, symbolFlags, symbolExcludes)
	}
}
func (b *Binder) bindTypeParameter(node ast.NodeRef) {
	parent := b.store.ParentRef(node)
	if b.store.KindAt(parent) == ast.KindInferType {
		container := b.getInferTypeContainer(parent)
		if container != ast.NoNodeRef {
			b.declareSymbol(b.getLocals(container), nil /*parent*/, node, ast.SymbolFlagsTypeParameter, ast.SymbolFlagsTypeParameterExcludes)
		} else {
			b.bindAnonymousDeclaration(node, ast.SymbolFlagsTypeParameter, b.getDeclarationName(node))
		}
	} else {
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsTypeParameter, ast.SymbolFlagsTypeParameterExcludes)
	}
}
func (b *Binder) lookupEntity(node ast.NodeRef, container ast.NodeRef) *ast.Symbol {
	kind := b.store.KindAt(node)
	if kind == ast.KindIdentifier {
		return b.lookupName(b.store.TextAt(node), container)
	}
	nodeHandle := b.store.At(node)
	expression := nodeHandle.Expression()
	if expression.IsNil() {
		return nil
	}
	debug.Assert(expression.Store() == b.store)
	if expression.Kind == ast.KindThisKeyword {
		if _, symbolTable := b.getThisClassAndSymbolTable(); symbolTable != nil {
			if name := ast.GetElementOrPropertyAccessName(nodeHandle); !name.IsNil() {
				return symbolTable[name.Text()]
			}
		}
		return nil
	}
	if symbol := getInitializerSymbol(b.lookupEntity(expression.Ref(), container)); symbol != nil && symbol.Exports != nil {
		if name := ast.GetElementOrPropertyAccessName(nodeHandle); !name.IsNil() {
			return symbol.Exports[name.Text()]
		}
	}
	return nil
}
func (b *Binder) lookupName(name string, container ast.NodeRef) *ast.Symbol {
	if locals := b.store.Locals(container); locals != nil {
		if local := locals[name]; local != nil {
			return core.OrElse(local.ExportSymbol, local)
		}
	}
	if declarationSymbol := b.store.Symbol(container); declarationSymbol != nil {
		return declarationSymbol.Exports[name]
	}
	return nil
}
func (b *Binder) checkContextualIdentifier(node ast.NodeRef) {
	if len(b.file.Diagnostics()) == 0 && b.store.FlagsAt(node)&(ast.NodeFlagsAmbient|ast.NodeFlagsJSDoc) == 0 && !ast.IsIdentifierName(b.store.At(node)) {
		originalKeywordKind := scanner.GetIdentifierToken(b.store.TextAt(node))
		if originalKeywordKind == ast.KindIdentifier {
			return
		}
		if originalKeywordKind >= ast.KindFirstFutureReservedWord && originalKeywordKind <= ast.KindLastFutureReservedWord {
			b.errorOnNode(node, b.getStrictModeIdentifierMessage(node), scanner.DeclarationNameToString(b.store.At(node)))
		} else if originalKeywordKind == ast.KindAwaitKeyword {
			if ast.IsExternalModule(b.file) && ast.IsInTopLevelContext(b.store.At(node)) {
				b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_at_the_top_level_of_a_module, scanner.DeclarationNameToString(b.store.At(node)))
			} else if b.store.FlagsAt(node)&ast.NodeFlagsAwaitContext != 0 {
				b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, scanner.DeclarationNameToString(b.store.At(node)))
			}
		} else if originalKeywordKind == ast.KindYieldKeyword && b.store.FlagsAt(node)&ast.NodeFlagsYieldContext != 0 {
			b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, scanner.DeclarationNameToString(b.store.At(node)))
		}
	}
}
func (b *Binder) checkPrivateIdentifier(node ast.NodeRef) {
	if b.store.TextAt(node) == "#constructor" && len(b.file.Diagnostics()) == 0 {
		b.errorOnNode(node, diagnostics.X_constructor_is_a_reserved_word, scanner.DeclarationNameToString(b.store.At(node)))
	}
}
func (b *Binder) getStrictModeIdentifierMessage(node ast.NodeRef) *diagnostics.Message {
	if !ast.GetContainingClass(b.store.At(node)).IsNil() {
		return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode_Class_definitions_are_automatically_in_strict_mode
	}
	if !b.file.ExternalModuleIndicator.IsNil() {
		return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode
}
func isUseStrictPrologueDirective(sourceFile *ast.SourceFile, node ast.Handle) bool {
	nodeText := scanner.GetSourceTextOfNodeFromSourceFile(sourceFile, node.Expression(), false /*includeTrivia*/)
	// Note: the node text must be exactly "use strict" or 'use strict'.  It is not ok for the
	// string to contain unicode escapes (as per ES5).
	return nodeText == "\"use strict\"" || nodeText == "'use strict'"
}

func FindUseStrictPrologue(sourceFile *ast.SourceFile, statements []ast.Handle) ast.Handle {
	for _, statement := range statements {
		if ast.IsPrologueDirective(statement) {
			if isUseStrictPrologueDirective(sourceFile, statement) {
				return statement
			}
		} else {
			return ast.Handle{}
		}
	}
	return ast.Handle{}
}
func (b *Binder) checkStrictModeFunctionName(node ast.NodeRef) {
	if b.store.FlagsAt(node)&ast.NodeFlagsAmbient == 0 {
		b.checkStrictModeEvalOrArguments(node, b.nameRefGenerated(node, b.store.KindAt(node)))
	}
}
func (b *Binder) getStrictModeBlockScopeFunctionDeclarationMessage(node ast.NodeRef) *diagnostics.Message {
	if !ast.GetContainingClass(b.store.At(node)).IsNil() {
		return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5_Class_definitions_are_automatically_in_strict_mode
	}
	if !b.file.ExternalModuleIndicator.IsNil() {
		return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5
}
func (b *Binder) checkStrictModeBinaryExpression(node ast.NodeRef) {
	expr := b.store.AccessBinaryExpression(node)
	if ast.IsLeftHandSideExpression(b.store.At(expr.Left)) && ast.IsAssignmentOperator(b.store.At(expr.Operator).Kind) {
		b.checkStrictModeEvalOrArguments(node, expr.Left)
	}
}
func (b *Binder) checkStrictModeCatchClause(node ast.NodeRef) {
	clause := b.store.AccessCatchClause(node)
	if clause.VariableDeclaration != ast.NoNodeRef {
		name := b.nameRefGenerated(clause.VariableDeclaration, b.store.KindAt(clause.VariableDeclaration))
		b.checkStrictModeEvalOrArguments(node, name)
	}
}
func (b *Binder) checkStrictModeDeleteExpression(node ast.NodeRef) {
	expression := b.store.AccessDeleteExpression(node).Expression
	if b.store.KindAt(expression) == ast.KindIdentifier {
		b.errorOnNode(expression, diagnostics.X_delete_cannot_be_called_on_an_identifier_in_strict_mode)
	}
}
func (b *Binder) checkStrictModePostfixUnaryExpression(node ast.NodeRef) {
	b.checkStrictModeEvalOrArguments(node, b.store.AccessPostfixUnaryExpression(node).Operand)
}
func (b *Binder) checkStrictModePrefixUnaryExpression(node ast.NodeRef) {
	expr := b.store.AccessPrefixUnaryExpression(node)
	operator := b.store.At(node).PrefixUnaryExpressionOperator()
	if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
		b.checkStrictModeEvalOrArguments(node, expr.Operand)
	}
}
func (b *Binder) checkStrictModeWithStatement(node ast.NodeRef) {
	// Grammar checking for withStatement
	b.errorOnFirstToken(node, diagnostics.X_with_statements_are_not_allowed_in_strict_mode)
}

func (b *Binder) checkStrictModeLabeledStatement(node ast.NodeRef) {
	statement := b.store.AccessLabeledStatement(node)
	kind := b.store.KindAt(statement.Statement)
	switch kind {
	case ast.KindFunctionDeclaration, ast.KindMissingDeclaration, ast.KindClassDeclaration,
		ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration,
		ast.KindEnumDeclaration, ast.KindModuleDeclaration, ast.KindImportDeclaration,
		ast.KindJSImportDeclaration, ast.KindImportEqualsDeclaration, ast.KindExportDeclaration,
		ast.KindExportAssignment, ast.KindNamespaceExportDeclaration, ast.KindVariableStatement:
		b.errorOnFirstToken(statement.Label, diagnostics.A_label_is_not_allowed_here)
	}
}
func isEvalOrArgumentsIdentifier(node ast.Handle) bool {
	if node.Kind == ast.KindIdentifier {
		text := node.Text()
		return text == "eval" || text == "arguments"
	}
	return false
}
func (b *Binder) checkStrictModeEvalOrArguments(contextNode ast.NodeRef, name ast.NodeRef) {
	if name != ast.NoNodeRef && isEvalOrArgumentsIdentifier(b.store.At(name)) {
		b.errorOnNode(name, b.getStrictModeEvalOrArgumentsMessage(contextNode), b.store.TextAt(name))
	}
}
func (b *Binder) getStrictModeEvalOrArgumentsMessage(node ast.NodeRef) *diagnostics.Message {
	if !ast.GetContainingClass(b.store.At(node)).IsNil() {
		return diagnostics.Code_contained_in_a_class_is_evaluated_in_JavaScript_s_strict_mode_which_does_not_allow_this_use_of_0_For_more_information_see_https_Colon_Slash_Slashdeveloper_mozilla_org_Slashen_US_Slashdocs_SlashWeb_SlashJavaScript_SlashReference_SlashStrict_mode
	}
	if !b.file.ExternalModuleIndicator.IsNil() {
		return diagnostics.Invalid_use_of_0_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Invalid_use_of_0_in_strict_mode
}
func (b *Binder) bindContainer(node ast.NodeRef, containerFlags ContainerFlags) {
	kind := b.store.KindAt(node)
	// Before we recurse into a node's children, we first save the existing parent, container
	// and block-container.  Then after we pop out of processing the children, we restore
	// these saved values.
	saveContainer := b.container
	saveThisContainer := b.thisContainer
	savedBlockScopeContainer := b.blockScopeContainer
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
			!ast.HasSyntacticModifier(b.store.At(node), ast.ModifierFlagsAsync) &&
			!isGeneratorFunctionExpression(b.store.At(node)) &&
			!ast.GetImmediatelyInvokedFunctionExpression(b.store.At(node)).IsNil()) || b.store.KindAt(node) == ast.KindClassStaticBlockDeclaration
		// A non-async, non-generator IIFE is considered part of the containing control flow. Return statements behave
		// similarly to break statements that exit to a label just past the statement body.
		if !isImmediatelyInvoked {
			flowStart := b.newFlowNode(ast.FlowFlagsStart)
			b.currentFlow = flowStart
			if containerFlags&(ContainerFlagsIsFunctionExpression|ContainerFlagsIsObjectLiteralOrClassExpressionMethodOrAccessor) != 0 {
				flowStart.Node = b.store.At(node)
			}
		}
		// We create a return control flow graph for IIFEs and constructors. For constructors
		// we use the return control flow graph in strict property initialization checks.
		if isImmediatelyInvoked || b.store.KindAt(node) == ast.KindConstructor {
			b.currentReturnTarget = b.newFlowNode(ast.FlowFlagsBranchLabel)
		} else {
			b.currentReturnTarget = nil
		}
		b.currentExceptionTarget = nil
		b.currentBreakTarget = nil
		b.currentContinueTarget = nil
		b.activeLabelList = nil
		b.hasExplicitReturn = false
		b.seenThisKeyword = false
		b.bindChildren(node)
		// Reset flags (for incremental scenarios)
		b.store.SetFlagsAt(node, b.store.FlagsAt(node)&^(ast.NodeFlagsReachabilityAndEmitFlags|ast.NodeFlagsContainsThis))
		if b.currentFlow.Flags&ast.FlowFlagsUnreachable == 0 && containerFlags&ContainerFlagsIsFunctionLike != 0 {
			body := b.bodyRefGenerated(node, kind)
			bodyLoc := b.store.LocAt(body)
			// A static block has a Body child but does not embed upstream BodyBase.
			if kind != ast.KindClassStaticBlockDeclaration && body != ast.NoNodeRef && !(bodyLoc.Pos() == bodyLoc.End() && bodyLoc.Pos() >= 0 && b.store.KindAt(body) != ast.KindEndOfFile) {
				b.store.SetFlagsAt(node, b.store.FlagsAt(node)|(ast.NodeFlagsHasImplicitReturn))
				if b.hasExplicitReturn {
					b.store.SetFlagsAt(node, b.store.FlagsAt(node)|(ast.NodeFlagsHasExplicitReturn))
				}
				b.store.SetEndFlow(node, b.currentFlow)
			}
		}
		if b.seenThisKeyword {
			b.store.SetFlagsAt(node, b.store.FlagsAt(node)|(ast.NodeFlagsContainsThis))
		}
		if b.store.KindAt(node) == ast.KindSourceFile {
			b.store.SetFlagsAt(node, b.store.FlagsAt(node)|(b.emitFlags))
		}
		if b.currentReturnTarget != nil {
			b.addAntecedent(b.currentReturnTarget, b.currentFlow)
			b.currentFlow = b.finishFlowLabel(b.currentReturnTarget)
			if b.store.KindAt(node) == ast.KindConstructor || b.store.KindAt(node) == ast.KindClassStaticBlockDeclaration {
				b.store.SetReturnFlow(node, b.currentFlow)
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
			b.store.SetFlagsAt(node, b.store.FlagsAt(node)|(ast.NodeFlagsContainsThis))
		} else {
			b.store.SetFlagsAt(node, b.store.FlagsAt(node)&^(ast.NodeFlagsContainsThis))
		}
		b.seenThisKeyword = saveSeenThisKeyword
	} else {
		b.bindChildren(node)
	}
	if b.store.KindAt(node) == ast.KindSourceFile && b.store.FlagsAt(node)&ast.NodeFlagsJavaScriptFile != 0 {
		// Binding of top-level JSTypeAliasDeclaration nodes is deferred to ensure CommonJS module
		// indicators, if any, are processed first.
		statements := b.store.AccessSourceFile(node).Statements
		for i, n := 0, b.store.ListLen(statements); i < n; i++ {
			statement := b.store.ListElem(statements, i)
			if b.store.KindAt(statement) == ast.KindJSTypeAliasDeclaration {
				b.bindBlockScopedDeclaration(statement, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
			}
		}
		if !b.file.CommonJSModuleIndicator.IsNil() {
			b.declareCommonJSVariable("module")
			b.declareCommonJSVariable("exports")
		}
	}
	if b.store.KindAt(node) == ast.KindSourceFile && ast.IsExternalOrCommonJSModule(b.file) || ast.IsAmbientModule(b.store.At(node)) {
		b.bindCommonJSTypeExports(b.store.Symbol(node))
	}
	b.container = saveContainer
	b.thisContainer = saveThisContainer
	b.blockScopeContainer = savedBlockScopeContainer
}

func (b *Binder) declareCommonJSVariable(name string) {
	_, root := b.file.ParseTreeRef()
	locals := b.getLocals(root)
	if locals[name] == nil {
		symbol := b.newSymbol(ast.SymbolFlagsFunctionScopedVariable|ast.SymbolFlagsModuleExports, name)
		symbol.Declarations = b.newSingleDeclaration(b.store.At(root))
		symbol.ValueDeclaration = symbol.Declarations[0]
		if name == "module" {
			exportsProperty := b.newSymbol(ast.SymbolFlagsModuleExports|ast.SymbolFlagsProperty, "exports")
			exportsProperty.Declarations = symbol.Declarations
			exportsProperty.ValueDeclaration = symbol.ValueDeclaration
			exportsProperty.Parent = symbol
			symbol.Members = make(ast.SymbolTable, 1)
			symbol.Members["exports"] = exportsProperty
		}
		locals[name] = symbol
	}
}

func (b *Binder) bindChildren(node ast.NodeRef) {
	saveInAssignmentPattern := b.inAssignmentPattern
	// Most nodes aren't valid in an assignment pattern, so we clear the value here
	// and set it before we descend into nodes that could actually be part of an assignment pattern.
	b.inAssignmentPattern = false

	if b.currentFlow == b.unreachableFlow {
		if b.store.Flow(node) != nil {
			b.store.SetFlow(node, nil)
		}
		if ast.IsPotentiallyExecutableNode(b.store.At(node)) {
			b.store.SetFlagsAt(node, b.store.FlagsAt(node)|(ast.NodeFlagsUnreachable))
		}
		b.bindEachChild(node)
		b.inAssignmentPattern = saveInAssignmentPattern
		return
	}

	if ast.KindFirstStatement <= b.store.KindAt(node) && b.store.KindAt(node) <= ast.KindLastStatement {
		b.store.SetFlow(node, b.currentFlow)
	}

	switch b.store.KindAt(node) {
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
		if ast.IsDestructuringAssignment(b.store.At(node)) {
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
		sourceFile := b.store.AccessSourceFile(node)
		b.bindEachStatementFunctionsFirst(sourceFile.Statements)
		b.bind(sourceFile.EndOfFileToken)
	case ast.KindBlock, ast.KindModuleBlock:
		b.bindEachStatementFunctionsFirst(b.statementsRefGenerated(node, b.store.KindAt(node)))
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

func (b *Binder) bindEachChild(node ast.NodeRef) {
	b.forEachBindChildGenerated(node, b.store.KindAt(node))
}

func (b *Binder) bindEach(nodes ast.ListRef) {
	for i, n := 0, b.store.ListLen(nodes); i < n; i++ {
		b.bind(b.store.ListElem(nodes, i))
	}
}

func (b *Binder) bindNodeList(nodeList ast.ListRef) {
	b.bindEach(nodeList)
}

func (b *Binder) bindModifiers(modifiers ast.ListRef) {
	b.bindEach(modifiers)
}

func (b *Binder) bindEachStatementFunctionsFirst(statements ast.ListRef) {
	for i, n := 0, b.store.ListLen(statements); i < n; i++ {
		node := b.store.ListElem(statements, i)
		if b.store.KindAt(node) == ast.KindFunctionDeclaration {
			b.bind(node)
		}
	}
	for i, n := 0, b.store.ListLen(statements); i < n; i++ {
		node := b.store.ListElem(statements, i)
		if b.store.KindAt(node) != ast.KindFunctionDeclaration {
			b.bind(node)
		}
	}
}

func (b *Binder) setContinueTarget(node ast.NodeRef, target *ast.FlowLabel) *ast.FlowLabel {
	label := b.activeLabelList
	for label != nil && b.store.KindAt(b.store.ParentRef(node)) == ast.KindLabeledStatement {
		label.continueTarget = target
		label = label.next
		node = b.store.ParentRef(node)
	}
	return target
}

func (b *Binder) doWithConditionalBranches(action func(b *Binder, value ast.NodeRef) bool, value ast.NodeRef, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	savedTrueTarget := b.currentTrueTarget
	savedFalseTarget := b.currentFalseTarget
	b.currentTrueTarget = trueTarget
	b.currentFalseTarget = falseTarget
	action(b, value)
	b.currentTrueTarget = savedTrueTarget
	b.currentFalseTarget = savedFalseTarget
}

func (b *Binder) bindCondition(node ast.NodeRef, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	b.doWithConditionalBranches((*Binder).bind, node, trueTarget, falseTarget)
	if node == ast.NoNodeRef {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
		return
	}
	kind := b.store.KindAt(node)
	isOptionalChain := b.store.FlagsAt(node)&ast.NodeFlagsOptionalChain != 0 && (kind == ast.KindPropertyAccessExpression || kind == ast.KindElementAccessExpression || kind == ast.KindCallExpression || kind == ast.KindNonNullExpression)
	if !isLogicalAssignmentExpression(b.store.At(node)) && !ast.IsLogicalExpression(b.store.At(node)) && !(isOptionalChain && ast.IsOutermostOptionalChain(b.store.At(node))) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}

func (b *Binder) bindIterativeStatement(node ast.NodeRef, breakTarget *ast.FlowLabel, continueTarget *ast.FlowLabel) {
	saveBreakTarget := b.currentBreakTarget
	saveContinueTarget := b.currentContinueTarget
	b.currentBreakTarget = breakTarget
	b.currentContinueTarget = continueTarget
	b.bind(node)
	b.currentBreakTarget = saveBreakTarget
	b.currentContinueTarget = saveContinueTarget
}

func isLogicalAssignmentExpression(node ast.Handle) bool {
	return ast.IsLogicalOrCoalescingAssignmentExpression(ast.SkipParentheses(node))
}

func (b *Binder) bindAssignmentTargetFlow(node ast.NodeRef) {
	switch b.store.KindAt(node) {
	case ast.KindArrayLiteralExpression:
		elements := b.store.AccessArrayLiteralExpression(node).Elements
		for i := 0; i < b.store.ListLen(elements); i++ {
			element := b.store.ListRefAt(elements, i)
			if b.store.KindAt(element) == ast.KindSpreadElement {
				b.bindAssignmentTargetFlow(b.store.AccessSpreadElement(element).Expression)
			} else {
				b.bindDestructuringTargetFlow(element)
			}
		}
	case ast.KindObjectLiteralExpression:
		properties := b.store.AccessObjectLiteralExpression(node).Properties
		for i := 0; i < b.store.ListLen(properties); i++ {
			property := b.store.ListRefAt(properties, i)
			switch b.store.KindAt(property) {
			case ast.KindPropertyAssignment:
				b.bindDestructuringTargetFlow(b.store.AccessPropertyAssignment(property).Initializer)
			case ast.KindShorthandPropertyAssignment:
				b.bindAssignmentTargetFlow(b.store.AccessShorthandPropertyAssignment(property).Name)
			case ast.KindSpreadAssignment:
				b.bindAssignmentTargetFlow(b.store.AccessSpreadAssignment(property).Expression)
			}
		}
	default:
		if isNarrowableReference(b.store.At(node)) {
			b.currentFlow = b.createFlowMutation(ast.FlowFlagsAssignment, b.currentFlow, node)
		}
	}
}

func (b *Binder) bindDestructuringTargetFlow(node ast.NodeRef) {
	if b.store.KindAt(node) == ast.KindBinaryExpression {
		expr := b.store.AccessBinaryExpression(node)
		if b.store.KindAt(expr.Operator) == ast.KindEqualsToken {
			b.bindAssignmentTargetFlow(expr.Left)
			return
		}
		b.bindAssignmentTargetFlow(node)
	} else {
		b.bindAssignmentTargetFlow(node)
	}
}

func (b *Binder) bindWhileStatement(node ast.NodeRef) {
	stmt := b.store.AccessWhileStatement(node)
	preWhileLabel := b.setContinueTarget(node, b.createLoopLabel())
	preBodyLabel := b.createBranchLabel()
	postWhileLabel := b.createBranchLabel()
	b.addAntecedent(preWhileLabel, b.currentFlow)
	b.currentFlow = preWhileLabel
	b.bindCondition(stmt.Expression, preBodyLabel, postWhileLabel)
	b.currentFlow = b.finishFlowLabel(preBodyLabel)
	b.bindIterativeStatement(stmt.Statement, postWhileLabel, preWhileLabel)
	b.addAntecedent(preWhileLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postWhileLabel)
}

func (b *Binder) bindDoStatement(node ast.NodeRef) {
	stmt := b.store.AccessDoStatement(node)
	preDoLabel := b.createLoopLabel()
	preConditionLabel := b.setContinueTarget(node, b.createBranchLabel())
	postDoLabel := b.createBranchLabel()
	b.addAntecedent(preDoLabel, b.currentFlow)
	b.currentFlow = preDoLabel
	b.bindIterativeStatement(stmt.Statement, postDoLabel, preConditionLabel)
	b.addAntecedent(preConditionLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(preConditionLabel)
	b.bindCondition(stmt.Expression, preDoLabel, postDoLabel)
	b.currentFlow = b.finishFlowLabel(postDoLabel)
}

func (b *Binder) bindForStatement(node ast.NodeRef) {
	stmt := b.store.AccessForStatement(node)
	b.bind(stmt.Initializer)
	if b.currentFlow == b.unreachableFlow {
		// Unlike while/do, the for-loop initializer is bound inside this function before the loop's
		// flow graph is constructed. If it makes flow unreachable (e.g. a throwing IIFE), addAntecedent
		// will filter out the unreachable entry to preLoopLabel, leaving only the back-edge from the
		// incrementor. This creates a cycle with no exit that crashes isReachableFlowNodeWorker.
		// Bail out early and just bind the remaining children with unreachable flow.
		b.bind(stmt.Condition)
		b.bind(stmt.Statement)
		b.bind(stmt.Incrementor)
		return
	}
	preLoopLabel := b.setContinueTarget(node, b.createLoopLabel())
	preBodyLabel := b.createBranchLabel()
	preIncrementorLabel := b.createBranchLabel()
	postLoopLabel := b.createBranchLabel()
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = preLoopLabel
	b.bindCondition(stmt.Condition, preBodyLabel, postLoopLabel)
	b.currentFlow = b.finishFlowLabel(preBodyLabel)
	b.bindIterativeStatement(stmt.Statement, postLoopLabel, preIncrementorLabel)
	b.addAntecedent(preIncrementorLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(preIncrementorLabel)
	b.bind(stmt.Incrementor)
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postLoopLabel)
}

func (b *Binder) bindForInOrForOfStatement(node ast.NodeRef) {
	stmt := b.store.AccessForInOrOfStatement(node)
	b.bind(stmt.Expression)
	if b.currentFlow == b.unreachableFlow {
		// Like the for-loop initializer, the for-in/for-of expression is bound before the loop's
		// flow graph is constructed. If it makes flow unreachable (e.g. a throwing IIFE), addAntecedent
		// will filter out the unreachable entry to preLoopLabel, leaving only the back-edge from the
		// loop body. This creates a cycle with no exit that crashes isReachableFlowNodeWorker.
		// Bail out early and just bind the remaining children with unreachable flow.
		b.bind(stmt.Initializer)
		b.bind(stmt.Statement)
		return
	}
	preLoopLabel := b.setContinueTarget(node, b.createLoopLabel())
	postLoopLabel := b.createBranchLabel()
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = preLoopLabel
	if b.store.KindAt(node) == ast.KindForOfStatement {
		b.bind(stmt.AwaitModifier)
	}
	b.addAntecedent(postLoopLabel, b.currentFlow)
	b.bind(stmt.Initializer)
	if b.store.KindAt(stmt.Initializer) != ast.KindVariableDeclarationList {
		b.bindAssignmentTargetFlow(stmt.Initializer)
	}
	b.bindIterativeStatement(stmt.Statement, postLoopLabel, preLoopLabel)
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postLoopLabel)
}

func (b *Binder) bindIfStatement(node ast.NodeRef) {
	stmt := b.store.AccessIfStatement(node)
	thenLabel := b.createBranchLabel()
	elseLabel := b.createBranchLabel()
	postIfLabel := b.createBranchLabel()
	b.bindCondition(stmt.Condition, thenLabel, elseLabel)
	b.currentFlow = b.finishFlowLabel(thenLabel)
	b.bind(stmt.ThenStatement)
	b.addAntecedent(postIfLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(elseLabel)
	b.bind(stmt.ElseStatement)
	b.addAntecedent(postIfLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postIfLabel)
}

func (b *Binder) bindReturnStatement(node ast.NodeRef) {
	b.bind(b.store.AccessReturnStatement(node).Expression)
	if b.currentReturnTarget != nil {
		b.addAntecedent(b.currentReturnTarget, b.currentFlow)
	}
	b.currentFlow = b.unreachableFlow
	b.hasExplicitReturn = true
	b.hasFlowEffects = true
}

func (b *Binder) bindThrowStatement(node ast.NodeRef) {
	b.bind(b.store.AccessThrowStatement(node).Expression)
	b.currentFlow = b.unreachableFlow
	b.hasFlowEffects = true
}

func (b *Binder) bindBreakStatement(node ast.NodeRef) {
	b.bindBreakOrContinueStatement(b.store.AccessBreakStatement(node).Label, b.currentBreakTarget, (*ActiveLabel).BreakTarget)
}

func (b *Binder) bindContinueStatement(node ast.NodeRef) {
	b.bindBreakOrContinueStatement(b.store.AccessContinueStatement(node).Label, b.currentContinueTarget, (*ActiveLabel).ContinueTarget)
}

func (b *Binder) bindBreakOrContinueStatement(label ast.NodeRef, currentTarget *ast.FlowNode, getTarget func(*ActiveLabel) *ast.FlowNode) {
	b.bind(label)
	if label != ast.NoNodeRef {
		activeLabel := b.findActiveLabel(b.store.TextAt(label))
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

func (b *Binder) bindBreakOrContinueFlow(flowLabel *ast.FlowLabel) {
	if flowLabel != nil {
		b.addAntecedent(flowLabel, b.currentFlow)
		b.currentFlow = b.unreachableFlow
		b.hasFlowEffects = true
	}
}

func (b *Binder) bindTryStatement(node ast.NodeRef) {
	// We conservatively assume that *any* code in the try block can cause an exception, but we only need
	// to track code that causes mutations (because only mutations widen the possible control flow type of
	// a variable). The exceptionLabel is the target label for control flows that result from exceptions.
	// We add all mutation flow nodes as antecedents of this label such that we can analyze them as possible
	// antecedents of the start of catch or finally blocks. Furthermore, we add the current control flow to
	// represent exceptions that occur before any mutations.
	stmt := b.store.AccessTryStatement(node)
	saveReturnTarget := b.currentReturnTarget
	saveExceptionTarget := b.currentExceptionTarget
	normalExitLabel := b.createBranchLabel()
	returnLabel := b.createBranchLabel()
	exceptionLabel := b.createBranchLabel()
	if stmt.FinallyBlock != ast.NoNodeRef {
		b.currentReturnTarget = returnLabel
	}
	b.addAntecedent(exceptionLabel, b.currentFlow)
	b.currentExceptionTarget = exceptionLabel
	b.bind(stmt.TryBlock)
	b.addAntecedent(normalExitLabel, b.currentFlow)
	if stmt.CatchClause != ast.NoNodeRef {
		// Start of catch clause is the target of exceptions from try block.
		b.currentFlow = b.finishFlowLabel(exceptionLabel)
		// The currentExceptionTarget now represents control flows from exceptions in the catch clause.
		// Effectively, in a try-catch-finally, if an exception occurs in the try block, the catch block
		// acts like a second try block.
		exceptionLabel = b.createBranchLabel()
		b.addAntecedent(exceptionLabel, b.currentFlow)
		b.currentExceptionTarget = exceptionLabel
		b.bind(stmt.CatchClause)
		b.addAntecedent(normalExitLabel, b.currentFlow)
	}
	b.currentReturnTarget = saveReturnTarget
	b.currentExceptionTarget = saveExceptionTarget
	if stmt.FinallyBlock != ast.NoNodeRef {
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
		finallyLabel.Antecedents = b.combineFlowLists(normalExitLabel.Antecedents, b.combineFlowLists(exceptionLabel.Antecedents, returnLabel.Antecedents))
		b.currentFlow = finallyLabel
		b.bind(stmt.FinallyBlock)
		if b.currentFlow.Flags&ast.FlowFlagsUnreachable != 0 {
			// If the end of the finally block is unreachable, the end of the entire try statement is unreachable.
			b.currentFlow = b.unreachableFlow
		} else {
			// If we have an IIFE return target and return statements in the try or catch blocks, add a control
			// flow that goes back through the finally block and back through only the return statements.
			if b.currentReturnTarget != nil && returnLabel.Antecedents != nil {
				b.addAntecedent(b.currentReturnTarget, b.createReduceLabel(finallyLabel, returnLabel.Antecedents, b.currentFlow))
			}
			// If we have an outer exception target (i.e. a containing try-finally or try-catch-finally), add a
			// control flow that goes back through the finally block and back through each possible exception source.
			if b.currentExceptionTarget != nil && exceptionLabel.Antecedents != nil {
				b.addAntecedent(b.currentExceptionTarget, b.createReduceLabel(finallyLabel, exceptionLabel.Antecedents, b.currentFlow))
			}
			// If the end of the finally block is reachable, but the end of the try and catch blocks are not,
			// convert the current flow to unreachable. For example, 'try { return 1; } finally { ... }' should
			// result in an unreachable current control flow.
			if normalExitLabel.Antecedents != nil {
				b.currentFlow = b.createReduceLabel(finallyLabel, normalExitLabel.Antecedents, b.currentFlow)
			} else {
				b.currentFlow = b.unreachableFlow
			}
		}
	} else {
		b.currentFlow = b.finishFlowLabel(normalExitLabel)
	}
}

func (b *Binder) bindSwitchStatement(node ast.NodeRef) {
	stmt := b.store.AccessSwitchStatement(node)
	postSwitchLabel := b.createBranchLabel()
	b.bind(stmt.Expression)
	saveBreakTarget := b.currentBreakTarget
	savePreSwitchCaseFlow := b.preSwitchCaseFlow
	b.currentBreakTarget = postSwitchLabel
	b.preSwitchCaseFlow = b.currentFlow
	b.bind(stmt.CaseBlock)
	b.addAntecedent(postSwitchLabel, b.currentFlow)
	clauses := b.store.AccessCaseBlock(stmt.CaseBlock).Clauses
	hasDefault := false
	for i := 0; i < b.store.ListLen(clauses); i++ {
		if b.store.KindAt(b.store.ListRefAt(clauses, i)) == ast.KindDefaultClause {
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

func (b *Binder) bindCaseBlock(node ast.NodeRef) {
	switchStatement := b.store.ParentRef(node)
	switchExpression := b.store.AccessSwitchStatement(switchStatement).Expression
	clauses := b.store.AccessCaseBlock(node).Clauses
	isNarrowingSwitch := b.store.KindAt(switchExpression) == ast.KindTrueKeyword || isNarrowingExpression(b.store.At(switchExpression))
	var fallthroughFlow *ast.FlowNode = b.unreachableFlow
	for i := 0; i < b.store.ListLen(clauses); i++ {
		clauseStart := i
		clause := b.store.ListRefAt(clauses, i)
		for b.store.ListLen(b.store.AccessCaseOrDefaultClause(clause).Statements) == 0 && i+1 < b.store.ListLen(clauses) {
			if fallthroughFlow == b.unreachableFlow {
				b.currentFlow = b.preSwitchCaseFlow
			}
			b.bind(clause)
			i++
			clause = b.store.ListRefAt(clauses, i)
		}
		preCaseLabel := b.createBranchLabel()
		preCaseFlow := b.preSwitchCaseFlow
		if isNarrowingSwitch {
			preCaseFlow = b.createFlowSwitchClause(b.preSwitchCaseFlow, switchStatement, clauseStart, i+1)
		}
		b.addAntecedent(preCaseLabel, preCaseFlow)
		b.addAntecedent(preCaseLabel, fallthroughFlow)
		b.currentFlow = b.finishFlowLabel(preCaseLabel)
		b.bind(clause)
		fallthroughFlow = b.currentFlow
		if b.currentFlow.Flags&ast.FlowFlagsUnreachable == 0 && i != b.store.ListLen(clauses)-1 {
			b.store.At(clause).SetFallthroughFlowNode(b.currentFlow)
		}
	}
}

func (b *Binder) bindCaseOrDefaultClause(node ast.NodeRef) {
	clause := b.store.AccessCaseOrDefaultClause(node)
	if clause.Expression != ast.NoNodeRef {
		saveCurrentFlow := b.currentFlow
		b.currentFlow = b.preSwitchCaseFlow
		b.bind(clause.Expression)
		b.currentFlow = saveCurrentFlow
	}
	b.bindEach(clause.Statements)
}

func (b *Binder) bindExpressionStatement(node ast.NodeRef) {
	stmt := b.store.AccessExpressionStatement(node)
	b.bind(stmt.Expression)
	b.maybeBindExpressionFlowIfCall(stmt.Expression)
}

func (b *Binder) maybeBindExpressionFlowIfCall(node ast.NodeRef) {
	// A top level or comma expression call expression with a dotted function name and at least one argument
	// is potentially an assertion and is therefore included in the control flow.
	if b.store.KindAt(node) == ast.KindCallExpression {
		expression := b.store.AccessCallExpression(node).Expression
		if b.store.KindAt(expression) != ast.KindSuperKeyword && ast.IsDottedName(b.store.At(expression)) {
			b.currentFlow = b.createFlowCall(b.currentFlow, node)
		}
	}
}

func (b *Binder) bindLabeledStatement(node ast.NodeRef) {
	stmt := b.store.AccessLabeledStatement(node)
	postStatementLabel := b.createBranchLabel()
	b.activeLabelList = &ActiveLabel{
		next:           b.activeLabelList,
		name:           b.store.TextAt(stmt.Label),
		breakTarget:    postStatementLabel,
		continueTarget: nil,
		referenced:     false,
	}
	b.bind(stmt.Label)
	b.bind(stmt.Statement)
	if !b.activeLabelList.referenced {
		// Mark the label as unused; the checker will decide whether to report it
		b.store.SetFlagsAt(stmt.Label, b.store.FlagsAt(stmt.Label)|ast.NodeFlagsUnreachable)
	}
	b.activeLabelList = b.activeLabelList.next
	b.addAntecedent(postStatementLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postStatementLabel)
}

func (b *Binder) bindPrefixUnaryExpressionFlow(node ast.NodeRef) {
	expr := b.store.AccessPrefixUnaryExpression(node)
	operator := b.store.At(node).PrefixUnaryExpressionOperator()
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
			b.bindAssignmentTargetFlow(expr.Operand)
		}
	}
}

func (b *Binder) bindPostfixUnaryExpressionFlow(node ast.NodeRef) {
	expr := b.store.AccessPostfixUnaryExpression(node)
	operator := b.store.At(node).PostfixUnaryExpressionOperator()
	b.bindEachChild(node)
	if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
		b.bindAssignmentTargetFlow(expr.Operand)
	}
}

func (b *Binder) bindDestructuringAssignmentFlow(node ast.NodeRef) {
	expr := b.store.AccessBinaryExpression(node)
	if b.inAssignmentPattern {
		b.inAssignmentPattern = false
		b.bind(expr.Operator)
		b.bind(expr.Right)
		b.inAssignmentPattern = true
		b.bind(expr.Left)
		b.bind(expr.Type)
	} else {
		b.inAssignmentPattern = true
		b.bind(expr.Left)
		b.bind(expr.Type)
		b.inAssignmentPattern = false
		b.bind(expr.Operator)
		b.bind(expr.Right)
	}
	b.bindAssignmentTargetFlow(expr.Left)
}

func (b *Binder) bindBinaryExpressionFlow(node ast.NodeRef) {
	expr := b.store.AccessBinaryExpression(node)
	operator := b.store.KindAt(expr.Operator)
	if ast.IsLogicalOrCoalescingBinaryOperator(operator) || ast.IsLogicalOrCoalescingAssignmentOperator(operator) {
		if isTopLevelLogicalExpression(b.store.At(node)) {
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
		b.bind(expr.Left)
		b.bind(expr.Type)
		if operator == ast.KindCommaToken {
			b.maybeBindExpressionFlowIfCall(expr.Left)
		}
		b.bind(expr.Operator)
		b.bind(expr.Right)
		if operator == ast.KindCommaToken {
			b.maybeBindExpressionFlowIfCall(expr.Right)
		}
		if ast.IsAssignmentOperator(operator) && !ast.IsAssignmentTarget(b.store.At(node)) {
			b.bindAssignmentTargetFlow(expr.Left)
			if operator == ast.KindEqualsToken && b.store.KindAt(expr.Left) == ast.KindElementAccessExpression {
				elementAccess := b.store.AccessElementAccessExpression(expr.Left)
				if isNarrowableOperand(b.store.At(elementAccess.Expression)) {
					b.currentFlow = b.createFlowMutation(ast.FlowFlagsArrayMutation, b.currentFlow, node)
				}
			}
		}
	}
}

func (b *Binder) bindLogicalLikeExpression(node ast.NodeRef, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	expr := b.store.AccessBinaryExpression(node)
	preRightLabel := b.createBranchLabel()
	operator := b.store.KindAt(expr.Operator)
	if operator == ast.KindAmpersandAmpersandToken || operator == ast.KindAmpersandAmpersandEqualsToken {
		b.bindCondition(expr.Left, preRightLabel, falseTarget)
	} else {
		b.bindCondition(expr.Left, trueTarget, preRightLabel)
	}
	b.currentFlow = b.finishFlowLabel(preRightLabel)
	b.bind(expr.Operator)
	if ast.IsLogicalOrCoalescingAssignmentOperator(operator) {
		b.doWithConditionalBranches((*Binder).bind, expr.Right, trueTarget, falseTarget)
		b.bindAssignmentTargetFlow(expr.Left)
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	} else {
		b.bindCondition(expr.Right, trueTarget, falseTarget)
	}
}

func (b *Binder) bindDeleteExpressionFlow(node ast.NodeRef) {
	expr := b.store.AccessDeleteExpression(node)
	b.bindEachChild(node)
	if b.store.KindAt(expr.Expression) == ast.KindPropertyAccessExpression {
		b.bindAssignmentTargetFlow(expr.Expression)
	}
}

func (b *Binder) bindConditionalExpressionFlow(node ast.NodeRef) {
	expr := b.store.AccessConditionalExpression(node)
	trueLabel := b.createBranchLabel()
	falseLabel := b.createBranchLabel()
	postExpressionLabel := b.createBranchLabel()
	saveCurrentFlow := b.currentFlow
	saveHasFlowEffects := b.hasFlowEffects
	b.hasFlowEffects = false
	b.bindCondition(expr.Condition, trueLabel, falseLabel)
	b.currentFlow = b.finishFlowLabel(trueLabel)
	b.bind(expr.Question)
	b.bind(expr.WhenTrue)
	b.addAntecedent(postExpressionLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(falseLabel)
	b.bind(expr.ColonToken)
	b.bind(expr.WhenFalse)
	b.addAntecedent(postExpressionLabel, b.currentFlow)
	if b.hasFlowEffects {
		b.currentFlow = b.finishFlowLabel(postExpressionLabel)
	} else {
		b.currentFlow = saveCurrentFlow
	}
	b.hasFlowEffects = b.hasFlowEffects || saveHasFlowEffects
}

func (b *Binder) bindVariableDeclarationFlow(node ast.NodeRef) {
	b.bindEachChild(node)
	declaration := b.store.AccessVariableDeclaration(node)
	parent := b.store.ParentRef(node)
	grandparent := b.store.ParentRef(parent)
	grandparentKind := b.store.KindAt(grandparent)
	if declaration.Initializer != ast.NoNodeRef || grandparentKind == ast.KindForInStatement || grandparentKind == ast.KindForOfStatement {
		b.bindInitializedVariableFlow(node)
	}
}

func (b *Binder) bindInitializedVariableFlow(node ast.NodeRef) {
	var name ast.NodeRef
	switch b.store.KindAt(node) {
	case ast.KindVariableDeclaration:
		name = b.store.AccessVariableDeclaration(node).Name
	case ast.KindBindingElement:
		name = b.store.AccessBindingElement(node).Name
	}
	if name != ast.NoNodeRef && (b.store.KindAt(name) == ast.KindObjectBindingPattern || b.store.KindAt(name) == ast.KindArrayBindingPattern) {
		elements := b.store.AccessBindingPattern(name).Elements
		for i := 0; i < b.store.ListLen(elements); i++ {
			b.bindInitializedVariableFlow(b.store.ListRefAt(elements, i))
		}
	} else {
		b.currentFlow = b.createFlowMutation(ast.FlowFlagsAssignment, b.currentFlow, node)
	}
}

func (b *Binder) bindAccessExpressionFlow(node ast.NodeRef) {
	kind := b.store.KindAt(node)
	isOptionalChain := b.store.FlagsAt(node)&ast.NodeFlagsOptionalChain != 0 &&
		(kind == ast.KindPropertyAccessExpression || kind == ast.KindElementAccessExpression || kind == ast.KindCallExpression || kind == ast.KindNonNullExpression)
	if isOptionalChain {
		b.bindOptionalChainFlow(node)
	} else {
		b.bindEachChild(node)
	}
}

func (b *Binder) bindOptionalChainFlow(node ast.NodeRef) {
	if isTopLevelLogicalExpression(b.store.At(node)) {
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

func (b *Binder) bindOptionalChain(node ast.NodeRef, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
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
	var preChainLabel *ast.FlowLabel
	if ast.IsOptionalChainRoot(b.store.At(node)) {
		preChainLabel = b.createBranchLabel()
	}
	kind := b.store.KindAt(node)
	expression := b.expressionRefGenerated(node, kind)
	b.bindOptionalExpression(expression, core.IfElse(preChainLabel != nil, preChainLabel, trueTarget), falseTarget)
	if preChainLabel != nil {
		b.currentFlow = b.finishFlowLabel(preChainLabel)
	}
	b.doWithConditionalBranches((*Binder).bindOptionalChainRest, node, trueTarget, falseTarget)
	if ast.IsOutermostOptionalChain(b.store.At(node)) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}

func (b *Binder) bindOptionalExpression(node ast.NodeRef, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	b.doWithConditionalBranches((*Binder).bind, node, trueTarget, falseTarget)
	kind := b.store.KindAt(node)
	isOptionalChain := b.store.FlagsAt(node)&ast.NodeFlagsOptionalChain != 0 &&
		(kind == ast.KindPropertyAccessExpression || kind == ast.KindElementAccessExpression || kind == ast.KindCallExpression || kind == ast.KindNonNullExpression)
	if !isOptionalChain || ast.IsOutermostOptionalChain(b.store.At(node)) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}

func (b *Binder) bindOptionalChainRest(node ast.NodeRef) bool {
	switch b.store.KindAt(node) {
	case ast.KindPropertyAccessExpression:
		access := b.store.AccessPropertyAccessExpression(node)
		b.bind(access.QuestionDot)
		b.bind(access.Name)
	case ast.KindElementAccessExpression:
		access := b.store.AccessElementAccessExpression(node)
		b.bind(access.QuestionDot)
		b.bind(access.ArgumentExpression)
	case ast.KindCallExpression:
		call := b.store.AccessCallExpression(node)
		b.bind(call.QuestionDot)
		b.bindNodeList(call.TypeArguments)
		b.bindEach(call.Arguments)
	}
	return false
}

func (b *Binder) bindCallExpressionFlow(node ast.NodeRef) {
	call := b.store.AccessCallExpression(node)
	kind := b.store.KindAt(node)
	isOptionalChain := b.store.FlagsAt(node)&ast.NodeFlagsOptionalChain != 0 &&
		(kind == ast.KindPropertyAccessExpression || kind == ast.KindElementAccessExpression || kind == ast.KindCallExpression || kind == ast.KindNonNullExpression)
	if isOptionalChain {
		b.bindOptionalChainFlow(node)
	} else {
		// If the target of the call expression is a function expression or arrow function we have
		// an immediately invoked function expression (IIFE). Initialize the flowNode property to
		// the current control flow (which includes evaluation of the IIFE arguments).
		expr := ast.SkipParentheses(b.store.At(call.Expression))
		if expr.Kind == ast.KindFunctionExpression || expr.Kind == ast.KindArrowFunction {
			b.bindNodeList(call.TypeArguments)
			b.bindEach(call.Arguments)
			b.bind(call.Expression)
		} else {
			b.bindEachChild(node)
			if b.store.KindAt(call.Expression) == ast.KindSuperKeyword {
				b.currentFlow = b.createFlowCall(b.currentFlow, node)
			}
		}
	}
	if b.store.KindAt(call.Expression) == ast.KindPropertyAccessExpression {
		access := b.store.AccessPropertyAccessExpression(call.Expression)
		nameKind := b.store.KindAt(access.Name)
		nameText := b.store.TextAt(access.Name)
		if nameKind == ast.KindIdentifier && isNarrowableOperand(b.store.At(access.Expression)) && (nameText == "push" || nameText == "unshift") {
			b.currentFlow = b.createFlowMutation(ast.FlowFlagsArrayMutation, b.currentFlow, node)
		}
	}
}

func (b *Binder) bindNonNullExpressionFlow(node ast.NodeRef) {
	kind := b.store.KindAt(node)
	isOptionalChain := b.store.FlagsAt(node)&ast.NodeFlagsOptionalChain != 0 &&
		(kind == ast.KindPropertyAccessExpression || kind == ast.KindElementAccessExpression || kind == ast.KindCallExpression || kind == ast.KindNonNullExpression)
	if isOptionalChain {
		b.bindOptionalChainFlow(node)
	} else {
		b.bindEachChild(node)
	}
}

func (b *Binder) bindBindingElementFlow(node ast.NodeRef) {
	// When evaluating a binding pattern, the initializer is evaluated before the binding pattern, per:
	// - https://tc39.es/ecma262/#sec-destructuring-binding-patterns-runtime-semantics-iteratorbindinginitialization
	//   - `BindingElement: BindingPattern Initializer?`
	// - https://tc39.es/ecma262/#sec-runtime-semantics-keyedbindinginitialization
	//   - `BindingElement: BindingPattern Initializer?`
	elem := b.store.AccessBindingElement(node)
	b.bind(elem.Rest)
	b.bind(elem.PropertyName)
	b.bindInitializer(elem.Initializer)
	b.bind(elem.Name)
}

func (b *Binder) bindParameterFlow(node ast.NodeRef) {
	param := b.store.AccessParameter(node)
	b.bindModifiers(param.Modifiers)
	b.bind(param.Rest)
	b.bind(param.Question)
	b.bind(param.Type)
	b.bindInitializer(param.Initializer)
	b.bind(param.Name)
}

// a BindingElement/Parameter does not have side effects if initializers are not evaluated and used. (see GH#49759)
func (b *Binder) bindInitializer(node ast.NodeRef) {
	if node == ast.NoNodeRef {
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

func isGeneratorFunctionExpression(node ast.Handle) bool {
	return node.Kind == ast.KindFunctionExpression && !node.FunctionExpressionAsteriskToken().IsNil()
}
func (b *Binder) addToContainerChain(next ast.NodeRef) {
	if b.lastContainer != ast.NoNodeRef {
		b.store.SetNextContainer(b.lastContainer, next)
	}
	b.lastContainer = next
}
func (b *Binder) addDeclarationToSymbol(symbol *ast.Symbol, node ast.NodeRef, symbolFlags ast.SymbolFlags) {
	symbol.Flags |= symbolFlags
	b.store.SetSymbol(node, symbol)
	if symbol.Declarations == nil {
		symbol.Declarations = b.newSingleDeclaration(b.store.At(node))
	} else {
		symbol.Declarations = core.AppendIfUnique(symbol.Declarations, b.store.At(node))
	}
	if b.store.KindAt(node) == ast.KindSourceFile {
		b.file.Symbol = symbol
	}
	// On merge of const enum module with class or function, reset const enum only flag (namespaces will already recalculate)
	if symbol.Flags&ast.SymbolFlagsConstEnumOnlyModule != 0 && symbol.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsClass|ast.SymbolFlagsRegularEnum) != 0 {
		symbol.Flags &^= ast.SymbolFlagsConstEnumOnlyModule
		b.notConstEnumOnlyModules.Add(symbol)
	}
	if symbolFlags&ast.SymbolFlagsValue != 0 {
		SetValueDeclaration(symbol, b.store.At(node))
	}
}
func SetValueDeclaration(symbol *ast.Symbol, node ast.Handle) {
	valueDeclaration := symbol.ValueDeclaration
	if valueDeclaration.IsNil() ||
		isAssignmentDeclaration(valueDeclaration) && !isAssignmentDeclaration(node) ||
		valueDeclaration.Kind != node.Kind && isEffectiveModuleDeclaration(valueDeclaration) {
		// Non-assignment declarations take precedence over assignment declarations and
		// non-namespace declarations take precedence over namespace declarations.
		symbol.ValueDeclaration = node
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

func GetContainerFlags(node ast.Handle) ContainerFlags {
	switch node.Kind {
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
		if ast.IsObjectLiteralOrClassExpressionMethodOrAccessor(node) {
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
		if ast.IsFunctionLike(parent) || ast.IsClassStaticBlockDeclaration(parent) {
			return ContainerFlagsNone
		} else {
			return ContainerFlagsIsBlockScopedContainer | ContainerFlagsHasLocals
		}
	}
	return ContainerFlagsNone
}

func isNarrowingExpression(expr ast.Handle) bool {
	switch expr.Kind {
	case ast.KindIdentifier, ast.KindThisKeyword:
		return true
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		return containsNarrowableReference(expr)
	case ast.KindCallExpression:
		return hasNarrowableArgument(expr)
	case ast.KindParenthesizedExpression, ast.KindNonNullExpression, ast.KindTypeOfExpression:
		return isNarrowingExpression(expr.Expression())
	case ast.KindBinaryExpression:
		return isNarrowingBinaryExpression(expr)
	case ast.KindPrefixUnaryExpression:
		return expr.PrefixUnaryExpressionOperator() == ast.KindExclamationToken && isNarrowingExpression(expr.PrefixUnaryExpressionOperand())
	}
	return false
}

func containsNarrowableReference(expr ast.Handle) bool {
	if isNarrowableReference(expr) {
		return true
	}
	if expr.Flags()&ast.NodeFlagsOptionalChain != 0 {
		switch expr.Kind {
		case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression, ast.KindCallExpression, ast.KindNonNullExpression:
			return containsNarrowableReference(expr.Expression())
		}
	}
	return false
}

func isNarrowableReference(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindIdentifier, ast.KindThisKeyword, ast.KindSuperKeyword, ast.KindMetaProperty:
		return true
	case ast.KindPropertyAccessExpression, ast.KindParenthesizedExpression, ast.KindNonNullExpression:
		return isNarrowableReference(node.Expression())
	case ast.KindElementAccessExpression:
		argument := node.ArgumentExpression()
		return ast.IsStringOrNumericLiteralLike(argument) ||
			ast.IsEntityNameExpression(argument) && isNarrowableReference(node.Expression())
	case ast.KindBinaryExpression:
		operator := node.OperatorToken().Kind
		return operator == ast.KindCommaToken && isNarrowableReference(node.Right()) ||
			ast.IsAssignmentOperator(operator) && ast.IsLeftHandSideExpression(node.Left())
	}
	return false
}

func hasNarrowableArgument(expr ast.Handle) bool {
	for _, argument := range expr.ArgumentsSeq().All() {
		if containsNarrowableReference(argument) {
			return true
		}
	}
	callee := expr.Expression()
	if ast.IsPropertyAccessExpression(callee) {
		if containsNarrowableReference(callee.Expression()) {
			return true
		}
	}
	return false
}

func isNarrowingBinaryExpression(expr ast.Handle) bool {
	switch expr.OperatorToken().Kind {
	case ast.KindEqualsToken, ast.KindBarBarEqualsToken, ast.KindAmpersandAmpersandEqualsToken, ast.KindQuestionQuestionEqualsToken:
		return containsNarrowableReference(expr.Left())
	case ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken, ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken:
		left := ast.SkipParentheses(expr.Left())
		right := ast.SkipParentheses(expr.Right())
		return isNarrowableOperand(left) || isNarrowableOperand(right) ||
			isNarrowingTypeOfOperands(right, left) || isNarrowingTypeOfOperands(left, right) ||
			(ast.IsBooleanLiteral(right) && isNarrowingExpression(left) || ast.IsBooleanLiteral(left) && isNarrowingExpression(right))
	case ast.KindInstanceOfKeyword:
		return isNarrowableOperand(expr.Left())
	case ast.KindInKeyword:
		return isNarrowingExpression(expr.Right())
	case ast.KindCommaToken:
		return isNarrowingExpression(expr.Right())
	}
	return false
}

func isNarrowableOperand(expr ast.Handle) bool {
	switch expr.Kind {
	case ast.KindParenthesizedExpression:
		return isNarrowableOperand(expr.Expression())
	case ast.KindBinaryExpression:
		switch expr.OperatorToken().Kind {
		case ast.KindEqualsToken:
			return isNarrowableOperand(expr.Left())
		case ast.KindCommaToken:
			return isNarrowableOperand(expr.Right())
		}
	}
	return containsNarrowableReference(expr)
}

func isNarrowingTypeOfOperands(expr1 ast.Handle, expr2 ast.Handle) bool {
	return ast.IsTypeOfExpression(expr1) && isNarrowableOperand(expr1.Expression()) && ast.IsStringLiteralLike(expr2)
}

func (b *Binder) errorOnNode(node ast.NodeRef, message *diagnostics.Message, args ...any) {
	b.addDiagnostic(b.createDiagnosticForNode(node, message, args...))
}

func (b *Binder) errorOnFirstToken(node ast.NodeRef, message *diagnostics.Message, args ...any) {
	span := scanner.GetRangeOfTokenAtPosition(b.file, b.store.LocAt(node).Pos())
	b.addDiagnostic(ast.NewDiagnostic(b.file, span, message, args...))
}

// Inside the binder, we may create a diagnostic for an as-yet unbound node (with potentially no parent pointers, implying no accessible source file)
// If so, the node _must_ be in the current file (as that's the only way anything could have traversed to it to yield it as the error node)
// This version of `createDiagnosticForNode` uses the binder's context to account for this, and always yields correct diagnostics even in these situations.
func (b *Binder) createDiagnosticForNode(node ast.NodeRef, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	return ast.NewDiagnostic(b.file, scanner.GetErrorRangeForNode(b.file, b.store.At(node)), message, args...)
}

func (b *Binder) addDiagnostic(diagnostic *ast.Diagnostic) {
	b.file.SetBindDiagnostics(append(b.file.BindDiagnostics(), diagnostic))
}

func isSignedNumericLiteral(node ast.Handle) bool {
	if node.Kind == ast.KindPrefixUnaryExpression {
		operator := node.PrefixUnaryExpressionOperator()
		return (operator == ast.KindPlusToken || operator == ast.KindMinusToken) && ast.IsNumericLiteral(node.PrefixUnaryExpressionOperand())
	}
	return false
}

func getOptionalSymbolFlagForNode(node ast.Handle) ast.SymbolFlags {
	postfixToken := node.PostfixToken()
	return core.IfElse(!postfixToken.IsNil() && postfixToken.Kind == ast.KindQuestionToken, ast.SymbolFlagsOptional, ast.SymbolFlagsNone)
}

func isFunctionSymbol(symbol *ast.Symbol) bool {
	d := symbol.ValueDeclaration
	if !d.IsNil() {
		if ast.IsFunctionDeclaration(d) {
			return true
		}
		if ast.IsVariableDeclaration(d) {
			initializer := d.Initializer()
			return !initializer.IsNil() && ast.IsFunctionLike(initializer)
		}
	}
	return false
}

func isStatementCondition(node ast.Handle) bool {
	parent := node.Parent()
	switch parent.Kind {
	case ast.KindIfStatement, ast.KindWhileStatement, ast.KindDoStatement:
		return parent.Expression() == node
	case ast.KindForStatement:
		return parent.ForStatementCondition() == node
	case ast.KindConditionalExpression:
		return parent.ConditionalExpressionCondition() == node
	}
	return false
}

func isTopLevelLogicalExpression(node ast.Handle) bool {
	parent := node.Parent()
	for ast.IsParenthesizedExpression(parent) || ast.IsPrefixUnaryExpression(parent) && parent.PrefixUnaryExpressionOperator() == ast.KindExclamationToken {
		node = parent
		parent = node.Parent()
	}
	return !isStatementCondition(node) && !ast.IsLogicalExpression(parent) && !(ast.IsOptionalChain(parent) && parent.Expression() == node)
}

func isAssignmentDeclaration(decl ast.Handle) bool {
	return ast.IsBinaryExpression(decl) || ast.IsAccessExpression(decl) || ast.IsIdentifier(decl) || ast.IsCallExpression(decl)
}

func isEffectiveModuleDeclaration(node ast.Handle) bool {
	return ast.IsModuleDeclaration(node) || ast.IsIdentifier(node)
}
