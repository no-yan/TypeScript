package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/ls/autoimport"
	"github.com/microsoft/TypeScript/tsc/internal/ls/change"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"strconv"
)

var isolatedDeclarationsFixErrorCodes = []int32{ // IsolatedDeclarationsFixProvider is the CodeFixProvider for isolatedDeclarations-related type annotation fixes.
	// canHaveTypeAnnotationKinds are the node kinds that can have type annotations added.
	// declarationEmitNodeBuilderFlags are the node builder flags used for declaration emit.
	// typeof X
	// widened literal type
	// Match TS ordering: Full annotation, Relative annotation, Widened annotation,
	// Full inline, Relative inline, Widened inline, Full extract
	// extractAsVariable only in Full mode
	// importAdder may be nil if the auto-import registry is not available;
	// type node transformation still works without it, just without adding imports.
	// Add any symbols that need to be imported to existing import declarations
	// Add import edits if import adder has fixes
	// isolatedDeclarationsFixer encapsulates the state for fixing isolated declarations errors.
	// set by inferType/relativeType when the target was mutated (e.g., spread decomposition)
	// skip symbols that already have a variable declaration
	// Set the flags for namespace
	// needsParenthesizedExpressionForAssertion checks if an expression needs parentheses for an assertion.
	// createAsExpression creates an `expr as Type` expression, parenthesizing if needed.
	// No inline assertions for expando members
	// Go's IsDeclaration is broader than TS's isDeclaration (e.g. CallExpression has DeclarationData
	// in Go but is not a declaration kind in TS). Use isNamedDeclarationKind to match TS behavior.
	// No inline assertions on binding patterns
	// No inline assertions on enum members
	// No support for typeof in extends clauses
	// Can't inline type spread elements
	// Can't use typeof on unique symbols
	// Insert `: expr as Type` after the shorthand property name
	// Replace expression with `(expression) satisfies Type as Type` or `expression satisfies Type as Type`
	// Array literals should be marked as const
	// Identifiers or entity names can already be typeof-ed
	// Handle spread elements: walk up to the spread's parent and handle const assertions
	// findExpandoFunction finds the function declaration that has expando properties assigned to it.
	// isExpandoPropertyDeclarationForFix matches TS's isExpandoPropertyDeclaration which includes
	// PropertyAccessExpression, ElementAccessExpression, and BinaryExpression. The shared
	// ast.IsExpandoPropertyDeclaration was narrowed to BinaryExpression only for checker purposes.

	// Some late bound expando members use the whole expression as the declaration.
	// Avoid creating duplicate fixes for the same node
	// Deep clone the expression so synthesized nodes don't reference original source positions
	// Create: const <BaseName>: <type> = <expression>;
	// Replace the heritage expression with the base class name
	// Create a temporary variable for complex expressions
	// Use a new identifier to avoid referencing original source positions
	// Extract each binding element as a separate variable with type annotation
	// If the enclosing variable statement has multiple declarations, preserve the non-destructuring ones
	// Build property access expression
	// Handle computed property names: create a temp variable for the computed expression
	// Use property name text (handles identifiers, string literals, numeric literals)
	// emitBindingElementVariable creates a variable declaration for a single binding element,
	// handling default initializers by creating a ternary `temp === undefined ? default : temp`.
	// Create a temp variable to hold the accessed value, then use a conditional expression
	// to apply the default: temp === undefined ? defaultValue : temp
	// Handle Relative mode first: return typeof X for identifiers
	// Handle Widened mode: return widened literal type if different
	// widened type is same, no fix needed
	// For parameters that require adding implicit undefined, add it to the type
	// createTypeOfFromEntityNameExpression creates a `typeof X` type query node.
	// typeFromArraySpreadElements decomposes an array literal with spread elements into
	// separate variables, returning a tuple type of typeof references.
	// typeFromObjectSpreadAssignment decomposes an object literal with spread assignments into
	// separate variables, returning an intersection type of typeof references.
	// typeFromSpreads is the generic spread decomposition function, ported from TS's typeFromSpreads.
	// It splits a literal with spread elements into separate const variables and returns a composed type.
	// makeSpreadVariable creates a const variable for a spread expression and adds it to the decomposition.
	// finalizesVariablePart finalizes accumulated non-spread properties into a variable.
	// isConstAssertion checks if a node is an `as const` or `<const>` assertion.
	// relativeType creates a typeof expression for a node, used in TypePrintMode.Relative.
	// Instead of spelling out the full type, returns `typeof X` for identifiers.
	// For object/array literals with spreads, decomposes into separate variables.
	// typeToMinimizedReferenceType converts a type to a type node, then trims trailing
	// type arguments that match their defaults. Ported from TS's
	// services/codefixes/helpers.ts typeToMinimizedReferenceType.
	// !!! When truncation tracking is supported, check if the type was truncated
	// and return factory.NewKeywordTypeNode(ast.KindAnyKeyword) instead of the truncated node.
	// Trim trailing default type arguments
	// Convert import type references (e.g. import("./path").Name) to simple type references
	// and collect symbols that need to be imported
	// endOfRequiredTypeParameters finds the number of type arguments that are
	// actually required (i.e., differ from their defaults). Ported from TS's
	// services/codefixes/helpers.ts endOfRequiredTypeParameters.
	// Skip cutoff positions where the local type parameter has no default.
	// This matches TS's check for constraint === undefined on localTypeParameters,
	// which in practice skips type parameters without defaults (e.g. Set<T>
	// where T has no default should not have <unknown> elided).
	// typeParamHasDefault checks if a type parameter has a default type declaration.
	// Parenthesize paren-less arrow function parameters (`x => ...`) so the inserted `: T`
	// produces `(x: T) => ...` instead of the invalid `x: T => ...`. Queued after the type
	// annotation so that the `)` edit at param.End() sorts after the annotation insertion.
	// typeToStringForDiag converts a type node to a string for use in diagnostic descriptions.
	// It reuses the change tracker's EmitContext so that generated identifier names are resolved
	// consistently with the actual code edits, and passes the source file so that the printer's
	// name generator can check for conflicts with existing file-level identifiers.
	// findAncestorWithMissingType walks up the ancestor chain to find a node that
	// can have a type annotation and is missing one.
	// findBestFittingNode walks up from the token to find the node that best fits the diagnostic span.
	// isNamedDeclarationKind matches TS's isDeclarationKind, which is narrower than Go's IsDeclaration.
	// Go's IsDeclaration returns true for any node with DeclarationData (including CallExpression),
	// while TS's isDeclaration only returns true for specific named declaration kinds.
	// isValueSignatureDeclaration checks if a node is a function-like declaration that produces a value.
	// getIdentifierNameForNode derives a meaningful variable name from a node expression.
	// For property access expressions like `obj.foo`, returns "foo". Otherwise returns "newLocal".
	// Ported from TS's getIdentifierForNode in services/refactors/helpers.ts.
	// addSymbolToExistingImport finds the existing import declaration for the symbol's module
	// and adds the symbol name to the named imports.
	// Find the module specifier for this symbol
	// Walk the source file's import declarations to find the one importing from the same module
	// Check if this import is from the same module
	// Found the matching import - add the symbol to named imports
	// Add to existing named imports
	diagnostics.Function_must_have_an_explicit_return_type_annotation_with_isolatedDeclarations.Code(), diagnostics.Method_must_have_an_explicit_return_type_annotation_with_isolatedDeclarations.Code(), diagnostics.At_least_one_accessor_must_have_an_explicit_type_annotation_with_isolatedDeclarations.Code(), diagnostics.Variable_must_have_an_explicit_type_annotation_with_isolatedDeclarations.Code(), diagnostics.Parameter_must_have_an_explicit_type_annotation_with_isolatedDeclarations.Code(), diagnostics.Property_must_have_an_explicit_type_annotation_with_isolatedDeclarations.Code(), diagnostics.Expression_type_can_t_be_inferred_with_isolatedDeclarations.Code(), diagnostics.Binding_elements_with_initializers_can_t_be_exported_directly_with_isolatedDeclarations.Code(), diagnostics.Computed_property_names_on_class_or_object_literals_cannot_be_inferred_with_isolatedDeclarations.Code(), diagnostics.Computed_properties_must_be_number_or_string_literals_variables_or_dotted_expressions_with_isolatedDeclarations.Code(), diagnostics.Enum_member_initializers_must_be_computable_without_references_to_external_symbols_with_isolatedDeclarations.Code(), diagnostics.Extends_clause_can_t_contain_an_expression_with_isolatedDeclarations.Code(), diagnostics.Objects_that_contain_shorthand_properties_can_t_be_inferred_with_isolatedDeclarations.Code(), diagnostics.Objects_that_contain_spread_assignments_can_t_be_inferred_with_isolatedDeclarations.Code(), diagnostics.Arrays_with_spread_elements_can_t_inferred_with_isolatedDeclarations.Code(), diagnostics.Default_exports_can_t_be_inferred_with_isolatedDeclarations.Code(), diagnostics.Only_const_arrays_can_be_inferred_with_isolatedDeclarations.Code(), diagnostics.Assigning_properties_to_functions_without_declaring_them_is_not_supported_with_isolatedDeclarations_Add_an_explicit_declaration_for_the_properties_assigned_to_this_function.Code(), diagnostics.Declaration_emit_for_this_parameter_requires_implicitly_adding_undefined_to_its_type_This_is_not_supported_with_isolatedDeclarations.Code(), diagnostics.Type_containing_private_name_0_can_t_be_used_with_isolatedDeclarations.Code(), diagnostics.Add_satisfies_and_a_type_assertion_to_this_expression_satisfies_T_as_T_to_make_the_type_explicit.Code()}

const fixMissingTypeAnnotationOnExportsFixID = "fixMissingTypeAnnotationOnExports"

var IsolatedDeclarationsFixProvider = &CodeFixProvider{ErrorCodes: isolatedDeclarationsFixErrorCodes, GetCodeActions: getIsolatedDeclarationsCodeActions, FixIds: []string{fixMissingTypeAnnotationOnExportsFixID}, GetAllCodeActions: getAllIsolatedDeclarationsCodeActions}

var canHaveTypeAnnotationKinds = map[ast.Kind]bool{ast.KindGetAccessor: true, ast.KindMethodDeclaration: true, ast.KindPropertyDeclaration: true, ast.KindFunctionDeclaration: true, ast.KindFunctionExpression: true, ast.KindArrowFunction: true, ast.KindVariableDeclaration: true, ast.KindParameter: true, ast.KindExportAssignment: true, ast.KindClassDeclaration: true, ast.KindObjectBindingPattern: true, ast.KindArrayBindingPattern: true}

var declarationEmitNodeBuilderFlags = nodebuilder.FlagsMultilineObjectLiterals | nodebuilder.FlagsWriteClassExpressionAsTypeLiteral | nodebuilder.FlagsUseTypeOfFunction | nodebuilder.FlagsUseStructuralFallback | nodebuilder.FlagsAllowEmptyTuple | nodebuilder.FlagsGenerateNamesForShadowedTypeParams | nodebuilder.FlagsNoTruncation

type typePrintMode int

const (
	typePrintModeFull typePrintMode = iota
	typePrintModeRelative
	typePrintModeWidened
)

func getIsolatedDeclarationsCodeActions(ctx context.Context, fixContext *CodeFixContext) ([]*CodeAction, error) {
	ch, done := fixContext.Program.GetTypeCheckerForFile(ctx, fixContext.SourceFile)
	defer done()
	var fixes []*CodeAction
	addFix := func(action *CodeAction) {
		if action == nil {
			return
		}
		fixes = append(fixes, action)
	}
	modes := []typePrintMode{typePrintModeFull, typePrintModeRelative, typePrintModeWidened}
	for _, mode := range modes {
		addFix(tryCodeAction(ctx, fixContext, ch, func(f *isolatedDeclarationsFixer) string {
			f.typePrintMode = mode
			return f.addTypeAnnotation(fixContext.Span)
		}))
	}
	for _, mode := range modes {
		addFix(tryCodeAction(ctx, fixContext, ch, func(f *isolatedDeclarationsFixer) string {
			f.typePrintMode = mode
			return f.addInlineAssertion(fixContext.Span)
		}))
	}
	addFix(tryCodeAction(ctx, fixContext, ch, func(f *isolatedDeclarationsFixer) string {
		f.typePrintMode = typePrintModeFull
		return f.extractAsVariable(fixContext.Span)
	}))
	return fixes, nil
}
func getAllIsolatedDeclarationsCodeActions(ctx context.Context, fixContext *CodeFixContext) (*CombinedCodeActions, error) {
	ch, done := fixContext.Program.GetTypeCheckerForFile(ctx, fixContext.SourceFile)
	defer done()
	changeTracker := change.NewTracker(ctx, fixContext.Program.Options(), fixContext.LS.FormatOptions(), fixContext.LS.converters)
	fixer := &isolatedDeclarationsFixer{sourceFile: fixContext.SourceFile, program: fixContext.Program, checker: ch, changeTracker: changeTracker, locale: locale.FromContext(ctx), fixedNodes: make(map[ast.Handle]bool), typePrintMode: typePrintModeFull}
	allDiags := getAllDiagnostics(ctx, fixContext.Program, fixContext.SourceFile)
	for _, diag := range allDiags {
		if isFixableDiagnostic(diag, isolatedDeclarationsFixErrorCodes) {
			span := core.NewTextRange(diag.Loc().Pos(), diag.Loc().End())
			fixer.addTypeAnnotation(span)
		}
	}
	for _, sym := range fixer.symbolsToImport {
		fixer.addSymbolToExistingImport(sym)
	}
	changes, _ := changeTracker.GetChanges()
	fileChanges := changes[fixContext.SourceFile.OriginalFileName()]
	if len(fileChanges) == 0 {
		return nil, nil
	}
	return &CombinedCodeActions{Description: diagnostics.Add_all_missing_type_annotations.Localize(locale.FromContext(ctx)), Changes: fileChanges}, nil
}
func tryCodeAction(ctx context.Context, fixContext *CodeFixContext, ch *checker.Checker, fn func(*isolatedDeclarationsFixer) string) *CodeAction {
	changeTracker := change.NewTracker(ctx, fixContext.Program.Options(), fixContext.LS.FormatOptions(), fixContext.LS.converters)
	var importAdder autoimport.ImportAdder
	fixer := &isolatedDeclarationsFixer{sourceFile: fixContext.SourceFile, program: fixContext.Program, checker: ch, changeTracker: changeTracker, importAdder: importAdder, locale: locale.FromContext(ctx), fixedNodes: make(map[ast.Handle]bool)}
	description := fn(fixer)
	if description == "" {
		return nil
	}
	for _, sym := range fixer.symbolsToImport {
		fixer.addSymbolToExistingImport(sym)
	}
	changes, _ := changeTracker.GetChanges()
	fileChanges := changes[fixContext.SourceFile.OriginalFileName()]
	if importAdder != nil && importAdder.HasFixes() {
		fileChanges = append(fileChanges, importAdder.Edits()...)
	}
	if len(fileChanges) == 0 {
		return nil
	}
	return &CodeAction{Description: description, Changes: fileChanges, FixID: fixMissingTypeAnnotationOnExportsFixID, FixAllDescription: diagnostics.Add_all_missing_type_annotations.Localize(locale.FromContext(ctx))}
}

type isolatedDeclarationsFixer struct {
	sourceFile      *ast.SourceFile
	program         *compiler.Program
	checker         *checker.Checker
	changeTracker   *change.Tracker
	importAdder     autoimport.ImportAdder
	locale          locale.Locale
	fixedNodes      map[ast.Handle]bool
	typePrintMode   typePrintMode
	symbolsToImport []*ast.Symbol
	mutatedTarget   bool
}

func (f *isolatedDeclarationsFixer) addTypeAnnotation(span core.TextRange) string {
	nodeWithDiag := astnav.GetTokenAtPosition(f.sourceFile, span.Pos())
	expandoFunction := findExpandoFunction(f.checker, nodeWithDiag)
	if !expandoFunction.IsNil() {
		if ast.IsFunctionDeclaration(expandoFunction) {
			return f.createNamespaceForExpandoProperties(expandoFunction)
		}
		return f.fixIsolatedDeclarationError(expandoFunction)
	}
	nodeMissingType := findAncestorWithMissingType(nodeWithDiag)
	if !nodeMissingType.IsNil() {
		return f.fixIsolatedDeclarationError(nodeMissingType)
	}
	return ""
}
func (f *isolatedDeclarationsFixer) createNamespaceForExpandoProperties(expandoFunc ast.Handle) string {
	funcDecl := expandoFunc
	if funcDecl.Name().IsNil() {
		return ""
	}
	t := f.checker.GetTypeAtLocation(expandoFunc)
	elements := f.checker.GetPropertiesOfType(t)
	if len(elements) == 0 {
		return ""
	}
	factory := f.changeTracker.HandleFactory
	var newProperties []ast.Handle
	for _, symbol := range elements {
		if !scanner.IsIdentifierText(symbol.Name, core.LanguageVariantStandard) {
			continue
		}
		if symbol.ValueDeclaration != 0 && ast.IsVariableDeclaration(ast.NodeOf(symbol.ValueDeclaration)) {
			continue
		}
		symType := f.checker.GetTypeOfSymbol(symbol)
		typeNode := f.typeToMinimizedReferenceType(symType, expandoFunc, declarationEmitNodeBuilderFlags)
		if typeNode.IsNil() {
			continue
		}
		varDecl := factory.NewVariableDeclaration(factory.NewIdentifier(symbol.Name), ast.Handle{}, typeNode, ast.Handle{})
		exportToken := factory.NewToken(ast.KindExportKeyword)
		varDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{varDecl}), ast.NodeFlagsNone)
		varStmt := factory.NewVariableStatement(factory.NewModifierList([]ast.Handle{exportToken}), varDeclList)
		newProperties = append(newProperties, varStmt)
	}
	if len(newProperties) == 0 {
		return ""
	}
	var modifiers []ast.Handle
	if ast.HasSyntacticModifier(expandoFunc, ast.ModifierFlagsExport) {
		modifiers = append(modifiers, factory.NewToken(ast.KindExportKeyword))
	}
	modifiers = append(modifiers, factory.NewToken(ast.KindDeclareKeyword))
	namespace := factory.NewModuleDeclaration(factory.NewModifierList(modifiers), ast.KindNamespaceKeyword, factory.NewIdentifier(funcDecl.Name().Text()), factory.NewModuleBlock(factory.NewList(newProperties)))
	namespace.SetFlags(ast.NodeFlagsAmbient | ast.NodeFlagsExportContext | ast.NodeFlagsContextFlags)
	f.changeTracker.InsertNodeAfter(f.sourceFile, expandoFunc, namespace)
	return diagnostics.Annotate_types_of_properties_expando_function_in_a_namespace.Localize(f.locale)
}

func needsParenthesizedExpressionForAssertion(node ast.Handle) bool {
	return !ast.IsEntityNameExpression(node) && !ast.IsCallExpression(node) && !ast.IsObjectLiteralExpression(node) && !ast.IsArrayLiteralExpression(node)
}

func createAsExpression(factory ast.HandleFactory, node ast.Handle, typeNode ast.Handle) ast.Handle {
	if needsParenthesizedExpressionForAssertion(node) {
		node = factory.NewParenthesizedExpression(node)
	}
	return factory.NewAsExpression(node, typeNode)
}
func (f *isolatedDeclarationsFixer) addInlineAssertion(span core.TextRange) string {
	nodeWithDiag := astnav.GetTokenAtPosition(f.sourceFile, span.Pos())
	expandoFunction := findExpandoFunction(f.checker, nodeWithDiag)
	if !expandoFunction.IsNil() {
		return ""
	}
	targetNode := findBestFittingNode(nodeWithDiag, span)
	if targetNode.IsNil() || isValueSignatureDeclaration(targetNode) || isValueSignatureDeclaration(targetNode.Parent()) {
		return ""
	}
	isExpressionTarget := ast.IsExpression(targetNode)
	isShorthandPropertyAssignmentTarget := ast.IsShorthandPropertyAssignment(targetNode)
	if !isShorthandPropertyAssignmentTarget && isNamedDeclarationKind(targetNode) {
		return ""
	}
	if !ast.FindAncestor(targetNode, ast.IsBindingPattern).IsNil() {
		return ""
	}
	if !ast.FindAncestor(targetNode, ast.IsEnumMember).IsNil() {
		return ""
	}
	if isExpressionTarget && (!ast.FindAncestorKind(targetNode, ast.KindHeritageClause).IsNil() || !ast.FindAncestor(targetNode, ast.IsTypeNode).IsNil()) {
		return ""
	}
	if ast.IsSpreadElement(targetNode) {
		return ""
	}
	variableDeclaration := ast.FindAncestorKind(targetNode, ast.KindVariableDeclaration)
	var variableType *checker.Type
	if !variableDeclaration.IsNil() {
		variableType = f.checker.GetTypeAtLocation(variableDeclaration)
	}
	if variableType != nil && variableType.Flags()&checker.TypeFlagsUniqueESSymbol != 0 {
		return ""
	}
	if !isExpressionTarget && !isShorthandPropertyAssignmentTarget {
		return ""
	}
	typeNode := f.inferType(targetNode, variableType)
	if typeNode.IsNil() || f.mutatedTarget {
		return ""
	}
	factory := f.changeTracker.HandleFactory
	if isShorthandPropertyAssignmentTarget {
		clonedName := factory.DeepCloneNode(targetNode.ShorthandPropertyAssignmentName())
		asExpr := createAsExpression(factory, clonedName, typeNode)
		f.changeTracker.InsertNodeAt(f.sourceFile, core.TextPos(targetNode.End()), asExpr, change.NodeOptions{Prefix: ": "})
	} else if isExpressionTarget {
		clonedTarget := factory.DeepCloneNode(targetNode)
		if needsParenthesizedExpressionForAssertion(targetNode) {
			clonedTarget = factory.NewParenthesizedExpression(clonedTarget)
		}
		clonedType := factory.DeepCloneNode(typeNode)
		satisfiesAsExpr := factory.NewAsExpression(factory.NewSatisfiesExpression(clonedTarget, clonedType), typeNode)
		f.changeTracker.ReplaceNode(f.sourceFile, targetNode, satisfiesAsExpr, nil)
	} else {
		return ""
	}
	return diagnostics.Add_satisfies_and_an_inline_type_assertion_with_0.Localize(f.locale, typeToStringForDiag(typeNode, f.sourceFile, f.changeTracker))
}
func (f *isolatedDeclarationsFixer) extractAsVariable(span core.TextRange) string {
	nodeWithDiag := astnav.GetTokenAtPosition(f.sourceFile, span.Pos())
	targetNode := findBestFittingNode(nodeWithDiag, span)
	if targetNode.IsNil() || isValueSignatureDeclaration(targetNode) || isValueSignatureDeclaration(targetNode.Parent()) {
		return ""
	}
	if !ast.IsExpression(targetNode) {
		return ""
	}
	factory := f.changeTracker.HandleFactory
	if ast.IsArrayLiteralExpression(targetNode) {
		constRef := factory.NewTypeReferenceNode(factory.NewIdentifier("const"), 0)
		cloned := factory.DeepCloneNode(targetNode)
		f.changeTracker.ReplaceNode(f.sourceFile, targetNode, createAsExpression(factory, cloned, constRef), nil)
		return diagnostics.Mark_array_literal_as_const.Localize(f.locale)
	}
	parentPropertyAssignment := ast.FindAncestorKind(targetNode, ast.KindPropertyAssignment)
	if !parentPropertyAssignment.IsNil() {
		if parentPropertyAssignment == targetNode.Parent() && ast.IsEntityNameExpression(targetNode) {
			return ""
		}
		tempName := f.changeTracker.EmitContext.Factory.NewUniqueNameEx(getIdentifierNameForNode(targetNode), printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		replacementTarget := targetNode
		initializationNode := targetNode
		if ast.IsSpreadElement(replacementTarget) {
			replacementTarget = ast.WalkUpParenthesizedExpressions(replacementTarget.Parent())
			if isConstAssertion(replacementTarget.Parent()) {
				replacementTarget = replacementTarget.Parent()
				initializationNode = replacementTarget
			} else {
				constRef := factory.NewTypeReferenceNode(factory.NewIdentifier("const"), 0)
				initializationNode = createAsExpression(factory, factory.DeepCloneNode(replacementTarget), constRef)
			}
		}
		if ast.IsEntityNameExpression(replacementTarget) {
			return ""
		}
		clonedInit := factory.DeepCloneNode(initializationNode)
		varDecl := factory.NewVariableDeclaration(tempName, ast.Handle{}, ast.Handle{}, clonedInit)
		varDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst)
		varStmt := factory.NewVariableStatement(0, varDeclList)
		statement := ast.FindAncestor(targetNode, ast.IsStatement)
		if statement.IsNil() {
			return ""
		}
		f.changeTracker.InsertNodeBefore(f.sourceFile, statement, varStmt, false, change.LeadingTriviaOptionNone)
		typeQuery := factory.NewTypeQueryNode(tempName, 0)
		asExpr := factory.NewAsExpression(tempName, typeQuery)
		f.changeTracker.ReplaceNode(f.sourceFile, replacementTarget, asExpr, nil)
		idText := typeToStringForDiag(tempName, f.sourceFile, f.changeTracker)
		return diagnostics.Extract_to_variable_and_replace_with_0_as_typeof_0.Localize(f.locale, idText)
	}
	return ""
}

func isExpandoPropertyDeclarationForFix(node ast.Handle) bool {
	return !node.IsNil() && (ast.IsPropertyAccessExpression(node) || ast.IsElementAccessExpression(node) || ast.IsBinaryExpression(node))
}
func findExpandoFunction(ch *checker.Checker, node ast.Handle) ast.Handle {
	expandoDeclaration := ast.FindAncestorOrQuit(node, func(n ast.Handle) ast.FindAncestorResult {
		if ast.IsStatement(n) {
			return ast.FindAncestorQuit
		}
		if isExpandoPropertyDeclarationForFix(n) {
			return ast.FindAncestorTrue
		}
		return ast.FindAncestorFalse
	})
	if expandoDeclaration.IsNil() || !isExpandoPropertyDeclarationForFix(expandoDeclaration) {
		return ast.Handle{}
	}
	assignmentTarget := expandoDeclaration
	if ast.IsBinaryExpression(assignmentTarget) {
		assignmentTarget = assignmentTarget.BinaryExpressionLeft()
		if !isExpandoPropertyDeclarationForFix(assignmentTarget) {
			return ast.Handle{}
		}
	}
	var expression ast.Handle
	if ast.IsPropertyAccessExpression(assignmentTarget) {
		expression = assignmentTarget.PropertyAccessExpressionExpression()
	} else if ast.IsElementAccessExpression(assignmentTarget) {
		expression = assignmentTarget.ElementAccessExpressionExpression()
	} else {
		return ast.Handle{}
	}
	targetType := ch.GetTypeAtLocation(expression)
	if targetType == nil {
		return ast.Handle{}
	}
	properties := ch.GetPropertiesOfType(targetType)
	found := false
	for _, p := range properties {
		if ast.NodeOf(p.ValueDeclaration) == expandoDeclaration || ast.NodeOf(p.ValueDeclaration) == expandoDeclaration.Parent() {
			found = true
			break
		}
	}
	if !found {
		return ast.Handle{}
	}
	symbol := targetType.Symbol()
	if symbol == nil || symbol.ValueDeclaration == 0 {
		return ast.Handle{}
	}
	fn := ast.NodeOf(symbol.ValueDeclaration)
	if (ast.IsFunctionExpression(fn) || ast.IsArrowFunction(fn)) && ast.IsVariableDeclaration(fn.Parent()) {
		return fn.Parent()
	}
	if ast.IsFunctionDeclaration(fn) {
		return fn
	}
	return ast.Handle{}
}
func (f *isolatedDeclarationsFixer) fixIsolatedDeclarationError(node ast.Handle) string {
	if f.fixedNodes[node] {
		return ""
	}
	f.fixedNodes[node] = true
	switch node.Kind {
	case ast.KindParameter, ast.KindPropertyDeclaration, ast.KindVariableDeclaration:
		return f.addTypeToVariableLike(node)
	case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor:
		return f.addTypeToSignatureDeclaration(node)
	case ast.KindExportAssignment:
		return f.transformExportAssignment(node)
	case ast.KindClassDeclaration:
		return f.transformExtendsClauseWithExpression(node)
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		return f.transformDestructuringPatterns(node)
	default:
		return ""
	}
}
func (f *isolatedDeclarationsFixer) addTypeToSignatureDeclaration(funcNode ast.Handle) string {
	if !funcNode.Type().IsNil() {
		return ""
	}
	typeNode := f.inferType(funcNode, nil)
	if typeNode.IsNil() {
		return ""
	}
	f.changeTracker.TryInsertTypeAnnotation(f.sourceFile, funcNode, typeNode)
	return diagnostics.Add_return_type_0.Localize(f.locale, typeToStringForDiag(typeNode, f.sourceFile, f.changeTracker))
}
func (f *isolatedDeclarationsFixer) transformExportAssignment(defaultExport ast.Handle) string {
	exportAssignment := defaultExport
	if exportAssignment.IsExportEquals() {
		return ""
	}
	expression := exportAssignment.Expression()
	typeNode := f.inferType(expression, nil)
	if typeNode.IsNil() {
		return ""
	}
	factory := f.changeTracker.HandleFactory
	defaultIdentifier := f.changeTracker.EmitContext.Factory.NewUniqueName("_default")
	clonedExpression := factory.DeepCloneNode(expression)
	varDecl := factory.NewVariableDeclaration(defaultIdentifier, ast.Handle{}, typeNode, clonedExpression)
	varDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst)
	varStmt := factory.NewVariableStatement(0, varDeclList)
	newExport := factory.UpdateExportAssignment(defaultExport, defaultExport.Modifiers(), false, ast.Handle{}, defaultIdentifier)
	f.changeTracker.ReplaceNodeWithNodes(f.sourceFile, defaultExport, []ast.Handle{varStmt, newExport}, nil)
	return diagnostics.Extract_default_export_to_variable.Localize(f.locale)
}
func (f *isolatedDeclarationsFixer) transformExtendsClauseWithExpression(classDecl ast.Handle) string {
	cd := classDecl
	var extendsClause ast.Handle
	if cd.HeritageClauses() != 0 {
		for _, clause := range cd.Store().ListSlice(cd.HeritageClauses()) {
			if clause.HeritageClauseToken() == ast.KindExtendsKeyword {
				extendsClause = clause
				break
			}
		}
	}
	if extendsClause.IsNil() {
		return ""
	}
	heritageTypes := extendsClause.HeritageClauseTypes()
	if heritageTypes == 0 || classDecl.Store().ListLen(heritageTypes) == 0 {
		return ""
	}
	heritageExpression := classDecl.Store().ListAt(heritageTypes, 0)
	expression := heritageExpression.ExpressionWithTypeArgumentsExpression()
	heritageTypeNode := f.inferType(expression, nil)
	if heritageTypeNode.IsNil() {
		return ""
	}
	factory := f.changeTracker.HandleFactory
	baseName := "Anonymous"
	if !cd.Name().IsNil() {
		baseName = cd.Name().Text() + "Base"
	}
	baseClassName := f.changeTracker.EmitContext.Factory.NewUniqueNameEx(baseName, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
	clonedExpression := factory.DeepCloneNode(expression)
	varDecl := factory.NewVariableDeclaration(baseClassName, ast.Handle{}, heritageTypeNode, clonedExpression)
	varDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst)
	varStmt := factory.NewVariableStatement(0, varDeclList)
	f.changeTracker.InsertNodeBefore(f.sourceFile, classDecl, varStmt, false, change.LeadingTriviaOptionNone)
	f.changeTracker.ReplaceNode(f.sourceFile, heritageExpression, factory.NewExpressionWithTypeArguments(baseClassName, 0), nil)
	return diagnostics.Extract_base_class_to_variable.Localize(f.locale)
}
func (f *isolatedDeclarationsFixer) transformDestructuringPatterns(bindingPattern ast.Handle) string {
	enclosingVariableDeclaration := bindingPattern.Parent()
	if !ast.IsVariableDeclaration(enclosingVariableDeclaration) {
		return ""
	}
	enclosingVarStmt := enclosingVariableDeclaration.Parent().Parent()
	if !ast.IsVariableStatement(enclosingVarStmt) {
		return ""
	}
	initializer := enclosingVariableDeclaration.Initializer()
	if initializer.IsNil() {
		return ""
	}
	factory := f.changeTracker.HandleFactory
	var newNodes []ast.Handle
	var baseExprNode ast.Handle
	if !ast.IsIdentifier(initializer) {
		tempName := f.changeTracker.EmitContext.Factory.NewUniqueNameEx("dest", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		clonedInitializer := factory.DeepCloneNode(initializer)
		varDecl := factory.NewVariableDeclaration(tempName, ast.Handle{}, ast.Handle{}, clonedInitializer)
		varDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst)
		varStmt := factory.NewVariableStatement(0, varDeclList)
		newNodes = append(newNodes, varStmt)
		baseExprNode = tempName
	} else {
		baseExprNode = factory.NewIdentifier(initializer.Text())
	}
	f.extractBindingElements(bindingPattern, baseExprNode, &newNodes, enclosingVarStmt)
	if len(newNodes) == 0 {
		return ""
	}
	declList := enclosingVarStmt.VariableStatementDeclarationList()
	decls := declList.Declarations()
	if len(decls) > 1 {
		var remainingDecls []ast.Handle
		for _, d := range decls {
			if d != enclosingVariableDeclaration {
				remainingDecls = append(remainingDecls, d)
			}
		}
		if len(remainingDecls) > 0 {
			newNodes = append(newNodes, factory.UpdateVariableStatement(enclosingVarStmt, enclosingVarStmt.VariableStatementModifiers(), factory.UpdateVariableDeclarationList(declList, factory.NewList(remainingDecls), declList.Flags())))
		}
	}
	f.changeTracker.ReplaceNodeWithNodes(f.sourceFile, enclosingVarStmt, newNodes, nil)
	return diagnostics.Extract_binding_expressions_to_variable.Localize(f.locale)
}
func (f *isolatedDeclarationsFixer) extractBindingElements(bindingPattern ast.Handle, baseExpr ast.Handle, newNodes *[]ast.Handle, enclosingVarStmt ast.Handle) {
	factory := f.changeTracker.HandleFactory
	if ast.IsObjectBindingPattern(bindingPattern) {
		for _, element := range bindingPattern.Store().ListSlice(bindingPattern.BindingPatternElements()) {
			if ast.IsOmittedExpression(element) {
				continue
			}
			be := element
			name := be.Name()
			if name.IsNil() {
				continue
			}
			var accessExpr ast.Handle
			if !be.PropertyName().IsNil() && ast.IsComputedPropertyName(be.PropertyName()) {
				computedExpression := be.PropertyName().ComputedPropertyNameExpression()
				identifierForComputedProperty := f.changeTracker.EmitContext.Factory.NewGeneratedNameForNode(computedExpression)
				compVarDecl := factory.NewVariableDeclaration(identifierForComputedProperty, ast.Handle{}, ast.Handle{}, computedExpression)
				compVarDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{compVarDecl}), ast.NodeFlagsConst)
				compVarStmt := factory.NewVariableStatement(0, compVarDeclList)
				*newNodes = append(*newNodes, compVarStmt)
				accessExpr = factory.NewElementAccessExpression(baseExpr, ast.Handle{}, identifierForComputedProperty, ast.NodeFlagsNone)
			} else if !be.PropertyName().IsNil() {
				propText := be.PropertyName().Text()
				accessExpr = factory.NewPropertyAccessExpression(baseExpr, ast.Handle{}, factory.NewIdentifier(propText), ast.NodeFlagsNone)
			} else if ast.IsIdentifier(name) {
				accessExpr = factory.NewPropertyAccessExpression(baseExpr, ast.Handle{}, factory.NewIdentifier(name.Text()), ast.NodeFlagsNone)
			} else {
				continue
			}
			if ast.IsBindingPattern(name) {
				f.extractBindingElements(name, accessExpr, newNodes, enclosingVarStmt)
			} else {
				f.emitBindingElementVariable(factory, name, be, accessExpr, newNodes, enclosingVarStmt)
			}
		}
	} else if ast.IsArrayBindingPattern(bindingPattern) {
		for i, element := range bindingPattern.Store().ListSlice(bindingPattern.BindingPatternElements()) {
			if ast.IsOmittedExpression(element) {
				continue
			}
			be := element
			name := be.Name()
			if name.IsNil() {
				continue
			}
			accessExpr := factory.NewElementAccessExpression(baseExpr, ast.Handle{}, factory.NewNumericLiteral(strconv.Itoa(i), ast.TokenFlagsNone), ast.NodeFlagsNone)
			if ast.IsBindingPattern(name) {
				f.extractBindingElements(name, accessExpr, newNodes, enclosingVarStmt)
			} else {
				f.emitBindingElementVariable(factory, name, be, accessExpr, newNodes, enclosingVarStmt)
			}
		}
	}
}

func (f *isolatedDeclarationsFixer) emitBindingElementVariable(factory ast.HandleFactory, name ast.Handle, be ast.Handle, accessExpr ast.Handle, newNodes *[]ast.Handle, enclosingVarStmt ast.Handle) {
	typeNode := f.inferType(name, nil)
	variableInitializer := accessExpr
	if !be.Initializer().IsNil() {
		propName := be.PropertyName()
		tempBaseName := "temp"
		if !propName.IsNil() && ast.IsIdentifier(propName) {
			tempBaseName = propName.Text()
		}
		tempName := f.changeTracker.EmitContext.Factory.NewUniqueNameEx(tempBaseName, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		tempVarDecl := factory.NewVariableDeclaration(tempName, ast.Handle{}, ast.Handle{}, variableInitializer)
		tempVarDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{tempVarDecl}), ast.NodeFlagsConst)
		tempVarStmt := factory.NewVariableStatement(0, tempVarDeclList)
		*newNodes = append(*newNodes, tempVarStmt)
		variableInitializer = factory.NewConditionalExpression(factory.NewBinaryExpression(0, tempName, ast.Handle{}, factory.NewToken(ast.KindEqualsEqualsEqualsToken), factory.NewIdentifier("undefined")), factory.NewToken(ast.KindQuestionToken), be.Initializer(), factory.NewToken(ast.KindColonToken), variableInitializer)
	}
	exportModifier := f.getExportModifier(enclosingVarStmt)
	varDecl := factory.NewVariableDeclaration(factory.NewIdentifier(name.Text()), ast.Handle{}, typeNode, variableInitializer)
	varDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst)
	varStmt := factory.NewVariableStatement(exportModifier, varDeclList)
	*newNodes = append(*newNodes, varStmt)
}
func (f *isolatedDeclarationsFixer) getExportModifier(enclosingVarStmt ast.Handle) ast.ListRef {
	if ast.HasSyntacticModifier(enclosingVarStmt, ast.ModifierFlagsExport) {
		exportToken := f.changeTracker.HandleFactory.NewToken(ast.KindExportKeyword)
		return f.changeTracker.HandleFactory.NewModifierList([]ast.Handle{exportToken})
	}
	return 0
}
func (f *isolatedDeclarationsFixer) inferType(node ast.Handle, variableType *checker.Type) ast.Handle {
	f.mutatedTarget = false
	if f.typePrintMode == typePrintModeRelative {
		return f.relativeType(node)
	}
	var t *checker.Type
	if isValueSignatureDeclaration(node) {
		signature := f.checker.GetSignatureFromDeclaration(node)
		if signature != nil {
			typePredicate := f.checker.GetTypePredicateOfSignature(signature)
			if typePredicate != nil {
				if typePredicate.Type() == nil {
					return ast.Handle{}
				}
				enclosingDecl := ast.FindAncestor(node, ast.IsDeclaration)
				if enclosingDecl.IsNil() {
					enclosingDecl = f.sourceFile.ParseRoot()
				}
				flags := declarationEmitNodeBuilderFlags
				if typePredicate.Type().Flags()&checker.TypeFlagsUniqueESSymbol != 0 {
					flags |= nodebuilder.FlagsAllowUniqueESSymbolType
				}
				result := f.checker.TypePredicateToTypePredicateNode(typePredicate, enclosingDecl, flags, nil)
				if !result.IsNil() {
					return result
				}
				return ast.Handle{}
			}
			t = f.checker.GetReturnTypeOfSignature(signature)
		}
	} else {
		t = f.checker.GetTypeAtLocation(node)
	}
	if t == nil {
		return ast.Handle{}
	}
	if f.typePrintMode == typePrintModeWidened {
		if variableType != nil {
			t = variableType
		}
		widenedType := f.checker.GetWidenedLiteralType(t)
		if f.checker.IsTypeAssignableTo(widenedType, t) {
			return ast.Handle{}
		}
		t = widenedType
	}
	enclosingDecl := ast.FindAncestor(node, ast.IsDeclaration)
	if enclosingDecl.IsNil() {
		enclosingDecl = f.sourceFile.ParseRoot()
	}
	flags := declarationEmitNodeBuilderFlags | f.getExtraFlags(node, t)
	if ast.IsParameterDeclaration(node) && f.checker.RequiresAddingImplicitUndefined(node) {
		t = f.checker.GetUnionTypeEx([]*checker.Type{f.checker.GetUndefinedType(), t}, checker.UnionReductionNone)
	}
	typeNode := f.typeToMinimizedReferenceType(t, enclosingDecl, flags)
	return typeNode
}
func (f *isolatedDeclarationsFixer) getExtraFlags(node ast.Handle, t *checker.Type) nodebuilder.Flags {
	if (ast.IsVariableDeclaration(node) || (ast.IsPropertyDeclaration(node) && ast.HasSyntacticModifier(node, ast.ModifierFlagsStatic|ast.ModifierFlagsReadonly))) && t.Flags()&checker.TypeFlagsUniqueESSymbol != 0 {
		return nodebuilder.FlagsAllowUniqueESSymbolType
	}
	return nodebuilder.FlagsNone
}

func (f *isolatedDeclarationsFixer) createTypeOfFromEntityNameExpression(node ast.Handle) ast.Handle {
	return f.changeTracker.HandleFactory.NewTypeQueryNode(f.changeTracker.HandleFactory.DeepCloneNode(node), 0)
}

func (f *isolatedDeclarationsFixer) typeFromArraySpreadElements(node ast.Handle, name string) ast.Handle {
	isInConstContext := !ast.FindAncestor(node, isConstAssertion).IsNil()
	if !isInConstContext {
		return ast.Handle{}
	}
	if name == "" {
		name = "temp"
	}
	factory := f.changeTracker.HandleFactory
	return f.typeFromSpreads(node, name, isInConstContext, func(n ast.Handle) []ast.Handle {
		return n.Store().ListSlice(n.ArrayLiteralExpressionElements()).Slice()
	}, ast.IsSpreadElement, func(expr ast.Handle) ast.Handle {
		return factory.NewSpreadElement(expr)
	}, func(elements []ast.Handle) ast.Handle {
		return factory.NewArrayLiteralExpression(factory.NewList(elements), true)
	}, func(types []ast.Handle) ast.Handle {
		restTypes := make([]ast.Handle, len(types))
		for i, t := range types {
			restTypes[i] = factory.NewRestTypeNode(t)
		}
		return factory.NewTupleTypeNode(factory.NewList(restTypes))
	})
}

func (f *isolatedDeclarationsFixer) typeFromObjectSpreadAssignment(node ast.Handle, name string) ast.Handle {
	isInConstContext := !ast.FindAncestor(node, isConstAssertion).IsNil()
	if name == "" {
		name = "temp"
	}
	factory := f.changeTracker.HandleFactory
	return f.typeFromSpreads(node, name, isInConstContext, func(n ast.Handle) []ast.Handle {
		if n.ObjectLiteralExpressionProperties() != 0 {
			return n.Store().ListSlice(n.ObjectLiteralExpressionProperties()).Slice()
		}
		return nil
	}, ast.IsSpreadAssignment, func(expr ast.Handle) ast.Handle {
		return factory.NewSpreadAssignment(expr)
	}, func(elements []ast.Handle) ast.Handle {
		return factory.NewObjectLiteralExpression(factory.NewList(elements), true)
	}, func(types []ast.Handle) ast.Handle {
		return factory.NewIntersectionTypeNode(factory.NewList(types))
	})
}

func (f *isolatedDeclarationsFixer) typeFromSpreads(node ast.Handle, name string, isInConstContext bool, getChildren func(ast.Handle) []ast.Handle, isSpread func(ast.Handle) bool, createSpread func(ast.Handle) ast.Handle, makeNodeOfKind func([]ast.Handle) ast.Handle, finalType func([]ast.Handle) ast.Handle) ast.Handle {
	factory := f.changeTracker.HandleFactory
	var intersectionTypes []ast.Handle
	var newSpreads []ast.Handle
	var currentVariableProperties []ast.Handle
	statement := ast.FindAncestor(node, ast.IsStatement)
	children := getChildren(node)
	for _, prop := range children {
		if isSpread(prop) {
			f.finalizesVariablePart(factory, name, isInConstContext, statement, makeNodeOfKind, createSpread, &currentVariableProperties, &intersectionTypes, &newSpreads)
			if ast.IsEntityNameExpression(prop.Expression()) {
				intersectionTypes = append(intersectionTypes, f.createTypeOfFromEntityNameExpression(prop.Expression()))
				newSpreads = append(newSpreads, prop)
			} else {
				f.makeSpreadVariable(factory, name, isInConstContext, statement, createSpread, prop.Expression(), &intersectionTypes, &newSpreads)
			}
		} else {
			currentVariableProperties = append(currentVariableProperties, prop)
		}
	}
	if len(newSpreads) == 0 {
		return ast.Handle{}
	}
	f.finalizesVariablePart(factory, name, isInConstContext, statement, makeNodeOfKind, createSpread, &currentVariableProperties, &intersectionTypes, &newSpreads)
	f.changeTracker.ReplaceNode(f.sourceFile, node, makeNodeOfKind(newSpreads), nil)
	f.mutatedTarget = true
	return finalType(intersectionTypes)
}

func (f *isolatedDeclarationsFixer) makeSpreadVariable(factory ast.HandleFactory, name string, isInConstContext bool, statement ast.Handle, createSpread func(ast.Handle) ast.Handle, expression ast.Handle, intersectionTypes *[]ast.Handle, newSpreads *[]ast.Handle) {
	tempName := f.changeTracker.EmitContext.Factory.NewUniqueNameEx(name+"_Part"+strconv.Itoa(len(*newSpreads)+1), printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
	var initializer ast.Handle
	if !isInConstContext {
		initializer = factory.DeepCloneNode(expression)
	} else {
		constRef := factory.NewTypeReferenceNode(factory.NewIdentifier("const"), 0)
		initializer = factory.NewAsExpression(factory.DeepCloneNode(expression), constRef)
	}
	varDecl := factory.NewVariableDeclaration(tempName, ast.Handle{}, ast.Handle{}, initializer)
	varDeclList := factory.NewVariableDeclarationList(factory.NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst)
	varStmt := factory.NewVariableStatement(0, varDeclList)
	if !statement.IsNil() {
		f.changeTracker.InsertNodeBefore(f.sourceFile, statement, varStmt, false, change.LeadingTriviaOptionNone)
	}
	*intersectionTypes = append(*intersectionTypes, f.createTypeOfFromEntityNameExpression(tempName))
	*newSpreads = append(*newSpreads, createSpread(tempName))
}

func (f *isolatedDeclarationsFixer) finalizesVariablePart(factory ast.HandleFactory, name string, isInConstContext bool, statement ast.Handle, makeNodeOfKind func([]ast.Handle) ast.Handle, createSpread func(ast.Handle) ast.Handle, currentVariableProperties *[]ast.Handle, intersectionTypes *[]ast.Handle, newSpreads *[]ast.Handle) {
	if len(*currentVariableProperties) > 0 {
		f.makeSpreadVariable(factory, name, isInConstContext, statement, createSpread, makeNodeOfKind(*currentVariableProperties), intersectionTypes, newSpreads)
		*currentVariableProperties = nil
	}
}

func isConstAssertion(node ast.Handle) bool {
	if ast.IsAssertionExpression(node) {
		typeNode := node.Type()
		return ast.IsConstTypeReference(typeNode)
	}
	return false
}

func (f *isolatedDeclarationsFixer) relativeType(node ast.Handle) ast.Handle {
	if ast.IsParameterDeclaration(node) {
		return ast.Handle{}
	}
	if ast.IsShorthandPropertyAssignment(node) {
		return f.createTypeOfFromEntityNameExpression(node.ShorthandPropertyAssignmentName())
	}
	if ast.IsEntityNameExpression(node) {
		return f.createTypeOfFromEntityNameExpression(node)
	}
	if isConstAssertion(node) {
		return f.relativeType(node.Expression())
	}
	if ast.IsArrayLiteralExpression(node) {
		varDecl := ast.FindAncestorKind(node, ast.KindVariableDeclaration)
		partName := ""
		if !varDecl.IsNil() && ast.IsIdentifier(varDecl.Name()) {
			partName = varDecl.Name().IdentifierText()
		}
		return f.typeFromArraySpreadElements(node, partName)
	}
	if ast.IsObjectLiteralExpression(node) {
		varDecl := ast.FindAncestorKind(node, ast.KindVariableDeclaration)
		partName := ""
		if !varDecl.IsNil() && ast.IsIdentifier(varDecl.Name()) {
			partName = varDecl.Name().IdentifierText()
		}
		return f.typeFromObjectSpreadAssignment(node, partName)
	}
	if ast.IsVariableDeclaration(node) && !node.Initializer().IsNil() {
		return f.relativeType(node.Initializer())
	}
	if ast.IsConditionalExpression(node) {
		cond := node
		trueType := f.relativeType(cond.WhenTrue())
		if trueType.IsNil() {
			return ast.Handle{}
		}
		trueMutated := f.mutatedTarget
		falseType := f.relativeType(cond.WhenFalse())
		if falseType.IsNil() {
			return ast.Handle{}
		}
		f.mutatedTarget = trueMutated || f.mutatedTarget
		factory := f.changeTracker.HandleFactory
		return factory.NewUnionTypeNode(factory.NewList([]ast.Handle{trueType, falseType}))
	}
	return ast.Handle{}
}

func (f *isolatedDeclarationsFixer) typeToMinimizedReferenceType(t *checker.Type, enclosingDecl ast.Handle, flags nodebuilder.Flags) ast.Handle {
	idToSymbol := make(map[ast.Handle]*ast.Symbol)
	typeNode := f.checker.TypeToTypeNodeEx(t, enclosingDecl, flags, nodebuilder.InternalFlagsWriteComputedProps, idToSymbol)
	if typeNode.IsNil() {
		return ast.Handle{}
	}
	if ast.IsTypeReferenceNode(typeNode) && t.ObjectFlags()&checker.ObjectFlagsReference != 0 {
		typeArgs := f.checker.GetTypeArguments(t)
		nodeTypeArgs := typeNode.TypeArguments()
		if len(typeArgs) > 0 && len(nodeTypeArgs) > 0 {
			cutoff := endOfRequiredTypeParameters(f.checker, t)
			if cutoff < len(nodeTypeArgs) {
				trimmedArgs := f.changeTracker.HandleFactory.NewList(nodeTypeArgs[:cutoff])
				typeNode = f.changeTracker.HandleFactory.UpdateTypeReferenceNode(typeNode, typeNode.TypeReferenceNodeTypeName(), trimmedArgs)
			}
		}
	}
	referenceTypeNode, importableSymbols := autoimport.TryGetAutoImportableReferenceFromTypeNode(typeNode, idToSymbol)
	if !referenceTypeNode.IsNil() {
		typeNode = referenceTypeNode
		f.symbolsToImport = append(f.symbolsToImport, importableSymbols...)
	}
	return typeNode
}

func endOfRequiredTypeParameters(ch *checker.Checker, t *checker.Type) int {
	typeArgs := ch.GetTypeArguments(t)
	if len(typeArgs) == 0 {
		return 0
	}
	target := t.Target()
	if target == nil || target.AsInterfaceType() == nil {
		return len(typeArgs)
	}
	typeParams := target.AsInterfaceType().TypeParameters()
	localTypeParams := target.AsInterfaceType().LocalTypeParameters()
	outerCount := len(typeParams) - len(localTypeParams)
	for cutoff := range typeArgs {
		localIdx := cutoff - outerCount
		if localIdx < 0 || localIdx >= len(localTypeParams) || !typeParamHasDefault(localTypeParams[localIdx]) {
			continue
		}
		filledIn := ch.FillMissingTypeArguments(typeArgs[:cutoff], typeParams, cutoff, false)
		allMatch := true
		for i, fill := range filledIn {
			if fill != typeArgs[i] {
				allMatch = false
				break
			}
		}
		if allMatch {
			return cutoff
		}
	}
	return len(typeArgs)
}

func typeParamHasDefault(tp *checker.Type) bool {
	sym := tp.Symbol()
	if sym == nil {
		return false
	}
	for _, decl := range ast.DeclarationNodes(sym) {
		if ast.IsTypeParameterDeclaration(decl) && !decl.TypeParameterDeclarationDefaultType().IsNil() {
			return true
		}
	}
	return false
}
func (f *isolatedDeclarationsFixer) addTypeToVariableLike(decl ast.Handle) string {
	typeNode := f.inferType(decl, nil)
	if typeNode.IsNil() {
		return ""
	}
	if !decl.Type().IsNil() {
		f.changeTracker.ReplaceNode(f.sourceFile, decl.Type(), typeNode, nil)
	} else {
		f.changeTracker.TryInsertTypeAnnotation(f.sourceFile, decl, typeNode)
		if ast.IsParameterDeclaration(decl) && !decl.Parent().IsNil() && ast.IsArrowFunction(decl.Parent()) {
			f.changeTracker.ParenthesizeArrowParameters(f.sourceFile, decl.Parent())
		}
	}
	return diagnostics.Add_annotation_of_type_0.Localize(f.locale, typeToStringForDiag(typeNode, f.sourceFile, f.changeTracker))
}

func typeToStringForDiag(typeNode ast.Handle, sourceFile *ast.SourceFile, ct *change.Tracker) string {
	savedFlags := ct.EmitContext.EmitFlags(typeNode)
	ct.EmitContext.SetEmitFlags(typeNode, savedFlags|printer.EFSingleLine)
	p := printer.NewPrinter(printer.PrinterOptions{NewLine: core.NewLineKindLF}, printer.PrintHandlers{}, ct.EmitContext)
	writer, release := printer.GetSingleLineStringWriter()
	defer release()
	p.Write(typeNode, sourceFile, writer, nil)
	ct.EmitContext.SetEmitFlags(typeNode, savedFlags)
	result := writer.String()
	if len(result) > 160 {
		return result[:157] + "..."
	}
	return result
}

func findAncestorWithMissingType(node ast.Handle) ast.Handle {
	return ast.FindAncestor(node, func(n ast.Handle) bool {
		if !canHaveTypeAnnotationKinds[n.Kind] {
			return false
		}
		if ast.IsObjectBindingPattern(n) || ast.IsArrayBindingPattern(n) {
			return ast.IsVariableDeclaration(n.Parent())
		}
		return true
	})
}

func findBestFittingNode(node ast.Handle, span core.TextRange) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	for !node.IsNil() && node.End() < span.Pos()+span.Len() {
		node = node.Parent()
	}
	for !node.Parent().IsNil() && node.Parent().Pos() == node.Pos() && node.Parent().End() == node.End() {
		node = node.Parent()
	}
	if ast.IsIdentifier(node) && ast.HasInitializer(node.Parent()) && !node.Parent().Initializer().IsNil() {
		return node.Parent().Initializer()
	}
	if ast.IsIdentifier(node) && ast.IsShorthandPropertyAssignment(node.Parent()) {
		return node.Parent()
	}
	return node
}

func isNamedDeclarationKind(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindArrowFunction, ast.KindBindingElement, ast.KindClassDeclaration, ast.KindClassExpression, ast.KindClassStaticBlockDeclaration, ast.KindConstructor, ast.KindEnumDeclaration, ast.KindEnumMember, ast.KindExportSpecifier, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindGetAccessor, ast.KindImportClause, ast.KindImportEqualsDeclaration, ast.KindImportSpecifier, ast.KindInterfaceDeclaration, ast.KindJsxAttribute, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindModuleDeclaration, ast.KindNamespaceExportDeclaration, ast.KindNamespaceImport, ast.KindNamespaceExport, ast.KindParameter, ast.KindPropertyAssignment, ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindSetAccessor, ast.KindShorthandPropertyAssignment, ast.KindTypeAliasDeclaration, ast.KindTypeParameter, ast.KindVariableDeclaration, ast.KindJSDocTypedefTag, ast.KindJSDocCallbackTag, ast.KindJSDocPropertyTag, ast.KindNamedTupleMember:
		return true
	}
	return false
}

func isValueSignatureDeclaration(node ast.Handle) bool {
	return ast.IsFunctionExpression(node) || ast.IsArrowFunction(node) || ast.IsMethodDeclaration(node) || ast.IsAccessor(node) || ast.IsFunctionDeclaration(node) || ast.IsConstructorDeclaration(node)
}

func getIdentifierNameForNode(node ast.Handle) string {
	if ast.IsPropertyAccessExpression(node) {
		name := node.PropertyAccessExpressionName()
		if ast.IsIdentifier(name) && !ast.IsPrivateIdentifier(name) && scanner.IdentifierToKeywordKind(name) == ast.KindUnknown {
			return name.Text()
		}
	}
	return "newLocal"
}

func (f *isolatedDeclarationsFixer) addSymbolToExistingImport(sym *ast.Symbol) {
	if sym == nil || sym.Parent == nil {
		return
	}
	moduleSymbol := sym.Parent
	symbolName := sym.Name
	for _, stmt := range f.sourceFile.ParseRoot().Statements() {
		if !ast.IsImportDeclaration(stmt) {
			continue
		}
		importDecl := stmt
		if importDecl.ImportClause().IsNil() {
			continue
		}
		importModuleSymbol := f.checker.GetSymbolAtLocation(importDecl.ModuleSpecifier())
		if importModuleSymbol == nil || f.checker.GetMergedSymbol(importModuleSymbol) != f.checker.GetMergedSymbol(moduleSymbol) {
			continue
		}
		importClause := importDecl.ImportClause()
		if !importClause.NamedBindings().IsNil() && ast.IsNamedImports(importClause.NamedBindings()) {
			existingElements := importClause.NamedBindings().Elements()
			factory := f.changeTracker.HandleFactory
			newSpecifier := factory.NewImportSpecifier(false, ast.Handle{}, factory.NewIdentifier(symbolName))
			newElements := append(existingElements, newSpecifier)
			newNamedImports := factory.NewNamedImports(factory.NewList(newElements))
			newImportClause := factory.UpdateImportClause(importClause, importClause.ImportClausePhaseModifier(), importClause.Name(), newNamedImports)
			newImportDecl := factory.UpdateImportDeclaration(importDecl, importDecl.Modifiers(), newImportClause, importDecl.ModuleSpecifier(), importDecl.Attributes())
			f.changeTracker.ReplaceNode(f.sourceFile, stmt, newImportDecl, nil)
		}
		return
	}
}
