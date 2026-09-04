package binder

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"strconv"
	"sync"
)

type ContainerFlags int32

const (
	ContainerFlagsNone                                             ContainerFlags = 0
	ContainerFlagsIsContainer                                      ContainerFlags = 1 << 0
	ContainerFlagsIsBlockScopedContainer                           ContainerFlags = 1 << 1
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
	node           ast.NodeRef
	nodeKind       ast.Kind
	container      ast.NodeRef
	containerKind  ast.Kind
	blockContainer ast.NodeRef
	blockKind      ast.Kind
}
type Binder struct {
	file                    *ast.SourceFile
	store                   *ast.Store
	unreachableFlow         *ast.FlowNode
	container               ast.NodeRef
	containerKind           ast.Kind
	thisContainer           ast.NodeRef
	thisContainerKind       ast.Kind
	blockScopeContainer     ast.NodeRef
	blockScopeContainerKind ast.Kind
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
	flowNodeArena           core.Arena[ast.FlowNode]
	flowListArena           core.Arena[ast.FlowList]
	singleDeclarationsArena core.Arena[ast.GlobalRef]
	expandoAssignments      []ExpandoAssignmentInfo
}
type ActiveLabel struct {
	next           *ActiveLabel
	breakTarget    *ast.FlowLabel
	continueTarget *ast.FlowLabel
	name           string
	referenced     bool
}

func (label *ActiveLabel) BreakTarget() *ast.FlowNode {
	return label.breakTarget
}
func (label *ActiveLabel) ContinueTarget() *ast.FlowNode {
	return label.continueTarget
}
func BindSourceFile(file *ast.SourceFile) {
	if !file.IsBound() {
		bindSourceFile(file)
	}
}

var binderPool = sync.Pool{New: func() any {
	return &Binder{}
}}

func getBinder() *Binder {
	return binderPool.Get().(*Binder)
}
func putBinder(b *Binder) {
	*b = Binder{}
	binderPool.Put(b)
}
func bindSourceFile(file *ast.SourceFile) {
	file.BindOnce(func() {
		store, root := file.ParseTreeRef()
		b := getBinder()
		defer putBinder(b)
		b.file = file
		b.store = store
		ast.RegisterFile(file)
		if b.store != nil {
			b.store.PrepareBindTables()
		}
		b.unreachableFlow = b.newFlowNode(ast.FlowFlagsUnreachable)
		b.bindRef(root, ast.KindUnknown)
		b.bindDeferredExpandoAssignments()
		file.SymbolCount = b.symbolCount
	})
}

func (b *Binder) bindRef(id ast.NodeRef, parentKind ast.Kind) bool {
	if id == 0 || b.store == nil {
		return false
	}
	return b.bindKind(id, b.store.KindAt(id), parentKind)
}

func (b *Binder) symbol(ref ast.NodeRef) *ast.Symbol {
	return b.store.Symbol(ref)
}

func (b *Binder) getLocals(ref ast.NodeRef) ast.SymbolTable {
	if ref == 0 {
		return nil
	}
	if locals := b.store.Locals(ref); locals != nil {
		return locals
	}
	locals := make(ast.SymbolTable)
	b.store.SetLocals(ref, locals)
	return locals
}

func (b *Binder) bind(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	parentKind := ast.KindUnknown
	if parent := b.store.ParentRef(node.Ref()); parent != 0 {
		parentKind = b.store.KindAt(parent)
	}
	return b.bindKind(node.Ref(), node.Kind, parentKind)
}

func (b *Binder) bindKind(id ast.NodeRef, kind ast.Kind, parentKind ast.Kind) bool {
	var node ast.Handle
	switch kind {
	case ast.KindIdentifier:
		b.store.SetFlow(id, b.currentFlow)
		node = ast.HandleOf(b.store, id, kind)
		b.checkContextualIdentifier(node)
	case ast.KindThisKeyword, ast.KindSuperKeyword:
		if kind == ast.KindThisKeyword {
			b.seenThisKeyword = true
		}
		b.store.SetFlow(id, b.currentFlow)
	case ast.KindQualifiedName:
		node = ast.HandleOf(b.store, id, kind)
		if b.currentFlow != nil && ast.IsPartOfTypeQuery(node) {
			b.store.SetFlow(id, b.currentFlow)
		}
	case ast.KindMetaProperty:
		b.store.SetFlow(id, b.currentFlow)
	case ast.KindPrivateIdentifier:
		node = ast.HandleOf(b.store, id, kind)
		b.checkPrivateIdentifier(node)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		node = ast.HandleOf(b.store, id, kind)
		if b.currentFlow != nil && isNarrowableReference(node) {
			b.store.SetFlow(id, b.currentFlow)
		}
	case ast.KindBinaryExpression:
		node = ast.HandleOf(b.store, id, kind)
		switch ast.GetAssignmentDeclarationKind(node) {
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
		node = ast.HandleOf(b.store, id, kind)
		b.checkStrictModeCatchClause(node)
	case ast.KindDeleteExpression:
		node = ast.HandleOf(b.store, id, kind)
		b.checkStrictModeDeleteExpression(node)
	case ast.KindPostfixUnaryExpression:
		node = ast.HandleOf(b.store, id, kind)
		b.checkStrictModePostfixUnaryExpression(node)
	case ast.KindPrefixUnaryExpression:
		node = ast.HandleOf(b.store, id, kind)
		b.checkStrictModePrefixUnaryExpression(node)
	case ast.KindWithStatement:
		node = ast.HandleOf(b.store, id, kind)
		b.checkStrictModeWithStatement(node)
	case ast.KindLabeledStatement:
		node = ast.HandleOf(b.store, id, kind)
		b.checkStrictModeLabeledStatement(node)
	case ast.KindThisType:
		b.seenThisKeyword = true
	case ast.KindTypeParameter:
		node = ast.HandleOf(b.store, id, kind)
		b.bindTypeParameter(node)
	case ast.KindParameter:
		b.bindParameterRef(id, kind)
	case ast.KindVariableDeclaration:
		b.bindVariableDeclarationOrBindingElementRef(id, kind)
	case ast.KindBindingElement:
		b.store.SetFlow(id, b.currentFlow)
		b.bindVariableDeclarationOrBindingElementRef(id, kind)
	case ast.KindPropertyDeclaration, ast.KindPropertySignature:
		b.bindPropertyWorkerRef(id, kind)
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment:
		b.bindPropertyOrMethodOrAccessorRef(id, kind, ast.SymbolFlagsProperty, ast.SymbolFlagsPropertyExcludes)
	case ast.KindEnumMember:
		b.bindPropertyOrMethodOrAccessorRef(id, kind, ast.SymbolFlagsEnumMember, ast.SymbolFlagsEnumMemberExcludes)
	case ast.KindCallSignature, ast.KindConstructSignature, ast.KindIndexSignature:
		b.declareSymbolAndAddToSymbolTableRef(id, kind, ast.SymbolFlagsSignature, ast.SymbolFlagsNone)
	case ast.KindMethodDeclaration, ast.KindMethodSignature:
		b.bindPropertyOrMethodOrAccessorRef(id, kind, ast.SymbolFlagsMethod|b.getOptionalSymbolFlagForRef(id, kind), core.IfElse(b.isObjectLiteralMethodRef(id, kind), ast.SymbolFlagsValue, ast.SymbolFlagsMethodExcludes))
	case ast.KindFunctionDeclaration:
		b.bindFunctionDeclarationRef(id, kind)
	case ast.KindConstructor:
		b.declareSymbolAndAddToSymbolTableRef(id, kind, ast.SymbolFlagsConstructor, ast.SymbolFlagsNone)
	case ast.KindGetAccessor:
		b.bindPropertyOrMethodOrAccessorRef(id, kind, ast.SymbolFlagsGetAccessor, ast.SymbolFlagsGetAccessorExcludes)
	case ast.KindSetAccessor:
		b.bindPropertyOrMethodOrAccessorRef(id, kind, ast.SymbolFlagsSetAccessor, ast.SymbolFlagsSetAccessorExcludes)
	case ast.KindFunctionType, ast.KindConstructorType:
		b.bindFunctionOrConstructorTypeRef(id, kind)
	case ast.KindTypeLiteral, ast.KindMappedType:
		b.bindAnonymousDeclarationRef(id, kind, ast.SymbolFlagsTypeLiteral, ast.InternalSymbolNameType)
	case ast.KindObjectLiteralExpression:
		b.bindAnonymousDeclarationRef(id, kind, ast.SymbolFlagsObjectLiteral, ast.InternalSymbolNameObject)
	case ast.KindFunctionExpression, ast.KindArrowFunction:
		b.bindFunctionExpressionRef(id, kind)
	case ast.KindClassExpression, ast.KindClassDeclaration:
		b.bindClassLikeDeclarationRef(id, kind)
	case ast.KindInterfaceDeclaration:
		b.bindBlockScopedDeclarationRef(id, kind, ast.SymbolFlagsInterface, ast.SymbolFlagsInterfaceExcludes)
	case ast.KindCallExpression:
		node = ast.HandleOf(b.store, id, kind)
		switch ast.GetAssignmentDeclarationKind(node) {
		case ast.JSDeclarationKindObjectDefinePropertyValue:
			b.bindExpandoPropertyAssignment(node)
		case ast.JSDeclarationKindObjectDefinePropertyExports:
			b.bindExportsOrObjectDefineProperty(node)
		}
		if ast.IsInJSFile(node) {
			b.bindCallExpression(node)
		}
	case ast.KindTypeAliasDeclaration:
		b.bindBlockScopedDeclarationRef(id, kind, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
	case ast.KindJSTypeAliasDeclaration:
		if b.blockScopeContainerKind != ast.KindSourceFile {
			b.bindBlockScopedDeclarationRef(id, kind, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
		}
	case ast.KindEnumDeclaration:
		b.bindEnumDeclarationRef(id, kind)
	case ast.KindModuleDeclaration:
		node = ast.HandleOf(b.store, id, kind)
		b.bindModuleDeclaration(node)
	case ast.KindImportEqualsDeclaration, ast.KindNamespaceImport, ast.KindImportSpecifier, ast.KindExportSpecifier:
		b.declareSymbolAndAddToSymbolTableRef(id, kind, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	case ast.KindNamespaceExportDeclaration:
		b.bindNamespaceExportDeclarationRef(id, kind)
	case ast.KindImportClause:
		b.bindImportClauseRef(id, kind)
	case ast.KindExportDeclaration:
		b.bindExportDeclarationRef(id, kind)
	case ast.KindExportAssignment:
		b.bindExportAssignmentRef(id, kind)
	case ast.KindSourceFile:
		b.bindSourceFileIfExternalModuleRef(id, kind)
	case ast.KindJsxAttributes:
		b.bindAnonymousDeclarationRef(id, kind, ast.SymbolFlagsObjectLiteral, ast.InternalSymbolNameJSXAttributes)
	case ast.KindJsxAttribute:
		b.declareSymbolAndAddToSymbolTableRef(id, kind, ast.SymbolFlagsProperty, ast.SymbolFlagsPropertyExcludes)
	}
	flags := b.store.FlagsAt(id)
	thisNodeOrAnySubnodesHasError := flags&ast.NodeFlagsThisNodeHasError != 0
	if kind > ast.KindLastToken {
		saveSeenParseError := b.seenParseError
		b.seenParseError = false
		containerFlags := getContainerFlagsRef(b.store, id, kind, parentKind)
		if containerFlags == ContainerFlagsNone {
			b.bindChildrenRef(id, kind)
		} else {
			b.bindContainer(id, kind, containerFlags)
		}
		if b.seenParseError {
			thisNodeOrAnySubnodesHasError = true
		}
		b.seenParseError = saveSeenParseError
	}
	if thisNodeOrAnySubnodesHasError {
		b.store.SetFlagsAt(id, b.store.FlagsAt(id)|ast.NodeFlagsThisNodeOrAnySubNodesHasError)
		b.seenParseError = true
	}
	return false
}
func (b *Binder) newSymbol(flags ast.SymbolFlags, name string) *ast.Symbol {
	b.symbolCount++
	result := b.symbolArena.New()
	result.Flags = flags
	result.Name = name
	return result
}

func (b *Binder) declareSymbol(symbolTable ast.SymbolTable, parent *ast.Symbol, node ast.Handle, includes ast.SymbolFlags, excludes ast.SymbolFlags) *ast.Symbol {
	// TODO: Evaluate any unique-declaration specialization separately from the
	// DoD representation port; this path intentionally keeps the full merge and
	// diagnostic algorithm shared by all declarations.
	return b.declareSymbolRef(symbolTable, parent, node.Ref(), node.Kind, includes, excludes, false, false)
}
func (b *Binder) declareSymbolEx(symbolTable ast.SymbolTable, parent *ast.Symbol, node ast.Handle, includes ast.SymbolFlags, excludes ast.SymbolFlags, isReplaceableByMethod bool, isComputedName bool) *ast.Symbol {
	return b.declareSymbolRef(symbolTable, parent, node.Ref(), node.Kind, includes, excludes, isReplaceableByMethod, isComputedName)
}

func (b *Binder) declareSymbolRef(symbolTable ast.SymbolTable, parent *ast.Symbol, ref ast.NodeRef, kind ast.Kind, includes ast.SymbolFlags, excludes ast.SymbolFlags, isReplaceableByMethod bool, isComputedName bool) *ast.Symbol {
	debug.Assert(isComputedName || !b.hasDynamicNameRef(ref, kind))
	isDefaultExport := b.hasSyntacticModifierRef(ref, kind, ast.ModifierFlagsDefault) || kind == ast.KindExportSpecifier && b.store.TextAt(b.nameRefGenerated(ref, kind)) == ast.InternalSymbolNameDefault
	var name string
	switch {
	case isComputedName:
		name = ast.InternalSymbolNameComputed
	case isDefaultExport && parent != nil:
		name = ast.InternalSymbolNameDefault
	default:
		name = b.getDeclarationNameRef(ref, kind)
	}
	var symbol *ast.Symbol
	if name == ast.InternalSymbolNameMissing {
		symbol = b.newSymbol(ast.SymbolFlagsNone, ast.InternalSymbolNameMissing)
	} else {
		symbol = symbolTable[name]
		if symbol == nil {
			symbol = b.newSymbol(ast.SymbolFlagsNone, name)
			symbolTable[name] = symbol
			if isReplaceableByMethod {
				symbol.Flags |= ast.SymbolFlagsReplaceableByMethod
			}
		} else if isReplaceableByMethod && symbol.Flags&ast.SymbolFlagsReplaceableByMethod == 0 {
			return symbol
		} else if symbol.Flags&excludes != 0 {
			if symbol.Flags&ast.SymbolFlagsReplaceableByMethod != 0 {
				symbol = b.newSymbol(ast.SymbolFlagsNone, name)
				symbolTable[name] = symbol
			} else if !(includes&ast.SymbolFlagsVariable != 0 && symbol.Flags&ast.SymbolFlagsAssignment != 0 || includes&ast.SymbolFlagsAssignment != 0 && symbol.Flags&ast.SymbolFlagsVariable != 0) {
				node := ast.HandleOf(b.store, ref, kind)
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
					if isDefaultExport {
						message = diagnostics.A_module_cannot_have_multiple_default_exports
						messageNeedsName = false
						multipleDefaultExports = true
					} else {
						if len(symbol.Declarations) != 0 && ast.IsExportAssignment(node) && !node.ExportAssignmentIsExportEquals() {
							message = diagnostics.A_module_cannot_have_multiple_default_exports
							messageNeedsName = false
							multipleDefaultExports = true
						}
					}
				}
				var declarationName ast.Handle = ast.GetNameOfDeclaration(node)
				if declarationName.IsNil() {
					declarationName = node
				}
				var diag *ast.Diagnostic
				if messageNeedsName {
					diag = b.createDiagnosticForNode(declarationName, message, b.getDisplayName(node))
				} else {
					diag = b.createDiagnosticForNode(declarationName, message)
				}
				if ast.IsTypeAliasDeclaration(node) && ast.NodeIsMissing(node.Type()) && ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) && symbol.Flags&(ast.SymbolFlagsAlias|ast.SymbolFlagsType|ast.SymbolFlagsNamespace) != 0 {
					diag.AddRelatedInfo(b.createDiagnosticForNode(node, diagnostics.Did_you_mean_0, "export type { "+node.TypeAliasDeclarationName().Text()+" }"))
				}
				for index, declarationRef := range symbol.Declarations {
					declaration := ast.NodeOf(declarationRef)
					var decl ast.Handle = ast.GetNameOfDeclaration(declaration)
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
				if symbol.Flags&ast.SymbolFlagsAccessor != 0 && symbol.Flags&ast.SymbolFlagsAccessor != includes&ast.SymbolFlagsAccessor {
					symbol.Flags |= ast.SymbolFlagsAccessor
				}
				symbol = b.newSymbol(ast.SymbolFlagsNone, name)
			}
		}
	}
	b.addDeclarationToSymbolRef(symbol, ref, kind, includes)
	if symbol.Parent == nil {
		symbol.Parent = parent
	} else if symbol.Parent != parent {
		panic("Existing symbol parent should match new one")
	}
	return symbol
}

func (b *Binder) getDeclarationName(node ast.Handle) string {
	return b.getDeclarationNameRef(node.Ref(), node.Kind)
}

func (b *Binder) getDeclarationNameRef(ref ast.NodeRef, kind ast.Kind) string {
	if kind == ast.KindExportAssignment {
		node := ast.HandleOf(b.store, ref, kind)
		return core.IfElse(node.ExportAssignmentIsExportEquals(), ast.InternalSymbolNameExportEquals, ast.InternalSymbolNameDefault)
	}
	nameRef, nameKind := b.nameOfDeclarationRef(ref, kind)
	if nameRef != 0 {
		if b.isAmbientModuleRef(ref, kind) {
			node := ast.HandleOf(b.store, ref, kind)
			moduleName := b.store.TextAt(nameRef)
			if ast.IsGlobalScopeAugmentation(node) {
				return ast.InternalSymbolNameGlobal
			}
			return "\"" + moduleName + "\""
		}
		if nameKind == ast.KindPrivateIdentifier {
			node := ast.HandleOf(b.store, ref, kind)
			name := ast.HandleOf(b.store, nameRef, nameKind)
			containingClass := ast.GetContainingClass(node)
			if containingClass.IsNil() {
				return ast.InternalSymbolNameMissing
			}
			return GetSymbolNameForPrivateIdentifier(containingClass.Symbol(), name.Text())
		}
		if isPropertyNameLiteralKind(nameKind) {
			return b.store.TextAt(nameRef)
		}
		if nameKind == ast.KindJsxNamespacedName {
			return ast.HandleOf(b.store, nameRef, nameKind).Text()
		}
		if nameKind == ast.KindComputedPropertyName {
			name := ast.HandleOf(b.store, nameRef, nameKind)
			nameExpression := name.Expression()
			if ast.IsStringOrNumericLiteralLike(nameExpression) {
				return nameExpression.Text()
			}
			if ast.IsSignedNumericLiteral(nameExpression) {
				unaryExpression := nameExpression
				return scanner.TokenToString(unaryExpression.PrefixUnaryExpressionOperator()) + unaryExpression.PrefixUnaryExpressionOperand().Text()
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

func (b *Binder) modifierFlagsRef(ref ast.NodeRef, kind ast.Kind) ast.ModifierFlags {
	list := b.modifiersRefGenerated(ref, kind)
	var flags ast.ModifierFlags
	for i, n := 0, b.store.ListLen(list); i < n; i++ {
		if modifier := b.store.ListElem(list, i); modifier != 0 {
			flags |= ast.ModifierToFlag(b.store.KindAt(modifier))
		}
	}
	return flags
}

func (b *Binder) hasSyntacticModifierRef(ref ast.NodeRef, kind ast.Kind, flags ast.ModifierFlags) bool {
	return b.modifierFlagsRef(ref, kind)&flags != 0
}

func (b *Binder) combinedModifierFlagsRef(ref ast.NodeRef, kind ast.Kind) ast.ModifierFlags {
	ref, kind = b.rootDeclarationRef(ref, kind)
	flags := b.modifierFlagsRef(ref, kind)
	if kind == ast.KindVariableDeclaration {
		ref = b.store.ParentRef(ref)
		kind = b.store.KindAt(ref)
	}
	if ref != 0 && kind == ast.KindVariableDeclarationList {
		flags |= b.modifierFlagsRef(ref, kind)
		ref = b.store.ParentRef(ref)
		kind = b.store.KindAt(ref)
	}
	if ref != 0 && kind == ast.KindVariableStatement {
		flags |= b.modifierFlagsRef(ref, kind)
	}
	return flags
}

func (b *Binder) rootDeclarationRef(ref ast.NodeRef, kind ast.Kind) (ast.NodeRef, ast.Kind) {
	for kind == ast.KindBindingElement {
		parent := b.store.ParentRef(ref)
		ref = b.store.ParentRef(parent)
		kind = b.store.KindAt(ref)
	}
	return ref, kind
}

func (b *Binder) combinedNodeFlagsRef(ref ast.NodeRef, kind ast.Kind) ast.NodeFlags {
	ref, kind = b.rootDeclarationRef(ref, kind)
	flags := b.store.FlagsAt(ref)
	if kind == ast.KindVariableDeclaration {
		ref = b.store.ParentRef(ref)
		kind = b.store.KindAt(ref)
	}
	if ref != 0 && kind == ast.KindVariableDeclarationList {
		flags |= b.store.FlagsAt(ref)
		ref = b.store.ParentRef(ref)
		kind = b.store.KindAt(ref)
	}
	if ref != 0 && kind == ast.KindVariableStatement {
		flags |= b.store.FlagsAt(ref)
	}
	return flags
}

func (b *Binder) isImplicitlyExportedJSDocDeclarationRef(ref ast.NodeRef, kind ast.Kind) bool {
	parent := b.store.ParentRef(ref)
	if parent == 0 || b.store.KindAt(parent) != ast.KindSourceFile || !ast.IsExternalOrCommonJSModule(b.file) {
		return false
	}
	if kind == ast.KindJSTypeAliasDeclaration {
		return true
	}
	return kind == ast.KindModuleDeclaration && b.store.FlagsAt(ref)&ast.NodeFlagsReparsed != 0
}

func (b *Binder) isStaticRef(ref ast.NodeRef, kind ast.Kind) bool {
	if kind == ast.KindClassStaticBlockDeclaration {
		return true
	}
	switch kind {
	case ast.KindConstructor, ast.KindPropertyDeclaration, ast.KindMethodDeclaration,
		ast.KindGetAccessor, ast.KindSetAccessor, ast.KindIndexSignature,
		ast.KindSemicolonClassElement:
		return b.hasSyntacticModifierRef(ref, kind, ast.ModifierFlagsStatic)
	}
	return false
}

func (b *Binder) nameOfDeclarationRef(ref ast.NodeRef, kind ast.Kind) (ast.NodeRef, ast.Kind) {
	if name := b.nameRefGenerated(ref, kind); name != 0 {
		return name, b.store.KindAt(name)
	}
	switch kind {
	case ast.KindBinaryExpression, ast.KindCallExpression, ast.KindExportAssignment,
		ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindClassExpression:
		name := ast.GetNameOfDeclaration(ast.HandleOf(b.store, ref, kind))
		if !name.IsNil() {
			return name.Ref(), name.Kind
		}
	}
	return 0, ast.KindUnknown
}

func (b *Binder) hasDynamicNameRef(ref ast.NodeRef, kind ast.Kind) bool {
	nameRef, nameKind := b.nameOfDeclarationRef(ref, kind)
	if nameRef == 0 {
		return false
	}
	switch nameKind {
	case ast.KindComputedPropertyName, ast.KindElementAccessExpression:
		return ast.IsDynamicName(ast.HandleOf(b.store, nameRef, nameKind))
	}
	return false
}

func isPropertyNameLiteralKind(kind ast.Kind) bool {
	switch kind {
	case ast.KindIdentifier, ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindNumericLiteral:
		return true
	}
	return false
}
func (b *Binder) getDisplayName(node ast.Handle) string {
	nameNode := node.Name()
	if !nameNode.IsNil() {
		return scanner.DeclarationNameToString(nameNode)
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
func (b *Binder) declareModuleMember(node ast.Handle, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	return b.declareModuleMemberRef(node.Ref(), node.Kind, symbolFlags, symbolExcludes)
}

func (b *Binder) declareModuleMemberRef(ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	container := b.container
	containerSymbol := b.symbol(container)
	hasExportModifier := b.combinedModifierFlagsRef(ref, kind)&ast.ModifierFlagsExport != 0 || b.isImplicitlyExportedJSDocDeclarationRef(ref, kind)
	if symbolFlags&ast.SymbolFlagsAlias != 0 {
		if kind == ast.KindExportSpecifier || (kind == ast.KindImportEqualsDeclaration && hasExportModifier) {
			return b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, ref, kind, symbolFlags, symbolExcludes, false, false)
		}
		return b.declareSymbolRef(b.getLocals(container), nil, ref, kind, symbolFlags, symbolExcludes, false, false)
	}
	if !b.isAmbientModuleRef(ref, kind) && (hasExportModifier || b.store.FlagsAt(container)&ast.NodeFlagsExportContext != 0) {
		if !ast.IsLocalsContainerKind(b.containerKind) || (b.hasSyntacticModifierRef(ref, kind, ast.ModifierFlagsDefault) && b.getDeclarationNameRef(ref, kind) == ast.InternalSymbolNameMissing) {
			return b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, ref, kind, symbolFlags, symbolExcludes, false, false)
		}
		exportKind := ast.SymbolFlagsNone
		if symbolFlags&ast.SymbolFlagsValue != 0 {
			exportKind = ast.SymbolFlagsExportValue
		}
		local := b.declareSymbolRef(b.getLocals(container), nil, ref, kind, exportKind, symbolExcludes, false, false)
		local.ExportSymbol = b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, ref, kind, symbolFlags, symbolExcludes, false, false)
		b.store.SetLocalSymbol(ref, local)
		return local
	}
	return b.declareSymbolRef(b.getLocals(container), nil, ref, kind, symbolFlags, symbolExcludes, false, false)
}
func (b *Binder) declareClassMember(node ast.Handle, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	return b.declareClassMemberRef(node.Ref(), node.Kind, symbolFlags, symbolExcludes)
}

func (b *Binder) declareClassMemberRef(ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	containerSymbol := b.symbol(b.container)
	if b.isStaticRef(ref, kind) {
		return b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, ref, kind, symbolFlags, symbolExcludes, false, false)
	}
	return b.declareSymbolRef(ast.GetMembers(containerSymbol), containerSymbol, ref, kind, symbolFlags, symbolExcludes, false, false)
}
func (b *Binder) declareSourceFileMember(node ast.Handle, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	return b.declareSourceFileMemberRef(node.Ref(), node.Kind, symbolFlags, symbolExcludes)
}

func (b *Binder) declareSourceFileMemberRef(ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	if ast.IsExternalModule(b.file) {
		return b.declareModuleMemberRef(ref, kind, symbolFlags, symbolExcludes)
	}
	return b.declareSymbolRef(b.getLocals(b.container), nil, ref, kind, symbolFlags, symbolExcludes, false, false)
}
func (b *Binder) declareSymbolAndAddToSymbolTable(node ast.Handle, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	return b.declareSymbolAndAddToSymbolTableRef(node.Ref(), node.Kind, symbolFlags, symbolExcludes)
}

func (b *Binder) declareSymbolAndAddToSymbolTableRef(ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) *ast.Symbol {
	containerSymbol := b.symbol(b.container)
	switch b.containerKind {
	case ast.KindModuleDeclaration:
		return b.declareModuleMemberRef(ref, kind, symbolFlags, symbolExcludes)
	case ast.KindSourceFile:
		return b.declareSourceFileMemberRef(ref, kind, symbolFlags, symbolExcludes)
	case ast.KindClassExpression, ast.KindClassDeclaration:
		return b.declareClassMemberRef(ref, kind, symbolFlags, symbolExcludes)
	case ast.KindEnumDeclaration:
		return b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, ref, kind, symbolFlags, symbolExcludes, false, false)
	case ast.KindTypeLiteral, ast.KindObjectLiteralExpression, ast.KindInterfaceDeclaration, ast.KindJsxAttributes:
		return b.declareSymbolRef(ast.GetMembers(containerSymbol), containerSymbol, ref, kind, symbolFlags, symbolExcludes, false, false)
	case ast.KindFunctionType, ast.KindConstructorType, ast.KindCallSignature, ast.KindConstructSignature, ast.KindIndexSignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindClassStaticBlockDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindMappedType:
		return b.declareSymbolRef(b.getLocals(b.container), nil, ref, kind, symbolFlags, symbolExcludes, false, false)
	}
	panic("Unhandled case in declareSymbolAndAddToSymbolTable")
}
func (b *Binder) newFlowNode(flags ast.FlowFlags) *ast.FlowNode {
	result := b.flowNodeArena.New()
	result.Flags = flags
	return result
}
func (b *Binder) newFlowNodeEx(flags ast.FlowFlags, node ast.Handle, antecedent *ast.FlowNode) *ast.FlowNode {
	result := b.newFlowNode(flags)
	result.Node = node
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
func (b *Binder) createFlowCondition(flags ast.FlowFlags, antecedent *ast.FlowNode, expression ast.Handle) *ast.FlowNode {
	if antecedent.Flags&ast.FlowFlagsUnreachable != 0 {
		return antecedent
	}
	if expression.IsNil() {
		if flags&ast.FlowFlagsTrueCondition != 0 {
			return antecedent
		}
		return b.unreachableFlow
	}
	if (expression.Kind == ast.KindTrueKeyword && flags&ast.FlowFlagsFalseCondition != 0 || expression.Kind == ast.KindFalseKeyword && flags&ast.FlowFlagsTrueCondition != 0) && !ast.IsExpressionOfOptionalChainRoot(expression) && !ast.IsNullishCoalesce(expression.Parent()) {
		return b.unreachableFlow
	}
	if !isNarrowingExpression(expression) {
		return antecedent
	}
	setFlowNodeReferenced(antecedent)
	return b.newFlowNodeEx(flags, expression, antecedent)
}
func (b *Binder) createFlowMutation(flags ast.FlowFlags, antecedent *ast.FlowNode, node ast.Handle) *ast.FlowNode {
	setFlowNodeReferenced(antecedent)
	b.hasFlowEffects = true
	result := b.newFlowNodeEx(flags, node, antecedent)
	if b.currentExceptionTarget != nil {
		b.addAntecedent(b.currentExceptionTarget, result)
	}
	return result
}
func (b *Binder) createFlowSwitchClause(antecedent *ast.FlowNode, switchStatement ast.Handle, clauseStart int, clauseEnd int) *ast.FlowNode {
	setFlowNodeReferenced(antecedent)
	return b.newFlowData(ast.FlowFlagsSwitchClause, ast.NewFlowSwitchClauseData(switchStatement, clauseStart, clauseEnd), antecedent)
}
func (b *Binder) createFlowCall(antecedent *ast.FlowNode, node ast.Handle) *ast.FlowNode {
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
func (b *Binder) newSingleDeclaration(declaration ast.Handle) []ast.GlobalRef {
	return b.singleDeclarationsArena.NewSlice1(declaration.Global())
}
func setFlowNodeReferenced(flow *ast.FlowNode) {
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
func (b *Binder) bindPropertyWorker(node ast.Handle) {
	b.bindPropertyWorkerRef(node.Ref(), node.Kind)
}

func (b *Binder) bindPropertyWorkerRef(ref ast.NodeRef, kind ast.Kind) {
	isAutoAccessor := kind == ast.KindPropertyDeclaration && b.hasSyntacticModifierRef(ref, kind, ast.ModifierFlagsAccessor)
	includes := core.IfElse(isAutoAccessor, ast.SymbolFlagsAccessor, ast.SymbolFlagsProperty)
	excludes := core.IfElse(isAutoAccessor, ast.SymbolFlagsAccessorExcludes, ast.SymbolFlagsPropertyExcludes)
	b.bindPropertyOrMethodOrAccessorRef(ref, kind, includes|b.getOptionalSymbolFlagForRef(ref, kind), excludes)
}
func (b *Binder) bindSourceFileIfExternalModule() {
	_, ref := b.file.ParseTreeRef()
	b.bindSourceFileIfExternalModuleRef(ref, ast.KindSourceFile)
}

func (b *Binder) bindSourceFileIfExternalModuleRef(ref ast.NodeRef, kind ast.Kind) {
	b.setExportContextFlagRef(ref, kind)
	if ast.IsExternalOrCommonJSModule(b.file) {
		b.bindSourceFileAsExternalModuleRef(ref, kind)
	} else if ast.IsJsonSourceFile(b.file) {
		b.bindSourceFileAsExternalModuleRef(ref, kind)
		originalSymbol := b.file.Symbol
		b.declareSymbolRef(ast.GetSymbolTable(&b.file.Symbol.Exports), b.file.Symbol, ref, kind, ast.SymbolFlagsProperty, ast.SymbolFlagsAll, false, false)
		b.file.Symbol = originalSymbol
	}
}
func (b *Binder) bindSourceFileAsExternalModule() {
	_, ref := b.file.ParseTreeRef()
	b.bindSourceFileAsExternalModuleRef(ref, ast.KindSourceFile)
}

func (b *Binder) bindSourceFileAsExternalModuleRef(ref ast.NodeRef, kind ast.Kind) {
	b.bindAnonymousDeclarationRef(ref, kind, ast.SymbolFlagsValueModule, "\""+tspath.RemoveFileExtension(b.file.FileName())+"\"")
}
func (b *Binder) bindModuleDeclaration(node ast.Handle) {
	b.setExportContextFlag(node)
	if ast.IsAmbientModule(node) {
		if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
			b.errorOnFirstToken(node, diagnostics.X_export_modifier_cannot_be_applied_to_ambient_modules_and_module_augmentations_since_they_are_always_visible)
		}
		if ast.IsModuleAugmentationExternal(node) {
			b.declareModuleSymbol(node)
		} else {
			name := node.ModuleDeclarationName()
			symbol := b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsValueModule, ast.SymbolFlagsValueModuleExcludes)
			if ast.IsStringLiteral(name) {
				pattern := core.TryParsePattern(name.Text())
				if !pattern.IsValid() {
					b.errorOnFirstToken(name, diagnostics.Pattern_0_can_have_at_most_one_Asterisk_character, name.Text())
				} else if pattern.StarIndex >= 0 {
					b.file.PatternAmbientModules = append(b.file.PatternAmbientModules, &ast.PatternAmbientModule{Pattern: pattern, Symbol: symbol})
				}
			}
		}
	} else {
		state := b.declareModuleSymbol(node)
		if state != ast.ModuleInstanceStateNonInstantiated {
			symbol := node.Symbol()
			constEnumOnlyModule := (symbol.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsClass|ast.SymbolFlagsRegularEnum) == 0) && state == ast.ModuleInstanceStateConstEnumOnly && !b.notConstEnumOnlyModules.Has(symbol)
			if constEnumOnlyModule {
				symbol.Flags |= ast.SymbolFlagsConstEnumOnlyModule
			} else {
				symbol.Flags &^= ast.SymbolFlagsConstEnumOnlyModule
				b.notConstEnumOnlyModules.Add(symbol)
			}
		}
	}
}
func (b *Binder) declareModuleSymbol(node ast.Handle) ast.ModuleInstanceState {
	state := ast.GetModuleInstanceState(node)
	instantiated := state != ast.ModuleInstanceStateNonInstantiated
	b.declareSymbolAndAddToSymbolTable(node, core.IfElse(instantiated, ast.SymbolFlagsValueModule, ast.SymbolFlagsNamespaceModule), core.IfElse(instantiated, ast.SymbolFlagsValueModuleExcludes, ast.SymbolFlagsNamespaceModuleExcludes))
	return state
}
func (b *Binder) bindNamespaceExportDeclaration(node ast.Handle) {
	b.bindNamespaceExportDeclarationRef(node.Ref(), node.Kind)
}

func (b *Binder) bindNamespaceExportDeclarationRef(ref ast.NodeRef, kind ast.Kind) {
	if b.modifiersRefGenerated(ref, kind) != 0 {
		b.errorOnNode(ast.HandleOf(b.store, ref, kind), diagnostics.Modifiers_cannot_appear_here)
	}
	parent := b.store.ParentRef(ref)
	switch {
	case b.store.KindAt(parent) != ast.KindSourceFile:
		b.errorOnNode(ast.HandleOf(b.store, ref, kind), diagnostics.Global_module_exports_may_only_appear_at_top_level)
	case !ast.IsExternalModule(b.file):
		b.errorOnNode(ast.HandleOf(b.store, ref, kind), diagnostics.Global_module_exports_may_only_appear_in_module_files)
	case !b.file.IsDeclarationFile:
		b.errorOnNode(ast.HandleOf(b.store, ref, kind), diagnostics.Global_module_exports_may_only_appear_in_declaration_files)
	default:
		b.declareSymbolRef(ast.GetSymbolTable(&b.file.GlobalExports), b.file.Symbol, ref, kind, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes, false, false)
	}
}
func (b *Binder) bindImportClause(node ast.Handle) {
	b.bindImportClauseRef(node.Ref(), node.Kind)
}

func (b *Binder) bindImportClauseRef(ref ast.NodeRef, kind ast.Kind) {
	if b.nameRefGenerated(ref, kind) != 0 {
		b.declareSymbolAndAddToSymbolTableRef(ref, kind, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
	}
}
func (b *Binder) bindExportDeclaration(node ast.Handle) {
	b.bindExportDeclarationRef(node.Ref(), node.Kind)
}

func (b *Binder) bindExportDeclarationRef(ref ast.NodeRef, kind ast.Kind) {
	containerSymbol := b.symbol(b.container)
	if containerSymbol == nil {
		b.bindAnonymousDeclarationRef(ref, kind, ast.SymbolFlagsExportStar, b.getDeclarationNameRef(ref, kind))
	} else if b.exportClauseRefGenerated(ref, kind) == 0 {
		b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, ref, kind, ast.SymbolFlagsExportStar, ast.SymbolFlagsNone, false, false)
	} else if b.store.KindAt(b.exportClauseRefGenerated(ref, kind)) == ast.KindNamespaceExport {
		clause := b.exportClauseRefGenerated(ref, kind)
		b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, clause, ast.KindNamespaceExport, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes, false, false)
	}
}
func (b *Binder) bindExportAssignment(node ast.Handle) {
	b.bindExportAssignmentRef(node.Ref(), node.Kind)
}

func (b *Binder) bindExportAssignmentRef(ref ast.NodeRef, kind ast.Kind) {
	container := b.container
	containerSymbol := b.symbol(container)
	if containerSymbol == nil {
		b.bindAnonymousDeclarationRef(ref, kind, ast.SymbolFlagsValue, b.getDeclarationNameRef(ref, kind))
	} else {
		expression := b.expressionRefGenerated(ref, kind)
		flags := core.IfElse(b.expressionIsAliasRef(expression, b.store.KindAt(expression)), ast.SymbolFlagsAlias, ast.SymbolFlagsProperty)
		symbol := b.declareSymbolRef(ast.GetExports(containerSymbol), containerSymbol, ref, kind, flags, ast.SymbolFlagsAll, false, false)
		if b.isExportEqualsRefGenerated(ref, kind) {
			b.setValueDeclarationRef(symbol, ref, kind, b.store.GlobalRef(ref))
		}
	}
}

func (b *Binder) expressionIsAliasRef(ref ast.NodeRef, kind ast.Kind) bool {
	if ref == 0 {
		return false
	}
	if kind == ast.KindIdentifier {
		return true
	}
	if kind == ast.KindClassExpression {
		return true
	}
	if kind != ast.KindPropertyAccessExpression || b.nameRefGenerated(ref, kind) == 0 || b.store.KindAt(b.nameRefGenerated(ref, kind)) != ast.KindIdentifier {
		return false
	}
	expression := b.expressionRefGenerated(ref, kind)
	return b.expressionIsAliasRef(expression, b.store.KindAt(expression))
}
func (b *Binder) bindJsxAttributes(node ast.Handle) {
	b.bindAnonymousDeclaration(node, ast.SymbolFlagsObjectLiteral, ast.InternalSymbolNameJSXAttributes)
}
func (b *Binder) bindJsxAttribute(node ast.Handle, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	b.declareSymbolAndAddToSymbolTable(node, symbolFlags, symbolExcludes)
}
func (b *Binder) setExportContextFlag(node ast.Handle) {
	b.setExportContextFlagRef(node.Ref(), node.Kind)
}

func (b *Binder) setExportContextFlagRef(ref ast.NodeRef, kind ast.Kind) {
	flags := b.store.FlagsAt(ref)
	if flags&ast.NodeFlagsAmbient != 0 && !b.hasExportDeclarationsRef(ref, kind) {
		b.store.SetFlagsAt(ref, flags|ast.NodeFlagsExportContext)
	} else {
		b.store.SetFlagsAt(ref, flags&^ast.NodeFlagsExportContext)
	}
}
func (b *Binder) hasExportDeclarations(node ast.Handle) bool {
	return b.hasExportDeclarationsRef(node.Ref(), node.Kind)
}

func (b *Binder) hasExportDeclarationsRef(ref ast.NodeRef, kind ast.Kind) bool {
	var list ast.ListRef
	switch kind {
	case ast.KindSourceFile:
		list = b.statementsRefGenerated(ref, kind)
	case ast.KindModuleDeclaration:
		body := b.bodyRefGenerated(ref, kind)
		if body != 0 && b.store.KindAt(body) == ast.KindModuleBlock {
			list = b.statementsRefGenerated(body, ast.KindModuleBlock)
		}
	}
	if list == 0 {
		return false
	}
	s := b.store
	n := s.ListLen(list)
	for i := 0; i < n; i++ {
		statement := s.ListElem(list, i)
		statementKind := s.KindAt(statement)
		if statementKind == ast.KindExportDeclaration || statementKind == ast.KindExportAssignment {
			return true
		}
	}
	return false
}
func (b *Binder) bindFunctionExpression(node ast.Handle) {
	b.bindFunctionExpressionRef(node.Ref(), node.Kind)
}

func (b *Binder) bindFunctionExpressionRef(ref ast.NodeRef, kind ast.Kind) {
	if !b.file.IsDeclarationFile && b.store.FlagsAt(ref)&ast.NodeFlagsAmbient == 0 && b.isAsyncFunctionRef(ref, kind) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	b.store.SetFlow(ref, b.currentFlow)
	bindingName := ast.InternalSymbolNameFunction
	nameRef, _ := b.nameOfDeclarationRef(ref, kind)
	if kind == ast.KindFunctionExpression && nameRef != 0 {
		b.checkStrictModeFunctionNameRef(ref, kind)
		bindingName = b.store.TextAt(nameRef)
	}
	b.bindAnonymousDeclarationRef(ref, kind, ast.SymbolFlagsFunction, bindingName)
}
func (b *Binder) bindCallExpression(node ast.Handle) {
	if b.file.CommonJSModuleIndicator.IsNil() && ast.IsRequireCall(node, false) {
		b.setCommonJSModuleIndicator(node)
	}
}
func (b *Binder) setCommonJSModuleIndicator(node ast.Handle) bool {
	if !b.file.ExternalModuleIndicator.IsNil() && b.file.ExternalModuleIndicator != b.file.ParseRoot() {
		return false
	}
	if b.file.CommonJSModuleIndicator.IsNil() {
		b.file.CommonJSModuleIndicator = node
		if b.file.ExternalModuleIndicator.IsNil() {
			b.bindSourceFileAsExternalModule()
		}
	}
	return true
}
func (b *Binder) bindClassLikeDeclaration(node ast.Handle) {
	b.bindClassLikeDeclarationRef(node.Ref(), node.Kind)
}

func (b *Binder) bindClassLikeDeclarationRef(ref ast.NodeRef, kind ast.Kind) {
	nameRef, _ := b.nameOfDeclarationRef(ref, kind)
	switch kind {
	case ast.KindClassDeclaration:
		b.bindBlockScopedDeclarationRef(ref, kind, ast.SymbolFlagsClass, ast.SymbolFlagsClassExcludes)
	case ast.KindClassExpression:
		nameText := ast.InternalSymbolNameClass
		if nameRef != 0 {
			nameText = b.store.TextAt(nameRef)
		}
		b.bindAnonymousDeclarationRef(ref, kind, ast.SymbolFlagsClass, nameText)
	}
	symbol := b.symbol(ref)
	prototypeSymbol := b.newSymbol(ast.SymbolFlagsProperty|ast.SymbolFlagsPrototype, "prototype")
	symbolExport := ast.GetExports(symbol)[prototypeSymbol.Name]
	if symbolExport != nil {
		b.errorOnNode(ast.NodeOf(symbolExport.Declarations[0]), diagnostics.Duplicate_identifier_0, ast.SymbolName(prototypeSymbol))
	}
	ast.GetExports(symbol)[prototypeSymbol.Name] = prototypeSymbol
	prototypeSymbol.Parent = symbol
}
func (b *Binder) bindPropertyOrMethodOrAccessor(node ast.Handle, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	b.bindPropertyOrMethodOrAccessorRef(node.Ref(), node.Kind, symbolFlags, symbolExcludes)
}

func (b *Binder) bindPropertyOrMethodOrAccessorRef(ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	if !b.file.IsDeclarationFile && b.store.FlagsAt(ref)&ast.NodeFlagsAmbient == 0 && b.isAsyncFunctionRef(ref, kind) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	if b.currentFlow != nil && b.isObjectLiteralOrClassExpressionMethodOrAccessorRef(ref, kind) {
		b.store.SetFlow(ref, b.currentFlow)
	}
	if b.hasDynamicNameRef(ref, kind) {
		b.bindAnonymousDeclarationRef(ref, kind, symbolFlags, ast.InternalSymbolNameComputed)
	} else {
		b.declareSymbolAndAddToSymbolTableRef(ref, kind, symbolFlags, symbolExcludes)
	}
}

func (b *Binder) isObjectLiteralOrClassExpressionMethodOrAccessorRef(ref ast.NodeRef, kind ast.Kind) bool {
	switch kind {
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		parentKind := b.store.KindAt(b.store.ParentRef(ref))
		return parentKind == ast.KindObjectLiteralExpression || parentKind == ast.KindClassExpression
	}
	return false
}

func (b *Binder) isObjectLiteralMethodRef(ref ast.NodeRef, kind ast.Kind) bool {
	return kind == ast.KindMethodDeclaration && b.store.KindAt(b.store.ParentRef(ref)) == ast.KindObjectLiteralExpression
}

func (b *Binder) questionTokenOfRef(ref ast.NodeRef, kind ast.Kind) ast.NodeRef {
	if token := b.questionTokenRefGenerated(ref, kind); token != 0 {
		return token
	}
	return b.postfixTokenRefGenerated(ref, kind)
}

func (b *Binder) getOptionalSymbolFlagForRef(ref ast.NodeRef, kind ast.Kind) ast.SymbolFlags {
	postfixToken := b.questionTokenOfRef(ref, kind)
	return core.IfElse(postfixToken != 0 && b.store.KindAt(postfixToken) == ast.KindQuestionToken, ast.SymbolFlagsOptional, ast.SymbolFlagsNone)
}
func (b *Binder) bindFunctionOrConstructorType(node ast.Handle) {
	b.bindFunctionOrConstructorTypeRef(node.Ref(), node.Kind)
}

func (b *Binder) bindFunctionOrConstructorTypeRef(ref ast.NodeRef, kind ast.Kind) {
	symbol := b.newSymbol(ast.SymbolFlagsSignature, b.getDeclarationNameRef(ref, kind))
	b.addDeclarationToSymbolRef(symbol, ref, kind, ast.SymbolFlagsSignature)
	typeLiteralSymbol := b.newSymbol(ast.SymbolFlagsTypeLiteral, ast.InternalSymbolNameType)
	b.addDeclarationToSymbolRef(typeLiteralSymbol, ref, kind, ast.SymbolFlagsTypeLiteral)
	typeLiteralSymbol.Members = make(ast.SymbolTable)
	typeLiteralSymbol.Members[symbol.Name] = symbol
}
func (b *Binder) addLateBoundAssignmentDeclarationToSymbol(node ast.Handle, symbol *ast.Symbol) {
	exports := ast.GetExports(symbol)
	assignmentSymbol := exports[ast.InternalSymbolNameAssignmentDeclaration]
	if assignmentSymbol == nil {
		assignmentSymbol = b.newSymbol(ast.SymbolFlagsNone, ast.InternalSymbolNameAssignmentDeclaration)
		exports[ast.InternalSymbolNameAssignmentDeclaration] = assignmentSymbol
	}
	assignmentSymbol.Declarations = append(assignmentSymbol.Declarations, node.Global())
}
func (b *Binder) bindModuleExportsAssignment(node ast.Handle) {
	if b.setCommonJSModuleIndicator(node) {
		container := b.file.ParseRoot()
		flags := core.IfElse(ast.ExpressionIsAlias(node.BinaryExpressionRight()), ast.SymbolFlagsAlias, ast.SymbolFlagsProperty)
		symbol := b.declareSymbol(ast.GetExports(container.Symbol()), container.Symbol(), node, flags, 0)
		SetValueDeclaration(symbol, node)
	}
}
func (b *Binder) bindExpandoPropertyAssignment(node ast.Handle) {
	b.expandoAssignments = append(b.expandoAssignments, ExpandoAssignmentInfo{
		node:           node.Ref(),
		nodeKind:       node.Kind,
		container:      b.container,
		containerKind:  b.containerKind,
		blockContainer: b.blockScopeContainer,
		blockKind:      b.blockScopeContainerKind,
	})
}
func (b *Binder) bindDeferredExpandoAssignments() {
	for _, info := range b.expandoAssignments {
		b.container = info.container
		b.containerKind = info.containerKind
		b.blockScopeContainer = info.blockContainer
		b.blockScopeContainerKind = info.blockKind
		b.bindDeferredExpandoAssignment(ast.HandleOf(b.store, info.node, info.nodeKind))
	}
}

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
func (b *Binder) bindDeferredExpandoAssignment(node ast.Handle) {
	parent := getParentOfPropertyAssignment(node)
	symbol := b.lookupEntity(parent, b.blockScopeContainer)
	if symbol == nil {
		symbol = b.lookupEntity(parent, b.container)
	}
	if symbol = getInitializerSymbol(symbol); symbol != nil {
		if ast.HasDynamicName(node) {
			b.bindAnonymousDeclaration(node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.InternalSymbolNameComputed)
			b.addLateBoundAssignmentDeclarationToSymbol(node, symbol)
		} else {
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
		return node.Store().ListAt(node.ArgumentList(), 0)
	}
	panic("Unhandled case in getParentOfPropertyAssignment")
}
func (b *Binder) bindExportsOrObjectDefineProperty(node ast.Handle) {
	if b.setCommonJSModuleIndicator(node) {
		container := b.file.ParseRoot()
		flags := core.IfElse(ast.IsBinaryExpression(node) && ast.ExpressionIsAlias(node.BinaryExpressionRight()), ast.SymbolFlagsAlias, ast.SymbolFlagsFunctionScopedVariable)
		b.declareSymbol(ast.GetExports(container.Symbol()), container.Symbol(), node, flags, ast.SymbolFlagsFunctionScopedVariableExcludes)
	}
}
func getInitializerSymbol(symbol *ast.Symbol) *ast.Symbol {
	if symbol == nil || symbol.ValueDeclaration == 0 {
		return nil
	}
	declaration := ast.NodeOf(symbol.ValueDeclaration)
	if declaration.IsNil() {
		return nil
	}
	switch {
	case ast.IsFunctionDeclaration(declaration) || ast.IsInJSFile(declaration) && ast.IsClassDeclaration(declaration):
		return symbol
	case ast.IsVariableDeclaration(declaration) && (declaration.Parent().Flags()&ast.NodeFlagsConst != 0 || ast.IsInJSFile(declaration)):
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
func (b *Binder) bindThisPropertyAssignment(node ast.Handle) {
	if !ast.IsInJSFile(node) {
		return
	}
	bin := node
	if ast.IsPropertyAccessExpression(bin.Left()) && ast.IsPrivateIdentifier(bin.Left().Name()) || b.thisContainer == 0 {
		return
	}
	if classSymbol, symbolTable := b.getThisClassAndSymbolTable(); symbolTable != nil {
		if ast.HasDynamicName(node) {
			b.declareSymbolEx(symbolTable, classSymbol, node, ast.SymbolFlagsProperty, ast.SymbolFlagsNone, true, true)
			b.addLateBoundAssignmentDeclarationToSymbol(node, classSymbol)
		} else {
			b.declareSymbolEx(symbolTable, classSymbol, node, ast.SymbolFlagsProperty|ast.SymbolFlagsAssignment, ast.SymbolFlagsNone, true, false)
		}
	} else if b.thisContainerKind != ast.KindFunctionDeclaration && b.thisContainerKind != ast.KindFunctionExpression {
		panic("Unhandled case in bindThisPropertyAssignment: " + b.thisContainerKind.String())
	}
}
func (b *Binder) getThisClassAndSymbolTable() (classSymbol *ast.Symbol, symbolTable ast.SymbolTable) {
	if b.thisContainer == 0 {
		return nil, nil
	}
	switch b.thisContainerKind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression:
	case ast.KindConstructor, ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindClassStaticBlockDeclaration:
		classSymbol = b.symbol(b.store.ParentRef(b.thisContainer))
		if b.isStaticRef(b.thisContainer, b.thisContainerKind) {
			symbolTable = ast.GetExports(classSymbol)
		} else {
			symbolTable = ast.GetMembers(classSymbol)
		}
	}
	return classSymbol, symbolTable
}
func (b *Binder) bindEnumDeclaration(node ast.Handle) {
	b.bindEnumDeclarationRef(node.Ref(), node.Kind)
}

func (b *Binder) bindEnumDeclarationRef(ref ast.NodeRef, kind ast.Kind) {
	if b.combinedModifierFlagsRef(ref, kind)&ast.ModifierFlagsConst != 0 {
		b.bindBlockScopedDeclarationRef(ref, kind, ast.SymbolFlagsConstEnum, ast.SymbolFlagsConstEnumExcludes)
	} else {
		b.bindBlockScopedDeclarationRef(ref, kind, ast.SymbolFlagsRegularEnum, ast.SymbolFlagsRegularEnumExcludes)
	}
}
func (b *Binder) bindVariableDeclarationOrBindingElement(node ast.Handle) {
	b.bindVariableDeclarationOrBindingElementRef(node.Ref(), node.Kind)
}

func (b *Binder) bindVariableDeclarationOrBindingElementRef(ref ast.NodeRef, kind ast.Kind) {
	nameRef, nameKind := b.nameOfDeclarationRef(ref, kind)
	b.checkStrictModeEvalOrArgumentsRef(ref, kind, nameRef, nameKind)
	if nameRef != 0 && !isBindingPatternKind(nameKind) {
		switch {
		case b.isVariableDeclarationInitializedToRequireRef(ref, kind):
			b.declareSymbolAndAddToSymbolTableRef(ref, kind, ast.SymbolFlagsAlias, ast.SymbolFlagsAliasExcludes)
		case b.isBlockOrCatchScopedRef(ref, kind):
			b.bindBlockScopedDeclarationRef(ref, kind, ast.SymbolFlagsBlockScopedVariable, ast.SymbolFlagsBlockScopedVariableExcludes)
		case b.isPartOfParameterDeclarationRef(ref, kind):
			b.declareSymbolAndAddToSymbolTableRef(ref, kind, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsParameterExcludes)
		default:
			b.declareSymbolAndAddToSymbolTableRef(ref, kind, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsFunctionScopedVariableExcludes)
		}
	}
}

func isBindingPatternKind(kind ast.Kind) bool {
	return kind == ast.KindObjectBindingPattern || kind == ast.KindArrayBindingPattern
}

func (b *Binder) isVariableDeclarationInitializedToRequireRef(ref ast.NodeRef, kind ast.Kind) bool {
	ref, kind = b.rootDeclarationRef(ref, kind)
	if b.store.FlagsAt(ref)&ast.NodeFlagsJavaScriptFile == 0 || kind != ast.KindVariableDeclaration {
		return false
	}
	initializer := b.initializerRefGenerated(ref, kind)
	if initializer == 0 {
		return false
	}
	declarationList := b.store.ParentRef(ref)
	variableStatement := b.store.ParentRef(declarationList)
	return b.modifierFlagsRef(variableStatement, b.store.KindAt(variableStatement))&ast.ModifierFlagsExport == 0 &&
		b.typeRefGenerated(ref, kind) == 0 &&
		b.isRequireCallRef(initializer, b.store.KindAt(initializer), true)
}

func (b *Binder) isRequireCallRef(ref ast.NodeRef, kind ast.Kind, requireStringLiteralLikeArgument bool) bool {
	if kind != ast.KindCallExpression {
		return false
	}
	expression := b.expressionRefGenerated(ref, kind)
	if expression == 0 || b.store.KindAt(expression) != ast.KindIdentifier || b.store.TextAt(expression) != "require" {
		return false
	}
	arguments := b.argumentsRefGenerated(ref, kind)
	if b.store.ListLen(arguments) != 1 {
		return false
	}
	if !requireStringLiteralLikeArgument {
		return true
	}
	argument := b.store.ListElem(arguments, 0)
	argumentKind := b.store.KindAt(argument)
	return argumentKind == ast.KindStringLiteral || argumentKind == ast.KindNoSubstitutionTemplateLiteral
}

func (b *Binder) isBlockOrCatchScopedRef(ref ast.NodeRef, kind ast.Kind) bool {
	if b.combinedNodeFlagsRef(ref, kind)&ast.NodeFlagsBlockScoped != 0 {
		return true
	}
	root, rootKind := b.rootDeclarationRef(ref, kind)
	return rootKind == ast.KindVariableDeclaration && b.store.KindAt(b.store.ParentRef(root)) == ast.KindCatchClause
}

func (b *Binder) isPartOfParameterDeclarationRef(ref ast.NodeRef, kind ast.Kind) bool {
	_, rootKind := b.rootDeclarationRef(ref, kind)
	return rootKind == ast.KindParameter
}
func (b *Binder) bindParameter(node ast.Handle) {
	b.bindParameterRef(node.Ref(), node.Kind)
}

func (b *Binder) bindParameterRef(ref ast.NodeRef, kind ast.Kind) {
	nameRef, nameKind := b.nameOfDeclarationRef(ref, kind)
	if b.store.FlagsAt(ref)&ast.NodeFlagsAmbient == 0 {
		b.checkStrictModeEvalOrArgumentsRef(ref, kind, nameRef, nameKind)
	}
	parent := b.store.ParentRef(ref)
	parentKind := b.store.KindAt(parent)
	if isBindingPatternKind(nameKind) {
		index := listIndexRef(b.store, b.parametersRefGenerated(parent, parentKind), ref)
		b.bindAnonymousDeclarationRef(ref, kind, ast.SymbolFlagsFunctionScopedVariable, "__"+strconv.Itoa(index))
	} else {
		b.declareSymbolAndAddToSymbolTableRef(ref, kind, ast.SymbolFlagsFunctionScopedVariable, ast.SymbolFlagsParameterExcludes)
	}
	if b.hasSyntacticModifierRef(ref, kind, ast.ModifierFlagsParameterPropertyModifier) && parentKind == ast.KindConstructor {
		classDeclaration := b.store.ParentRef(parent)
		classSymbol := b.symbol(classDeclaration)
		flags := ast.SymbolFlagsProperty | core.IfElse(b.questionTokenRefGenerated(ref, kind) != 0, ast.SymbolFlagsOptional, ast.SymbolFlagsNone)
		b.declareSymbolRef(ast.GetMembers(classSymbol), classSymbol, ref, kind, flags, ast.SymbolFlagsPropertyExcludes, false, false)
	}
}
func (b *Binder) bindFunctionDeclaration(node ast.Handle) {
	b.bindFunctionDeclarationRef(node.Ref(), node.Kind)
}

func (b *Binder) bindFunctionDeclarationRef(ref ast.NodeRef, kind ast.Kind) {
	if !b.file.IsDeclarationFile && b.store.FlagsAt(ref)&ast.NodeFlagsAmbient == 0 && b.isAsyncFunctionRef(ref, kind) {
		b.emitFlags |= ast.NodeFlagsHasAsyncFunctions
	}
	b.checkStrictModeFunctionNameRef(ref, kind)
	b.bindBlockScopedDeclarationRef(ref, kind, ast.SymbolFlagsFunction, ast.SymbolFlagsFunctionExcludes)
}

func (b *Binder) isAsyncFunctionRef(ref ast.NodeRef, kind ast.Kind) bool {
	switch kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration:
		return b.functionLikeBodyRefGenerated(ref, kind) != 0 && b.asteriskTokenRefGenerated(ref, kind) == 0 && b.hasSyntacticModifierRef(ref, kind, ast.ModifierFlagsAsync)
	}
	return false
}
func (b *Binder) getInferTypeContainer(node ast.Handle) ast.Handle {
	extendsType := ast.FindAncestor(node, func(n ast.Handle) bool {
		parent := n.Parent()
		return !parent.IsNil() && ast.IsConditionalTypeNode(parent) && parent.ConditionalTypeNodeExtendsType() == n
	})
	if !extendsType.IsNil() {
		return extendsType.Parent()
	}
	return ast.Handle{}
}
func (b *Binder) bindAnonymousDeclaration(node ast.Handle, symbolFlags ast.SymbolFlags, name string) {
	b.bindAnonymousDeclarationRef(node.Ref(), node.Kind, symbolFlags, name)
}

func (b *Binder) bindAnonymousDeclarationRef(ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags, name string) {
	symbol := b.newSymbol(symbolFlags, name)
	if symbolFlags&(ast.SymbolFlagsEnumMember|ast.SymbolFlagsClassMember) != 0 {
		symbol.Parent = b.symbol(b.container)
	}
	b.addDeclarationToSymbolRef(symbol, ref, kind, symbolFlags)
}
func (b *Binder) bindBlockScopedDeclaration(node ast.Handle, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	b.bindBlockScopedDeclarationRef(node.Ref(), node.Kind, symbolFlags, symbolExcludes)
}

func (b *Binder) bindBlockScopedDeclarationRef(ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags, symbolExcludes ast.SymbolFlags) {
	switch b.blockScopeContainerKind {
	case ast.KindModuleDeclaration:
		b.declareModuleMemberRef(ref, kind, symbolFlags, symbolExcludes)
	case ast.KindSourceFile:
		if ast.IsExternalOrCommonJSModule(b.file) {
			b.declareModuleMemberRef(ref, kind, symbolFlags, symbolExcludes)
			break
		}
		fallthrough
	default:
		b.declareSymbolRef(b.getLocals(b.blockScopeContainer), nil, ref, kind, symbolFlags, symbolExcludes, false, false)
	}
}
func (b *Binder) bindTypeParameter(node ast.Handle) {
	if node.Parent().Kind == ast.KindInferType {
		container := b.getInferTypeContainer(node.Parent())
		if !container.IsNil() {
			b.declareSymbol(ast.GetLocals(container), nil, node, ast.SymbolFlagsTypeParameter, ast.SymbolFlagsTypeParameterExcludes)
		} else {
			b.bindAnonymousDeclaration(node, ast.SymbolFlagsTypeParameter, b.getDeclarationName(node))
		}
	} else {
		b.declareSymbolAndAddToSymbolTable(node, ast.SymbolFlagsTypeParameter, ast.SymbolFlagsTypeParameterExcludes)
	}
}
func (b *Binder) lookupEntity(node ast.Handle, container ast.NodeRef) *ast.Symbol {
	if ast.IsIdentifier(node) {
		return b.lookupName(node.Text(), container)
	}
	if node.Expression().Kind == ast.KindThisKeyword {
		if _, symbolTable := b.getThisClassAndSymbolTable(); symbolTable != nil {
			if name := ast.GetElementOrPropertyAccessName(node); !name.IsNil() {
				return symbolTable[name.Text()]
			}
		}
		return nil
	}
	if symbol := getInitializerSymbol(b.lookupEntity(node.Expression(), container)); symbol != nil && symbol.Exports != nil {
		if name := ast.GetElementOrPropertyAccessName(node); !name.IsNil() {
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
	if symbol := b.symbol(container); symbol != nil {
		return symbol.Exports[name]
	}
	return nil
}

func (b *Binder) checkContextualIdentifier(node ast.Handle) {
	if len(b.file.Diagnostics()) == 0 && node.Flags()&ast.NodeFlagsAmbient == 0 && node.Flags()&ast.NodeFlagsJSDoc == 0 && !ast.IsIdentifierName(node) {
		originalKeywordKind := scanner.GetIdentifierToken(node.Text())
		if originalKeywordKind == ast.KindIdentifier {
			return
		}
		if originalKeywordKind >= ast.KindFirstFutureReservedWord && originalKeywordKind <= ast.KindLastFutureReservedWord {
			b.errorOnNode(node, b.getStrictModeIdentifierMessage(node), scanner.DeclarationNameToString(node))
		} else if originalKeywordKind == ast.KindAwaitKeyword {
			if ast.IsExternalModule(b.file) && ast.IsInTopLevelContext(node) {
				b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_at_the_top_level_of_a_module, scanner.DeclarationNameToString(node))
			} else if node.Flags()&ast.NodeFlagsAwaitContext != 0 {
				b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, scanner.DeclarationNameToString(node))
			}
		} else if originalKeywordKind == ast.KindYieldKeyword && node.Flags()&ast.NodeFlagsYieldContext != 0 {
			b.errorOnNode(node, diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, scanner.DeclarationNameToString(node))
		}
	}
}
func (b *Binder) checkPrivateIdentifier(node ast.Handle) {
	if node.Text() == "#constructor" {
		if len(b.file.Diagnostics()) == 0 {
			b.errorOnNode(node, diagnostics.X_constructor_is_a_reserved_word, scanner.DeclarationNameToString(node))
		}
	}
}
func (b *Binder) getStrictModeIdentifierMessage(node ast.Handle) *diagnostics.Message {
	if !ast.GetContainingClass(node).IsNil() {
		return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode_Class_definitions_are_automatically_in_strict_mode
	}
	if !b.file.ExternalModuleIndicator.IsNil() {
		return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Identifier_expected_0_is_a_reserved_word_in_strict_mode
}

func isUseStrictPrologueDirective(sourceFile *ast.SourceFile, node ast.Handle) bool {
	nodeText := scanner.GetSourceTextOfNodeFromSourceFile(sourceFile, node.Expression(), false)
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
func (b *Binder) checkStrictModeFunctionName(node ast.Handle) {
	b.checkStrictModeFunctionNameRef(node.Ref(), node.Kind)
}

func (b *Binder) checkStrictModeFunctionNameRef(ref ast.NodeRef, kind ast.Kind) {
	if b.store.FlagsAt(ref)&ast.NodeFlagsAmbient == 0 {
		nameRef, nameKind := b.nameOfDeclarationRef(ref, kind)
		b.checkStrictModeEvalOrArgumentsRef(ref, kind, nameRef, nameKind)
	}
}
func (b *Binder) getStrictModeBlockScopeFunctionDeclarationMessage(node ast.Handle) *diagnostics.Message {
	if !ast.GetContainingClass(node).IsNil() {
		return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5_Class_definitions_are_automatically_in_strict_mode
	}
	if !b.file.ExternalModuleIndicator.IsNil() {
		return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Function_declarations_are_not_allowed_inside_blocks_in_strict_mode_when_targeting_ES5
}
func (b *Binder) checkStrictModeBinaryExpression(node ast.Handle) {
	expr := node
	if ast.IsLeftHandSideExpression(expr.Left()) && ast.IsAssignmentOperator(expr.Operator().Kind) {
		b.checkStrictModeEvalOrArguments(node, expr.Left())
	}
}
func (b *Binder) checkStrictModeCatchClause(node ast.Handle) {
	clause := node
	if !clause.CatchClauseVariableDeclaration().IsNil() {
		b.checkStrictModeEvalOrArguments(node, clause.CatchClauseVariableDeclaration().Name())
	}
}
func (b *Binder) checkStrictModeDeleteExpression(node ast.Handle) {
	expr := node
	if expr.Expression().Kind == ast.KindIdentifier {
		b.errorOnNode(expr.Expression(), diagnostics.X_delete_cannot_be_called_on_an_identifier_in_strict_mode)
	}
}
func (b *Binder) checkStrictModePostfixUnaryExpression(node ast.Handle) {
	b.checkStrictModeEvalOrArguments(node, node.PostfixUnaryExpressionOperand())
}
func (b *Binder) checkStrictModePrefixUnaryExpression(node ast.Handle) {
	expr := node
	if expr.PrefixUnaryExpressionOperator() == ast.KindPlusPlusToken || expr.PrefixUnaryExpressionOperator() == ast.KindMinusMinusToken {
		b.checkStrictModeEvalOrArguments(node, expr.PrefixUnaryExpressionOperand())
	}
}
func (b *Binder) checkStrictModeWithStatement(node ast.Handle) {
	b.errorOnFirstToken(node, diagnostics.X_with_statements_are_not_allowed_in_strict_mode)
}
func (b *Binder) checkStrictModeLabeledStatement(node ast.Handle) {
	data := node
	if ast.IsDeclarationStatement(data.LabeledStatementStatement()) || ast.IsVariableStatement(data.LabeledStatementStatement()) {
		b.errorOnFirstToken(data.LabeledStatementLabel(), diagnostics.A_label_is_not_allowed_here)
	}
}
func isEvalOrArgumentsIdentifier(node ast.Handle) bool {
	if ast.IsIdentifier(node) {
		text := node.Text()
		return text == "eval" || text == "arguments"
	}
	return false
}
func (b *Binder) checkStrictModeEvalOrArguments(contextNode ast.Handle, name ast.Handle) {
	b.checkStrictModeEvalOrArgumentsRef(contextNode.Ref(), contextNode.Kind, name.Ref(), name.Kind)
}

func (b *Binder) checkStrictModeEvalOrArgumentsRef(contextRef ast.NodeRef, contextKind ast.Kind, nameRef ast.NodeRef, nameKind ast.Kind) {
	if nameRef != 0 && nameKind == ast.KindIdentifier {
		nameText := b.store.TextAt(nameRef)
		if nameText == "eval" || nameText == "arguments" {
			contextNode := ast.HandleOf(b.store, contextRef, contextKind)
			name := ast.HandleOf(b.store, nameRef, nameKind)
			b.errorOnNode(name, b.getStrictModeEvalOrArgumentsMessage(contextNode), nameText)
		}
	}
}
func (b *Binder) getStrictModeEvalOrArgumentsMessage(node ast.Handle) *diagnostics.Message {
	if !ast.GetContainingClass(node).IsNil() {
		return diagnostics.Code_contained_in_a_class_is_evaluated_in_JavaScript_s_strict_mode_which_does_not_allow_this_use_of_0_For_more_information_see_https_Colon_Slash_Slashdeveloper_mozilla_org_Slashen_US_Slashdocs_SlashWeb_SlashJavaScript_SlashReference_SlashStrict_mode
	}
	if !b.file.ExternalModuleIndicator.IsNil() {
		return diagnostics.Invalid_use_of_0_Modules_are_automatically_in_strict_mode
	}
	return diagnostics.Invalid_use_of_0_in_strict_mode
}

func (b *Binder) bindContainer(id ast.NodeRef, kind ast.Kind, containerFlags ContainerFlags) {
	saveContainer := b.container
	saveContainerKind := b.containerKind
	saveThisContainer := b.thisContainer
	saveThisContainerKind := b.thisContainerKind
	savedBlockScopeContainer := b.blockScopeContainer
	savedBlockScopeContainerKind := b.blockScopeContainerKind
	if containerFlags&ContainerFlagsIsContainer != 0 {
		b.container = id
		b.containerKind = kind
		b.blockScopeContainer = id
		b.blockScopeContainerKind = kind
		if containerFlags&ContainerFlagsHasLocals != 0 {
			b.addToContainerChain(id)
		}
	} else if containerFlags&ContainerFlagsIsBlockScopedContainer != 0 {
		b.blockScopeContainer = id
		b.blockScopeContainerKind = kind
		b.addToContainerChain(id)
	}
	if containerFlags&ContainerFlagsIsThisContainer != 0 {
		b.thisContainer = id
		b.thisContainerKind = kind
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
		isImmediatelyInvoked := false
		if containerFlags&ContainerFlagsIsFunctionExpression != 0 {
			isImmediatelyInvoked = !b.hasSyntacticModifierRef(id, kind, ast.ModifierFlagsAsync) && !b.isGeneratorFunctionExpressionRef(id, kind) && b.getImmediatelyInvokedFunctionExpressionRef(id, kind) != 0
		}
		if !isImmediatelyInvoked && kind == ast.KindClassStaticBlockDeclaration {
			isImmediatelyInvoked = true
		}
		if !isImmediatelyInvoked {
			flowStart := b.newFlowNode(ast.FlowFlagsStart)
			b.currentFlow = flowStart
			if containerFlags&(ContainerFlagsIsFunctionExpression|ContainerFlagsIsObjectLiteralOrClassExpressionMethodOrAccessor) != 0 {
				flowStart.Node = ast.HandleOf(b.store, id, kind)
			}
		}
		if isImmediatelyInvoked || kind == ast.KindConstructor {
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
		b.bindChildrenRef(id, kind)
		b.store.SetFlagsAt(id, b.store.FlagsAt(id)&^(ast.NodeFlagsReachabilityAndEmitFlags|ast.NodeFlagsContainsThis))
		if b.currentFlow.Flags&ast.FlowFlagsUnreachable == 0 && containerFlags&ContainerFlagsIsFunctionLike != 0 {
			if body := b.functionLikeBodyRefGenerated(id, kind); nodeIsPresentRef(b.store, body) {
				b.store.SetFlagsAt(id, b.store.FlagsAt(id)|ast.NodeFlagsHasImplicitReturn)
				if b.hasExplicitReturn {
					b.store.SetFlagsAt(id, b.store.FlagsAt(id)|ast.NodeFlagsHasExplicitReturn)
				}
				b.store.SetEndFlow(id, b.currentFlow)
			}
		}
		if b.seenThisKeyword {
			b.store.SetFlagsAt(id, b.store.FlagsAt(id)|ast.NodeFlagsContainsThis)
		}
		if kind == ast.KindSourceFile {
			b.store.SetFlagsAt(id, b.store.FlagsAt(id)|b.emitFlags)
		}
		if b.currentReturnTarget != nil {
			b.addAntecedent(b.currentReturnTarget, b.currentFlow)
			b.currentFlow = b.finishFlowLabel(b.currentReturnTarget)
			if kind == ast.KindConstructor || kind == ast.KindClassStaticBlockDeclaration {
				b.store.SetReturnFlow(id, b.currentFlow)
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
		b.bindChildrenRef(id, kind)
		if b.seenThisKeyword {
			b.store.SetFlagsAt(id, b.store.FlagsAt(id)|ast.NodeFlagsContainsThis)
		} else {
			b.store.SetFlagsAt(id, b.store.FlagsAt(id)&^ast.NodeFlagsContainsThis)
		}
		b.seenThisKeyword = saveSeenThisKeyword
	} else {
		b.bindChildrenRef(id, kind)
	}
	if kind == ast.KindSourceFile && b.store.FlagsAt(id)&ast.NodeFlagsJavaScriptFile != 0 {
		node := ast.HandleOf(b.store, id, kind)
		list := node.StatementList()
		s := node.Store()
		n := s.ListLen(list)
		for i := 0; i < n; i++ {
			statement := s.ListAt(list, i)
			if ast.IsJSTypeAliasDeclaration(statement) {
				b.bindBlockScopedDeclaration(statement, ast.SymbolFlagsTypeAlias, ast.SymbolFlagsTypeAliasExcludes)
			}
		}
		if !b.file.CommonJSModuleIndicator.IsNil() {
			b.declareCommonJSVariable("module")
			b.declareCommonJSVariable("exports")
		}
	}
	if kind == ast.KindSourceFile && ast.IsExternalOrCommonJSModule(b.file) || b.isAmbientModuleRef(id, kind) {
		b.bindCommonJSTypeExports(b.symbol(id))
	}
	b.container = saveContainer
	b.containerKind = saveContainerKind
	b.thisContainer = saveThisContainer
	b.thisContainerKind = saveThisContainerKind
	b.blockScopeContainer = savedBlockScopeContainer
	b.blockScopeContainerKind = savedBlockScopeContainerKind
}

func (b *Binder) isGeneratorFunctionExpressionRef(ref ast.NodeRef, kind ast.Kind) bool {
	return kind == ast.KindFunctionExpression && b.asteriskTokenRefGenerated(ref, kind) != 0
}

func (b *Binder) getImmediatelyInvokedFunctionExpressionRef(ref ast.NodeRef, kind ast.Kind) ast.NodeRef {
	if kind != ast.KindFunctionExpression && kind != ast.KindArrowFunction {
		return 0
	}
	prev := ref
	parent := b.store.ParentRef(ref)
	for b.store.KindAt(parent) == ast.KindParenthesizedExpression {
		prev = parent
		parent = b.store.ParentRef(parent)
	}
	if b.store.KindAt(parent) == ast.KindCallExpression && b.expressionRefGenerated(parent, ast.KindCallExpression) == prev {
		return parent
	}
	return 0
}

func (b *Binder) isAmbientModuleRef(ref ast.NodeRef, kind ast.Kind) bool {
	if kind != ast.KindModuleDeclaration {
		return false
	}
	return ast.IsAmbientModule(ast.HandleOf(b.store, ref, kind))
}

func nodeIsPresentRef(store *ast.Store, ref ast.NodeRef) bool {
	if ref == 0 {
		return false
	}
	loc := store.LocAt(ref)
	return loc.Pos() != loc.End() || loc.Pos() < 0 || store.KindAt(ref) == ast.KindEndOfFile
}
func (b *Binder) declareCommonJSVariable(name string) {
	locals := ast.GetLocals(b.file.ParseRoot())
	if locals[name] == nil {
		symbol := b.newSymbol(ast.SymbolFlagsFunctionScopedVariable|ast.SymbolFlagsModuleExports, name)
		symbol.Declarations = b.newSingleDeclaration(b.file.ParseRoot())
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
func (b *Binder) bindChildren(node ast.Handle) {
	b.bindChildrenRef(node.Ref(), node.Kind)
}

func (b *Binder) bindChildrenRef(id ast.NodeRef, kind ast.Kind) {
	saveInAssignmentPattern := b.inAssignmentPattern
	b.inAssignmentPattern = false
	if b.currentFlow == b.unreachableFlow {
		b.store.SetFlow(id, nil)
		node := ast.HandleOf(b.store, id, kind)
		if ast.IsPotentiallyExecutableNode(node) {
			b.store.SetFlagsAt(id, b.store.FlagsAt(id)|ast.NodeFlagsUnreachable)
		}
		b.bindChildrenOf(id, kind)
		b.inAssignmentPattern = saveInAssignmentPattern
		return
	}
	if ast.KindFirstStatement <= kind && kind <= ast.KindLastStatement {
		b.store.SetFlow(id, b.currentFlow)
	}
	switch kind {
	case ast.KindWhileStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindWhileStatement(node)
	case ast.KindDoStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindDoStatement(node)
	case ast.KindForStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindForStatement(node)
	case ast.KindForInStatement, ast.KindForOfStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindForInOrForOfStatement(node)
	case ast.KindIfStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindIfStatement(node)
	case ast.KindReturnStatement:
		b.bindReturnStatementRef(id, kind)
	case ast.KindThrowStatement:
		b.bindThrowStatementRef(id, kind)
	case ast.KindBreakStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindBreakStatement(node)
	case ast.KindContinueStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindContinueStatement(node)
	case ast.KindTryStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindTryStatement(node)
	case ast.KindSwitchStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindSwitchStatement(node)
	case ast.KindCaseBlock:
		b.bindCaseBlockRef(id, kind)
	case ast.KindCaseClause, ast.KindDefaultClause:
		b.bindCaseOrDefaultClauseRef(id, kind)
	case ast.KindExpressionStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindExpressionStatement(node)
	case ast.KindLabeledStatement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindLabeledStatement(node)
	case ast.KindPrefixUnaryExpression:
		node := ast.HandleOf(b.store, id, kind)
		b.bindPrefixUnaryExpressionFlow(node)
	case ast.KindPostfixUnaryExpression:
		node := ast.HandleOf(b.store, id, kind)
		b.bindPostfixUnaryExpressionFlow(node)
	case ast.KindBinaryExpression:
		node := ast.HandleOf(b.store, id, kind)
		if ast.IsDestructuringAssignment(node) {
			b.inAssignmentPattern = saveInAssignmentPattern
			b.bindDestructuringAssignmentFlow(node)
			return
		}
		b.bindBinaryExpressionFlow(node)
	case ast.KindDeleteExpression:
		node := ast.HandleOf(b.store, id, kind)
		b.bindDeleteExpressionFlow(node)
	case ast.KindConditionalExpression:
		node := ast.HandleOf(b.store, id, kind)
		b.bindConditionalExpressionFlow(node)
	case ast.KindVariableDeclaration:
		node := ast.HandleOf(b.store, id, kind)
		b.bindVariableDeclarationFlow(node)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		node := ast.HandleOf(b.store, id, kind)
		b.bindAccessExpressionFlow(node)
	case ast.KindCallExpression:
		node := ast.HandleOf(b.store, id, kind)
		b.bindCallExpressionFlow(node)
	case ast.KindNonNullExpression:
		node := ast.HandleOf(b.store, id, kind)
		b.bindNonNullExpressionFlow(node)
	case ast.KindSourceFile:
		b.bindFunctionsFirstChildrenGenerated(id, kind)
	case ast.KindBlock, ast.KindModuleBlock:
		b.bindFunctionsFirstChildrenGenerated(id, kind)
	case ast.KindBindingElement:
		node := ast.HandleOf(b.store, id, kind)
		b.bindBindingElementFlow(node)
	case ast.KindParameter:
		node := ast.HandleOf(b.store, id, kind)
		b.bindParameterFlow(node)
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression, ast.KindPropertyAssignment, ast.KindSpreadElement:
		b.inAssignmentPattern = saveInAssignmentPattern
		b.bindChildrenOf(id, kind)
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor,
		ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionType, ast.KindConstructorType,
		ast.KindCallSignature, ast.KindConstructSignature, ast.KindIndexSignature,
		ast.KindClassStaticBlockDeclaration:
		b.bindFunctionLikeChildrenGenerated(id, kind)
	default:
		b.bindChildrenOf(id, kind)
	}
	b.inAssignmentPattern = saveInAssignmentPattern
}
func (b *Binder) bindEachChild(node ast.Handle) {
	b.bindChildrenOf(node.Ref(), node.Kind)
}

func (b *Binder) bindChildRef(ref ast.NodeRef, parentKind ast.Kind) {
	if ref != 0 {
		b.bindKind(ref, b.store.KindAt(ref), parentKind)
	}
}

func (b *Binder) bindNode(node ast.Handle, parentKind ast.Kind) {
	if node.IsNil() {
		return
	}
	b.bindKind(node.Ref(), node.Kind, parentKind)
}

func (b *Binder) bindList(node ast.Handle, list ast.ListRef) {
	b.bindListRef(list, node.Kind)
}

func (b *Binder) bindChildrenOf(id ast.NodeRef, parentKind ast.Kind) {
	s := b.store
	if s == nil || id == 0 {
		return
	}
	for i := 0; i < s.NumChildrenAt(id); i++ {
		if child := s.ChildRef(id, uint32(i)); child != 0 {
			b.bindKind(child, s.KindAt(child), parentKind)
		}
	}
	for i := 0; i < s.NumListSlotsAt(id); i++ {
		b.bindListRef(s.ListSlotAt(id, uint32(i)), parentKind)
	}
}

func (b *Binder) bindListRef(list ast.ListRef, parentKind ast.Kind) {
	s := b.store
	if list == 0 || s == nil {
		return
	}
	n := s.ListLen(list)
	for i := 0; i < n; i++ {
		if id := s.ListElem(list, i); id != 0 {
			b.bindKind(id, s.KindAt(id), parentKind)
		}
	}
}

func eachList(node ast.Handle, list ast.ListRef, fn func(ast.Handle)) {
	if list == 0 || node.IsNil() {
		return
	}
	s := node.Store()
	n := s.ListLen(list)
	for i := 0; i < n; i++ {
		if id := s.ListElem(list, i); id != 0 {
			fn(ast.HandleOf(s, id, s.KindAt(id)))
		}
	}
}

func listIndex(list ast.ListRef, node ast.Handle) int {
	if list == 0 || node.IsNil() {
		return -1
	}
	return listIndexRef(node.Store(), list, node.Ref())
}

func listIndexRef(store *ast.Store, list ast.ListRef, ref ast.NodeRef) int {
	if store == nil || list == 0 || ref == 0 {
		return -1
	}
	s := store
	n := s.ListLen(list)
	for i := 0; i < n; i++ {
		if s.ListElem(list, i) == ref {
			return i
		}
	}
	return -1
}
func (b *Binder) bindModifiers(node ast.Handle) {
	b.bindListRef(node.Modifiers(), node.Kind)
}
func (b *Binder) bindEachStatementFunctionsFirstRef(list ast.ListRef, parentKind ast.Kind) {
	if list == 0 || b.store == nil {
		return
	}
	s := b.store
	n := s.ListLen(list)
	for i := 0; i < n; i++ {
		id := s.ListElem(list, i)
		if id == 0 {
			continue
		}
		if s.KindAt(id) == ast.KindFunctionDeclaration {
			b.bindKind(id, ast.KindFunctionDeclaration, parentKind)
		}
	}
	for i := 0; i < n; i++ {
		id := s.ListElem(list, i)
		if id == 0 {
			continue
		}
		kind := s.KindAt(id)
		if kind != ast.KindFunctionDeclaration {
			b.bindKind(id, kind, parentKind)
		}
	}
}
func (b *Binder) setContinueTarget(node ast.Handle, target *ast.FlowLabel) *ast.FlowLabel {
	label := b.activeLabelList
	for label != nil && node.Parent().Kind == ast.KindLabeledStatement {
		label.continueTarget = target
		label = label.next
		node = node.Parent()
	}
	return target
}
func (b *Binder) doWithConditionalBranches(action func(b *Binder, value ast.Handle) bool, value ast.Handle, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	savedTrueTarget := b.currentTrueTarget
	savedFalseTarget := b.currentFalseTarget
	b.currentTrueTarget = trueTarget
	b.currentFalseTarget = falseTarget
	action(b, value)
	b.currentTrueTarget = savedTrueTarget
	b.currentFalseTarget = savedFalseTarget
}
func (b *Binder) bindCondition(node ast.Handle, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	b.doWithConditionalBranches((*Binder).bind, node, trueTarget, falseTarget)
	if node.IsNil() || !isLogicalAssignmentExpression(node) && !ast.IsLogicalExpression(node) && !(ast.IsOptionalChain(node) && ast.IsOutermostOptionalChain(node)) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}
func (b *Binder) bindIterativeStatement(node ast.Handle, breakTarget *ast.FlowLabel, continueTarget *ast.FlowLabel) {
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
func (b *Binder) bindAssignmentTargetFlow(node ast.Handle) {
	switch node.Kind {
	case ast.KindArrayLiteralExpression:
		eachList(node, node.ElementList(), func(e ast.Handle) {
			if e.Kind == ast.KindSpreadElement {
				b.bindAssignmentTargetFlow(e.Expression())
			} else {
				b.bindDestructuringTargetFlow(e)
			}
		})
	case ast.KindObjectLiteralExpression:
		eachList(node, node.PropertyList(), func(p ast.Handle) {
			switch p.Kind {
			case ast.KindPropertyAssignment:
				b.bindDestructuringTargetFlow(p.Initializer())
			case ast.KindShorthandPropertyAssignment:
				b.bindAssignmentTargetFlow(p.ShorthandPropertyAssignmentName())
			case ast.KindSpreadAssignment:
				b.bindAssignmentTargetFlow(p.Expression())
			}
		})
	default:
		if isNarrowableReference(node) {
			b.currentFlow = b.createFlowMutation(ast.FlowFlagsAssignment, b.currentFlow, node)
		}
	}
}
func (b *Binder) bindDestructuringTargetFlow(node ast.Handle) {
	if ast.IsBinaryExpression(node) && node.BinaryExpressionOperatorToken().Kind == ast.KindEqualsToken {
		b.bindAssignmentTargetFlow(node.BinaryExpressionLeft())
	} else {
		b.bindAssignmentTargetFlow(node)
	}
}
func (b *Binder) bindWhileStatement(node ast.Handle) {
	stmt := node
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
func (b *Binder) bindDoStatement(node ast.Handle) {
	stmt := node
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
func (b *Binder) bindForStatement(node ast.Handle) {
	stmt := node
	b.bind(stmt.Initializer())
	if b.currentFlow == b.unreachableFlow {
		b.bind(stmt.ForStatementCondition())
		b.bind(stmt.Statement())
		b.bind(stmt.ForStatementIncrementor())
		return
	}
	preLoopLabel := b.setContinueTarget(node, b.createLoopLabel())
	preBodyLabel := b.createBranchLabel()
	preIncrementorLabel := b.createBranchLabel()
	postLoopLabel := b.createBranchLabel()
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = preLoopLabel
	b.bindCondition(stmt.ForStatementCondition(), preBodyLabel, postLoopLabel)
	b.currentFlow = b.finishFlowLabel(preBodyLabel)
	b.bindIterativeStatement(stmt.Statement(), postLoopLabel, preIncrementorLabel)
	b.addAntecedent(preIncrementorLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(preIncrementorLabel)
	b.bind(stmt.ForStatementIncrementor())
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postLoopLabel)
}
func (b *Binder) bindForInOrForOfStatement(node ast.Handle) {
	stmt := node
	b.bind(stmt.Expression())
	if b.currentFlow == b.unreachableFlow {
		b.bind(stmt.Initializer())
		b.bind(stmt.Statement())
		return
	}
	preLoopLabel := b.setContinueTarget(node, b.createLoopLabel())
	postLoopLabel := b.createBranchLabel()
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = preLoopLabel
	if node.Kind == ast.KindForOfStatement {
		b.bind(stmt.ForInOrOfStatementAwaitModifier())
	}
	b.addAntecedent(postLoopLabel, b.currentFlow)
	b.bind(stmt.Initializer())
	if stmt.Initializer().Kind != ast.KindVariableDeclarationList {
		b.bindAssignmentTargetFlow(stmt.Initializer())
	}
	b.bindIterativeStatement(stmt.Statement(), postLoopLabel, preLoopLabel)
	b.addAntecedent(preLoopLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postLoopLabel)
}
func (b *Binder) bindIfStatement(node ast.Handle) {
	stmt := node
	thenLabel := b.createBranchLabel()
	elseLabel := b.createBranchLabel()
	postIfLabel := b.createBranchLabel()
	b.bindCondition(stmt.Expression(), thenLabel, elseLabel)
	b.currentFlow = b.finishFlowLabel(thenLabel)
	b.bind(stmt.IfStatementThenStatement())
	b.addAntecedent(postIfLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(elseLabel)
	b.bind(stmt.IfStatementElseStatement())
	b.addAntecedent(postIfLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postIfLabel)
}
func (b *Binder) bindReturnStatement(node ast.Handle) {
	b.bind(node.Expression())
	if b.currentReturnTarget != nil {
		b.addAntecedent(b.currentReturnTarget, b.currentFlow)
	}
	b.currentFlow = b.unreachableFlow
	b.hasExplicitReturn = true
	b.hasFlowEffects = true
}

func (b *Binder) bindReturnStatementRef(ref ast.NodeRef, kind ast.Kind) {
	b.bindRef(b.expressionRefGenerated(ref, kind), kind)
	if b.currentReturnTarget != nil {
		b.addAntecedent(b.currentReturnTarget, b.currentFlow)
	}
	b.currentFlow = b.unreachableFlow
	b.hasExplicitReturn = true
	b.hasFlowEffects = true
}

func (b *Binder) bindThrowStatement(node ast.Handle) {
	b.bind(node.Expression())
	b.currentFlow = b.unreachableFlow
	b.hasFlowEffects = true
}

func (b *Binder) bindThrowStatementRef(ref ast.NodeRef, kind ast.Kind) {
	b.bindRef(b.expressionRefGenerated(ref, kind), kind)
	b.currentFlow = b.unreachableFlow
	b.hasFlowEffects = true
}
func (b *Binder) bindBreakStatement(node ast.Handle) {
	b.bindBreakOrContinueStatement(node.Label(), b.currentBreakTarget, (*ActiveLabel).BreakTarget)
}
func (b *Binder) bindContinueStatement(node ast.Handle) {
	b.bindBreakOrContinueStatement(node.Label(), b.currentContinueTarget, (*ActiveLabel).ContinueTarget)
}
func (b *Binder) bindBreakOrContinueStatement(label ast.Handle, currentTarget *ast.FlowNode, getTarget func(*ActiveLabel) *ast.FlowNode) {
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
func (b *Binder) bindBreakOrContinueFlow(flowLabel *ast.FlowLabel) {
	if flowLabel != nil {
		b.addAntecedent(flowLabel, b.currentFlow)
		b.currentFlow = b.unreachableFlow
		b.hasFlowEffects = true
	}
}
func (b *Binder) bindTryStatement(node ast.Handle) {
	stmt := node
	saveReturnTarget := b.currentReturnTarget
	saveExceptionTarget := b.currentExceptionTarget
	normalExitLabel := b.createBranchLabel()
	returnLabel := b.createBranchLabel()
	exceptionLabel := b.createBranchLabel()
	if !stmt.TryStatementFinallyBlock().IsNil() {
		b.currentReturnTarget = returnLabel
	}
	b.addAntecedent(exceptionLabel, b.currentFlow)
	b.currentExceptionTarget = exceptionLabel
	b.bind(stmt.TryStatementTryBlock())
	b.addAntecedent(normalExitLabel, b.currentFlow)
	if !stmt.TryStatementCatchClause().IsNil() {
		b.currentFlow = b.finishFlowLabel(exceptionLabel)
		exceptionLabel = b.createBranchLabel()
		b.addAntecedent(exceptionLabel, b.currentFlow)
		b.currentExceptionTarget = exceptionLabel
		b.bind(stmt.TryStatementCatchClause())
		b.addAntecedent(normalExitLabel, b.currentFlow)
	}
	b.currentReturnTarget = saveReturnTarget
	b.currentExceptionTarget = saveExceptionTarget
	if !stmt.TryStatementFinallyBlock().IsNil() {
		finallyLabel := b.createBranchLabel()
		finallyLabel.Antecedents = b.combineFlowLists(normalExitLabel.Antecedents, b.combineFlowLists(exceptionLabel.Antecedents, returnLabel.Antecedents))
		b.currentFlow = finallyLabel
		b.bind(stmt.TryStatementFinallyBlock())
		if b.currentFlow.Flags&ast.FlowFlagsUnreachable != 0 {
			b.currentFlow = b.unreachableFlow
		} else {
			if b.currentReturnTarget != nil && returnLabel.Antecedents != nil {
				b.addAntecedent(b.currentReturnTarget, b.createReduceLabel(finallyLabel, returnLabel.Antecedents, b.currentFlow))
			}
			if b.currentExceptionTarget != nil && exceptionLabel.Antecedents != nil {
				b.addAntecedent(b.currentExceptionTarget, b.createReduceLabel(finallyLabel, exceptionLabel.Antecedents, b.currentFlow))
			}
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
func (b *Binder) bindSwitchStatement(node ast.Handle) {
	stmt := node
	postSwitchLabel := b.createBranchLabel()
	b.bind(stmt.Expression())
	saveBreakTarget := b.currentBreakTarget
	savePreSwitchCaseFlow := b.preSwitchCaseFlow
	b.currentBreakTarget = postSwitchLabel
	b.preSwitchCaseFlow = b.currentFlow
	b.bind(stmt.SwitchStatementCaseBlock())
	b.addAntecedent(postSwitchLabel, b.currentFlow)
	hasDefault := false
	eachList(stmt.SwitchStatementCaseBlock(), stmt.SwitchStatementCaseBlock().CaseBlockClauses(), func(c ast.Handle) {
		if c.Kind == ast.KindDefaultClause {
			hasDefault = true
		}
	})
	if !hasDefault {
		b.addAntecedent(postSwitchLabel, b.createFlowSwitchClause(b.preSwitchCaseFlow, node, 0, 0))
	}
	b.currentBreakTarget = saveBreakTarget
	b.preSwitchCaseFlow = savePreSwitchCaseFlow
	b.currentFlow = b.finishFlowLabel(postSwitchLabel)
}
func (b *Binder) bindCaseBlock(node ast.Handle) {
	switchStatement := node.Parent()
	s := node.Store()
	clauses := node.CaseBlockClauses()
	n := s.ListLen(clauses)
	isNarrowingSwitch := switchStatement.Expression().Kind == ast.KindTrueKeyword || isNarrowingExpression(switchStatement.Expression())
	var fallthroughFlow *ast.FlowNode = b.unreachableFlow
	for i := 0; i < n; i++ {
		clauseStart := i
		for s.ListLen(s.ListAt(clauses, i).StatementList()) == 0 && i+1 < n {
			if fallthroughFlow == b.unreachableFlow {
				b.currentFlow = b.preSwitchCaseFlow
			}
			b.bind(s.ListAt(clauses, i))
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
		clause := s.ListAt(clauses, i)
		b.bind(clause)
		fallthroughFlow = b.currentFlow
		if b.currentFlow.Flags&ast.FlowFlagsUnreachable == 0 && i != n-1 {
			clause.SetEndFlowNode(b.currentFlow)
		}
	}
}

func (b *Binder) bindCaseBlockRef(ref ast.NodeRef, kind ast.Kind) {
	s := b.store
	switchStatementRef := s.ParentRef(ref)
	switchStatement := ast.HandleOf(s, switchStatementRef, ast.KindSwitchStatement)
	clauses := b.caseBlockClausesRefGenerated(ref, kind)
	n := s.ListLen(clauses)
	switchExpression := b.expressionRefGenerated(switchStatementRef, ast.KindSwitchStatement)
	isNarrowingSwitch := s.KindAt(switchExpression) == ast.KindTrueKeyword || isNarrowingExpression(ast.HandleOf(s, switchExpression, s.KindAt(switchExpression)))
	var fallthroughFlow *ast.FlowNode = b.unreachableFlow
	for i := 0; i < n; i++ {
		clauseStart := i
		for clauseRef := s.ListElem(clauses, i); s.ListLen(b.statementsRefGenerated(clauseRef, s.KindAt(clauseRef))) == 0 && i+1 < n; clauseRef = s.ListElem(clauses, i) {
			if fallthroughFlow == b.unreachableFlow {
				b.currentFlow = b.preSwitchCaseFlow
			}
			b.bindRef(clauseRef, ast.KindCaseBlock)
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
		clauseRef := s.ListElem(clauses, i)
		b.bindRef(clauseRef, ast.KindCaseBlock)
		fallthroughFlow = b.currentFlow
		if b.currentFlow.Flags&ast.FlowFlagsUnreachable == 0 && i != n-1 {
			s.SetEndFlow(clauseRef, b.currentFlow)
		}
	}
}

func (b *Binder) bindCaseOrDefaultClause(node ast.Handle) {
	clause := node
	if !clause.Expression().IsNil() {
		saveCurrentFlow := b.currentFlow
		b.currentFlow = b.preSwitchCaseFlow
		b.bind(clause.Expression())
		b.currentFlow = saveCurrentFlow
	}
	b.bindList(clause, clause.StatementList())
}

func (b *Binder) bindCaseOrDefaultClauseRef(ref ast.NodeRef, kind ast.Kind) {
	if expression := b.expressionRefGenerated(ref, kind); expression != 0 {
		saveCurrentFlow := b.currentFlow
		b.currentFlow = b.preSwitchCaseFlow
		b.bindRef(expression, kind)
		b.currentFlow = saveCurrentFlow
	}
	b.bindListRef(b.statementsRefGenerated(ref, kind), kind)
}

func (b *Binder) bindExpressionStatement(node ast.Handle) {
	stmt := node
	b.bind(stmt.Expression())
	b.maybeBindExpressionFlowIfCall(stmt.Expression())
}

func (b *Binder) maybeBindExpressionFlowIfCall(node ast.Handle) {
	if ast.IsCallExpression(node) {
		if node.Expression().Kind != ast.KindSuperKeyword && ast.IsDottedName(node.Expression()) {
			b.currentFlow = b.createFlowCall(b.currentFlow, node)
		}
	}
}

func (b *Binder) bindLabeledStatement(node ast.Handle) {
	stmt := node
	postStatementLabel := b.createBranchLabel()
	b.activeLabelList = &ActiveLabel{next: b.activeLabelList, name: stmt.Label().Text(), breakTarget: postStatementLabel, continueTarget: nil, referenced: false}
	b.bind(stmt.Label())
	b.bind(stmt.Statement())
	if !b.activeLabelList.referenced {
		stmt.Label().SetFlags(stmt.Label().Flags() | ast.NodeFlagsUnreachable)
	}
	b.activeLabelList = b.activeLabelList.next
	b.addAntecedent(postStatementLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(postStatementLabel)
}
func (b *Binder) bindPrefixUnaryExpressionFlow(node ast.Handle) {
	expr := node
	if expr.PrefixUnaryExpressionOperator() == ast.KindExclamationToken {
		saveTrueTarget := b.currentTrueTarget
		b.currentTrueTarget = b.currentFalseTarget
		b.currentFalseTarget = saveTrueTarget
		b.bindEachChild(node)
		b.currentFalseTarget = b.currentTrueTarget
		b.currentTrueTarget = saveTrueTarget
	} else {
		b.bindEachChild(node)
		if expr.PrefixUnaryExpressionOperator() == ast.KindPlusPlusToken || expr.PrefixUnaryExpressionOperator() == ast.KindMinusMinusToken {
			b.bindAssignmentTargetFlow(expr.PrefixUnaryExpressionOperand())
		}
	}
}
func (b *Binder) bindPostfixUnaryExpressionFlow(node ast.Handle) {
	expr := node
	b.bindEachChild(node)
	if expr.PrefixUnaryExpressionOperator() == ast.KindPlusPlusToken || expr.PrefixUnaryExpressionOperator() == ast.KindMinusMinusToken {
		b.bindAssignmentTargetFlow(expr.PrefixUnaryExpressionOperand())
	}
}
func (b *Binder) bindDestructuringAssignmentFlow(node ast.Handle) {
	expr := node
	if b.inAssignmentPattern {
		b.inAssignmentPattern = false
		b.bind(expr.Operator())
		b.bind(expr.Right())
		b.inAssignmentPattern = true
		b.bind(expr.Left())
		b.bind(expr.Type())
	} else {
		b.inAssignmentPattern = true
		b.bind(expr.Left())
		b.bind(expr.Type())
		b.inAssignmentPattern = false
		b.bind(expr.Operator())
		b.bind(expr.Right())
	}
	b.bindAssignmentTargetFlow(expr.Left())
}
func (b *Binder) bindBinaryExpressionFlow(node ast.Handle) {
	expr := node
	operator := expr.Operator().Kind
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
		b.bind(expr.Left())
		b.bind(expr.Type())
		if operator == ast.KindCommaToken {
			b.maybeBindExpressionFlowIfCall(expr.Left())
		}
		b.bind(expr.Operator())
		b.bind(expr.Right())
		if operator == ast.KindCommaToken {
			b.maybeBindExpressionFlowIfCall(expr.Right())
		}
		if ast.IsAssignmentOperator(operator) && !ast.IsAssignmentTarget(node) {
			b.bindAssignmentTargetFlow(expr.Left())
			if operator == ast.KindEqualsToken && expr.Left().Kind == ast.KindElementAccessExpression {
				elementAccess := expr.Left()
				if isNarrowableOperand(elementAccess.Expression()) {
					b.currentFlow = b.createFlowMutation(ast.FlowFlagsArrayMutation, b.currentFlow, node)
				}
			}
		}
	}
}
func (b *Binder) bindLogicalLikeExpression(node ast.Handle, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	expr := node
	preRightLabel := b.createBranchLabel()
	if expr.Operator().Kind == ast.KindAmpersandAmpersandToken || expr.Operator().Kind == ast.KindAmpersandAmpersandEqualsToken {
		b.bindCondition(expr.Left(), preRightLabel, falseTarget)
	} else {
		b.bindCondition(expr.Left(), trueTarget, preRightLabel)
	}
	b.currentFlow = b.finishFlowLabel(preRightLabel)
	b.bind(expr.Operator())
	if ast.IsLogicalOrCoalescingAssignmentOperator(expr.Operator().Kind) {
		b.doWithConditionalBranches((*Binder).bind, expr.Right(), trueTarget, falseTarget)
		b.bindAssignmentTargetFlow(expr.Left())
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	} else {
		b.bindCondition(expr.Right(), trueTarget, falseTarget)
	}
}
func (b *Binder) bindDeleteExpressionFlow(node ast.Handle) {
	expr := node
	b.bindEachChild(node)
	if expr.Expression().Kind == ast.KindPropertyAccessExpression {
		b.bindAssignmentTargetFlow(expr.Expression())
	}
}
func (b *Binder) bindConditionalExpressionFlow(node ast.Handle) {
	expr := node
	trueLabel := b.createBranchLabel()
	falseLabel := b.createBranchLabel()
	postExpressionLabel := b.createBranchLabel()
	saveCurrentFlow := b.currentFlow
	saveHasFlowEffects := b.hasFlowEffects
	b.hasFlowEffects = false
	b.bindCondition(expr.ConditionalExpressionCondition(), trueLabel, falseLabel)
	b.currentFlow = b.finishFlowLabel(trueLabel)
	b.bind(expr.ConditionalExpressionQuestionToken())
	b.bind(expr.ConditionalExpressionWhenTrue())
	b.addAntecedent(postExpressionLabel, b.currentFlow)
	b.currentFlow = b.finishFlowLabel(falseLabel)
	b.bind(expr.ConditionalExpressionColonToken())
	b.bind(expr.ConditionalExpressionWhenFalse())
	b.addAntecedent(postExpressionLabel, b.currentFlow)
	if b.hasFlowEffects {
		b.currentFlow = b.finishFlowLabel(postExpressionLabel)
	} else {
		b.currentFlow = saveCurrentFlow
	}
	b.hasFlowEffects = b.hasFlowEffects || saveHasFlowEffects
}
func (b *Binder) bindVariableDeclarationFlow(node ast.Handle) {
	b.bindEachChild(node)
	if !node.Initializer().IsNil() || ast.IsForInOrOfStatement(node.Parent().Parent()) {
		b.bindInitializedVariableFlow(node)
	}
}
func (b *Binder) bindInitializedVariableFlow(node ast.Handle) {
	var name ast.Handle
	switch node.Kind {
	case ast.KindVariableDeclaration:
		name = node.VariableDeclarationName()
	case ast.KindBindingElement:
		name = node.BindingElementName()
	}
	if !name.IsNil() && ast.IsBindingPattern(name) {
		eachList(name, name.ElementList(), func(child ast.Handle) {
			b.bindInitializedVariableFlow(child)
		})
	} else {
		b.currentFlow = b.createFlowMutation(ast.FlowFlagsAssignment, b.currentFlow, node)
	}
}
func (b *Binder) bindAccessExpressionFlow(node ast.Handle) {
	if ast.IsOptionalChain(node) {
		b.bindOptionalChainFlow(node)
	} else {
		b.bindEachChild(node)
	}
}
func (b *Binder) bindOptionalChainFlow(node ast.Handle) {
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
func (b *Binder) bindOptionalChain(node ast.Handle, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	var preChainLabel *ast.FlowLabel
	if ast.IsOptionalChainRoot(node) {
		preChainLabel = b.createBranchLabel()
	}
	b.bindOptionalExpression(node.Expression(), core.IfElse(preChainLabel != nil, preChainLabel, trueTarget), falseTarget)
	if preChainLabel != nil {
		b.currentFlow = b.finishFlowLabel(preChainLabel)
	}
	b.doWithConditionalBranches((*Binder).bindOptionalChainRest, node, trueTarget, falseTarget)
	if ast.IsOutermostOptionalChain(node) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}
func (b *Binder) bindOptionalExpression(node ast.Handle, trueTarget *ast.FlowLabel, falseTarget *ast.FlowLabel) {
	b.doWithConditionalBranches((*Binder).bind, node, trueTarget, falseTarget)
	if !ast.IsOptionalChain(node) || ast.IsOutermostOptionalChain(node) {
		b.addAntecedent(trueTarget, b.createFlowCondition(ast.FlowFlagsTrueCondition, b.currentFlow, node))
		b.addAntecedent(falseTarget, b.createFlowCondition(ast.FlowFlagsFalseCondition, b.currentFlow, node))
	}
}
func (b *Binder) bindOptionalChainRest(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		b.bind(node.QuestionDotToken())
		b.bind(node.Name())
	case ast.KindElementAccessExpression:
		b.bind(node.QuestionDotToken())
		b.bind(node.ElementAccessExpressionArgumentExpression())
	case ast.KindCallExpression:
		b.bind(node.QuestionDotToken())
		b.bindList(node, node.TypeArgumentList())
		b.bindList(node, node.ArgumentList())
	}
	return false
}
func (b *Binder) bindCallExpressionFlow(node ast.Handle) {
	call := node
	if ast.IsOptionalChain(node) {
		b.bindOptionalChainFlow(node)
	} else {
		expr := ast.SkipParentheses(call.Expression())
		if expr.Kind == ast.KindFunctionExpression || expr.Kind == ast.KindArrowFunction {
			b.bindList(call, call.TypeArgumentList())
			b.bindList(call, call.ArgumentList())
			b.bind(call.Expression())
		} else {
			b.bindEachChild(node)
			if call.Expression().Kind == ast.KindSuperKeyword {
				b.currentFlow = b.createFlowCall(b.currentFlow, node)
			}
		}
	}
	if ast.IsPropertyAccessExpression(call.Expression()) {
		access := call.Expression()
		if ast.IsIdentifier(access.Name()) && isNarrowableOperand(access.Expression()) && ast.IsPushOrUnshiftIdentifier(access.Name()) {
			b.currentFlow = b.createFlowMutation(ast.FlowFlagsArrayMutation, b.currentFlow, node)
		}
	}
}
func (b *Binder) bindNonNullExpressionFlow(node ast.Handle) {
	if ast.IsOptionalChain(node) {
		b.bindOptionalChainFlow(node)
	} else {
		b.bindEachChild(node)
	}
}
func (b *Binder) bindBindingElementFlow(node ast.Handle) {
	elem := node
	b.bind(elem.DotDotDotToken())
	b.bind(elem.PropertyName())
	b.bindInitializer(elem.Initializer())
	b.bind(elem.Name())
}
func (b *Binder) bindParameterFlow(node ast.Handle) {
	param := node
	b.bindModifiers(param)
	b.bind(param.DotDotDotToken())
	b.bind(param.QuestionToken())
	b.bind(param.Type())
	b.bindInitializer(param.Initializer())
	b.bind(param.Name())
}

func (b *Binder) bindInitializer(node ast.Handle) {
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
func setFlowNode(node ast.Handle, flowNode *ast.FlowNode) {
	node.SetFlowNode(flowNode)
}
func setReturnFlowNode(node ast.Handle, returnFlowNode *ast.FlowNode) {
	switch node.Kind {
	case ast.KindConstructor:
		node.SetReturnFlowNode(returnFlowNode)
	case ast.KindFunctionDeclaration:
		node.SetReturnFlowNode(returnFlowNode)
	case ast.KindFunctionExpression:
		node.SetReturnFlowNode(returnFlowNode)
	case ast.KindClassStaticBlockDeclaration:
		node.SetReturnFlowNode(returnFlowNode)
	}
}
func isGeneratorFunctionExpression(node ast.Handle) bool {
	return ast.IsFunctionExpression(node) && !node.FunctionExpressionAsteriskToken().IsNil()
}
func (b *Binder) addToContainerChain(next ast.NodeRef) {
	if b.lastContainer != 0 {
		b.store.SetNextContainer(b.lastContainer, next)
	}
	b.lastContainer = next
}
func (b *Binder) addDeclarationToSymbol(symbol *ast.Symbol, node ast.Handle, symbolFlags ast.SymbolFlags) {
	b.addDeclarationToSymbolRef(symbol, node.Ref(), node.Kind, symbolFlags)
}

func (b *Binder) addDeclarationToSymbolRef(symbol *ast.Symbol, ref ast.NodeRef, kind ast.Kind, symbolFlags ast.SymbolFlags) {
	symbol.Flags |= symbolFlags
	b.store.SetSymbol(ref, symbol)
	if kind == ast.KindSourceFile {
		if file := b.store.SourceFile(); file != nil {
			file.Symbol = symbol
		}
	}
	declaration := b.store.GlobalRef(ref)
	if symbol.Declarations == nil {
		symbol.Declarations = b.singleDeclarationsArena.NewSlice1(declaration)
	} else {
		symbol.Declarations = core.AppendIfUnique(symbol.Declarations, declaration)
	}
	if symbol.Flags&ast.SymbolFlagsConstEnumOnlyModule != 0 && symbol.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsClass|ast.SymbolFlagsRegularEnum) != 0 {
		symbol.Flags &^= ast.SymbolFlagsConstEnumOnlyModule
		b.notConstEnumOnlyModules.Add(symbol)
	}
	if symbolFlags&ast.SymbolFlagsValue != 0 {
		b.setValueDeclarationRef(symbol, ref, kind, declaration)
	}
}

func (b *Binder) setValueDeclarationRef(symbol *ast.Symbol, ref ast.NodeRef, kind ast.Kind, declaration ast.GlobalRef) {
	valueDeclaration := ast.NodeOf(symbol.ValueDeclaration)
	if valueDeclaration.IsNil() || isAssignmentDeclaration(valueDeclaration) && !isAssignmentDeclarationKind(kind) || valueDeclaration.Kind != kind && isEffectiveModuleDeclaration(valueDeclaration) {
		if b.store.SourceFile() == nil {
			return
		}
		symbol.ValueDeclaration = declaration
	}
}

func SetValueDeclaration(symbol *ast.Symbol, node ast.Handle) {
	valueDeclaration := ast.NodeOf(symbol.ValueDeclaration)
	if valueDeclaration.IsNil() || isAssignmentDeclaration(valueDeclaration) && !isAssignmentDeclaration(node) || valueDeclaration.Kind != node.Kind && isEffectiveModuleDeclaration(valueDeclaration) {
		file := ast.GetSourceFileOfNode(node)
		if file == nil {
			return
		}
		symbol.ValueDeclaration = node.Global()
	}
}
func GetContainerFlags(node ast.Handle) ContainerFlags {
	if node.IsNil() {
		return ContainerFlagsNone
	}
	parentKind := ast.KindUnknown
	if parent := node.Parent(); !parent.IsNil() {
		parentKind = parent.Kind
	}
	return getContainerFlagsRef(node.Store(), node.Ref(), node.Kind, parentKind)
}

func getContainerFlagsRef(store *ast.Store, ref ast.NodeRef, kind, parentKind ast.Kind) ContainerFlags {
	switch kind {
	case ast.KindClassExpression, ast.KindClassDeclaration, ast.KindEnumDeclaration, ast.KindObjectLiteralExpression, ast.KindTypeLiteral, ast.KindJsxAttributes:
		return ContainerFlagsIsContainer
	case ast.KindInterfaceDeclaration:
		return ContainerFlagsIsContainer | ContainerFlagsIsInterface
	case ast.KindModuleDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindMappedType, ast.KindIndexSignature:
		return ContainerFlagsIsContainer | ContainerFlagsHasLocals
	case ast.KindSourceFile:
		return ContainerFlagsIsContainer | ContainerFlagsIsControlFlowContainer | ContainerFlagsHasLocals
	case ast.KindGetAccessor, ast.KindSetAccessor, ast.KindMethodDeclaration:
		node := ast.HandleOf(store, ref, kind)
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
		node := ast.HandleOf(store, ref, kind)
		if !node.Initializer().IsNil() {
			return ContainerFlagsIsControlFlowContainer | ContainerFlagsIsThisContainer
		} else {
			return ContainerFlagsNone
		}
	case ast.KindCatchClause, ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindCaseBlock:
		return ContainerFlagsIsBlockScopedContainer | ContainerFlagsHasLocals
	case ast.KindBlock:
		if ast.IsFunctionLikeKind(parentKind) || parentKind == ast.KindClassStaticBlockDeclaration {
			return ContainerFlagsNone
		}
		return ContainerFlagsIsBlockScopedContainer | ContainerFlagsHasLocals
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
		expr := node
		return ast.IsStringOrNumericLiteralLike(expr.ElementAccessExpressionArgumentExpression()) || ast.IsEntityNameExpression(expr.ElementAccessExpressionArgumentExpression()) && isNarrowableReference(expr.Expression())
	case ast.KindBinaryExpression:
		expr := node
		return expr.Operator().Kind == ast.KindCommaToken && isNarrowableReference(expr.Right()) || ast.IsAssignmentOperator(expr.Operator().Kind) && ast.IsLeftHandSideExpression(expr.Left())
	}
	return false
}
func hasNarrowableArgument(expr ast.Handle) bool {
	call := expr
	args := call.ArgumentList()
	s := call.Store()
	n := s.ListLen(args)
	for i := 0; i < n; i++ {
		if containsNarrowableReference(s.ListAt(args, i)) {
			return true
		}
	}
	if ast.IsPropertyAccessExpression(call.Expression()) {
		if containsNarrowableReference(call.Expression().Expression()) {
			return true
		}
	}
	return false
}
func isNarrowingBinaryExpression(expr ast.Handle) bool {
	switch expr.Operator().Kind {
	case ast.KindEqualsToken, ast.KindBarBarEqualsToken, ast.KindAmpersandAmpersandEqualsToken, ast.KindQuestionQuestionEqualsToken:
		return containsNarrowableReference(expr.Left())
	case ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken, ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken:
		left := ast.SkipParentheses(expr.Left())
		right := ast.SkipParentheses(expr.Right())
		return isNarrowableOperand(left) || isNarrowableOperand(right) || isNarrowingTypeOfOperands(right, left) || isNarrowingTypeOfOperands(left, right) || (ast.IsBooleanLiteral(right) && isNarrowingExpression(left) || ast.IsBooleanLiteral(left) && isNarrowingExpression(right))
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
		binary := expr
		switch binary.Operator().Kind {
		case ast.KindEqualsToken:
			return isNarrowableOperand(binary.Left())
		case ast.KindCommaToken:
			return isNarrowableOperand(binary.Right())
		}
	}
	return containsNarrowableReference(expr)
}
func isNarrowingTypeOfOperands(expr1 ast.Handle, expr2 ast.Handle) bool {
	return ast.IsTypeOfExpression(expr1) && isNarrowableOperand(expr1.Expression()) && ast.IsStringLiteralLike(expr2)
}
func (b *Binder) errorOnNode(node ast.Handle, message *diagnostics.Message, args ...any) {
	b.addDiagnostic(b.createDiagnosticForNode(node, message, args...))
}
func (b *Binder) errorOnFirstToken(node ast.Handle, message *diagnostics.Message, args ...any) {
	span := scanner.GetRangeOfTokenAtPosition(b.file, node.Pos())
	b.addDiagnostic(ast.NewDiagnostic(b.file, span, message, args...))
}

func (b *Binder) createDiagnosticForNode(node ast.Handle, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	return ast.NewDiagnostic(b.file, scanner.GetErrorRangeForNode(b.file, node), message, args...)
}
func (b *Binder) addDiagnostic(diagnostic *ast.Diagnostic) {
	b.file.SetBindDiagnostics(append(b.file.BindDiagnostics(), diagnostic))
}
func isSignedNumericLiteral(node ast.Handle) bool {
	if node.Kind == ast.KindPrefixUnaryExpression {
		node := node
		return (node.PrefixUnaryExpressionOperator() == ast.KindPlusToken || node.PrefixUnaryExpressionOperator() == ast.KindMinusToken) && ast.IsNumericLiteral(node.PrefixUnaryExpressionOperand())
	}
	return false
}
func getOptionalSymbolFlagForNode(node ast.Handle) ast.SymbolFlags {
	postfixToken := node.QuestionToken()
	return core.IfElse(!postfixToken.IsNil() && postfixToken.Kind == ast.KindQuestionToken, ast.SymbolFlagsOptional, ast.SymbolFlagsNone)
}
func isFunctionSymbol(symbol *ast.Symbol) bool {
	d := ast.NodeOf(symbol.ValueDeclaration)
	if !d.IsNil() {
		if ast.IsFunctionDeclaration(d) {
			return true
		}
		if ast.IsVariableDeclaration(d) {
			varDecl := d
			if !varDecl.Initializer().IsNil() {
				return ast.IsFunctionLike(varDecl.Initializer())
			}
		}
	}
	return false
}
func isStatementCondition(node ast.Handle) bool {
	switch node.Parent().Kind {
	case ast.KindIfStatement, ast.KindWhileStatement, ast.KindDoStatement:
		return node.Parent().Expression() == node
	case ast.KindForStatement:
		return node.Parent().ForStatementCondition() == node
	case ast.KindConditionalExpression:
		return node.Parent().ConditionalExpressionCondition() == node
	}
	return false
}
func isTopLevelLogicalExpression(node ast.Handle) bool {
	for ast.IsParenthesizedExpression(node.Parent()) || ast.IsPrefixUnaryExpression(node.Parent()) && node.Parent().PrefixUnaryExpressionOperator() == ast.KindExclamationToken {
		node = node.Parent()
	}
	return !isStatementCondition(node) && !ast.IsLogicalExpression(node.Parent()) && !(ast.IsOptionalChain(node.Parent()) && node.Parent().Expression() == node)
}
func isAssignmentDeclaration(decl ast.Handle) bool {
	return isAssignmentDeclarationKind(decl.Kind)
}
func isAssignmentDeclarationKind(kind ast.Kind) bool {
	return kind == ast.KindBinaryExpression || kind == ast.KindPropertyAccessExpression || kind == ast.KindElementAccessExpression || kind == ast.KindIdentifier || kind == ast.KindCallExpression
}
func isEffectiveModuleDeclaration(node ast.Handle) bool {
	return ast.IsModuleDeclaration(node) || ast.IsIdentifier(node)
}
